package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/google/uuid"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const MaxMediaImageBytes int64 = 32 << 20
const MaxMediaReferenceBytes int64 = 64 << 20

type ImageCapabilities struct {
	MaxReferences int      `json:"max_references"`
	MaxCount      int      `json:"max_count"`
	CustomSize    bool     `json:"custom_size"`
	Advanced      bool     `json:"advanced"`
	Sizes         []string `json:"sizes"`
	Qualities     []string `json:"qualities"`
	Resolutions   []string `json:"resolutions"`
	Protocol      string   `json:"-"`
}

const (
	ImageProtocolImages = "images"
	ImageProtocolGemini = "gemini"
	ImageProtocolChat   = "chat"
)

type MultimediaImageRequest struct {
	Model             string   `json:"model"`
	Group             string   `json:"group"`
	Protocol          string   `json:"protocol,omitempty"`
	Prompt            string   `json:"prompt"`
	N                 *uint    `json:"n,omitempty"`
	Size              string   `json:"size"`
	Quality           string   `json:"quality"`
	Resolution        string   `json:"resolution,omitempty"`
	OutputFormat      string   `json:"output_format,omitempty"`
	OutputCompression *int     `json:"output_compression,omitempty"`
	References        []string `json:"references"`
	RequestKey        string   `json:"request_key"`
}

func ImageCapabilitiesForModel(name string) (ImageCapabilities, bool) {
	name = strings.ToLower(name)
	if strings.Contains(name, "embedding") || strings.Contains(name, "video") || strings.Contains(name, "-t2v") || strings.Contains(name, "-i2v") {
		return ImageCapabilities{}, false
	}
	if (strings.Contains(name, "gemini") && (strings.Contains(name, "image") || strings.Contains(name, "banana"))) || strings.Contains(name, "nano-banana") {
		return GeminiImageCapabilities(name), true
	}
	if strings.HasPrefix(name, "gpt-image-") {
		return ImageCapabilities{
			Protocol: ImageProtocolImages, MaxReferences: 9, MaxCount: 10, Advanced: true,
			CustomSize: strings.HasPrefix(name, "gpt-image-2"),
			Sizes:      []string{"auto", "1024x1024", "1536x1024", "1024x1536"},
			Qualities:  []string{"auto", "low", "medium", "high"},
		}, true
	}
	if name == "dall-e-3" {
		return ImageCapabilities{Protocol: ImageProtocolImages, MaxCount: 1, Sizes: []string{"1024x1024", "1792x1024", "1024x1792"}, Qualities: []string{"standard", "hd"}}, true
	}
	if name == "dall-e-2" || name == "dall-e" {
		return ImageCapabilities{Protocol: ImageProtocolImages, MaxReferences: 1, MaxCount: 10, Sizes: []string{"256x256", "512x512", "1024x1024"}, Qualities: []string{"standard"}}, true
	}
	if strings.HasPrefix(name, "imagen-") {
		return ImageCapabilities{Protocol: ImageProtocolImages, MaxCount: 4, Sizes: []string{"1:1", "3:4", "4:3", "9:16", "16:9"}, Qualities: []string{"1K", "2K"}}, true
	}
	if common.IsImageGenerationModel(name) || strings.Contains(name, "image") || strings.Contains(name, "seedream") || strings.Contains(name, "stable-diffusion") || strings.Contains(name, "wanx") || strings.Contains(name, "kolors") || strings.Contains(name, "ideogram") || strings.Contains(name, "recraft") {
		return CompatibleImageCapabilities(), true
	}
	return ImageCapabilities{}, false
}

// CompatibleImageCapabilities supplies basic controls for image endpoints whose
// model names do not identify a provider-specific parameter contract.
func CompatibleImageCapabilities() ImageCapabilities {
	return ImageCapabilities{Protocol: ImageProtocolImages, MaxCount: 1, CustomSize: true, Sizes: []string{"auto", "1024x1024", "1536x1024", "1024x1536"}, Qualities: []string{}}
}

