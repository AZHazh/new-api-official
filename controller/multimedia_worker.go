package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type multimediaRelayIdentityKey struct{}

var multimediaWorkerOnce sync.Once

// This private, in-process router uses the same distribution, quota reservation,
// relay, and settlement handlers as /v1. It is never mounted on an HTTP listener.
func newMultimediaRelayEngine() *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Recovery(), middleware.RequestId(), middleware.BodyStorageCleanup())
	engine.Use(func(c *gin.Context) {
		job, ok := c.Request.Context().Value(multimediaRelayIdentityKey{}).(*model.ImageGeneration)
		if !ok {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		user, err := model.GetUserCache(job.UserID)
		if err != nil || user.Status != common.UserStatusEnabled {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		var request service.MultimediaImageRequest
		if common.UnmarshalJsonStr(job.Parameters, &request) != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if _, err := videoGenerationGroups(user.Group, request.Group); err != nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		catalog, err := buildMultimediaCatalog(user, request.Group)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		index := slices.IndexFunc(catalog.Models, func(item multimediaImageModel) bool { return item.Name == request.Model })
		if index < 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "This image model is no longer available"}})
			return
		}
		selected := catalog.Models[index]
		if err := service.ValidateMultimediaImageRequest(&request, selected.Capabilities); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		protocol := request.Protocol
		if protocol == "" {
			protocol = service.ImageProtocolImages
		}
		if protocol != selected.Capabilities.Protocol {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "The model configuration has changed. Please submit the generation again."}})
			return
		}
		user.WriteContext(c)
		common.SetContextKey(c, constant.ContextKeyBrowserSessionBilling, true)
		c.Set("username", user.Username)
		c.Set("role", user.Role)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, request.Group)
		if err := middleware.SetupContextForToken(c, &model.Token{UserId: user.Id, Name: "multimedia-session", Group: request.Group, UnlimitedQuota: true}); err != nil {
			c.Abort()
			return
		}
		if selected.pinChannel {
			common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, strconv.Itoa(selected.routeChannelID))
		}
	})
	engine.Use(middleware.SystemPerformanceCheck(), middleware.ModelRequestRateLimit(), middleware.Distribute())
	for _, path := range []string{"/v1/images/generations", "/v1/images/edits"} {
		engine.POST(path, func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIImage) })
	}
	engine.POST("/v1beta/models/*path", func(c *gin.Context) { Relay(c, types.RelayFormatGemini) })
	engine.POST("/v1/chat/completions", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAI) })
	return engine
}

func StartMultimediaWorker() {
	multimediaWorkerOnce.Do(func() {
		engine := newMultimediaRelayEngine()
		go func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					common.SysError(fmt.Sprintf("multimedia worker panic: %v", recovered))
				}
			}()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			slots := make(chan struct{}, 2)
			lastCleanup := time.Time{}
			for range ticker.C {
				if time.Since(lastCleanup) >= time.Minute {
					lastCleanup = time.Now()
					// Never retry a possibly billed request after a process restart.
					if err := model.DB.Model(&model.ImageGeneration{}).Where("status = ? AND started_at < ?", "running", time.Now().Add(-20*time.Minute).Unix()).Updates(map[string]any{"status": "interrupted", "error": "Generation was interrupted. Check the consumption log before trying again.", "finished_at": time.Now().Unix()}).Error; err != nil {
						common.SysError("expire interrupted image jobs: " + err.Error())
					}
					if err := service.CleanupMediaAssets(context.Background()); err != nil {
						common.SysError("clean multimedia assets: " + err.Error())
					}
				}
				if len(slots) == cap(slots) {
					continue
				}
				var jobs []model.ImageGeneration
				if err := model.DB.Where("status = ?", "queued").Order("id ASC").Limit(cap(slots) - len(slots)).Find(&jobs).Error; err != nil {
					common.SysError("query queued image jobs: " + err.Error())
					continue
				}
				for _, job := range jobs {
					result := model.DB.Model(&model.ImageGeneration{}).Where("id = ? AND status = ?", job.ID, "queued").Updates(map[string]any{"status": "running", "started_at": time.Now().Unix()})
					if result.Error != nil || result.RowsAffected != 1 {
						continue
					}
					slots <- struct{}{}
					go func(job model.ImageGeneration) {
						defer func() { <-slots }()
						runMultimediaImageJob(engine, &job)
					}(job)
				}
			}
		}()
	})
}

type multimediaResponseCapture struct {
	header http.Header
	status int
	file   *os.File
	bytes  int64
}

