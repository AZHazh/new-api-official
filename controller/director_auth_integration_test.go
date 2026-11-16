package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectorWebAuthorizationHandlers(t *testing.T) {
	t.Setenv("DIRECTOR_AUTH_CLIENT_ID", "director-web")
	t.Setenv("DIRECTOR_AUTH_CLIENT_SECRET", strings.Repeat("s", 40))
	t.Setenv("DIRECTOR_AUTH_REDIRECT_URI", "http://127.0.0.1:4319/auth/callback")
	t.Setenv("SQL_DSN", "local")
	oldPath, oldMaster := common.SQLitePath, common.IsMasterNode
	oldDB, oldLogDB := model.DB, model.LOG_DB
	t.Cleanup(func() { model.DB, model.LOG_DB = oldDB, oldLogDB })
	common.SQLitePath, common.IsMasterNode = filepath.Join(t.TempDir(), "bootstrap.db"), false
	err := model.InitDB()
	common.SQLitePath, common.IsMasterNode = oldPath, oldMaster
	require.NoError(t, err)
	bootstrap, err := model.DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = bootstrap.Close() })
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Ability{}))
	user := model.User{Username: "director-test", Password: "not-a-real-password", DisplayName: "创作者", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	secretKey := strings.Repeat("k", 48)
	require.NoError(t, db.Create(&model.Token{UserId: user.Id, Key: secretKey, Name: "private-token", Status: common.TokenStatusEnabled, Group: "default"}).Error)
	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	router := gin.New()
	router.GET("/info", DirectorWebAuthorizationInfo)
	router.POST("/issue", func(c *gin.Context) { c.Set("id", user.Id); IssueDesktopSyncCode(c) })
	router.POST("/exchange", ExchangeDesktopSyncCode)
	request := func(path string, body any, basic bool) map[string]any {
		t.Helper()
		raw, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if basic {
			req.SetBasicAuth("director-web", strings.Repeat("s", 40))
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var result map[string]any
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		return result
	}
	issue := func(web bool) string {
		t.Helper()
		input := gin.H{"redirect_uri": "http://127.0.0.1:4321/callback"}
		if web {
			input = gin.H{"client_id": "director-web", "redirect_uri": "http://127.0.0.1:4319/auth/callback", "code_challenge": challenge, "code_challenge_method": "S256"}
		}
		result := request("/issue", input, false)
		require.Equal(t, true, result["success"], "%v", result)
		return result["data"].(map[string]any)["code"].(string)
	}
	exchange := func(code string) gin.H {
		return gin.H{"code": code, "client_id": "director-web", "redirect_uri": "http://127.0.0.1:4319/auth/callback", "code_verifier": verifier}
	}
	t.Run("registered metadata contains no account or secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/info?client_id=director-web&redirect_uri=http%3A%2F%2F127.0.0.1%3A4319%2Fauth%2Fcallback&code_challenge_method=S256&code_challenge="+challenge, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var result map[string]any
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.Equal(t, true, result["success"])
		assert.Equal(t, map[string]any{"client_id": "director-web", "redirect_uri": "http://127.0.0.1:4319/auth/callback"}, result["data"])
	})
	t.Run("web grant returns identity only and cannot be replayed", func(t *testing.T) {
		code := issue(true)
		result := request("/exchange", exchange(code), true)
		require.Equal(t, true, result["success"])
		assert.Equal(t, map[string]any{"user": map[string]any{"id": float64(user.Id), "username": user.Username, "display_name": user.DisplayName, "status": float64(common.UserStatusEnabled)}}, result["data"])
		assert.Equal(t, false, request("/exchange", exchange(code), true)["success"])
	})
	for _, field := range []string{"client_id", "redirect_uri", "code_verifier"} {
		t.Run("changed "+field+" is rejected", func(t *testing.T) {
			input := exchange(issue(true))
			input[field] = "incorrect"
			assert.Equal(t, false, request("/exchange", input, true)["success"])
		})
	}
	t.Run("exchange requires client credentials", func(t *testing.T) {
		assert.Equal(t, false, request("/exchange", exchange(issue(true)), false)["success"])
	})
	t.Run("web and desktop scopes cannot cross", func(t *testing.T) {
		assert.Equal(t, false, request("/exchange", gin.H{"code": issue(true)}, false)["success"])
		assert.Equal(t, false, request("/exchange", exchange(issue(false)), true)["success"])
	})
	t.Run("disabled account cannot log in", func(t *testing.T) {
		code := issue(true)
		require.NoError(t, db.Model(&user).Update("status", common.UserStatusDisabled).Error)
		defer db.Model(&user).Update("status", common.UserStatusEnabled)
		assert.Equal(t, false, request("/exchange", exchange(code), true)["success"])
	})
	t.Run("expired grant cannot log in", func(t *testing.T) {
		code := issue(true)
		desktopSyncMemoryStore.Lock()
		entry := desktopSyncMemoryStore.items[code]
		entry.ExpiresAt = time.Now().Add(-time.Minute)
		desktopSyncMemoryStore.items[code] = entry
		desktopSyncMemoryStore.Unlock()
		assert.Equal(t, false, request("/exchange", exchange(code), true)["success"])
	})
	t.Run("legacy desktop retains token list contract", func(t *testing.T) {
		result := request("/exchange", gin.H{"code": issue(false)}, false)
		require.Equal(t, true, result["success"])
		data := result["data"].(map[string]any)
		assert.Contains(t, data, "tokens")
		assert.NotContains(t, data, "user")
	})
}