func GeminiImageCapabilities(name string) ImageCapabilities {
	capability := ImageCapabilities{
		Protocol: ImageProtocolGemini, MaxReferences: 9, MaxCount: 1,
		Sizes: []string{"auto", "1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "4:5", "5:4", "21:9"}, Qualities: []string{},
	}
	if !strings.Contains(name, "2.0") && !strings.Contains(name, "2.5") {
		capability.Resolutions = []string{"1K", "2K", "4K"}
	}
	return capability
}

func ValidateMultimediaImageRequest(request *MultimediaImageRequest, capability ImageCapabilities) error {
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Model == "" || request.Prompt == "" || len(request.Prompt) > 32000 {
		return errors.New("a model and a prompt of up to 32000 bytes are required")
	}
	if len(request.RequestKey) < 8 || len(request.RequestKey) > 64 {
		return errors.New("a request key between 8 and 64 characters is required")
	}
	if request.N == nil {
		request.N = common.GetPointer(uint(1))
	}
	if *request.N < 1 || *request.N > dto.MaxImageN || *request.N > uint(capability.MaxCount) {
		return fmt.Errorf("image count must be between 1 and %d", capability.MaxCount)
	}
	if len(request.References) > capability.MaxReferences {
		return fmt.Errorf("at most %d reference images are supported", capability.MaxReferences)
	}
	if request.Size == "" {
		request.Size = capability.Sizes[0]
	}
	if !slices.Contains(capability.Sizes, request.Size) {
		var width, height int
		parts := strings.Split(request.Size, "x")
		if len(parts) == 2 {
			width, _ = strconv.Atoi(parts[0])
			height, _ = strconv.Atoi(parts[1])
		}
		if !capability.CustomSize || width < 256 || height < 256 || width > 3840 || height > 3840 {
			return errors.New("unsupported image size; custom dimensions must be between 256 and 3840 pixels")
		}
	}
	if request.Quality == "" && len(capability.Qualities) > 0 {
		request.Quality = capability.Qualities[0]
	}
	if request.Quality != "" && !slices.Contains(capability.Qualities, request.Quality) {
		return errors.New("unsupported image quality")
	}
	if request.Resolution == "" && len(capability.Resolutions) > 0 {
		request.Resolution = capability.Resolutions[0]
	}
	if request.Resolution != "" && !slices.Contains(capability.Resolutions, request.Resolution) {
		return errors.New("unsupported image resolution")
	}
	if !capability.Advanced {
		if request.OutputFormat != "" || request.OutputCompression != nil {
			return errors.New("advanced image settings are not supported by this model")
		}
		return nil
	}
	if !slices.Contains([]string{"", "png", "jpeg", "webp"}, request.OutputFormat) {
		return errors.New("unsupported output format")
	}
	if request.OutputCompression != nil && (*request.OutputCompression < 0 || *request.OutputCompression > 100 || (request.OutputFormat != "jpeg" && request.OutputFormat != "webp")) {
		return errors.New("compression must be between 0 and 100 and requires JPEG or WebP")
	}
	return nil
}

func MediaStorageDirectory() string {
	if directory := os.Getenv("MULTIMEDIA_STORAGE_DIR"); directory != "" {
		return directory
	}
	return filepath.Join("data", "multimedia")
}

func MediaRetentionDays() int {
	days, err := strconv.Atoi(os.Getenv("MULTIMEDIA_RETENTION_DAYS"))
	if err != nil || days < 1 || days > 3650 {
		return 30
	}
	return days
}

func MediaAssetPath(asset *model.MediaAsset, thumbnail bool) (string, error) {
	if asset.StorageKey == "" || filepath.Base(asset.StorageKey) != asset.StorageKey {
		return "", errors.New("invalid media storage key")
	}
	key := asset.StorageKey
	if thumbnail {
		key += ".thumb.jpg"
	}
	return filepath.Join(MediaStorageDirectory(), key), nil
}

func StoreMediaImage(userID int, generationID, kind, name string, reader io.Reader) (*model.MediaAsset, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxMediaImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || int64(len(data)) > MaxMediaImageBytes {
		return nil, errors.New("image exceeds the 32 MB file limit or is empty")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !slices.Contains([]string{"png", "jpeg", "webp"}, format) {
		return nil, errors.New("only valid PNG, JPEG, and WebP images are supported")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 32*1024*1024 {
		return nil, errors.New("image dimensions exceed the supported limit")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("image could not be decoded")
	}
	if err := os.MkdirAll(MediaStorageDirectory(), 0700); err != nil {
		return nil, err
	}
	key := uuid.NewString() + "." + format
	path := filepath.Join(MediaStorageDirectory(), key)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	// Keep gallery requests small; original files are fetched for previews/downloads.
	width, height := config.Width, config.Height
	if width > 384 || height > 384 {
		scale := min(384.0/float64(width), 384.0/float64(height))
		width, height = max(1, int(float64(width)*scale)), max(1, int(float64(height)*scale))
	}
	thumb := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(thumb, thumb.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), decoded, decoded.Bounds(), draw.Over, nil)
	var thumbnail bytes.Buffer
	err = jpeg.Encode(&thumbnail, thumb, &jpeg.Options{Quality: 80})
	if err == nil {
		err = os.WriteFile(path+".thumb.jpg", thumbnail.Bytes(), 0600)
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	name = filepath.Base(name)
	if name == "" || name == "." {
		name = "image"
	}
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if len(name) > 180 {
		name = "image"
	}
	name += "." + format
	asset := &model.MediaAsset{
		AssetID: "media_" + uuid.NewString(), UserID: userID, GenerationID: generationID,
		Kind: kind, Name: filepath.Base(name), StorageKey: key, MimeType: "image/" + format,
		Width: config.Width, Height: config.Height, Bytes: int64(len(data)), CreatedAt: time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Duration(MediaRetentionDays()) * 24 * time.Hour).Unix(),
	}
	if err := model.DB.Create(asset).Error; err != nil {
		_ = os.Remove(path)
		_ = os.Remove(path + ".thumb.jpg")
		return nil, err
	}
	return asset, nil
}

func ValidateImageReferences(userID int, request *MultimediaImageRequest) error {
	var referenceBytes int64
	for _, id := range request.References {
		asset, err := model.GetMediaAsset(userID, id)
		if err != nil || asset.Kind == "mask" {
			return errors.New("a reference image is unavailable")
		}
		referenceBytes += asset.Bytes
	}
	if (request.Protocol == ImageProtocolGemini || request.Protocol == ImageProtocolChat) && referenceBytes > MaxMediaReferenceBytes {
		return errors.New("combined reference images exceed 64 MB")
	}
	return nil
}

func CleanupMediaAssets(ctx context.Context) error {
	var assets []model.MediaAsset
	err := model.DB.WithContext(ctx).Where("expires_at <= ? OR removed_at > 0", time.Now().Unix()).Limit(100).Find(&assets).Error
	if err != nil {
		return err
	}
	for _, asset := range assets {
		path, err := MediaAssetPath(&asset, false)
		if err != nil {
			return err
		}
		for _, file := range []string{path, path + ".thumb.jpg"} {
			if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := model.DB.WithContext(ctx).Delete(&asset).Error; err != nil {
			return err
		}
	}
	return nil
}
