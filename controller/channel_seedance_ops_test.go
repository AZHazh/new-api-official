package controller

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchSeedanceModelsFiltersToRecognizedVideoCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "Bearer seedance-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"seedream-v5-pro-t2i"},{"id":"seedance-2.0-standard-t2v"},{"id":"midjourney-video"},{"id":"minmax-h3-context-ir-text"},{"id":"future-video-model"}]}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Type:    constant.ChannelTypeSeedance,
		Key:     "seedance-key",
		BaseURL: common.GetPointer(server.URL),
	}
	models, err := fetchChannelUpstreamModelIDs(channel)

	require.NoError(t, err)
	assert.Equal(t, []string{"seedance-2.0-standard-t2v", "midjourney-video", "minmax-h3-context-ir-text"}, models)

	models, discovery, err := fetchChannelUpstreamModelsForAdmin(channel)
	require.NoError(t, err)
	assert.Equal(t, []string{"seedance-2.0-standard-t2v", "midjourney-video", "minmax-h3-context-ir-text"}, models)
	require.Len(t, discovery, 5)
	assert.Equal(t, "non_video", string(discovery[0].Category))
	assert.False(t, discovery[0].Selectable)
	assert.Equal(t, "video_output", string(discovery[1].Category))
	assert.True(t, discovery[1].Selectable)
	assert.Equal(t, "midjourney_video", string(discovery[2].Category))
	assert.Equal(t, "video_prompt_enhancer", string(discovery[3].Category))
	assert.Equal(t, "unknown", string(discovery[4].Category))
	assert.False(t, discovery[4].Selectable)
}

func TestSeedanceChannelTestUsesReadOnlyModelsEndpoint(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "Bearer seedance-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"seedance-2.0-standard-t2v"}]}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Type:    constant.ChannelTypeSeedance,
		Key:     "seedance-key",
		BaseURL: common.GetPointer(server.URL),
	}
	result := testChannel(context.Background(), channel, 0, "", "", false)

	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	assert.Equal(t, 1, requestCount)
}

func TestSeedanceChannelTestRequiresRecognizedVideoOutputModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer seedance-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"minmax-h3-context-ir-text"},{"id":"gpt-4o"}]}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Type:    constant.ChannelTypeSeedance,
		Key:     "seedance-key",
		BaseURL: common.GetPointer(server.URL),
	}
	result := testChannel(context.Background(), channel, 0, "", "", false)

	require.ErrorContains(t, result.localErr, "no recognized video models")
	require.NotNil(t, result.newAPIError)
}

func TestSeedanceChannelTestRejectsInvalidBearerKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer invalid-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	channel := &model.Channel{
		Type:    constant.ChannelTypeSeedance,
		Key:     "invalid-key",
		BaseURL: common.GetPointer(server.URL),
	}
	result := testChannel(context.Background(), channel, 0, "", "", false)

	require.ErrorContains(t, result.localErr, "status code: 401")
	require.NotNil(t, result.newAPIError)
}