func (capture *multimediaResponseCapture) Header() http.Header { return capture.header }
func (capture *multimediaResponseCapture) WriteHeader(status int) {
	if capture.status == 0 {
		capture.status = status
	}
}
func (capture *multimediaResponseCapture) Flush() {}
func (capture *multimediaResponseCapture) Write(data []byte) (int, error) {
	if capture.status == 0 {
		capture.status = http.StatusOK
	}
	if capture.bytes+int64(len(data)) > 512<<20 {
		return 0, errors.New("image response exceeds the storage limit")
	}
	n, err := capture.file.Write(data)
	capture.bytes += int64(n)
	return n, err
}

func buildMultimediaRelayRequest(ctx context.Context, job *model.ImageGeneration) (*http.Request, func(), error) {
	var parameters service.MultimediaImageRequest
	if err := common.UnmarshalJsonStr(job.Parameters, &parameters); err != nil {
		return nil, nil, err
	}
	if err := service.ValidateImageReferences(job.UserID, &parameters); err != nil {
		return nil, nil, err
	}
	if parameters.N == nil || *parameters.N < 1 || *parameters.N > dto.MaxImageN {
		return nil, nil, errors.New("invalid persisted image count")
	}
	fields := map[string]any{"model": parameters.Model, "prompt": parameters.Prompt, "n": *parameters.N}
	if parameters.Size != "" && (parameters.Size != "auto" || parameters.Quality != "") {
		fields["size"] = parameters.Size
	}
	if parameters.Quality != "" {
		fields["quality"] = parameters.Quality
	}
	if parameters.OutputFormat != "" {
		fields["output_format"] = parameters.OutputFormat
	}
	if parameters.OutputCompression != nil {
		fields["output_compression"] = *parameters.OutputCompression
	}
	path := "/v1/images/generations"
	contentType := "application/json"
	var body io.Reader
	cleanup := func() {}
	if parameters.Protocol == service.ImageProtocolGemini || parameters.Protocol == service.ImageProtocolChat {
		var encoded []byte
		var err error
		path, encoded, err = buildMultimediaContentBody(job.UserID, &parameters)
		if err != nil {
			return nil, nil, err
		}
		body = bytes.NewReader(encoded)
	} else if len(parameters.References) == 0 {
		encoded, err := common.Marshal(fields)
		if err != nil {
			return nil, nil, err
		}
		body = bytes.NewReader(encoded)
	} else {
		path = "/v1/images/edits"
		file, err := os.CreateTemp("", "new-api-image-edit-*")
		if err != nil {
			return nil, nil, err
		}
		cleanup = func() { _ = file.Close(); _ = os.Remove(file.Name()) }
		writer := multipart.NewWriter(file)
		for key, value := range fields {
			if err := writer.WriteField(key, fmt.Sprint(value)); err != nil {
				cleanup()
				return nil, nil, err
			}
		}
		for _, id := range parameters.References {
			asset, err := model.GetMediaAsset(job.UserID, id)
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			assetPath, err := service.MediaAssetPath(asset, false)
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			source, err := os.Open(assetPath)
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			field := "image[]"
			if len(parameters.References) == 1 {
				field = "image"
			}
			part, err := writer.CreateFormFile(field, asset.Name)
			if err == nil {
				_, err = io.Copy(part, source)
			}
			_ = source.Close()
			if err != nil {
				cleanup()
				return nil, nil, err
			}
		}
		if err := writer.Close(); err != nil {
			cleanup()
			return nil, nil, err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			cleanup()
			return nil, nil, err
		}
		contentType = writer.FormDataContentType()
		body = file
	}
	request, err := http.NewRequestWithContext(context.WithValue(ctx, multimediaRelayIdentityKey{}, job), http.MethodPost, path, body)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	request.Header.Set("Content-Type", contentType)
	request.RemoteAddr = net.JoinHostPort(job.RequestIP, "0")
	return request, cleanup, nil
}

