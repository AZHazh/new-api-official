package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const redisRateLimitNamespace = "rateLimit:v2"

// Redis rate limiting intentionally uses a fixed window. The single Lua script
// makes increment, expiry, and the limit decision atomic, while retaining the
// simple fixed-window behavior: traffic at a window boundary can burst up to
// twice the configured limit. Do not replace this with a sliding-window ZSET
// unless that externally visible behavior is intentionally changed.
const redisFixedWindowScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
end
local ttl = redis.call('TTL', KEYS[1])
if ttl < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
  ttl = redis.call('TTL', KEYS[1])
end
if count > tonumber(ARGV[1]) then
  return {0, count, ttl}
end
return {1, count, ttl}
`

const redisProviderSlidingWindowScript = `
local server_time = redis.call('TIME')
local now_ms = tonumber(server_time[1]) * 1000 + math.floor(tonumber(server_time[2]) / 1000)
local short_maximum = tonumber(ARGV[1])
local short_window_ms = tonumber(ARGV[2])
local long_maximum = tonumber(ARGV[3])
local long_window_ms = tonumber(ARGV[4])
local short_start = now_ms - short_window_ms
local long_start = now_ms - long_window_ms

redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', long_start)

local short_count = redis.call('ZCOUNT', KEYS[1], '(' .. short_start, '+inf')
local long_count = redis.call('ZCARD', KEYS[1])
local retry_ms = 0

if short_count >= short_maximum then
  local oldest = redis.call('ZRANGEBYSCORE', KEYS[1], '(' .. short_start, '+inf', 'WITHSCORES', 'LIMIT', 0, 1)
  if oldest[2] then
    retry_ms = math.max(retry_ms, tonumber(oldest[2]) + short_window_ms - now_ms)
  end
end

if long_count >= long_maximum then
  local oldest = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  if oldest[2] then
    retry_ms = math.max(retry_ms, tonumber(oldest[2]) + long_window_ms - now_ms)
  end
end

redis.call('PEXPIRE', KEYS[1], long_window_ms + 1000)
redis.call('PEXPIRE', KEYS[2], long_window_ms + 1000)

if retry_ms > 0 then
  return {0, short_count, long_count, math.max(1, math.ceil(retry_ms / 1000))}
end

