package controller

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const desktopSyncCodeTTL = 5 * time.Minute
const desktopSyncTokenLimit = 100

var desktopSyncMemoryStore = struct {
	sync.Mutex
	items map[string]desktopSyncMemoryCode
}{
	items: make(map[string]desktopSyncMemoryCode),
}

type desktopSyncMemoryCode struct {
	Payload   string
	ExpiresAt time.Time
}

type desktopSyncIssueRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

type desktopSyncExchangeRequest struct {
	Code string `json:"code"`
}

type desktopSyncCodePayload struct {
	UserID    int   `json:"user_id"`
	CreatedAt int64 `json:"created_at"`
}

type desktopSyncTokenResponse struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Key            string `json:"key"`
	Group          string `json:"group"`
	Status         int    `json:"status"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
	RemainQuota    int    `json:"remain_quota"`
	ExpiredTime    int64  `json:"expired_time"`
}

func desktopSyncCodeKey(code string) string {
	return "desktop_sync_code:" + code
}

func storeDesktopSyncCode(ctx context.Context, code string, payload string) error {
	if common.RedisEnabled && common.RDB != nil {
		ok, err := common.RDB.SetNX(ctx, desktopSyncCodeKey(code), payload, desktopSyncCodeTTL).Result()
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("failed to issue desktop sync code")
		}
		return nil
	}

	desktopSyncMemoryStore.Lock()
	defer desktopSyncMemoryStore.Unlock()
	cleanupExpiredDesktopSyncCodesLocked(time.Now())
	if _, ok := desktopSyncMemoryStore.items[code]; ok {
		return errors.New("failed to issue desktop sync code")
	}
	desktopSyncMemoryStore.items[code] = desktopSyncMemoryCode{
		Payload:   payload,
		ExpiresAt: time.Now().Add(desktopSyncCodeTTL),
	}
	return nil
}

func consumeDesktopSyncCode(ctx context.Context, code string) (string, error) {
	if common.RedisEnabled && common.RDB != nil {
		payload, err := common.RDB.GetDel(ctx, desktopSyncCodeKey(code)).Result()
		if errors.Is(err, redis.Nil) {
			return "", errors.New("desktop sync code expired or already used")
		}
		return payload, err
	}

	desktopSyncMemoryStore.Lock()
	defer desktopSyncMemoryStore.Unlock()
	now := time.Now()
	cleanupExpiredDesktopSyncCodesLocked(now)
	item, ok := desktopSyncMemoryStore.items[code]
	if !ok {
		return "", errors.New("desktop sync code expired or already used")
	}
	delete(desktopSyncMemoryStore.items, code)
	if now.After(item.ExpiresAt) {
		return "", errors.New("desktop sync code expired or already used")
	}
	return item.Payload, nil
}

func cleanupExpiredDesktopSyncCodesLocked(now time.Time) {
	for code, item := range desktopSyncMemoryStore.items {
		if now.After(item.ExpiresAt) {
			delete(desktopSyncMemoryStore.items, code)
		}
	}
}

func validateDesktopSyncRedirectURI(raw string) error {
	if raw == "" {
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid redirect_uri: %w", err)
	}
	if u.Scheme != "http" {
		return errors.New("redirect_uri scheme must be http")
	}
	if u.Path != "/callback" {
		return errors.New("redirect_uri path must be /callback")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("redirect_uri must not contain user info, query, or fragment")
	}
	if u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		return errors.New("redirect_uri host must be localhost or 127.0.0.1")
	}
	port := u.Port()
	if port == "" {
		return errors.New("redirect_uri port is required")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 || p > 65535 {
		return errors.New("redirect_uri port is invalid")
	}
	return nil
}

func generateDesktopSyncCode() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func IssueDesktopSyncCode(c *gin.Context) {
	req := desktopSyncIssueRequest{}
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := common.DecodeJson(c.Request.Body, &req); err != nil && !errors.Is(err, io.EOF) {
			common.ApiError(c, err)
			return
		}
	}
	if err := validateDesktopSyncRedirectURI(req.RedirectURI); err != nil {
		common.ApiError(c, err)
		return
	}

	code, err := generateDesktopSyncCode()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	payload, err := common.Marshal(desktopSyncCodePayload{
		UserID:    c.GetInt("id"),
		CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	if err := storeDesktopSyncCode(ctx, code, string(payload)); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"code":       code,
		"expires_in": int(desktopSyncCodeTTL.Seconds()),
	})
}

func ExchangeDesktopSyncCode(c *gin.Context) {
	req := desktopSyncExchangeRequest{}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Code == "" {
		common.ApiErrorMsg(c, "missing code")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	payloadStr, err := consumeDesktopSyncCode(ctx, req.Code)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	payload := desktopSyncCodePayload{}
	if err := common.Unmarshal([]byte(payloadStr), &payload); err != nil {
		common.ApiError(c, err)
		return
	}
	if payload.UserID <= 0 {
		common.ApiErrorMsg(c, "invalid desktop sync code")
		return
	}

	tokens, err := model.GetAllUserTokens(payload.UserID, 0, desktopSyncTokenLimit)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	ids := make([]int, 0, len(tokens))
	for _, token := range tokens {
		ids = append(ids, token.Id)
	}

	keyByID := make(map[int]string, len(ids))
	if len(ids) > 0 {
		keyTokens, err := model.GetTokenKeysByIds(ids, payload.UserID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for _, token := range keyTokens {
			keyByID[token.Id] = token.Key
		}
	}

	user, err := model.GetUserCache(payload.UserID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 用户默认分组：令牌 group 为空时，relay 实际生效的分组（见 controller/model.go 与 relay_info.go 的回退逻辑）
	defaultGroup := user.Group

	// 组装 tokens，同时按契约约束 (2) 解析空 group 为真实分组名并回写，
	// 按约束 (4) 收集本次涉及的分组集合（去重，保留首次出现顺序）
	respTokens := make([]desktopSyncTokenResponse, 0, len(tokens))
	usedGroups := make([]string, 0)
	for _, token := range tokens {
		group := token.Group
		if group == "" {
			group = defaultGroup
		}
		if group != "" && !common.StringsContains(usedGroups, group) {
			usedGroups = append(usedGroups, group)
		}
		respTokens = append(respTokens, desktopSyncTokenResponse{
			ID:             token.Id,
			Name:           token.Name,
			Key:            keyByID[token.Id],
			Group:          group,
			Status:         token.Status,
			UnlimitedQuota: token.UnlimitedQuota,
			RemainQuota:    token.RemainQuota,
			ExpiredTime:    token.ExpiredTime,
		})
	}

	// 按约束 (3)(6) 组装 group_models：分组 -> 该分组启用的真实模型名（去重，不排序）
	// 按约束 (7) available_models 为所有返回分组的并集超集
	groupModels := make(map[string][]string, len(usedGroups))
	models := make([]string, 0)
	for _, group := range usedGroups {
		enabled := model.GetGroupEnabledModels(group)
		deduped := make([]string, 0, len(enabled))
		for _, m := range enabled {
			if !common.StringsContains(deduped, m) {
				deduped = append(deduped, m)
			}
			if !common.StringsContains(models, m) {
				models = append(models, m)
			}
		}
		groupModels[group] = deduped
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"site_name":        common.SystemName,
			"available_models": models,
			"group_models":     groupModels,
			"tokens":           respTokens,
		},
	})
}

// ========== 轮询支持（VSCode 插件使用）==========

const desktopSyncSessionTTL = 10 * time.Minute

var desktopSyncSessions = struct {
	sync.RWMutex
	items map[string]desktopSyncSession
}{
	items: make(map[string]desktopSyncSession),
}

type desktopSyncSession struct {
	State     string    `json:"state"`
	Code      string    `json:"code"`
	Status    string    `json:"status"` // "pending", "authorized", "consumed"
	CreatedAt time.Time `json:"created_at"`
}

type desktopSyncStoreSessionRequest struct {
	State string `json:"state" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

