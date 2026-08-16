package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVideoProxyControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	fetchSetting := system_setting.GetFetchSetting()
	originalFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	service.InitHttpClient()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		*fetchSetting = originalFetchSetting
		service.InitHttpClient()
	})
	return db
}

func executeVideoProxyRequest(t *testing.T, userID int, taskID, rawQuery string, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/videos/"+taskID+"/content"+rawQuery, nil)
	request.Header = headers.Clone()
	router := gin.New()
	router.GET("/v1/videos/:task_id/content", func(context *gin.Context) {
		context.Set("id", userID)
		VideoProxy(context)
	})
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestSelectStoredTaskResultURL(t *testing.T) {
	task := &model.Task{
		PrivateData: model.TaskPrivateData{
			ResultURL:  "https://cdn.example/default.mp4?sig=default-secret",
			ResultURLs: []string{"https://cdn.example/0.mp4?sig=zero-secret", "https://cdn.example/1.mp4?sig=one-secret"},
			ResultAssets: map[string]string{
				"last_frame": "https://cdn.example/frame.jpg?sig=frame-secret",
			},
		},
	}

	tests := []struct {
		name      string
		query     url.Values
		wantURL   string
		wantError string
	}{
		{name: "default first result", query: url.Values{}, wantURL: task.PrivateData.ResultURLs[0]},
		{name: "selected result", query: url.Values{"index": {"1"}}, wantURL: task.PrivateData.ResultURLs[1]},
		{name: "last frame asset", query: url.Values{"asset": {"last_frame"}}, wantURL: task.PrivateData.ResultAssets["last_frame"]},
		{name: "index out of range", query: url.Values{"index": {"2"}}, wantError: "out of range"},
		{name: "negative index", query: url.Values{"index": {"-1"}}, wantError: "non-negative integer"},
		{name: "unknown asset", query: url.Values{"asset": {"thumbnail"}}, wantError: "not available"},
		{name: "index and asset", query: url.Values{"index": {"0"}, "asset": {"last_frame"}}, wantError: "exactly one"},
		{name: "duplicate index", query: url.Values{"index": {"0", "1"}}, wantError: "exactly one"},
		{name: "missing index value", query: url.Values{"index": {}}, wantError: "exactly one"},
		{name: "missing asset value", query: url.Values{"asset": {}}, wantError: "exactly one"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resultURL, err := selectStoredTaskResultURL(task, test.query)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				assert.Empty(t, resultURL)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantURL, resultURL)
		})
	}
}

func TestVideoProxyTransportErrorsMaskSignedURLs(t *testing.T) {
	err := &url.Error{
		Op:  "Get",
		URL: "https://cdn.example/video.mp4?X-Amz-Signature=top-secret&token=private",
		Err: errors.New("connection reset"),
	}

	masked := common.MaskSensitiveInfo(err.Error())
	assert.NotContains(t, masked, "top-secret")
	assert.NotContains(t, masked, "private")
	assert.NotContains(t, masked, "/video.mp4")
	assert.Contains(t, masked, "X-Amz-Signature=***")
}

func TestVideoProxyForwardsRangesAndOnlySafeResponseHeaders(t *testing.T) {
	db := setupVideoProxyControllerTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/second.mp4", request.URL.Path)
		assert.Equal(t, "bytes=2-3", request.Header.Get("Range"))
		assert.Equal(t, `"video-etag"`, request.Header.Get("If-Range"))
		writer.Header().Set("Accept-Ranges", "bytes")
		writer.Header().Set("Content-Length", "2")
		writer.Header().Set("Content-Range", "bytes 2-3/4")
		writer.Header().Set("Content-Type", "video/mp4")
		writer.Header().Set("ETag", `"video-etag"`)
		writer.Header().Set("Content-Disposition", `attachment; filename="upstream-secret.html"`)
		writer.Header().Set("Set-Cookie", "provider_secret=value")
		writer.Header().Set("Location", "https://provider.example/private")
		writer.Header().Set("X-Upstream-Secret", "private")
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write([]byte("cd"))
	}))
	defer upstream.Close()

	channel := &model.Channel{Type: constant.ChannelTypeSeedance, Key: "key", Name: "Seedance"}
	require.NoError(t, db.Create(channel).Error)
	task := &model.Task{
		TaskID:          "task_range",
		Platform:        constant.TaskPlatformSeedance,
		UserId:          77,
		ChannelId:       channel.Id,
		Status:          model.TaskStatusSuccess,
		ResultExpiresAt: time.Now().Unix() + 3600,
		PrivateData: model.TaskPrivateData{ResultURLs: []string{
			upstream.URL + "/first.mp4?signature=private-first",
			upstream.URL + "/second.mp4?signature=private-second",
		}},
	}
	require.NoError(t, db.Create(task).Error)
	headers := make(http.Header)
	headers.Set("Range", "bytes=2-3")
	headers.Set("If-Range", `"video-etag"`)

	recorder := executeVideoProxyRequest(t, task.UserId, task.TaskID, "?index=1&download=true", headers)

	assert.Equal(t, http.StatusPartialContent, recorder.Code)
	assert.Equal(t, "cd", recorder.Body.String())
	assert.Equal(t, "bytes", recorder.Header().Get("Accept-Ranges"))
	assert.Equal(t, "bytes 2-3/4", recorder.Header().Get("Content-Range"))
	assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	assert.Equal(t, `"video-etag"`, recorder.Header().Get("ETag"))
	assert.Equal(t, `attachment; filename="task_range-1.mp4"`, recorder.Header().Get("Content-Disposition"))
	assert.Empty(t, recorder.Header().Get("Set-Cookie"))
	assert.Empty(t, recorder.Header().Get("Location"))
	assert.Empty(t, recorder.Header().Get("X-Upstream-Secret"))
	assert.NotContains(t, recorder.Header().Get("Content-Disposition"), "upstream-secret")
	assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
}