func TestValidateSeedanceChannelRequiresExactlyOneKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		channelInfo model.ChannelInfo
		wantError   string
	}{
		{name: "single key", key: "seedance-key"},
		{name: "blank key", key: "   ", wantError: "cannot be blank"},
		{name: "line feed separated keys", key: "first-key\nsecond-key", wantError: "exactly one API key"},
		{name: "carriage return separated keys", key: "first-key\rsecond-key", wantError: "exactly one API key"},
		{name: "JSON key array", key: `["first-key","second-key"]`, wantError: "exactly one API key"},
		{name: "multi key flag", key: "seedance-key", channelInfo: model.ChannelInfo{IsMultiKey: true}, wantError: "does not support multi-key"},
		{name: "multi key size", key: "seedance-key", channelInfo: model.ChannelInfo{MultiKeySize: 2}, wantError: "does not support multi-key"},
		{name: "multi key strategy", key: "seedance-key", channelInfo: model.ChannelInfo{MultiKeyMode: constant.MultiKeyModeRandom}, wantError: "does not support multi-key"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateChannel(&model.Channel{
				Type:        constant.ChannelTypeSeedance,
				Key:         test.key,
				ChannelInfo: test.channelInfo,
			}, true)

			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestAddSeedanceChannelRejectsNonSingleCreationModes(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		multiKeyMode constant.MultiKeyMode
		key          string
	}{
		{name: "batch even with one key", mode: "batch", key: "seedance-key"},
		{name: "multi key channel", mode: "multi_to_single", multiKeyMode: constant.MultiKeyModeRandom, key: "first-key\nsecond-key"},
		{name: "single mode with multi key strategy", mode: "single", multiKeyMode: constant.MultiKeyModePolling, key: "seedance-key"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupModelListControllerTestDB(t)
			body, err := common.Marshal(AddChannelRequest{
				Mode:         test.mode,
				MultiKeyMode: test.multiKeyMode,
				Channel: &model.Channel{
					Type:   constant.ChannelTypeSeedance,
					Key:    test.key,
					Name:   "Seedance",
					Models: "seedance-2.0-standard-t2v",
					Group:  "default",
				},
			})
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/", bytes.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			AddChannel(ctx)

			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Contains(t, response.Message, "single-key")
			var count int64
			require.NoError(t, db.Model(&model.Channel{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestUpdateChannelRejectsConvertingMultiKeyChannelToSeedance(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	channel := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "first-key\nsecond-key",
		Name:   "Multi-key OpenAI",
		Models: "gpt-4o",
		Group:  "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModeRandom,
		},
	}
	require.NoError(t, db.Create(channel).Error)
	body, err := common.Marshal(map[string]any{
		"id":   channel.Id,
		"type": constant.ChannelTypeSeedance,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", bytes.NewReader(body))
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)

	UpdateChannel(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "does not support multi-key")
	persisted, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, constant.ChannelTypeOpenAI, persisted.Type)
	assert.True(t, persisted.ChannelInfo.IsMultiKey)
}

func TestUpdateSeedanceChannelRejectsMultiKeyFieldsAndKeys(t *testing.T) {
	tests := []struct {
		name   string
		change map[string]any
	}{
		{name: "top-level multi key mode", change: map[string]any{"multi_key_mode": string(constant.MultiKeyModePolling)}},
		{name: "nested channel info", change: map[string]any{"channel_info": map[string]any{"is_multi_key": true, "multi_key_mode": string(constant.MultiKeyModeRandom)}}},
		{name: "line-separated key", change: map[string]any{"key": "first-key\nsecond-key"}},
		{name: "zero type cannot bypass key validation", change: map[string]any{"type": 0, "key": "first-key\nsecond-key"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupModelListControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.Task{}))
			channel := &model.Channel{
				Type:   constant.ChannelTypeSeedance,
				Key:    "seedance-key",
				Name:   "Seedance",
				Models: "seedance-2.0-standard-t2v",
				Group:  "default",
			}
			require.NoError(t, db.Create(channel).Error)
			request := map[string]any{"id": channel.Id}
			for field, value := range test.change {
				request[field] = value
			}
			body, err := common.Marshal(request)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", bytes.NewReader(body))
			ctx.Set("id", 1)
			ctx.Set("role", common.RoleRootUser)

			UpdateChannel(ctx)

			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			persisted, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.Equal(t, "seedance-key", persisted.Key)
			assert.False(t, persisted.ChannelInfo.IsMultiKey)
			assert.Empty(t, persisted.ChannelInfo.MultiKeyMode)
		})
	}
}

func createSeedanceChannelWithTask(t *testing.T, taskStatus model.TaskStatus) (*model.Channel, *model.Task) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}))

	channel := &model.Channel{
		Type:    constant.ChannelTypeSeedance,
		Key:     "old-key",
		Name:    "Seedance",
		Models:  "seedance-2.0-standard-t2v",
		Group:   "default",
		Status:  common.ChannelStatusEnabled,
		BaseURL: common.GetPointer("https://api.seedance.nz"),
	}
	require.NoError(t, db.Create(channel).Error)

	task := &model.Task{
		TaskID:    "local-task",
		Platform:  constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSeedance)),
		ChannelId: channel.Id,
		Status:    taskStatus,
	}
	require.NoError(t, db.Create(task).Error)
	return channel, task
}

