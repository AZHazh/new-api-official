package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useRateLimitMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()

	previousRedisEnabled := common.RedisEnabled
	previousRedisClient := common.RDB
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	require.NoError(t, redisClient.Ping(context.Background()).Err())

	common.RedisEnabled = true
	common.RDB = redisClient
	t.Cleanup(func() {
		_ = redisClient.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRedisClient
	})

	return redisServer, redisClient
}

func performRateLimitRequest(router http.Handler, path string, remoteAddr string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.RemoteAddr = remoteAddr
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestRedisIPRateLimiterThresholdTTLAndNamespace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/limited", rateLimitFactory(2, 37, "TEST"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	remoteAddr := "192.0.2.10:12345"
	legacyKey := "rateLimit:TEST192.0.2.10"
	_, err := redisServer.Push(legacyKey, "legacy-list-entry")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/limited", remoteAddr).Code)
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/limited", remoteAddr).Code)
	limitedResponse := performRateLimitRequest(router, "/limited", remoteAddr)
	assert.Equal(t, http.StatusTooManyRequests, limitedResponse.Code)
	assert.Equal(t, "37", limitedResponse.Header().Get("Retry-After"))

	key := redisIPRateLimitKey("TEST", "192.0.2.10")
	count, err := redisServer.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "3", count)
	assert.Equal(t, 37*time.Second, redisServer.TTL(key))
	assert.True(t, redisServer.Exists(legacyKey), "the v2 counter must not touch an old list key")
}

func TestRedisUserRateLimiterUsesSharedFixedWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)

	router := gin.New()
	router.GET(
		"/limited",
		func(c *gin.Context) { c.Set("id", 42) },
		userRateLimitFactory(1, 23, "USER"),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)

	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/limited", "192.0.2.20:12345").Code)
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/limited", "198.51.100.20:12345").Code)

	key := redisUserRateLimitKey("USER", 42)
	assert.True(t, redisServer.Exists(key))
	assert.Equal(t, 23*time.Second, redisServer.TTL(key))
}

func TestRedisEmailVerificationRateLimiterPreservesResponseAndTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/verify", EmailVerificationRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	remoteAddr := "192.0.2.30:12345"
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/verify", remoteAddr).Code)
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/verify", remoteAddr).Code)
	response := performRateLimitRequest(router, "/verify", remoteAddr)
	assert.Equal(t, http.StatusTooManyRequests, response.Code)
	assert.JSONEq(t, `{"success":false,"message":"发送过于频繁，请等待 30 秒后再试"}`, response.Body.String())

	key := redisIPRateLimitKey(EmailVerificationRateLimitMark, "192.0.2.30")
	assert.True(t, redisServer.Exists(key))
	assert.Equal(t, time.Duration(EmailVerificationDuration)*time.Second, redisServer.TTL(key))
}

func TestRedisFixedWindowIsAtomicUnderConcurrency(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	const (
		requestCount = 20
		maximumCount = 7
		duration     = int64(41)
	)
	key := redisIPRateLimitKey("CONCURRENT", "192.0.2.40")

	var allowedCount atomic.Int64
	errorsFound := make(chan error, requestCount)
	var waitGroup sync.WaitGroup
	waitGroup.Add(requestCount)
	for range requestCount {
		go func() {
			defer waitGroup.Done()
			allowed, _, _, err := redisFixedWindowTake(context.Background(), key, maximumCount, duration)
			if err != nil {
				errorsFound <- err
				return
			}
			if allowed {
				allowedCount.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		require.NoError(t, err)
	}

	assert.Equal(t, int64(maximumCount), allowedCount.Load())
	count, err := redisServer.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "20", count)
	assert.Equal(t, time.Duration(duration)*time.Second, redisServer.TTL(key))
}

func TestRedisFixedWindowResetsAtBoundary(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	const duration = int64(10)
	key := redisIPRateLimitKey("BOUNDARY", "192.0.2.50")

	for range 2 {
		allowed, _, _, err := redisFixedWindowTake(context.Background(), key, 2, duration)
		require.NoError(t, err)
		assert.True(t, allowed)
	}
	allowed, _, _, err := redisFixedWindowTake(context.Background(), key, 2, duration)
	require.NoError(t, err)
	assert.False(t, allowed)

	// This reset is intentional fixed-window behavior. A client can consume one
	// full allowance immediately before and another immediately after a boundary.
	redisServer.FastForward(time.Duration(duration) * time.Second)
	for range 2 {
		allowed, _, _, err = redisFixedWindowTake(context.Background(), key, 2, duration)
		require.NoError(t, err)
		assert.True(t, allowed)
	}
}

func TestRedisFixedWindowRepairsCounterWithoutTTL(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	const duration = int64(29)
	key := redisIPRateLimitKey("MISSING-TTL", "192.0.2.51")
	redisServer.Set(key, "5")

	allowed, count, ttl, err := redisFixedWindowTake(context.Background(), key, 3, duration)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, int64(6), count)
	assert.Equal(t, duration, ttl)
	assert.Equal(t, time.Duration(duration)*time.Second, redisServer.TTL(key))

	redisServer.FastForward(time.Duration(duration) * time.Second)
	assert.False(t, redisServer.Exists(key), "a recovered counter must not remain permanently rate-limited")
}

