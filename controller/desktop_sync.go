package controller

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samber/hot"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
)

// Desktop sync session storage structures
type DesktopAuthCode struct {
	Code      string    `json:"code"`
	UserID    int       `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type DesktopSession struct {
	State     string    `json:"state"`
	TokenName string    `json:"token_name,omitempty"`
	Token     string    `json:"token,omitempty"`
	Status    string    `json:"status"` // pending, approved, rejected
	ExpiresAt time.Time `json:"expires_at"`
}

const (
	authCodeTTL = 5 * time.Minute
	sessionTTL  = 10 * time.Minute
)

var (
	authCodeCache  *cachex.HybridCache[DesktopAuthCode]
	sessionCache   *cachex.HybridCache[DesktopSession]
	desktopCacheInit = false
)

func initDesktopCache() {
	if desktopCacheInit {
		return
	}
	desktopCacheInit = true

	authCodeCache = cachex.NewHybridCache(cachex.HybridCacheConfig[DesktopAuthCode]{
		Namespace:    cachex.Namespace("new-api:desktop_auth_code:v1"),
		Redis:        common.RDB,
		RedisCodec:   cachex.JSONCodec[DesktopAuthCode]{},
		RedisEnabled: func() bool { return common.RedisEnabled && common.RDB != nil },
		Memory: func() *hot.HotCache[string, DesktopAuthCode] {
			return hot.NewHotCache[string, DesktopAuthCode](hot.LRU, 100).Build()
		},
	})

	sessionCache = cachex.NewHybridCache(cachex.HybridCacheConfig[DesktopSession]{
		Namespace:    cachex.Namespace("new-api:desktop_session:v1"),
		Redis:        common.RDB,
		RedisCodec:   cachex.JSONCodec[DesktopSession]{},
		RedisEnabled: func() bool { return common.RedisEnabled && common.RDB != nil },
		Memory: func() *hot.HotCache[string, DesktopSession] {
			return hot.NewHotCache[string, DesktopSession](hot.LRU, 100).Build()
		},
	})
}

// IssueDesktopAuthCode generates a temporary authorization code for desktop client
func IssueDesktopAuthCode(c *gin.Context) {
	initDesktopCache()

	userId := c.GetInt("id")
	if userId == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "unauthorized",
		})
		return
	}

	// Generate random auth code
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to generate auth code",
		})
		return
	}
	code := base64.URLEncoding.EncodeToString(randomBytes)

	authCode := DesktopAuthCode{
		Code:      code,
		UserID:    userId,
		ExpiresAt: time.Now().Add(authCodeTTL),
	}

	if err := authCodeCache.SetWithTTL(code, authCode, authCodeTTL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to store auth code",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"code":       code,
			"expires_in": int(authCodeTTL.Seconds()),
		},
	})
}

// ExchangeDesktopToken exchanges auth code for user tokens
func ExchangeDesktopToken(c *gin.Context) {
	initDesktopCache()

	type ExchangeRequest struct {
		Code string `json:"code" binding:"required"`
	}

	var req ExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request",
		})
		return
	}

	// Retrieve auth code
	authCode, found, err := authCodeCache.Get(req.Code)
	if err != nil || !found || time.Now().After(authCode.ExpiresAt) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "invalid or expired code",
		})
		return
	}

	// Delete after use (one-time code)
	_, _ = authCodeCache.DeleteMany([]string{req.Code})

	// Get user tokens (fetch all by passing large limit)
	tokens, err := model.GetAllUserTokens(authCode.UserID, 0, 1000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to retrieve tokens",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"tokens": tokens,
		},
	})
}

// StoreDesktopSession stores a polling session for desktop authorization
func StoreDesktopSession(c *gin.Context) {
	initDesktopCache()

	userId := c.GetInt("id")
	if userId == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "unauthorized",
		})
		return
	}

	type SessionRequest struct {
		State     string `json:"state" binding:"required"`
		TokenName string `json:"token_name"`
		Token     string `json:"token"`
	}

	var req SessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request",
		})
		return
	}

	session := DesktopSession{
		State:     req.State,
		TokenName: req.TokenName,
		Token:     req.Token,
		Status:    "approved",
		ExpiresAt: time.Now().Add(sessionTTL),
	}

	if err := sessionCache.SetWithTTL(req.State, session, sessionTTL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to store session",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "session stored",
	})
}

// GetDesktopSession retrieves polling session status
func GetDesktopSession(c *gin.Context) {
	initDesktopCache()

	state := c.Param("state")
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "state parameter required",
		})
		return
	}

	session, found, err := sessionCache.Get(state)
	if err != nil || !found || time.Now().After(session.ExpiresAt) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "session not found or expired",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": session,
	})
}
