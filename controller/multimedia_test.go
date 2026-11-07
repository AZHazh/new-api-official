package controller

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMultimediaTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupVideoGenerationCatalogTest(t)
	require.NoError(t, i18n.Init())
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}, &model.Model{}, &model.ImageGeneration{}, &model.MediaAsset{}, &model.UserSubscription{}, &model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}))
	t.Setenv("MULTIMEDIA_STORAGE_DIR", t.TempDir())
	return db
}

func TestMultimediaJobUsesRelayBillingAndPersistsImage(t *testing.T) {
	db := setupMultimediaTest(t)
	previousBatch, previousLogs := common.BatchUpdateEnabled, common.LogConsumeEnabled
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.BatchUpdateEnabled = previousBatch; common.LogConsumeEnabled = previousLogs })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"dall-e-3":0.01}`))
	var imageData bytes.Buffer
	require.NoError(t, png.Encode(&imageData, image.NewNRGBA(image.Rect(0, 0, 4, 3))))
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/v1/images/generations", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		var request struct {
			N      uint   `json:"n"`
			Prompt string `json:"prompt"`
		}
		if !assert.NoError(t, common.DecodeJson(r.Body, &request)) {
			w.WriteHeader(400)
			return
		}
		assert.Equal(t, uint(1), request.N)
		assert.Equal(t, "A paper boat", request.Prompt)
		w.Header().Set("Content-Type", "application/json")
		data, err := common.Marshal(gin.H{"data": []gin.H{{"b64_json": base64.StdEncoding.EncodeToString(imageData.Bytes())}}})
		if !assert.NoError(t, err) {
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write(data)
	}))
	defer upstream.Close()
	service.InitHttpClient()
	baseURL := upstream.URL
	channel := model.Channel{Id: 8101, Name: "images", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "test-key", BaseURL: &baseURL, Models: "dall-e-3", Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "dall-e-3", ChannelId: channel.Id, Enabled: true}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	user := model.User{Id: 91, Username: "image-owner", Group: "default", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Quota: 1000000}
	require.NoError(t, db.Create(&user).Error)
	request := service.MultimediaImageRequest{Model: "dall-e-3", Group: "default", Prompt: "A paper boat", N: common.GetPointer(uint(1)), Size: "1024x1024", Quality: "standard", RequestKey: "one-request"}
	parameters, err := common.Marshal(request)
	require.NoError(t, err)
	job := model.ImageGeneration{UserID: user.Id, RequestKey: request.RequestKey, Model: request.Model, Prompt: request.Prompt, Parameters: string(parameters), RequestIP: "::1"}
	created, err := model.CreateImageGeneration(&job)
	require.NoError(t, err)
	require.True(t, created)
	duplicate := model.ImageGeneration{UserID: user.Id, RequestKey: request.RequestKey}
	created, err = model.CreateImageGeneration(&duplicate)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, job.GenerationID, duplicate.GenerationID)
	require.NoError(t, db.Model(&job).Updates(map[string]any{"status": "running", "started_at": time.Now().Unix()}).Error)
	runMultimediaImageJob(newMultimediaRelayEngine(), &job)
	require.NoError(t, db.First(&job, job.ID).Error)
	require.Equal(t, "succeeded", job.Status, job.Error)
	assert.Equal(t, int32(1), calls.Load())
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Less(t, user.Quota, 1000000)
	assert.Equal(t, 1000000-user.Quota, user.UsedQuota)
	assets, total, err := model.ListMediaAssets(user.Id, 0, 18, "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, assets, 1)
	assert.Equal(t, job.GenerationID, assets[0].GenerationID)
	assert.Equal(t, 4, assets[0].Width)
	var tokens int64
	require.NoError(t, db.Model(&model.Token{}).Count(&tokens).Error)
	assert.Zero(t, tokens)
}

func TestMultimediaCatalogMapsAliasesAndPinsIncompatibleRoutes(t *testing.T) {
	db := setupMultimediaTest(t)
	mapping := `{"paint":"gpt-image-2","ambiguous":"gpt-image-2"}`
	otherMapping := `{"ambiguous":"dall-e-3"}`
	channels := []model.Channel{
		{Id: 8201, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "key", Name: "images", Models: "paint,ambiguous", Group: "default", ModelMapping: &mapping},
		{Id: 8202, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "key", Name: "other", Models: "ambiguous", Group: "default", ModelMapping: &otherMapping},
	}
	for _, channel := range channels {
		require.NoError(t, db.Create(&channel).Error)
	}
	for _, ability := range []model.Ability{{Group: "default", Model: "paint", ChannelId: 8201, Enabled: true}, {Group: "default", Model: "ambiguous", ChannelId: 8201, Enabled: true}, {Group: "default", Model: "ambiguous", ChannelId: 8202, Enabled: true}} {
		require.NoError(t, db.Create(&ability).Error)
	}
	catalog, err := buildMultimediaCatalog(&model.UserBase{Id: 1, Group: "default"}, "default")
	require.NoError(t, err)
	require.Len(t, catalog.Models, 2)
	assert.Equal(t, "ambiguous", catalog.Models[0].Name)
	assert.True(t, catalog.Models[0].pinChannel)
	assert.Equal(t, 8201, catalog.Models[0].routeChannelID)
	assert.Equal(t, "paint", catalog.Models[1].Name)
	assert.True(t, catalog.Models[1].Capabilities.CustomSize)
	_, err = buildMultimediaCatalog(&model.UserBase{Id: 1, Group: "default"}, "private")
	require.Error(t, err)
}

func TestMultimediaPinnedAliasDoesNotRetryIntoAnIncompatibleChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	err := types.NewError(errors.New("invalid channel image parameters"), types.ErrorCodeChannelParamOverrideInvalid)
	assert.True(t, shouldRetry(c, err, 1))
	common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, "8201")
	assert.False(t, shouldRetry(c, err, 1))
}

func TestMultimediaCatalogAutomaticallyCombinesPermittedGroupsAndCustomImageEndpoints(t *testing.T) {
	db := setupMultimediaTest(t)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"gemini-image":1,"gpt-image":1,"private":1}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"gemini-image":"Gemini images","gpt-image":"GPT images","auto":"Automatic"}`))
	channels := []model.Channel{
		{Id: 8301, Type: constant.ChannelTypeGemini, Status: common.ChannelStatusEnabled, Key: "key", Name: "gemini-image", Models: "gemini-3.1-flash-image-preview", Group: "gemini-image"},
		{Id: 8302, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "key", Name: "generic", Models: "studio-v7,gpt-4o,gpt-image-2", Group: "gpt-image"},
		{Id: 8303, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "key", Name: "private", Models: "private-image", Group: "private"},
		{Id: 8304, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled, Key: "key", Name: "disabled", Models: "disabled-image", Group: "gpt-image"},
		{Id: 8305, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "key", Name: "default", Models: "gpt-image-2", Group: "default"},
	}
	for _, channel := range channels {
		require.NoError(t, db.Create(&channel).Error)
		for _, name := range strings.Split(channel.Models, ",") {
			require.NoError(t, db.Create(&model.Ability{Group: channel.Group, Model: name, ChannelId: channel.Id, Enabled: true}).Error)
		}
	}
	require.NoError(t, db.Create(&model.Model{ModelName: "studio-v7", Endpoints: `{"image-generation":"/v1/images/generations"}`, Status: 1}).Error)
	user := &model.UserBase{Id: 1, Group: "default"}
	catalog, err := buildMultimediaCatalog(user, "")
	require.NoError(t, err)
	require.Len(t, catalog.Models, 3)
	assert.Equal(t, "gemini-3.1-flash-image-preview", catalog.Models[0].Name)
	assert.Equal(t, "gemini-image", catalog.Models[0].routeGroup)
	assert.Equal(t, service.ImageProtocolGemini, catalog.Models[0].Capabilities.Protocol)
	assert.Equal(t, "gpt-image-2", catalog.Models[1].Name)
	assert.Equal(t, "default", catalog.Models[1].routeGroup)
	assert.Equal(t, "studio-v7", catalog.Models[2].Name)
	assert.Equal(t, "gpt-image", catalog.Models[2].routeGroup)
	_, err = buildMultimediaCatalog(user, "private")
	require.ErrorContains(t, err, "not available")

	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`[]`))
	catalog, err = buildMultimediaCatalog(user, "")
	require.NoError(t, err)
	assert.Len(t, catalog.Models, 3, "model selection must work without configured automatic groups")
}