func TestRedisFailurePolicies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, redisClient := useRateLimitMiniRedis(t)
	require.NoError(t, redisClient.Close())

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/ip", rateLimitFactory(1, 30, "FAIL-IP"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.GET(
		"/user",
		func(c *gin.Context) { c.Set("id", 7) },
		userRateLimitFactory(1, 30, "FAIL-USER"),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	router.GET("/email", EmailVerificationRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	ipResponse := performRateLimitRequest(router, "/ip", "192.0.2.60:12345")
	assert.Equal(t, http.StatusInternalServerError, ipResponse.Code)
	assert.Empty(t, ipResponse.Body.String())
	userResponse := performRateLimitRequest(router, "/user", "192.0.2.61:12345")
	assert.Equal(t, http.StatusInternalServerError, userResponse.Code)
	assert.Empty(t, userResponse.Body.String())
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/email", "192.0.2.62:12345").Code)
}

func TestRedisProviderRateLimitUsesRollingWindows(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	windowStart := time.Unix(1_700_000_000, 0)
	redisServer.SetTime(windowStart)
	fingerprint := strings.Repeat("a", 64)
	shortWindow := model.ProviderRateLimitWindow{Maximum: 2, DurationSeconds: 60}
	longWindow := model.ProviderRateLimitWindow{Maximum: 3, DurationSeconds: 300}

	for expectedCount := int64(1); expectedCount <= 2; expectedCount++ {
		allowed, shortCount, longCount, retryAfter, err := redisProviderSlidingWindowTake(
			context.Background(), "seedance", 42, fingerprint, shortWindow, longWindow,
		)
		require.NoError(t, err)
		assert.True(t, allowed)
		assert.Equal(t, expectedCount, shortCount)
		assert.Equal(t, expectedCount, longCount)
		assert.Zero(t, retryAfter)
	}
	allowed, shortCount, longCount, retryAfter, err := redisProviderSlidingWindowTake(
		context.Background(), "seedance", 42, fingerprint, shortWindow, longWindow,
	)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, int64(2), shortCount)
	assert.Equal(t, int64(2), longCount)
	assert.Equal(t, int64(60), retryAfter)

	redisServer.SetTime(windowStart.Add(61 * time.Second))
	allowed, shortCount, longCount, retryAfter, err = redisProviderSlidingWindowTake(
		context.Background(), "seedance", 42, fingerprint, shortWindow, longWindow,
	)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, int64(1), shortCount)
	assert.Equal(t, int64(3), longCount)
	assert.Zero(t, retryAfter)

	allowed, _, longCount, retryAfter, err = redisProviderSlidingWindowTake(
		context.Background(), "seedance", 42, fingerprint, shortWindow, longWindow,
	)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, int64(3), longCount)
	assert.InDelta(t, 239, retryAfter, 1)
}