// StoreDesktopSyncSession 存储授权会话
// POST /api/desktop-sync/sessions
// 浏览器授权完成后，前端调用此接口存储 code
func StoreDesktopSyncSession(c *gin.Context) {
	var req desktopSyncStoreSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	desktopSyncSessions.Lock()
	defer desktopSyncSessions.Unlock()

	// 清理过期会话（超过 10 分钟）
	now := time.Now()
	for state, session := range desktopSyncSessions.items {
		if now.Sub(session.CreatedAt) > desktopSyncSessionTTL {
			delete(desktopSyncSessions.items, state)
		}
	}

	// 存储新会话
	desktopSyncSessions.items[req.State] = desktopSyncSession{
		State:     req.State,
		Code:      req.Code,
		Status:    "authorized",
		CreatedAt: now,
	}

	common.ApiSuccess(c, gin.H{"status": "ok"})
}

// GetDesktopSyncSession 获取授权会话状态
// GET /api/desktop-sync/sessions/:state
// VSCode 插件轮询此接口检查授权状态
func GetDesktopSyncSession(c *gin.Context) {
	state := c.Param("state")
	if state == "" {
		common.ApiErrorMsg(c, "state required")
		return
	}

	desktopSyncSessions.RLock()
	session, exists := desktopSyncSessions.items[state]
	desktopSyncSessions.RUnlock()

	if !exists {
		// 会话不存在，返回 pending 状态
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"status": "pending",
			},
		})
		return
	}

	// 返回会话状态
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"status": session.Status,
			"code":   session.Code,
		},
	})
}