func TestVideoProxyPreservesRangeFailureWithoutUnsafeHeaders(t *testing.T) {
	db := setupVideoProxyControllerTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "bytes=99-", request.Header.Get("Range"))
		writer.Header().Set("Content-Range", "bytes */4")
		writer.Header().Set("Content-Disposition", "attachment; filename=provider-secret")
		writer.Header().Set("Set-Cookie", "provider_secret=value")
		writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer upstream.Close()

	channel := &model.Channel{Type: constant.ChannelTypeSeedance, Key: "key", Name: "Seedance"}
	require.NoError(t, db.Create(channel).Error)
	task := &model.Task{
		TaskID:          "task_range_failure",
		Platform:        constant.TaskPlatformSeedance,
		UserId:          78,
		ChannelId:       channel.Id,
		Status:          model.TaskStatusSuccess,
		ResultExpiresAt: time.Now().Unix() + 3600,
		PrivateData:     model.TaskPrivateData{ResultURL: upstream.URL + "/video.mp4?signature=private"},
	}
	require.NoError(t, db.Create(task).Error)
	headers := make(http.Header)
	headers.Set("Range", "bytes=99-")

	recorder := executeVideoProxyRequest(t, task.UserId, task.TaskID, "", headers)

	assert.Equal(t, http.StatusRequestedRangeNotSatisfiable, recorder.Code)
	assert.Equal(t, "bytes */4", recorder.Header().Get("Content-Range"))
	assert.Empty(t, recorder.Header().Get("Content-Disposition"))
	assert.Empty(t, recorder.Header().Get("Set-Cookie"))
	assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
}

func TestVideoProxyReturnsGoneForExpiredSeedanceResults(t *testing.T) {
	db := setupVideoProxyControllerTest(t)
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		status, err := strconv.Atoi(request.URL.Query().Get("status"))
		require.NoError(t, err)
		writer.WriteHeader(status)
	}))
	defer upstream.Close()

	channel := &model.Channel{Type: constant.ChannelTypeSeedance, Key: "key", Name: "Seedance"}
	require.NoError(t, db.Create(channel).Error)
	tests := []struct {
		expiresAt int64
		status    int
	}{
		{expiresAt: time.Now().Unix() - 1, status: http.StatusNotFound},
		{expiresAt: time.Now().Unix() + 3600, status: http.StatusForbidden},
		{expiresAt: time.Now().Unix() + 3600, status: http.StatusNotFound},
		{expiresAt: time.Now().Unix() + 3600, status: http.StatusGone},
	}
	for index, test := range tests {
		task := &model.Task{
			TaskID:          "task_gone_" + strconv.Itoa(index),
			Platform:        constant.TaskPlatformSeedance,
			UserId:          79,
			ChannelId:       channel.Id,
			Status:          model.TaskStatusSuccess,
			ResultExpiresAt: test.expiresAt,
			PrivateData: model.TaskPrivateData{ResultURL: upstream.URL + "/missing.mp4?signature=private&status=" +
				strconv.Itoa(test.status)},
		}
		require.NoError(t, db.Create(task).Error)
		recorder := executeVideoProxyRequest(t, task.UserId, task.TaskID, "", make(http.Header))
		assert.Equal(t, http.StatusGone, recorder.Code)
		assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
		assert.NotContains(t, recorder.Body.String(), "signature")
	}
	assert.Equal(t, 3, requestCount, "locally expired results must not access the upstream")
}

func TestVideoProxyRejectsUnsafeUpstreamContentTypeAndHeaders(t *testing.T) {
	destination := make(http.Header)
	source := make(http.Header)
	source.Set("Content-Type", "text/html; charset=utf-8")
	source.Set("Content-Disposition", "inline")
	source.Set("Set-Cookie", "secret=value")

	copyVideoResponseHeaders(destination, source)

	assert.Empty(t, destination.Get("Content-Type"))
	assert.Empty(t, destination.Get("Content-Disposition"))
	assert.Empty(t, destination.Get("Set-Cookie"))
	assert.True(t, safeVideoProxyContentType("video/mp4; codecs=h264"))
	assert.True(t, safeVideoProxyContentType("image/png"))
	assert.False(t, safeVideoProxyContentType("image/svg+xml"))
	assert.False(t, safeVideoProxyContentType("text/html"))
	assert.False(t, safeVideoProxyContentType("not a mime type"))
}