local sequence = redis.call('INCR', KEYS[2])
redis.call('ZADD', KEYS[1], now_ms, tostring(now_ms) .. ':' .. tostring(sequence))
redis.call('PEXPIRE', KEYS[1], long_window_ms + 1000)
redis.call('PEXPIRE', KEYS[2], long_window_ms + 1000)
return {1, short_count + 1, long_count + 1, 0}
`

const (
	seedanceUploadProvider              = "seedance"
	seedanceUploadMinuteMaximum         = 10
	seedanceUploadMinuteDurationSeconds = int64(60)
	seedanceUploadDailyMaximum          = 200
	seedanceUploadDailyDurationSeconds  = int64(24 * 60 * 60)
)

var inMemoryRateLimiter common.InMemoryRateLimiter

var defNext = func(c *gin.Context) {
	c.Next()
}

func redisIPRateLimitKey(mark string, clientIP string) string {
	return fmt.Sprintf("%s:ip:%s:%s", redisRateLimitNamespace, mark, clientIP)
}

func redisUserRateLimitKey(mark string, userID int) string {
	return fmt.Sprintf("%s:user:%s:%d", redisRateLimitNamespace, mark, userID)
}

func redisReplyInteger(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis integer reply type %T", value)
	}
}

func redisFixedWindowTake(ctx context.Context, key string, maxRequestNum int, duration int64) (bool, int64, int64, error) {
	if common.RDB == nil {
		return false, 0, 0, errors.New("Redis client is not initialized")
	}
	if key == "" {
		return false, 0, 0, errors.New("rate limit key is empty")
	}
	if maxRequestNum <= 0 {
		return false, 0, 0, errors.New("rate limit maximum must be positive")
	}
	if duration <= 0 {
		return false, 0, 0, errors.New("rate limit duration must be positive")
	}

	values, err := common.RDB.Eval(
		ctx,
		redisFixedWindowScript,
		[]string{key},
		maxRequestNum,
		duration,
	).Slice()
	if err != nil {
		return false, 0, 0, err
	}
	if len(values) != 3 {
		return false, 0, 0, fmt.Errorf("unexpected Redis rate limit reply length %d", len(values))
	}

	allowedValue, err := redisReplyInteger(values[0])
	if err != nil {
		return false, 0, 0, err
	}
	count, err := redisReplyInteger(values[1])
	if err != nil {
		return false, 0, 0, err
	}
	ttlSeconds, err := redisReplyInteger(values[2])
	if err != nil {
		return false, 0, 0, err
	}

	return allowedValue == 1, count, ttlSeconds, nil
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	allowed, _, ttlSeconds, err := redisFixedWindowTake(
		c.Request.Context(),
		redisIPRateLimitKey(mark, c.ClientIP()),
		maxRequestNum,
		duration,
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("rate limit check failed (mark=%s): %v", mark, err))
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if !allowed {
		writeRateLimited(c, ttlSeconds)
	}
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := mark + c.ClientIP()
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		writeRateLimited(c, duration)
		return
	}
}

// writeRateLimited rejects the request with 429 and a Retry-After hint so
// clients can back off instead of treating the rejection as a fatal error.
// The in-memory limiter cannot report the remaining window, so callers
// without a TTL pass the full window duration as a conservative upper bound.
func writeRateLimited(c *gin.Context, retryAfterSeconds int64) {
	if retryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.FormatInt(retryAfterSeconds, 10))
	}
	c.Status(http.StatusTooManyRequests)
	c.Abort()
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		memoryRateLimiter(c, maxRequestNum, duration, mark)
	}
}

func GlobalWebRateLimit() func(c *gin.Context) {
	if common.GlobalWebRateLimitEnable {
		return rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
	}
	return defNext
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	if common.GlobalApiRateLimitEnable {
		return rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA")
	}
	return defNext
}

func CriticalRateLimit() func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	}
	return defNext
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

// SeedanceUploadRateLimit is an additional per-user guard. The authoritative
// upstream-key aggregate limit runs in the upload controller after channel and
// credential selection.
func SeedanceUploadRateLimit() func(c *gin.Context) {
	minuteLimiter := userRateLimitFactory(seedanceUploadMinuteMaximum, seedanceUploadMinuteDurationSeconds, "SEEDANCE-UPLOAD-MINUTE")
	dailyLimiter := userRateLimitFactory(seedanceUploadDailyMaximum, seedanceUploadDailyDurationSeconds, "SEEDANCE-UPLOAD-DAY")
	return func(c *gin.Context) {
		minuteLimiter(c)
		if c.IsAborted() {
			return
		}
		dailyLimiter(c)
	}
}

func seedanceUploadCredentialFingerprint(apiKey string) (string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return "", errors.New("Seedance API key is empty")
	}
	digest := sha256.Sum256([]byte("new-api/provider-rate-limit/v1\x00" + apiKey))
	return hex.EncodeToString(digest[:]), nil
}

func redisProviderRateLimitKeys(provider string, channelID int, credentialFingerprint string) (string, string) {
	scope := fmt.Sprintf("{%s:%d:%s}", provider, channelID, credentialFingerprint)
	return fmt.Sprintf("%s:provider-upload:%s:events", redisRateLimitNamespace, scope),
		fmt.Sprintf("%s:provider-upload:%s:sequence", redisRateLimitNamespace, scope)
}

func redisProviderSlidingWindowTake(
	ctx context.Context,
	provider string,
	channelID int,
	credentialFingerprint string,
	shortWindow model.ProviderRateLimitWindow,
	longWindow model.ProviderRateLimitWindow,
) (bool, int64, int64, int64, error) {
	if common.RDB == nil {
		return false, 0, 0, 0, errors.New("Redis client is not initialized")
	}
	if provider == "" || channelID <= 0 || credentialFingerprint == "" ||
		shortWindow.Maximum <= 0 || shortWindow.DurationSeconds <= 0 ||
		longWindow.Maximum <= 0 || longWindow.DurationSeconds < shortWindow.DurationSeconds {
		return false, 0, 0, 0, errors.New("invalid provider rate limit")
	}
	eventsKey, sequenceKey := redisProviderRateLimitKeys(provider, channelID, credentialFingerprint)
	values, err := common.RDB.Eval(
		ctx,
		redisProviderSlidingWindowScript,
		[]string{eventsKey, sequenceKey},
		shortWindow.Maximum,
		shortWindow.DurationSeconds*1000,
		longWindow.Maximum,
		longWindow.DurationSeconds*1000,
	).Slice()
	if err != nil {
		return false, 0, 0, 0, err
	}
	if len(values) != 4 {
		return false, 0, 0, 0, fmt.Errorf("unexpected Redis provider rate limit reply length %d", len(values))
	}
	parsed := make([]int64, len(values))
	for index, value := range values {
		parsed[index], err = redisReplyInteger(value)
		if err != nil {
			return false, 0, 0, 0, err
		}
	}
	return parsed[0] == 1, parsed[1], parsed[2], parsed[3], nil
}

// TakeSeedanceUploadProviderRateLimit applies the upstream's aggregate limits
// to the exact selected channel and API key. Only the key fingerprint leaves
// this function.
func TakeSeedanceUploadProviderRateLimit(ctx context.Context, channelID int, apiKey string) (bool, int64, error) {
	credentialFingerprint, err := seedanceUploadCredentialFingerprint(apiKey)
	if err != nil {
		return false, 0, err
	}
	minuteWindow := model.ProviderRateLimitWindow{
		Maximum:         seedanceUploadMinuteMaximum,
		DurationSeconds: seedanceUploadMinuteDurationSeconds,
	}
	dailyWindow := model.ProviderRateLimitWindow{
		Maximum:         seedanceUploadDailyMaximum,
		DurationSeconds: seedanceUploadDailyDurationSeconds,
	}
	if common.RedisEnabled {
		// Do not switch counters during a Redis outage: an empty DB window could
		// let this process exceed the upstream credential's aggregate allowance.
		allowed, _, _, retryAfter, err := redisProviderSlidingWindowTake(
			ctx,
			seedanceUploadProvider,
			channelID,
			credentialFingerprint,
			minuteWindow,
			dailyWindow,
		)
		return allowed, retryAfter, err
	}
	return model.TakeProviderRateLimit(
		ctx,
		seedanceUploadProvider,
		channelID,
		credentialFingerprint,
		[]model.ProviderRateLimitWindow{minuteWindow, dailyWindow},
	)
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			userID := c.GetInt("id")
			if userID == 0 {
				c.Status(http.StatusUnauthorized)
				c.Abort()
				return
			}
			userRedisRateLimiter(c, maxRequestNum, duration, redisUserRateLimitKey(mark, userID))
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userID := c.GetInt("id")
		if userID == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		key := fmt.Sprintf("%s:user:%d", mark, userID)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			writeRateLimited(c, duration)
			return
		}
	}
}

// userRedisRateLimiter is like redisRateLimiter but accepts a pre-built key
// (to support user-ID-based keys).
func userRedisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	allowed, _, ttlSeconds, err := redisFixedWindowTake(c.Request.Context(), key, maxRequestNum, duration)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("rate limit check failed (key=%s): %v", key, err))
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if !allowed {
		writeRateLimited(c, ttlSeconds)
	}
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() func(c *gin.Context) {
	if !common.SearchRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(common.SearchRateLimitNum, common.SearchRateLimitDuration, "SR")
}
