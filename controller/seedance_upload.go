package controller

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/seedance"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

const seedanceMultipartOverheadBytes int64 = 1 << 20

var (
	errSeedanceUploadAdditionalPart   = errors.New("multipart body must contain exactly one file field named file")
	errSeedanceUploadMalformedPayload = errors.New("invalid multipart upload body")
)

type seedanceMultipartFileReader struct {
	file        io.Reader
	multipart   *multipart.Reader
	terminalErr error
}

func (reader *seedanceMultipartFileReader) Read(buffer []byte) (int, error) {
	if reader.terminalErr != nil {
		return 0, reader.terminalErr
	}

	read, err := reader.file.Read(buffer)
	if !errors.Is(err, io.EOF) {
		return read, err
	}

	nextPart, nextErr := reader.multipart.NextPart()
	switch {
	case errors.Is(nextErr, io.EOF):
		reader.terminalErr = io.EOF
	case nextErr != nil:
		reader.terminalErr = fmt.Errorf("%w: %v", errSeedanceUploadMalformedPayload, nextErr)
	default:
		_ = nextPart.Close()
		reader.terminalErr = errSeedanceUploadAdditionalPart
	}
	if read > 0 {
		return read, nil
	}
	return 0, reader.terminalErr
}

var seedanceUploadContentTypes = map[string]map[string]struct{}{
	".jpg":  {"image/jpeg": {}, "image/jpg": {}, "image/pjpeg": {}, "application/octet-stream": {}},
	".jpeg": {"image/jpeg": {}, "image/jpg": {}, "image/pjpeg": {}, "application/octet-stream": {}},
	".png":  {"image/png": {}, "image/x-png": {}, "application/octet-stream": {}},
	".webp": {"image/webp": {}, "application/octet-stream": {}},
	".mp3":  {"audio/mpeg": {}, "audio/mp3": {}, "audio/x-mpeg": {}, "audio/mpeg3": {}, "audio/x-mpeg-3": {}, "application/octet-stream": {}},
	".wav":  {"audio/wave": {}, "audio/wav": {}, "audio/x-wav": {}, "application/octet-stream": {}},
	".flac": {"audio/flac": {}, "audio/x-flac": {}, "application/x-flac": {}, "application/octet-stream": {}},
	".mp4":  {"video/mp4": {}, "video/x-m4v": {}, "application/octet-stream": {}},
	".avi":  {"video/x-msvideo": {}, "video/avi": {}, "video/msvideo": {}, "application/x-troff-msvideo": {}, "application/octet-stream": {}},
	".mov":  {"video/quicktime": {}, "video/x-quicktime": {}, "video/mp4": {}, "application/octet-stream": {}},
	".mkv":  {"video/x-matroska": {}, "video/matroska": {}, "video/webm": {}, "application/x-matroska": {}, "application/octet-stream": {}},
}

func seedanceUploadHasMatroskaDocType(header []byte) bool {
	if !bytes.HasPrefix(header, []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		return false
	}

	for offset := 4; offset+3 <= len(header); offset++ {
		if header[offset] != 0x42 || header[offset+1] != 0x82 {
			continue
		}
		firstSizeByte := header[offset+2]
		width := 1
		marker := byte(0x80)
		for width <= 8 && firstSizeByte&marker == 0 {
			width++
			marker >>= 1
		}
		if width > 8 || offset+2+width > len(header) {
			continue
		}

		size := uint64(firstSizeByte & (marker - 1))
		for index := 1; index < width; index++ {
			size = size<<8 | uint64(header[offset+2+index])
		}
		valueOffset := offset + 2 + width
		if size > uint64(len(header)-valueOffset) {
			continue
		}
		if bytes.Equal(header[valueOffset:valueOffset+int(size)], []byte("matroska")) {
			return true
		}
	}
	return false
}