func buildMultimediaContentBody(userID int, parameters *service.MultimediaImageRequest) (string, []byte, error) {
	if parameters.N == nil || *parameters.N != 1 {
		return "", nil, errors.New("this image model supports one output")
	}
	parts := []dto.GeminiPart{{Text: parameters.Prompt}}
	content := []map[string]any{{"type": "text", "text": parameters.Prompt}}
	for _, id := range parameters.References {
		asset, err := model.GetMediaAsset(userID, id)
		if err != nil {
			return "", nil, err
		}
		path, err := service.MediaAssetPath(asset, false)
		if err != nil {
			return "", nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", nil, err
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		parts = append(parts, dto.GeminiPart{InlineData: &dto.GeminiInlineData{MimeType: asset.MimeType, Data: encoded}})
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + asset.MimeType + ";base64," + encoded}})
	}
	if parameters.Protocol == service.ImageProtocolGemini {
		config := make(map[string]string)
		if parameters.Size != "" && parameters.Size != "auto" {
			config["aspectRatio"] = parameters.Size
		}
		if parameters.Resolution != "" {
			config["imageSize"] = parameters.Resolution
		}
		request := dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{{Role: "user", Parts: parts}}, GenerationConfig: dto.GeminiChatGenerationConfig{ResponseModalities: []string{"TEXT", "IMAGE"}}}
		if len(config) > 0 {
			encoded, err := common.Marshal(config)
			if err != nil {
				return "", nil, err
			}
			request.GenerationConfig.ImageConfig = encoded
		}
		encoded, err := common.Marshal(request)
		return "/v1beta/models/" + url.PathEscape(parameters.Model) + ":generateContent", encoded, err
	}
	config := make(map[string]string)
	if parameters.Size != "" && parameters.Size != "auto" {
		config["aspect_ratio"] = parameters.Size
	}
	if parameters.Resolution != "" {
		config["image_size"] = parameters.Resolution
	}
	request := map[string]any{"model": parameters.Model, "messages": []map[string]any{{"role": "user", "content": content}}, "modalities": []string{"text", "image"}, "stream": false}
	if len(config) > 0 {
		request["image_config"] = config
	}
	encoded, err := common.Marshal(request)
	return "/v1/chat/completions", encoded, err
}

func runMultimediaImageJob(engine *gin.Engine, job *model.ImageGeneration) {
	status, message := "failed", "Image generation failed"
	defer func() {
		if recovered := recover(); recovered != nil {
			common.SysError(fmt.Sprintf("image job %s panic: %v", job.GenerationID, recovered))
			message = "Generation was interrupted. Check the consumption log before trying again."
			status = "interrupted"
		}
		if err := model.DB.Model(job).Where("status = ?", "running").Updates(map[string]any{"status": status, "error": message, "finished_at": time.Now().Unix()}).Error; err != nil {
			common.SysError("save image job status: " + err.Error())
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	request, cleanup, err := buildMultimediaRelayRequest(ctx, job)
	if err != nil {
		message = err.Error()
		return
	}
	defer cleanup()
	output, err := os.CreateTemp("", "new-api-image-response-*")
	if err != nil {
		message = "Image storage is unavailable"
		return
	}
	defer func() { _ = output.Close(); _ = os.Remove(output.Name()) }()
	capture := &multimediaResponseCapture{header: make(http.Header), file: output}
	engine.ServeHTTP(capture, request)
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		return
	}
	if capture.status < 200 || capture.status >= 300 {
		var response struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if common.DecodeJson(io.LimitReader(output, 1<<20), &response) == nil && response.Error.Message != "" {
			message = common.MaskSensitiveInfo(response.Error.Message)
		}
		return
	}
	images, err := service.ExtractMultimediaImages(output)
	if err != nil {
		status = "storage_failed"
		message = "The image provider returned an invalid result. Check the consumption log before trying again."
		return
	}
	failed := 0
	for i, item := range images {
		var reader io.Reader
		var remote *http.Response
		if item.Base64 != "" {
			reader = base64.NewDecoder(base64.StdEncoding, strings.NewReader(item.Base64))
		} else if item.URL != "" {
			if err = service.ValidateSSRFProtectedFetchURL(item.URL); err == nil {
				var download *http.Request
				download, err = http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
				if err == nil {
					remote, err = service.GetSSRFProtectedHTTPClient().Do(download)
				}
			}
			if err == nil && remote.StatusCode == http.StatusOK {
				reader = remote.Body
			}
		}
		if reader == nil {
			failed++
			if remote != nil {
				_ = remote.Body.Close()
			}
			continue
		}
		_, storeErr := service.StoreMediaImage(job.UserID, job.GenerationID, "generated", "", reader)
		if remote != nil {
			_ = remote.Body.Close()
		}
		if storeErr != nil {
			failed++
			common.SysError("save image result " + job.GenerationID + "/" + strconv.Itoa(i) + ": " + common.MaskSensitiveInfo(storeErr.Error()))
		}
	}
	if failed > 0 {
		message = "Generation completed, but some images could not be saved. Check the consumption log before trying again."
		status = "partial"
		if failed == len(images) {
			status = "storage_failed"
		}
		return
	}
	status, message = "succeeded", ""
}
