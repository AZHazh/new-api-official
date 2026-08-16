package controller

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// videoProxyError returns a standardized OpenAI-style error response.
func videoProxyError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	})
}

func VideoProxy(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")

	taskID := c.Param("task_id")
	if taskID == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
		return
	}

	userID := c.GetInt("id")
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to query task")
		return
	}
	if !exists || task == nil {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		return
	}

	if task.Status != model.TaskStatusSuccess {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Task is not completed yet, current status: %s", task.Status))
		return
	}
	if task.ResultExpiresAt > 0 && task.ResultExpiresAt <= time.Now().Unix() {
		videoProxyError(c, http.StatusGone, "video_expired", "Video result has expired")
		return
	}

	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to get channel for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to retrieve channel information")
		return
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	var videoURL string
	proxy := channel.GetSetting().Proxy
	client := service.GetSSRFProtectedHTTPClient()
	if proxy != "" {
		// 渠道代理路径的连接由代理侧建立，无法做拨号时逐 IP 校验，
		// 因此后面对 videoURL 保留请求前的一次性 SSRF 校验。
		client, err = service.GetHttpClientWithProxy(proxy)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create proxy client for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy client")
			return
		}
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, "", nil)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create request: %s", err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	switch channel.Type {
	case constant.ChannelTypeGemini:
		apiKey := task.PrivateData.Key
		if apiKey == "" {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Missing stored API key for Gemini task %s", taskID))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "API key not stored for task")
			return
		}
		videoURL, err = getGeminiVideoURL(channel, task, apiKey)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Gemini video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Gemini video URL")
			return
		}
		req.Header.Set("x-goog-api-key", apiKey)
	case constant.ChannelTypeVertexAi:
		videoURL, err = getVertexVideoURL(channel, task)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Vertex video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Vertex video URL")
			return
		}
	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
		query := c.Request.URL.Query()
		_, hasIndex := query["index"]
		_, hasAsset := query["asset"]
		if hasIndex || hasAsset {
			videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "index and asset are not supported for this task")
			return
		}
		videoURL = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, task.GetUpstreamTaskID())
		req.Header.Set("Authorization", "Bearer "+channel.Key)
	default:
		videoURL, err = selectStoredTaskResultURL(task, c.Request.URL.Query())
		if err != nil {
			videoProxyError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
	}

	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL is empty for task %s", taskID))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}

	if strings.HasPrefix(videoURL, "data:") {
		if err := writeVideoDataURL(c, videoURL); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to decode video data URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		}
		return
	}

	if proxy != "" || !service.HasCustomFetchDNSResolver() {
		fetchSetting := system_setting.GetFetchSetting()
		if validateErr := common.ValidateURLWithFetchSetting(videoURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); validateErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL blocked for task %s: %v", taskID, validateErr))
			videoProxyError(c, http.StatusForbidden, "server_error", fmt.Sprintf("request blocked: %v", validateErr))
			return
		}
	}

	req.URL, err = url.Parse(videoURL)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to parse result URL for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}
	for _, header := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if value := c.GetHeader(header); value != "" {
			req.Header.Set(header, value)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch video for task %s: %s", taskID, common.MaskSensitiveInfo(err.Error())))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		copyVideoResponseHeaders(c.Writer.Header(), resp.Header)
		c.Status(http.StatusNotModified)
		return
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			copyVideoResponseHeaders(c.Writer.Header(), resp.Header)
			c.Status(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if channel.Type == constant.ChannelTypeSeedance &&
			(resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone) {
			videoProxyError(c, http.StatusGone, "video_expired", "Video result has expired")
			return
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("Upstream returned status %d for video task %s", resp.StatusCode, taskID))
		videoProxyError(c, http.StatusBadGateway, "server_error",
			fmt.Sprintf("Upstream service returned status %d", resp.StatusCode))
		return
	}

	copyVideoResponseHeaders(c.Writer.Header(), resp.Header)
	if c.Writer.Header().Get("Content-Type") == "" {
		c.Writer.Header().Set("Content-Type", "application/octet-stream")
	}
	if downloadRequested(c.Query("download")) {
		filename := task.TaskID + ".mp4"
		if c.Query("asset") != "" {
			filename = task.TaskID + "-" + c.Query("asset") + ".jpg"
		} else if c.Query("index") != "" {
			filename = task.TaskID + "-" + c.Query("index") + ".mp4"
		}
		c.Writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	}
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = io.Copy(c.Writer, resp.Body); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream video content: %s", err.Error()))
	}
}

func selectStoredTaskResultURL(task *model.Task, query url.Values) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is required")
	}
	indexValues, hasIndex := query["index"]
	assetValues, hasAsset := query["asset"]
	if (hasIndex && len(indexValues) != 1) || (hasAsset && len(assetValues) != 1) || (hasIndex && hasAsset) {
		return "", fmt.Errorf("specify exactly one index or asset")
	}
	if hasAsset {
		asset := strings.TrimSpace(assetValues[0])
		if asset == "" {
			return "", fmt.Errorf("asset must not be empty")
		}
		resultURL, ok := task.PrivateData.ResultAssets[asset]
		if !ok || strings.TrimSpace(resultURL) == "" {
			return "", fmt.Errorf("asset %q is not available", asset)
		}
		return resultURL, nil
	}

	index := 0
	if hasIndex {
		parsed, err := strconv.Atoi(strings.TrimSpace(indexValues[0]))
		if err != nil || parsed < 0 {
			return "", fmt.Errorf("index must be a non-negative integer")
		}
		index = parsed
	}
	if len(task.PrivateData.ResultURLs) > 0 {
		if index >= len(task.PrivateData.ResultURLs) {
			return "", fmt.Errorf("index %d is out of range", index)
		}
		return task.PrivateData.ResultURLs[index], nil
	}
	if hasIndex && index != 0 {
		return "", fmt.Errorf("index %d is out of range", index)
	}
	return task.GetResultURL(), nil
}

func downloadRequested(value string) bool {
	requested, err := strconv.ParseBool(value)
	return err == nil && requested
}

func copyVideoResponseHeaders(destination, source http.Header) {
	for _, header := range []string{
		"Accept-Ranges",
		"Content-Length",
		"Content-Range",
		"ETag",
		"Last-Modified",
	} {
		for _, value := range source.Values(header) {
			destination.Add(header, value)
		}
	}
	if contentType := source.Get("Content-Type"); safeVideoProxyContentType(contentType) {
		destination.Set("Content-Type", contentType)
	}
}

func safeVideoProxyContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "video/") || strings.HasPrefix(mediaType, "audio/") || mediaType == "application/octet-stream" {
		return true
	}
	switch mediaType {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func writeVideoDataURL(c *gin.Context, dataURL string) error {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid data url")
	}

	header := parts[0]
	payload := parts[1]
	if !strings.HasPrefix(header, "data:") || !strings.Contains(header, ";base64") {
		return fmt.Errorf("unsupported data url")
	}

	mimeType := strings.TrimPrefix(header, "data:")
	mimeType = strings.TrimSuffix(mimeType, ";base64")
	if mimeType == "" {
		mimeType = "video/mp4"
	}
	if !safeVideoProxyContentType(mimeType) {
		mimeType = "application/octet-stream"
	}

	videoBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		videoBytes, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return err
		}
	}

	c.Writer.Header().Set("Content-Type", mimeType)
	c.Writer.Header().Set("Cache-Control", "private, no-store")
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	c.Writer.WriteHeader(http.StatusOK)
	_, err = c.Writer.Write(videoBytes)
	return err
}