func seedanceUploadContentMatches(extension, declaredContentType string, header []byte) bool {
	allowedTypes, ok := seedanceUploadContentTypes[extension]
	if !ok {
		return false
	}

	if declaredContentType != "" {
		mediaType, _, err := mime.ParseMediaType(declaredContentType)
		if err != nil {
			return false
		}
		if _, ok := allowedTypes[strings.ToLower(mediaType)]; !ok {
			return false
		}
	}

	detectedType, _, err := mime.ParseMediaType(http.DetectContentType(header))
	if err != nil {
		return false
	}
	if _, ok := allowedTypes[strings.ToLower(detectedType)]; !ok {
		return false
	}

	switch extension {
	case ".jpg", ".jpeg":
		return bytes.HasPrefix(header, []byte{0xff, 0xd8, 0xff})
	case ".png":
		return bytes.HasPrefix(header, []byte("\x89PNG\r\n\x1a\n"))
	case ".webp":
		return len(header) >= 16 && bytes.Equal(header[:4], []byte("RIFF")) &&
			bytes.Equal(header[8:12], []byte("WEBP")) &&
			(bytes.Equal(header[12:16], []byte("VP8 ")) || bytes.Equal(header[12:16], []byte("VP8L")) || bytes.Equal(header[12:16], []byte("VP8X")))
	case ".mp3":
		if len(header) >= 10 && bytes.Equal(header[:3], []byte("ID3")) && header[3] >= 2 && header[3] <= 4 && header[4] != 0xff {
			return header[6]&0x80 == 0 && header[7]&0x80 == 0 && header[8]&0x80 == 0 && header[9]&0x80 == 0
		}
		return len(header) >= 4 && header[0] == 0xff && header[1]&0xe0 == 0xe0 && header[1]&0x18 != 0x08 && header[1]&0x06 != 0 &&
			header[2]&0xf0 != 0 && header[2]&0xf0 != 0xf0 && header[2]&0x0c != 0x0c
	case ".wav":
		return len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WAVE"))
	case ".flac":
		if len(header) < 42 || !bytes.Equal(header[:4], []byte("fLaC")) || header[4]&0x7f != 0 {
			return false
		}
		streamInfoSize := uint32(header[5])<<16 | uint32(header[6])<<8 | uint32(header[7])
		return streamInfoSize == 34
	case ".mp4", ".mov":
		if len(header) < 16 || !bytes.Equal(header[4:8], []byte("ftyp")) {
			return false
		}
		boxSize := binary.BigEndian.Uint32(header[:4])
		return boxSize >= 16 && boxSize%4 == 0 && uint64(boxSize) <= uint64(len(header))
	case ".avi":
		return len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("AVI "))
	case ".mkv":
		return seedanceUploadHasMatroskaDocType(header)
	default:
		return false
	}
}

func SeedanceFileUpload(c *gin.Context) {
	seedanceFileUpload(c, false)
}

func SeedanceSessionFileUpload(c *gin.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	if err := service.WriteVideoContentCookie(c, identity); err != nil {
		seedanceUploadError(c, http.StatusInternalServerError, "video_content_auth_failed", "failed to prepare video playback authentication")
		return
	}
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	requestedGroup := strings.TrimSpace(c.Query("group"))
	if requestedGroup == "" {
		requestedGroup = userGroup
	}
	if _, err := videoGenerationGroups(userGroup, requestedGroup); err != nil {
		seedanceUploadError(c, http.StatusForbidden, "group_access_denied", err.Error())
		return
	}
	common.SetContextKey(c, constant.ContextKeyUsingGroup, requestedGroup)
	seedanceFileUpload(c, true)
}

