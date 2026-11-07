package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMultimediaRequestValidationBoundsBillingQuantities(t *testing.T) {
	capability, ok := ImageCapabilitiesForModel("gpt-image-2")
	require.True(t, ok)
	cases := []struct {
		name        string
		count       uint
		compression *int
		format      string
		valid       bool
	}{
		{"one image", 1, nil, "png", true},
		{"zero count", 0, nil, "png", false},
		{"model maximum", 10, nil, "png", true},
		{"above model maximum", 11, nil, "png", false},
		{"wrapped unsigned count", ^uint(0), nil, "png", false},
		{"explicit zero compression", 1, common.GetPointer(0), "webp", true},
		{"negative compression", 1, common.GetPointer(-1), "jpeg", false},
		{"compression above maximum", 1, common.GetPointer(101), "jpeg", false},
		{"PNG compression", 1, common.GetPointer(50), "png", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := MultimediaImageRequest{Model: "gpt-image-2", Prompt: "A small red house", RequestKey: "request-123", N: &tc.count, OutputCompression: tc.compression, OutputFormat: tc.format}
			err := ValidateMultimediaImageRequest(&request, capability)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			encoded, err := common.Marshal(request)
			require.NoError(t, err)
			if tc.compression != nil {
				assert.Contains(t, string(encoded), `"output_compression":0`)
			}
		})
	}
}

func TestMultimediaRequestRejectsUnsupportedSettings(t *testing.T) {
	capability, _ := ImageCapabilitiesForModel("dall-e-3")
	request := MultimediaImageRequest{Model: "dall-e-3", Prompt: "A small red house", RequestKey: "request-123", OutputFormat: "webp"}
	require.ErrorContains(t, ValidateMultimediaImageRequest(&request, capability), "not supported")
	capability, _ = ImageCapabilitiesForModel("gpt-image-2")
	request = MultimediaImageRequest{Model: "gpt-image-2", Prompt: "A small red house", RequestKey: "request-123", Size: "3841x1024"}
	require.ErrorContains(t, ValidateMultimediaImageRequest(&request, capability), "size")
	request.Size = "1024x1024"
	request.OutputFormat = "gif"
	require.ErrorContains(t, ValidateMultimediaImageRequest(&request, capability), "output format")
}

func TestMultimediaGeminiImageSettingsFollowNativeCapabilities(t *testing.T) {
	capability, ok := ImageCapabilitiesForModel("gemini-3.1-flash-image-preview")
	require.True(t, ok)
	request := MultimediaImageRequest{Model: "gemini-3.1-flash-image-preview", Prompt: "A paper boat", RequestKey: "gemini-request", Size: "16:9", Resolution: "2K", N: common.GetPointer(uint(1)), References: []string{"reference"}}
	require.NoError(t, ValidateMultimediaImageRequest(&request, capability))
	for _, tc := range []struct {
		name   string
		change func(*MultimediaImageRequest)
	}{
		{"multiple outputs", func(r *MultimediaImageRequest) { r.N = common.GetPointer(uint(2)) }},
		{"unsupported resolution", func(r *MultimediaImageRequest) { r.Resolution = "8K" }},
		{"pixel size", func(r *MultimediaImageRequest) { r.Size = "1024x1024" }},
		{"OpenAI quality", func(r *MultimediaImageRequest) { r.Quality = "high" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := request
			tc.change(&invalid)
			require.Error(t, ValidateMultimediaImageRequest(&invalid, capability))
		})
	}
}

func TestMultimediaStorageProtectsOwnershipAndExpiry(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, e := db.DB()
		if e == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.MediaAsset{}))
	t.Setenv("MULTIMEDIA_STORAGE_DIR", t.TempDir())
	t.Setenv("MULTIMEDIA_RETENTION_DAYS", "30")
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 3, 2))))
	asset, err := StoreMediaImage(41, "", "reference", "wrong.jpg", bytes.NewReader(data.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, "wrong.png", asset.Name)
	assert.Equal(t, "image/png", asset.MimeType)
	path, err := MediaAssetPath(asset, false)
	require.NoError(t, err)
	_, err = os.Stat(path)
	require.NoError(t, err)
	_, err = model.GetMediaAsset(42, asset.AssetID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	request := MultimediaImageRequest{References: []string{asset.AssetID}}
	require.NoError(t, ValidateImageReferences(41, &request))
	require.Error(t, ValidateImageReferences(42, &request))
	require.NoError(t, db.Model(asset).Update("expires_at", time.Now().Unix()-1).Error)
	_, err = model.GetMediaAsset(41, asset.AssetID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NoError(t, CleanupMediaAssets(context.Background()))
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	var count int64
	require.NoError(t, db.Model(&model.MediaAsset{}).Where("asset_id = ?", asset.AssetID).Count(&count).Error)
	assert.Zero(t, count)
}