func TestMultimediaModelsGenerateWithoutChoosingAGroupAcrossImageProtocols(t *testing.T) {
	for _, tc := range []struct {
		name          string
		channelType   int
		protocol      string
		upstreamModel string
	}{
		{"native Gemini", constant.ChannelTypeGemini, service.ImageProtocolGemini, "gemini-3.1-flash-image-preview"},
		{"OpenAI compatible chat", constant.ChannelTypeOpenRouter, service.ImageProtocolChat, "gemini-3.1-flash-image-preview"},
		{"custom image endpoint", constant.ChannelTypeOpenAI, service.ImageProtocolImages, "studio-v7"},
		{"Imagen", constant.ChannelTypeGemini, service.ImageProtocolImages, "imagen-4.0-generate-001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupMultimediaTest(t)
			previousBatch, previousLogs := common.BatchUpdateEnabled, common.LogConsumeEnabled
			common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false
			t.Cleanup(func() { common.BatchUpdateEnabled, common.LogConsumeEnabled = previousBatch, previousLogs })
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"images":2}`))
			require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"images":"Images"}`))
			require.NoError(t, setting.UpdateAutoGroupsByJsonString(`[]`))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"paint":0.01}`))
			var imageData bytes.Buffer
			require.NoError(t, png.Encode(&imageData, image.NewNRGBA(image.Rect(0, 0, 4, 3))))
			encodedImage := base64.StdEncoding.EncodeToString(imageData.Bytes())
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request map[string]any
				if !assert.NoError(t, common.DecodeJson(r.Body, &request)) {
					w.WriteHeader(400)
					return
				}
				var response any
				if tc.protocol == service.ImageProtocolGemini {
					assert.Equal(t, "/v1beta/models/"+tc.upstreamModel+":generateContent", r.URL.Path)
					assert.Equal(t, "test-key", r.Header.Get("x-goog-api-key"))
					assert.Contains(t, request, "contents")
					config, ok := request["generationConfig"].(map[string]any)
					if assert.True(t, ok) {
						assert.Equal(t, map[string]any{"aspectRatio": "16:9", "imageSize": "2K"}, config["imageConfig"])
					}
					response = gin.H{"candidates": []gin.H{{"content": gin.H{"role": "model", "parts": []gin.H{{"text": "Here is your image"}, {"inlineData": gin.H{"mimeType": "image/png", "data": encodedImage}}}}}}, "usageMetadata": gin.H{"promptTokenCount": 20, "candidatesTokenCount": 1200, "totalTokenCount": 1220}}
				} else if tc.protocol == service.ImageProtocolChat {
					assert.Equal(t, "/v1/chat/completions", r.URL.Path)
					assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
					assert.Equal(t, tc.upstreamModel, request["model"])
					assert.Equal(t, map[string]any{"aspect_ratio": "16:9", "image_size": "2K"}, request["image_config"])
					response = gin.H{"choices": []gin.H{{"message": gin.H{"role": "assistant", "content": "", "images": []gin.H{{"type": "image_url", "image_url": gin.H{"url": "data:image/png;base64," + encodedImage}}}}, "finish_reason": "stop"}}, "usage": gin.H{"prompt_tokens": 20, "completion_tokens": 1200, "total_tokens": 1220}}
				} else if strings.HasPrefix(tc.upstreamModel, "imagen-") {
					assert.Equal(t, "/v1beta/models/"+tc.upstreamModel+":predict", r.URL.Path)
					parameters, ok := request["parameters"].(map[string]any)
					if assert.True(t, ok) {
						assert.Equal(t, float64(1), parameters["sampleCount"])
						assert.Equal(t, "16:9", parameters["aspectRatio"])
					}
					response = gin.H{"predictions": []gin.H{{"mimeType": "image/png", "bytesBase64Encoded": encodedImage}}}
				} else {
					assert.Equal(t, "/v1/images/generations", r.URL.Path)
					assert.Equal(t, tc.upstreamModel, request["model"])
					assert.Equal(t, "1024x1024", request["size"])
					assert.Equal(t, float64(1), request["n"])
					response = gin.H{"data": []gin.H{{"b64_json": encodedImage}}}
				}
				assert.NotContains(t, request, "group")
				assert.NotContains(t, request, "quality")
				assert.NotContains(t, request, "background")
				assert.NotContains(t, request, "moderation")
				assert.NotContains(t, request, "mask")
				data, err := common.Marshal(response)
				if !assert.NoError(t, err) {
					w.WriteHeader(500)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(data)
			}))
			defer upstream.Close()
			service.InitHttpClient()
			mappingData, err := common.Marshal(map[string]string{"paint": tc.upstreamModel})
			require.NoError(t, err)
			mapping := string(mappingData)
			if tc.upstreamModel == "studio-v7" {
				require.NoError(t, db.Create(&model.Model{ModelName: "studio-v7", Endpoints: `{"image-generation":"/v1/images/generations"}`, Status: 1}).Error)
			}
			baseURL := upstream.URL
			channel := model.Channel{Id: 8401, Type: tc.channelType, Status: common.ChannelStatusEnabled, Key: "test-key", Name: "images", Models: "paint", Group: "images", BaseURL: &baseURL, ModelMapping: &mapping}
			require.NoError(t, db.Create(&channel).Error)
			require.NoError(t, db.Create(&model.Ability{Group: "images", Model: "paint", ChannelId: channel.Id, Enabled: true}).Error)
			user := model.User{Id: 92, Username: "gemini-owner", Group: "default", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Quota: 1000000}
			require.NoError(t, db.Create(&user).Error)
			common.MemoryCacheEnabled = true
			model.InitChannelCache()
			input := gin.H{"model": "paint", "prompt": "A paper boat", "n": 1, "size": "16:9", "request_key": "image-job", "group": "private", "protocol": "forged", "channel_id": 999}
			if tc.protocol != service.ImageProtocolImages {
				reference, err := service.StoreMediaImage(user.Id, "", "reference", "reference.png", bytes.NewReader(imageData.Bytes()))
				require.NoError(t, err)
				input["references"] = []string{reference.AssetID}
				input["resolution"] = "2K"
			} else if tc.upstreamModel == "studio-v7" {
				input["size"] = "1024x1024"
			}
			body, err := common.Marshal(input)
			require.NoError(t, err)
			engine := gin.New()
			engine.Use(func(c *gin.Context) { c.Set("id", user.Id) })
			engine.POST("/generate", SubmitMultimediaImage)
			result := httptest.NewRecorder()
			engine.ServeHTTP(result, httptest.NewRequest("POST", "/generate", bytes.NewReader(body)))
			require.Equal(t, http.StatusAccepted, result.Code, result.Body.String())
			var job model.ImageGeneration
			require.NoError(t, db.First(&job).Error)
			var parameters service.MultimediaImageRequest
			require.NoError(t, common.UnmarshalJsonStr(job.Parameters, &parameters))
			assert.Equal(t, "images", parameters.Group)
			assert.Equal(t, tc.protocol, parameters.Protocol)
			assert.NotContains(t, job.Parameters, "channel_id")
			require.NoError(t, db.Model(&job).Update("status", "running").Error)
			runMultimediaImageJob(newMultimediaRelayEngine(), &job)
			require.NoError(t, db.First(&job, job.ID).Error)
			require.Equal(t, "succeeded", job.Status, job.Error)
			assert.Equal(t, int32(1), calls.Load())
			assets, total, err := model.ListMediaAssets(user.Id, 0, 18, "generated")
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, assets, 1)
			assert.Equal(t, job.GenerationID, assets[0].GenerationID)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 10000, user.UsedQuota)
			assert.Equal(t, 990000, user.Quota)
		})
	}
}

func TestMultimediaAssetOperationsAreAtomicAndOwnerScoped(t *testing.T) {
	db := setupMultimediaTest(t)
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	own, err := service.StoreMediaImage(101, "", "reference", "own.png", bytes.NewReader(data.Bytes()))
	require.NoError(t, err)
	foreign, err := service.StoreMediaImage(102, "", "reference", "private.png", bytes.NewReader(data.Bytes()))
	require.NoError(t, err)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 101) })
	engine.POST("/remove", RemoveMultimediaAssets)
	engine.POST("/download", DownloadMultimediaAssets)
	engine.GET("/assets/:id", GetMultimediaAssetContent)
	body, err := common.Marshal(gin.H{"ids": []string{own.AssetID, foreign.AssetID}})
	require.NoError(t, err)
	denied := httptest.NewRecorder()
	engine.ServeHTTP(denied, httptest.NewRequest("POST", "/remove", bytes.NewReader(body)))
	assert.Equal(t, 404, denied.Code)
	_, err = model.GetMediaAsset(101, own.AssetID)
	require.NoError(t, err)
	denied = httptest.NewRecorder()
	engine.ServeHTTP(denied, httptest.NewRequest("GET", "/assets/"+foreign.AssetID, nil))
	assert.Equal(t, 404, denied.Code)
	body, err = common.Marshal(gin.H{"ids": []string{own.AssetID}})
	require.NoError(t, err)
	downloaded := httptest.NewRecorder()
	engine.ServeHTTP(downloaded, httptest.NewRequest("POST", "/download", bytes.NewReader(body)))
	require.Equal(t, 200, downloaded.Code)
	archive, err := zip.NewReader(bytes.NewReader(downloaded.Body.Bytes()), int64(downloaded.Body.Len()))
	require.NoError(t, err)
	require.Len(t, archive.File, 1)
	entry, err := archive.File[0].Open()
	require.NoError(t, err)
	saved, err := io.ReadAll(entry)
	require.NoError(t, err)
	require.NoError(t, entry.Close())
	assert.Equal(t, data.Bytes(), saved)
	removed := httptest.NewRecorder()
	engine.ServeHTTP(removed, httptest.NewRequest("POST", "/remove", bytes.NewReader(body)))
	require.Equal(t, 200, removed.Code)
	_, err = model.GetMediaAsset(101, own.AssetID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	var row model.MediaAsset
	require.NoError(t, db.First(&row, own.ID).Error)
	assert.Positive(t, row.RemovedAt)
}

func TestMultimediaSubmissionLimitsActiveJobs(t *testing.T) {
	db := setupMultimediaTest(t)
	require.NoError(t, db.Create(&model.User{Id: 201, Username: "queue-user"}).Error)
	for _, key := range []string{"request-one", "request-two", "request-three"} {
		created, err := model.CreateImageGeneration(&model.ImageGeneration{UserID: 201, RequestKey: key})
		require.NoError(t, err)
		assert.True(t, created)
	}
	_, err := model.CreateImageGeneration(&model.ImageGeneration{UserID: 201, RequestKey: "request-four"})
	require.ErrorContains(t, err, "active")
	replay := model.ImageGeneration{UserID: 201, RequestKey: "request-one"}
	created, err := model.CreateImageGeneration(&replay)
	require.NoError(t, err)
	assert.False(t, created)
	assert.True(t, strings.HasPrefix(replay.GenerationID, "img_"))
}

func TestMultimediaAssetPaginationNeverAcceptsAnUnboundedLimit(t *testing.T) {
	db := setupMultimediaTest(t)
	for _, id := range []string{"first", "second"} {
		require.NoError(t, db.Create(&model.MediaAsset{AssetID: id, UserID: 101, Kind: "reference", ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)
	}
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 101) })
	engine.GET("/assets", GetMultimediaAssets)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest("GET", "/assets?page_size=-1&p=-2", nil))
	require.Equal(t, 200, response.Code)
	var result struct {
		Data struct {
			Items []model.MediaAsset `json:"items"`
			Total int                `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.Len(t, result.Data.Items, 1)
	assert.Equal(t, 2, result.Data.Total)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest("GET", "/assets?p=9223372036854775807&page_size=50", nil))
	assert.Equal(t, 400, response.Code)
}