func seedanceFileUpload(c *gin.Context, dashboardEnvelope bool) {
	if c.GetInt("role") < common.FileUploadPermission {
		seedanceUploadError(c, http.StatusForbidden, "upload_forbidden", "File upload is not permitted for this account")
		return
	}

	channel, modelName, err := selectSeedanceUploadChannel(c)
	if err != nil {
		status := http.StatusServiceUnavailable
		code := "seedance_upload_channel_unavailable"
		if errors.Is(err, errSeedanceUploadAccessDenied) {
			status = http.StatusForbidden
			code = "seedance_upload_access_denied"
		}
		seedanceUploadError(c, status, code, err.Error())
		return
	}
	if setupErr := middleware.SetupContextForSelectedChannel(c, channel, modelName); setupErr != nil {
		seedanceUploadError(c, http.StatusServiceUnavailable, "seedance_upload_channel_unavailable", setupErr.Error())
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, seedance.MaxUploadBytes+seedanceMultipartOverheadBytes)
	multipartReader, err := c.Request.MultipartReader()
	if err != nil {
		seedanceUploadError(c, http.StatusBadRequest, "invalid_upload", "request must use multipart/form-data")
		return
	}
	filePart, err := multipartReader.NextPart()
	if err != nil {
		status := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(err) {
			status = http.StatusRequestEntityTooLarge
		}
		seedanceUploadError(c, status, "invalid_upload", "multipart body must contain exactly one file field named file")
		return
	}
	defer filePart.Close()
	if filePart.FormName() != "file" || strings.TrimSpace(filePart.FileName()) == "" {
		seedanceUploadError(c, http.StatusBadRequest, "invalid_upload", "multipart body must contain exactly one file field named file")
		return
	}

	filename := filepath.Base(strings.TrimSpace(filePart.FileName()))
	extension := strings.ToLower(filepath.Ext(filename))
	_, ok := seedanceUploadContentTypes[extension]
	if !ok || filename == "." || filename == "" {
		seedanceUploadError(c, http.StatusBadRequest, "unsupported_file_type", "supported formats: JPG, JPEG, PNG, WEBP, MP3, WAV, FLAC, MP4, AVI, MOV, MKV")
		return
	}
	header := make([]byte, 512)
	read, readErr := io.ReadFull(filePart, header)
	if read == 0 {
		seedanceUploadError(c, http.StatusRequestEntityTooLarge, "invalid_upload_size", fmt.Sprintf("file size must be between 1 and %d bytes", seedance.MaxUploadBytes))
		return
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		status := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(readErr) {
			status = http.StatusRequestEntityTooLarge
		}
		seedanceUploadError(c, status, "invalid_upload", "failed to inspect uploaded file")
		return
	}
	if !seedanceUploadContentMatches(extension, filePart.Header.Get("Content-Type"), header[:read]) {
		seedanceUploadError(c, http.StatusBadRequest, "file_type_mismatch", "uploaded content does not match its filename extension")
		return
	}
	file := &seedanceMultipartFileReader{
		file:      io.MultiReader(bytes.NewReader(header[:read]), filePart),
		multipart: multipartReader,
	}
	apiKey := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	allowed, retryAfter, rateLimitErr := middleware.TakeSeedanceUploadProviderRateLimit(c.Request.Context(), channel.Id, apiKey)
	if rateLimitErr != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Seedance upload aggregate rate limit failed for channel %d: %v", channel.Id, rateLimitErr))
		seedanceUploadError(c, http.StatusInternalServerError, "upload_rate_limit_unavailable", "Upload rate limit is temporarily unavailable")
		return
	}
	if !allowed {
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
		}
		seedanceUploadError(c, http.StatusTooManyRequests, "upload_rate_limited", "Seedance upload rate limit exceeded")
		return
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeSeedance]
	}
	result, err := (seedance.UploadClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Proxy:   channel.GetSetting().Proxy,
	}).Upload(c.Request.Context(), filename, 0, file)
	if err != nil {
		if errors.Is(err, seedance.ErrUploadEmpty) || errors.Is(err, seedance.ErrUploadTooLarge) || common.IsRequestBodyTooLargeError(err) {
			seedanceUploadError(c, http.StatusRequestEntityTooLarge, "invalid_upload_size", fmt.Sprintf("file size must be between 1 and %d bytes", seedance.MaxUploadBytes))
			return
		}
		if errors.Is(err, errSeedanceUploadAdditionalPart) || errors.Is(err, errSeedanceUploadMalformedPayload) {
			seedanceUploadError(c, http.StatusBadRequest, "invalid_upload", err.Error())
			return
		}
		var upstreamErr *seedance.UpstreamUploadError
		if errors.As(err, &upstreamErr) {
			if upstreamErr.StatusCode == http.StatusTooManyRequests {
				if retryAfter := strings.TrimSpace(upstreamErr.RetryAfter); retryAfter != "" {
					c.Header("Retry-After", retryAfter)
				}
				seedanceUploadError(c, http.StatusTooManyRequests, "seedance_upload_rate_limited", upstreamErr.Message)
				return
			}
			if upstreamErr.StatusCode == http.StatusPaymentRequired {
				seedanceUploadError(c, http.StatusBadGateway, "upstream_balance_insufficient", upstreamErr.Message)
				return
			}
		}
		seedanceUploadError(c, http.StatusBadGateway, "seedance_upload_failed", common.MaskSensitiveInfo(err.Error()))
		return
	}
	if dashboardEnvelope {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
		return
	}
	c.JSON(http.StatusOK, result)
}