func TestUpdateSeedanceChannelBlocksConnectionChangesWithUnfinishedTask(t *testing.T) {
	tests := []struct {
		name   string
		change map[string]any
	}{
		{name: "key", change: map[string]any{"key": "new-key"}},
		{name: "base URL", change: map[string]any{"base_url": "https://seedance.example"}},
		{name: "type", change: map[string]any{"type": constant.ChannelTypeOpenAI}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel, _ := createSeedanceChannelWithTask(t, model.TaskStatusQueued)
			request := map[string]any{"id": channel.Id}
			for name, value := range test.change {
				request[name] = value
			}
			body, err := common.Marshal(request)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", bytes.NewReader(body))
			ctx.Set("id", 1)
			ctx.Set("role", common.RoleRootUser)

			UpdateChannel(ctx)

			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Contains(t, response.Message, model.ErrSeedanceChannelHasUnfinishedTasks.Error())

			persisted, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.Equal(t, constant.ChannelTypeSeedance, persisted.Type)
			assert.Equal(t, "old-key", persisted.Key)
			assert.Equal(t, "https://api.seedance.nz", persisted.GetBaseURL())
		})
	}
}

func TestUpdateSeedanceChannelAllowsMetadataChangeWithUnfinishedTask(t *testing.T) {
	channel, _ := createSeedanceChannelWithTask(t, model.TaskStatusInProgress)
	body, err := common.Marshal(map[string]any{
		"id":   channel.Id,
		"name": "renamed Seedance",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", bytes.NewReader(body))
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)

	UpdateChannel(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	persisted, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "renamed Seedance", persisted.Name)
}

func TestUpdateSeedanceChannelForceClosesUnfinishedTask(t *testing.T) {
	channel, task := createSeedanceChannelWithTask(t, model.TaskStatusQueued)
	body, err := common.Marshal(map[string]any{
		"id":    channel.Id,
		"key":   "new-key",
		"force": true,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", bytes.NewReader(body))
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)

	UpdateChannel(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	persisted, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "new-key", persisted.Key)
	var storedTask model.Task
	require.NoError(t, model.DB.Where("id = ?", task.ID).First(&storedTask).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), storedTask.Status)
	assert.Equal(t, model.TaskBillingStatusRefunded, storedTask.BillingStatus)
}

func TestSeedanceChannelDeleteWaitsForTerminalTasks(t *testing.T) {
	channel, task := createSeedanceChannelWithTask(t, model.TaskStatusSubmitted)

	err := (&model.Channel{Id: channel.Id}).Delete()
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrSeedanceChannelHasUnfinishedTasks))
	_, err = model.GetChannelById(channel.Id, true)
	require.NoError(t, err)

	require.NoError(t, model.DB.Model(task).Update("status", model.TaskStatusSuccess).Error)
	require.NoError(t, (&model.Channel{Id: channel.Id}).Delete())
	_, err = model.GetChannelById(channel.Id, true)
	require.Error(t, err)
}

func TestSeedanceChannelDeleteWaitsForPendingFailureRefund(t *testing.T) {
	channel, task := createSeedanceChannelWithTask(t, model.TaskStatusFailure)
	require.NoError(t, model.DB.Model(task).Update("billing_status", model.TaskBillingStatusRefundPending).Error)

	err := (&model.Channel{Id: channel.Id}).Delete()
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrSeedanceChannelHasUnfinishedTasks))
	require.NoError(t, model.DB.Model(task).Update("billing_status", model.TaskBillingStatusRefunded).Error)
	require.NoError(t, (&model.Channel{Id: channel.Id}).Delete())
}

func TestDeleteSeedanceChannelForceClosesUnfinishedTask(t *testing.T) {
	channel, task := createSeedanceChannelWithTask(t, model.TaskStatusInProgress)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/1?force=true", nil)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)

	DeleteChannel(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	_, err := model.GetChannelById(channel.Id, true)
	require.Error(t, err)
	var storedTask model.Task
	require.NoError(t, model.DB.Where("id = ?", task.ID).First(&storedTask).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), storedTask.Status)
	assert.Equal(t, model.TaskBillingStatusRefunded, storedTask.BillingStatus)
}

func TestBatchDeleteChannelsIsAtomicWhenSeedanceTaskIsUnfinished(t *testing.T) {
	seedanceChannel, _ := createSeedanceChannelWithTask(t, model.TaskStatusSubmitUnknown)
	otherChannel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "key", Name: "OpenAI"}
	require.NoError(t, model.DB.Create(otherChannel).Error)

	deleted, err := model.BatchDeleteChannels([]int{otherChannel.Id, seedanceChannel.Id})

	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrSeedanceChannelHasUnfinishedTasks))
	assert.Zero(t, deleted)
	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).
		Where("id IN ?", []int{otherChannel.Id, seedanceChannel.Id}).
		Count(&count).Error)
	assert.Equal(t, int64(2), count)
}