func TestMultimediaRelayEditOmitsRetiredSettingsAndPreservesReferencesAndZeroCompression(t *testing.T) {
	setupMultimediaTest(t)
	var pngData bytes.Buffer
	require.NoError(t, png.Encode(&pngData, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	var references []string
	for _, name := range []string{"first.jpg", "second.png"} {
		asset, err := service.StoreMediaImage(301, "", "reference", name, bytes.NewReader(pngData.Bytes()))
		require.NoError(t, err)
		references = append(references, asset.AssetID)
	}
	// Old persisted jobs may still contain removed fields. They must not be sent upstream.
	parameters, err := common.Marshal(gin.H{"model": "gpt-image-2", "group": "default", "prompt": "Change the lighting", "n": 1, "size": "1024x1024", "quality": "high", "background": "transparent", "moderation": "low", "output_format": "webp", "output_compression": 0, "references": references, "mask": "legacy-mask", "request_key": "edit-request"})
	require.NoError(t, err)
	request, cleanup, err := buildMultimediaRelayRequest(context.Background(), &model.ImageGeneration{UserID: 301, Parameters: string(parameters), RequestIP: "::1"})
	require.NoError(t, err)
	defer cleanup()
	assert.Equal(t, "/v1/images/edits", request.URL.Path)
	assert.Equal(t, "[::1]:0", request.RemoteAddr)
	require.NoError(t, request.ParseMultipartForm(1<<20))
	defer request.MultipartForm.RemoveAll()
	assert.Equal(t, "0", request.PostForm.Get("output_compression"))
	assert.Equal(t, "webp", request.PostForm.Get("output_format"))
	assert.NotContains(t, request.PostForm, "background")
	assert.NotContains(t, request.PostForm, "moderation")
	assert.Equal(t, "high", request.PostForm.Get("quality"))
	require.Len(t, request.MultipartForm.File["image[]"], 2)
	assert.NotContains(t, request.MultipartForm.File, "mask")
	for _, file := range request.MultipartForm.File["image[]"] {
		assert.True(t, strings.HasSuffix(file.Filename, ".png"))
		source, err := file.Open()
		require.NoError(t, err)
		content, err := io.ReadAll(source)
		require.NoError(t, err)
		require.NoError(t, source.Close())
		assert.Equal(t, pngData.Bytes(), content)
	}
}

func TestMultimediaSuccessfulResponseWithoutUsableImagesRequiresBillingReview(t *testing.T) {
	db := setupMultimediaTest(t)
	parameters, err := common.Marshal(service.MultimediaImageRequest{Model: "gpt-image-2", Prompt: "A paper boat", N: common.GetPointer(uint(1))})
	require.NoError(t, err)
	job := model.ImageGeneration{GenerationID: "empty-response", UserID: 401, RequestKey: "empty-response", Parameters: string(parameters), Status: "running"}
	require.NoError(t, db.Create(&job).Error)
	engine := gin.New()
	engine.POST("/v1/images/generations", func(c *gin.Context) { c.JSON(200, gin.H{"data": []any{}}) })
	runMultimediaImageJob(engine, &job)
	require.NoError(t, db.First(&job, job.ID).Error)
	assert.Equal(t, "storage_failed", job.Status)
	assert.Contains(t, job.Error, "Check the consumption log before trying again")
}