var errSeedanceUploadAccessDenied = errors.New("no permitted Seedance video model is available for this token")

func selectSeedanceUploadChannel(c *gin.Context) (*model.Channel, string, error) {
	channels, err := model.GetEnabledChannelsByType(constant.ChannelTypeSeedance)
	if err != nil {
		return nil, "", fmt.Errorf("query Seedance channels: %w", err)
	}
	if specific, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); ok {
		specificID, parseErr := strconv.Atoi(fmt.Sprint(specific))
		if parseErr != nil {
			return nil, "", errSeedanceUploadAccessDenied
		}
		channels = filterSeedanceChannels(channels, func(channel *model.Channel) bool { return channel.Id == specificID })
	}
	if len(channels) == 0 {
		return nil, "", fmt.Errorf("no enabled Seedance channel is configured")
	}

	groups := []string{common.GetContextKeyString(c, constant.ContextKeyUsingGroup)}
	if len(groups) == 1 && groups[0] == "auto" {
		groups = service.GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup))
	}
	modelLimited := common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled)
	var tokenModels map[string]bool
	if modelLimited {
		value, _ := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
		tokenModels, _ = value.(map[string]bool)
	}

	type eligibleChannel struct {
		channel   *model.Channel
		modelName string
	}
	eligible := make([]eligibleChannel, 0, len(channels))
	for _, channel := range channels {
		if channel.ChannelInfo.IsMultiKey {
			return nil, "", fmt.Errorf("Seedance channel %d uses unsupported multi-key mode", channel.Id)
		}
		matchedModel := ""
		for _, modelName := range channel.GetModels() {
			upstreamModel, mappingErr := seedance.ResolveMappedModel(modelName, channel.GetModelMapping())
			if mappingErr != nil {
				return nil, "", fmt.Errorf("resolve Seedance channel %d model mapping: %w", channel.Id, mappingErr)
			}
			if _, supported := seedance.CapabilityForModel(upstreamModel); !supported {
				continue
			}
			if modelLimited {
				matchingName := ratio_setting.FormatMatchingModelName(modelName)
				if _, ok := tokenModels[modelName]; !ok {
					if _, ok = tokenModels[matchingName]; !ok {
						continue
					}
				}
			}
			for _, group := range groups {
				if group != "" && model.IsChannelEnabledForGroupModel(group, modelName, channel.Id) {
					matchedModel = modelName
					break
				}
			}
			if matchedModel != "" {
				break
			}
		}
		if matchedModel != "" {
			eligible = append(eligible, eligibleChannel{channel: channel, modelName: matchedModel})
		}
	}
	if len(eligible) == 0 {
		return nil, "", errSeedanceUploadAccessDenied
	}
	if len(eligible) != 1 {
		return nil, "", fmt.Errorf("multiple Seedance channels are eligible for uploads; configure exactly one")
	}
	return eligible[0].channel, eligible[0].modelName, nil
}

func filterSeedanceChannels(channels []*model.Channel, keep func(*model.Channel) bool) []*model.Channel {
	filtered := make([]*model.Channel, 0, len(channels))
	for _, channel := range channels {
		if keep(channel) {
			filtered = append(filtered, channel)
		}
	}
	return filtered
}

func seedanceUploadError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"type": code, "code": code, "message": message}})
}
