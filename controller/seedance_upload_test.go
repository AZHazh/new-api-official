package controller

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSeedanceUploadSelectionTest(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.MemoryCacheEnabled = true
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		if previousMemoryCacheEnabled && previousDB != nil {
			model.InitChannelCache()
		}
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func seedanceISOBaseMediaHeader(majorBrand string, compatibleBrands ...string) []byte {
	boxSize := 16 + 4*len(compatibleBrands)
	header := make([]byte, boxSize)
	binary.BigEndian.PutUint32(header[:4], uint32(boxSize))
	copy(header[4:8], "ftyp")
	copy(header[8:12], majorBrand)
	for index, brand := range compatibleBrands {
		copy(header[16+index*4:20+index*4], brand)
	}
	return header
}

func TestSeedanceUploadContentMatchesSupportedFormats(t *testing.T) {
	flacHeader := make([]byte, 42)
	copy(flacHeader, "fLaC")
	flacHeader[4] = 0x80
	flacHeader[7] = 34

	tests := []struct {
		name        string
		extension   string
		contentType string
		header      []byte
	}{
		{name: "JPEG", extension: ".jpg", contentType: "image/jpeg", header: []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}},
		{name: "PNG", extension: ".png", contentType: "image/png", header: []byte("\x89PNG\r\n\x1a\n\x00")},
		{name: "WebP", extension: ".webp", contentType: "image/webp", header: []byte("RIFF\x10\x00\x00\x00WEBPVP8X")},
		{name: "MP3 with ID3", extension: ".mp3", contentType: "audio/mpeg", header: []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}},
		{name: "MP3 frame", extension: ".mp3", contentType: "application/octet-stream", header: []byte{0xff, 0xfb, 0x90, 0x64, 0, 0, 0, 0}},
		{name: "WAV", extension: ".wav", contentType: "audio/x-wav", header: []byte("RIFF\x10\x00\x00\x00WAVEfmt ")},
		{name: "FLAC", extension: ".flac", contentType: "audio/flac", header: flacHeader},
		{name: "MP4", extension: ".mp4", contentType: "video/mp4", header: seedanceISOBaseMediaHeader("isom", "mp41", "isom")},
		{name: "AVI", extension: ".avi", contentType: "video/x-msvideo", header: []byte("RIFF\x10\x00\x00\x00AVI LIST")},
		{name: "QuickTime MOV", extension: ".mov", contentType: "video/quicktime", header: seedanceISOBaseMediaHeader("qt  ", "qt  ")},
		{name: "Matroska", extension: ".mkv", contentType: "video/x-matroska", header: append([]byte{0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42, 0x82, 0x88}, []byte("matroska")...)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.True(t, seedanceUploadContentMatches(test.extension, test.contentType, test.header))
		})
	}
}

func TestSeedanceUploadContentRejectsSpoofedOrMismatchedFormats(t *testing.T) {
	tests := []struct {
		name        string
		extension   string
		contentType string
		header      []byte
	}{
		{name: "unsupported extension", extension: ".exe", contentType: "application/octet-stream", header: []byte("MZ")},
		{name: "declared MIME mismatch", extension: ".jpg", contentType: "text/plain", header: []byte{0xff, 0xd8, 0xff, 0xe0}},
		{name: "malformed MIME", extension: ".jpg", contentType: "image/jpeg; bad", header: []byte{0xff, 0xd8, 0xff, 0xe0}},
		{name: "renamed PNG", extension: ".jpg", contentType: "application/octet-stream", header: []byte("\x89PNG\r\n\x1a\n")},
		{name: "fake FLAC", extension: ".flac", contentType: "application/octet-stream", header: []byte{0, 1, 2, 3, 4}},
		{name: "WAV renamed AVI", extension: ".avi", contentType: "application/octet-stream", header: []byte("RIFF\x10\x00\x00\x00WAVEfmt ")},
		{name: "WebM renamed MKV", extension: ".mkv", contentType: "application/octet-stream", header: append([]byte{0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42, 0x82, 0x84}, []byte("webm")...)},
		{name: "invalid ISO media box", extension: ".mp4", contentType: "video/mp4", header: []byte("\x00\x00\x00\x0cftypmp41")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.False(t, seedanceUploadContentMatches(test.extension, test.contentType, test.header))
		})
	}
}

func TestSeedanceMultipartFileReaderRequiresExactlyOnePart(t *testing.T) {
	tests := []struct {
		name      string
		extraPart bool
		wantError bool
	}{
		{name: "single file"},
		{name: "additional field", extraPart: true, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			filePart, err := writer.CreateFormFile("file", "clip.mp4")
			require.NoError(t, err)
			_, err = filePart.Write([]byte("video-data"))
			require.NoError(t, err)
			if test.extraPart {
				require.NoError(t, writer.WriteField("unexpected", "value"))
			}
			require.NoError(t, writer.Close())

			multipartReader := multipart.NewReader(bytes.NewReader(body.Bytes()), writer.Boundary())
			incomingFile, err := multipartReader.NextPart()
			require.NoError(t, err)
			reader := &seedanceMultipartFileReader{file: incomingFile, multipart: multipartReader}
			content, err := io.ReadAll(reader)

			assert.Equal(t, []byte("video-data"), content)
			if test.wantError {
				require.ErrorIs(t, err, errSeedanceUploadAdditionalPart)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestSelectSeedanceUploadChannelSupportsMappedLocalModels(t *testing.T) {
	tests := []struct {
		name         string
		modelMapping string
	}{
		{
			name:         "local alias",
			modelMapping: `{"local-video":"seedance-2.0-mini-t2v"}`,
		},
		{
			name:         "chained local alias",
			modelMapping: `{"local-video":"team-video","team-video":"seedance-2.0-mini-t2v"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupSeedanceUploadSelectionTest(t)
			channel := &model.Channel{
				Id:           6101,
				Type:         constant.ChannelTypeSeedance,
				Key:          "seedance-test-key",
				Status:       common.ChannelStatusEnabled,
				Name:         "seedance-upload-selection",
				Models:       "local-video",
				Group:        "default",
				ModelMapping: &test.modelMapping,
			}
			require.NoError(t, db.Create(channel).Error)
			require.NoError(t, db.Create(&model.Ability{
				Group:     "default",
				Model:     "local-video",
				ChannelId: channel.Id,
				Enabled:   true,
			}).Error)
			model.InitChannelCache()

			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			common.SetContextKey(context, constant.ContextKeyUsingGroup, "default")

			selected, selectedModel, err := selectSeedanceUploadChannel(context)

			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, channel.Id, selected.Id)
			assert.Equal(t, "local-video", selectedModel)
		})
	}
}