func TestRedisProviderRateLimitIsAtomicUnderConcurrency(t *testing.T) {
	redisServer, redisClient := useRateLimitMiniRedis(t)
	redisServer.SetTime(time.Unix(1_700_000_000, 0))
	fingerprint := strings.Repeat("c", 64)
	shortWindow := model.ProviderRateLimitWindow{Maximum: 7, DurationSeconds: 60}
	longWindow := model.ProviderRateLimitWindow{Maximum: 100, DurationSeconds: 300}

	var allowedCount atomic.Int64
	errorsFound := make(chan error, 20)
	var waitGroup sync.WaitGroup
	waitGroup.Add(20)
	for range 20 {
		go func() {
			defer waitGroup.Done()
			allowed, _, _, _, err := redisProviderSlidingWindowTake(
				context.Background(), "seedance", 43, fingerprint, shortWindow, longWindow,
			)
			if err != nil {
				errorsFound <- err
				return
			}
			if allowed {
				allowedCount.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		require.NoError(t, err)
	}

	assert.Equal(t, int64(7), allowedCount.Load())
	eventsKey, _ := redisProviderRateLimitKeys("seedance", 43, fingerprint)
	eventCount, err := redisClient.ZCard(context.Background(), eventsKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(7), eventCount)
}

func TestSeedanceProviderRedisLimitUsesFingerprintAndExactUpstreamThreshold(t *testing.T) {
	redisServer, redisClient := useRateLimitMiniRedis(t)
	providerNow := time.Unix(1_700_000_000, 0)
	redisServer.SetTime(providerNow)
	const (
		channelID = 73
		apiKey    = "sk-seedance-do-not-store-this-value"
	)
	fingerprint, err := seedanceUploadCredentialFingerprint(apiKey)
	require.NoError(t, err)
	assert.Len(t, fingerprint, 64)

	for range 10 {
		allowed, retryAfter, err := TakeSeedanceUploadProviderRateLimit(context.Background(), channelID, apiKey)
		require.NoError(t, err)
		assert.True(t, allowed)
		assert.Zero(t, retryAfter)
	}
	allowed, retryAfter, err := TakeSeedanceUploadProviderRateLimit(context.Background(), channelID, apiKey)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, int64(60), retryAfter)

	keys := redisServer.Keys()
	require.Len(t, keys, 2)
	for _, key := range keys {
		assert.NotContains(t, key, apiKey)
		assert.Contains(t, key, fingerprint)
		assert.Contains(t, key, ":73:")
	}

	allowed, _, err = TakeSeedanceUploadProviderRateLimit(context.Background(), channelID+1, apiKey)
	require.NoError(t, err)
	assert.True(t, allowed, "a separately selected channel must have its own upstream scope")

	const dailyAPIKey = "sk-seedance-daily-threshold"
	dailyFingerprint, err := seedanceUploadCredentialFingerprint(dailyAPIKey)
	require.NoError(t, err)
	dailyEventsKey, _ := redisProviderRateLimitKeys(seedanceUploadProvider, channelID+2, dailyFingerprint)
	dailyEvents := make([]*redis.Z, 200)
	for index := range dailyEvents {
		dailyEvents[index] = &redis.Z{
			Score:  float64(providerNow.Add(-2*time.Minute).UnixMilli() + int64(index)),
			Member: index,
		}
	}
	require.NoError(t, redisClient.ZAdd(context.Background(), dailyEventsKey, dailyEvents...).Err())
	allowed, retryAfter, err = TakeSeedanceUploadProviderRateLimit(context.Background(), channelID+2, dailyAPIKey)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Positive(t, retryAfter, "the 201st request inside 24 hours must be rejected")
}

func TestSeedanceProviderRateLimitUsesDatabaseWithoutRedis(t *testing.T) {
	previousDB := model.DB
	previousDatabaseType := common.MainDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ProviderRateLimitGuard{}, &model.ProviderRateLimitRequest{}))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousDatabaseType)
		common.RedisEnabled = previousRedisEnabled
	})

	const apiKey = "sk-seedance-db-fallback-secret"
	for range 10 {
		allowed, retryAfter, err := TakeSeedanceUploadProviderRateLimit(context.Background(), 88, apiKey)
		require.NoError(t, err)
		assert.True(t, allowed)
		assert.Zero(t, retryAfter)
	}
	allowed, retryAfter, err := TakeSeedanceUploadProviderRateLimit(context.Background(), 88, apiKey)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Positive(t, retryAfter)

	var requests []model.ProviderRateLimitRequest
	require.NoError(t, db.Find(&requests).Error)
	assert.Len(t, requests, 10)
	for _, request := range requests {
		assert.NotEqual(t, apiKey, request.CredentialFingerprint)
		assert.Len(t, request.CredentialFingerprint, 64)
	}
}
