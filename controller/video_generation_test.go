package controller

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/seedance"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVideoGenerationCatalogTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMemoryCache := common.MemoryCacheEnabled
	previousRedis := common.RedisEnabled
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousPrices := ratio_setting.ModelPrice2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousAutoGroups := setting.AutoGroups2JsonString()

	common.MemoryCacheEnabled = false
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"local-video":2.4}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1.5}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default"]`))

	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.MemoryCacheEnabled = previousMemoryCache
		common.RedisEnabled = previousRedis
		common.SetDatabaseTypes(previousMainType, previousLogType)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestBuildVideoGenerationCatalogUsesRoutingAliasesAndSitePrice(t *testing.T) {
	db := setupVideoGenerationCatalogTest(t)
	mapping := `{"local-video":"seedance-2.0-standard-i2v"}`
	channels := []*model.Channel{
		{
			Id: 7201, Type: constant.ChannelTypeSeedance, Key: "seedance-key", Status: common.ChannelStatusEnabled,
			Name: "video-channel", Models: "local-video,unpriced-video,context-helper", Group: "default", ModelMapping: &mapping,
		},
		{
			Id: 7202, Type: constant.ChannelTypeOpenAI, Key: "openai-key", Status: common.ChannelStatusEnabled,
			Name: "wrong-channel-type", Models: "wrong-channel-video", Group: "default",
		},
		{
			Id: 7203, Type: constant.ChannelTypeSeedance, Key: "disabled-key", Status: common.ChannelStatusManuallyDisabled,
			Name: "disabled-video-channel", Models: "disabled-video", Group: "default",
		},
	}
	for _, channel := range channels {
		require.NoError(t, db.Create(channel).Error)
	}
	abilities := []*model.Ability{
		{Group: "default", Model: "local-video", ChannelId: 7201, Enabled: true},
		{Group: "default", Model: "unpriced-video", ChannelId: 7201, Enabled: true},
		{Group: "default", Model: "context-helper", ChannelId: 7201, Enabled: true},
		{Group: "default", Model: "wrong-channel-video", ChannelId: 7202, Enabled: true},
		{Group: "default", Model: "disabled-video", ChannelId: 7203, Enabled: true},
	}
	for _, ability := range abilities {
		require.NoError(t, db.Create(ability).Error)
	}

	channelMapping := `{
		"local-video":"seedance-2.0-standard-i2v",
		"unpriced-video":"seedance-2.5-standard-t2v",
		"context-helper":"minmax-h3-context-ir-text"
	}`
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 7201).Update("model_mapping", channelMapping).Error)
	catalog, err := buildVideoGenerationCatalog(
		&model.UserBase{Id: 81, Group: "default", Status: common.UserStatusEnabled},
		"default",
		[]string{"default"},
	)

	require.NoError(t, err)
	require.NotNil(t, catalog)
	require.Len(t, catalog.Models, 2)
	byID := make(map[string]videoGenerationModel, len(catalog.Models))
	for _, item := range catalog.Models {
		byID[item.ID] = item
	}
	priced, ok := byID["local-video"]
	require.True(t, ok)
	assert.Equal(t, "seedance-2.0", priced.Family)
	require.Len(t, priced.Capabilities.Media, 1)
	assert.Equal(t, "image", priced.Capabilities.Media[0].Type)
	assert.True(t, priced.PriceAvailable)
	require.NotNil(t, priced.PriceUSD)
	require.NotNil(t, priced.EstimatedPriceUSD)
	assert.InDelta(t, 2.4, *priced.PriceUSD, 0.000001)
	assert.InDelta(t, 1.5, priced.GroupRatio, 0.000001)
	assert.InDelta(t, 3.6, *priced.EstimatedPriceUSD, 0.000001)

	unpriced, ok := byID["unpriced-video"]
	require.True(t, ok)
	assert.False(t, unpriced.PriceAvailable)
	assert.Nil(t, unpriced.PriceUSD)
	assert.Nil(t, unpriced.EstimatedPriceUSD)
	assert.NotContains(t, byID, "context-helper")
	assert.NotContains(t, byID, "wrong-channel-video")
	assert.NotContains(t, byID, "disabled-video")
	assert.Equal(t, "/pg/files/upload", catalog.Upload.Path)
	assert.Equal(t, seedance.MaxUploadBytes, catalog.Upload.MaxBytes)
	assert.Equal(t, []string{"default", "auto"}, catalog.Groups)
}

func TestVideoGenerationGroupsRejectsUnavailableGroup(t *testing.T) {
	setupVideoGenerationCatalogTest(t)

	groups, err := videoGenerationGroups("default", "default")
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, groups)

	_, err = videoGenerationGroups("default", "vip")
	require.ErrorContains(t, err, "not available")
}

func TestNormalizeDashboardVideoGenerationRequest(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		mapping      string
		wantProtocol seedance.Protocol
		wantError    string
	}{
		{
			name:         "ordinary video alias",
			body:         `{"model":"local-video","group":"vip","prompt":"test"}`,
			mapping:      `{"local-video":"seedance-2.0-fast-t2v"}`,
			wantProtocol: seedance.ProtocolVideos,
		},
		{
			name:         "midjourney alias",
			body:         `{"model":"local-midjourney","group":"default","image_urls":["https://cdn.example/start.png"]}`,
			mapping:      `{"local-midjourney":"midjourney-video"}`,
			wantProtocol: seedance.ProtocolMidjourneyVideo,
		},
		{
			name:      "context helper is not video output",
			body:      `{"model":"context-helper","group":"default","prompt":"test"}`,
			mapping:   `{"context-helper":"minmax-h3-context-ir-text"}`,
			wantError: "not a supported video-output model",
		},
		{name: "model must be string", body: `{"model":123}`, wantError: "model must be a string"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, capability, err := normalizeDashboardVideoGenerationRequest([]byte(test.body), test.mapping)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				assert.Nil(t, normalized)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantProtocol, capability.Protocol)
			var body map[string]any
			require.NoError(t, common.Unmarshal(normalized, &body))
			assert.NotContains(t, body, "group")
			assert.NotEmpty(t, body["model"])
		})
	}
}

func TestTaskActionsQueryParsesVideoHistoryActions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(
		"GET",
		"/api/task/self?actions=seedance_video,seedance_midjourney_video,seedance_video",
		nil,
	)

	assert.Equal(t, []string{"seedance_video", "seedance_midjourney_video"}, taskActionsQuery(context))
}
