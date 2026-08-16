package seedance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	ActionVideo            = "seedance_video"
	ActionContextIR        = "seedance_context_ir"
	ActionMidjourneyVideo  = "seedance_midjourney_video"
	requestContextKey      = "seedance_task_request"
	capabilityContextKey   = "seedance_model_capability"
	metadataFieldsKey      = "seedance_metadata_fields"
	maxPromptLength        = 20480
	maxContextPromptLength = 7000
)

var videoRequestFields = stringSet("model", "prompt", "image", "images", "seconds", "duration", "metadata")

var videoMetadataFields = stringSet(
	"resolution", "ratio", "seed", "generate_audio", "return_last_frame", "duration", "content",
	"video_url", "video_urls", "audio_url", "audio_urls", "negative_prompt", "prompt_extend",
	"safety_tolerance", "draft", "draft_cache", "extend_from_task_id", "type", "script_name",
	"sessionId", "faceId",
)

var midjourneyRequestFields = stringSet(
	"model", "prompt", "image_urls", "task_id", "index", "video_type", "animate_mode", "motion", "batch_size", "end_url",
)

type parsedTaskRequest struct {
	Video      *VideoRequest
	Midjourney *MidjourneyVideoRequest
	Capability ModelCapability
	MetaFields map[string]struct{}
}

func validateAndStoreRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info == nil {
		return service.TaskErrorWrapperLocal(fmt.Errorf("relay info is nil"), "invalid_relay_info", http.StatusInternalServerError)
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	if info.ChannelMeta == nil {
		info.ChannelMeta = &relaycommon.ChannelMeta{}
	}
	if strings.HasSuffix(c.Request.URL.Path, "/remix") {
		return localTaskError("Seedance does not support the remix endpoint", "unsupported_operation")
	}

	body, err := common.GetBodyStorage(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "read_request_body_failed", http.StatusBadRequest)
	}
	bodyBytes, err := body.Bytes()
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "read_request_body_failed", http.StatusBadRequest)
	}

	if isMidjourneyVideoPath(c.Request.URL.Path) {
		return validateMidjourneyRequest(c, info, bodyBytes)
	}
	return validateVideoRequest(c, info, bodyBytes)
}

func validateVideoRequest(c *gin.Context, info *relaycommon.RelayInfo, body []byte) *dto.TaskError {
	raw, err := decodeObject(body, "request")
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_json", http.StatusBadRequest)
	}
	if err = rejectUnknownFields(raw, videoRequestFields, "request"); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	metaFields := map[string]struct{}{}
	if metadata, ok := raw["metadata"]; ok && string(metadata) != "null" {
		metaRaw, decodeErr := decodeObject(metadata, "metadata")
		if decodeErr != nil {
			return service.TaskErrorWrapperLocal(decodeErr, "invalid_request", http.StatusBadRequest)
		}
		if decodeErr = rejectUnknownFields(metaRaw, videoMetadataFields, "metadata"); decodeErr != nil {
			return service.TaskErrorWrapperLocal(decodeErr, "invalid_request", http.StatusBadRequest)
		}
		for key := range metaRaw {
			metaFields[key] = struct{}{}
		}
	}

	var req VideoRequest
	if err = common.Unmarshal(body, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		return localTaskError("model is required", "missing_model")
	}
	validationModel, mappingErr := ResolveMappedModel(req.Model, c.GetString("model_mapping"))
	if mappingErr != nil {
		return service.TaskErrorWrapperLocal(mappingErr, "invalid_model_mapping", http.StatusBadRequest)
	}
	capability, ok := CapabilityForModel(validationModel)
	if !ok || capability.Protocol == ProtocolMidjourneyVideo {
		return localTaskError(fmt.Sprintf("unsupported Seedance video model %q", req.Model), "unsupported_model")
	}
	if err = normalizeVideoRequest(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if err = validateModelRequest(&req, capability, metaFields); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	info.OriginModelName = req.Model
	if capability.Protocol == ProtocolContextIR {
		info.Action = ActionContextIR
	} else {
		info.Action = ActionVideo
	}
	parsed := parsedTaskRequest{Video: &req, Capability: capability, MetaFields: metaFields}
	c.Set(requestContextKey, parsed)
	c.Set(capabilityContextKey, capability)
	c.Set(metadataFieldsKey, metaFields)
	return nil
}

func validateMidjourneyRequest(c *gin.Context, info *relaycommon.RelayInfo, body []byte) *dto.TaskError {
	raw, err := decodeObject(body, "request")
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_json", http.StatusBadRequest)
	}
	if err = rejectUnknownFields(raw, midjourneyRequestFields, "request"); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	var req MidjourneyVideoRequest
	if err = common.Unmarshal(body, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	originModel := strings.TrimSpace(req.Model)
	if originModel == "" {
		originModel = MidjourneyVideoCapability.Name
	}
	validationModel, mappingErr := ResolveMappedModel(originModel, c.GetString("model_mapping"))
	if mappingErr != nil {
		return service.TaskErrorWrapperLocal(mappingErr, "invalid_model_mapping", http.StatusBadRequest)
	}
	if validationModel != MidjourneyVideoCapability.Name {
		return localTaskError("model must resolve to midjourney-video", "unsupported_model")
	}
	req.Model = MidjourneyVideoCapability.Name
	if err = validateMidjourneyVideoRequest(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	info.OriginModelName = originModel
	info.Action = ActionMidjourneyVideo
	c.Set(requestContextKey, parsedTaskRequest{Midjourney: &req, Capability: MidjourneyVideoCapability})
	c.Set(capabilityContextKey, MidjourneyVideoCapability)
	return nil
}

func decodeObject(data []byte, field string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := common.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", field, err)
	}
	if object == nil {
		return nil, fmt.Errorf("%s must be a JSON object", field)
	}
	return object, nil
}

func rejectUnknownFields(actual map[string]json.RawMessage, allowed map[string]struct{}, field string) error {
	for key := range actual {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("%s contains unsupported field %q", field, key)
		}
	}
	return nil
}

func normalizeVideoRequest(req *VideoRequest) error {
	if req.Prompt != nil {
		trimmed := strings.TrimSpace(*req.Prompt)
		req.Prompt = &trimmed
	}
	if req.Image != nil {
		image := strings.TrimSpace(*req.Image)
		if image == "" {
			return fmt.Errorf("image must not be empty")
		}
		if len(req.Images) > 0 {
			return fmt.Errorf("image and images cannot both be provided")
		}
		req.Images = []string{image}
		req.Image = nil
	}
	if req.Seconds != nil && req.Duration != nil {
		return fmt.Errorf("seconds and duration cannot both be provided")
	}
	if req.Metadata != nil && req.Metadata.Duration != nil && (req.Seconds != nil || req.Duration != nil) {
		return fmt.Errorf("metadata.duration cannot be combined with seconds or duration")
	}
	return nil
}

func validateModelRequest(req *VideoRequest, capability ModelCapability, metaFields map[string]struct{}) error {
	prompt := ""
	if req.Prompt != nil {
		prompt = *req.Prompt
	}
	promptLength := utf8.RuneCountInString(prompt)
	maxLength := maxPromptLength
	if capability.Protocol == ProtocolContextIR {
		maxLength = maxContextPromptLength
	}
	if promptLength > maxLength {
		return fmt.Errorf("prompt must not exceed %d characters", maxLength)
	}

	if err := validateMediaURLs(req); err != nil {
		return err
	}
	if err := validateMetadataForCapability(req, capability, metaFields); err != nil {
		return err
	}
	if err := validatePromptAndMedia(req, capability, prompt); err != nil {
		return err
	}
	if err := validateSeconds(req, capability); err != nil {
		return err
	}
	return validateResolutionAndRatio(req, capability)
}

func validatePromptAndMedia(req *VideoRequest, capability ModelCapability, prompt string) error {
	images := len(req.Images)
	contentImages, contentVideos, contentAudio := countContent(req.Metadata)
	videoCount := contentVideos + metadataURLCount(req.Metadata, "video")
	audioCount := contentAudio + metadataURLCount(req.Metadata, "audio")
	totalImages := images + contentImages

	requirePrompt := promptRequiredForCapability(capability)
	if requirePrompt && prompt == "" {
		return fmt.Errorf("prompt is required for model %s", capability.Name)
	}

	switch capability.Kind {
	case TaskKindText, TaskKindContextText:
		if totalImages+videoCount+audioCount != 0 {
			return fmt.Errorf("model %s does not accept reference media", capability.Name)
		}
	case TaskKindImage, TaskKindContextImage:
		maxImages := 2
		if capability.Family == "flux-3-video" {
			maxImages = 10
		} else if capability.Family == "happyhorse-1.1" || capability.Family == "wan-2.7-spicy" || strings.Contains(capability.Name, "audio-drive") {
			maxImages = 1
		}
		if totalImages < 1 || totalImages > maxImages {
			return fmt.Errorf("model %s requires 1 to %d images", capability.Name, maxImages)
		}
		if videoCount != 0 {
			return fmt.Errorf("model %s does not accept video references", capability.Name)
		}
		if strings.Contains(capability.Name, "audio-drive") && audioCount != 1 {
			return fmt.Errorf("model %s requires exactly one audio URL", capability.Name)
		}
	case TaskKindMulti, TaskKindContextMulti:
		return validateMultiMedia(req, capability, totalImages, videoCount, audioCount, prompt)
	case TaskKindReference:
		if totalImages < 1 || totalImages > 9 {
			return fmt.Errorf("model %s requires 1 to 9 reference images", capability.Name)
		}
	case TaskKindStartEnd:
		if totalImages != 2 {
			return fmt.Errorf("model %s requires exactly 2 images", capability.Name)
		}
	case TaskKindVideo, TaskKindEdit, TaskKindMotion, TaskKindLipIdentify:
		if videoCount != 1 {
			return fmt.Errorf("model %s requires exactly one video_url", capability.Name)
		}
	case TaskKindDraftEnhance:
		if req.Metadata == nil || req.Metadata.DraftCache == nil || !strings.HasPrefix(*req.Metadata.DraftCache, "artifact_") {
			return fmt.Errorf("model %s requires a local artifact_* draft_cache", capability.Name)
		}
	case TaskKindElements:
		if totalImages < 1 || totalImages > 9 {
			return fmt.Errorf("model %s requires 1 to 9 images", capability.Name)
		}
	case TaskKindLipTTS:
		if req.Metadata == nil || req.Metadata.SessionID == nil || req.Metadata.FaceID == nil ||
			!strings.HasPrefix(*req.Metadata.SessionID, "artifact_") || !strings.HasPrefix(*req.Metadata.FaceID, "artifact_") {
			return fmt.Errorf("model %s requires sessionId and faceId", capability.Name)
		}
	case TaskKindLipVideo:
		if req.Metadata == nil || req.Metadata.SessionID == nil || req.Metadata.FaceID == nil ||
			!strings.HasPrefix(*req.Metadata.SessionID, "artifact_") || !strings.HasPrefix(*req.Metadata.FaceID, "artifact_") || audioCount != 1 {
			return fmt.Errorf("model %s requires sessionId, faceId, and one audio URL", capability.Name)
		}
	case TaskKindUpscale:
		if prompt != "" {
			return fmt.Errorf("zhenzhen-upscaler does not accept prompt")
		}
		if req.Metadata == nil || len(req.Metadata.Content) != 1 || contentVideos != 1 || totalImages != 0 || audioCount != 0 {
			return fmt.Errorf("zhenzhen-upscaler requires exactly one metadata.content video_url")
		}
	}
	return nil
}

func validateMultiMedia(req *VideoRequest, capability ModelCapability, images, videos, audio int, prompt string) error {
	if capability.Protocol == ProtocolContextIR {
		if images > 9 || videos > 3 || audio > 3 || images+videos+audio == 0 {
			return fmt.Errorf("multimodal Context IR requires 1 or more references, with at most 9 images, 3 videos, and 3 audio files")
		}
		return nil
	}
	switch capability.Family {
	case "seedance-2.0", "hailuo-h3":
		if len(req.Images) != 0 {
			return fmt.Errorf("model %s requires all references in metadata.content", capability.Name)
		}
		if images > 9 || videos > 3 || audio > 3 || images+videos+audio == 0 {
			return fmt.Errorf("model %s requires 1 or more references, with at most 9 images, 3 videos, and 3 audio files", capability.Name)
		}
	case "seedance-2.5":
		if len(req.Images) != 0 {
			return fmt.Errorf("model %s requires all references in metadata.content", capability.Name)
		}
		if images > 30 || videos > 10 || audio > 10 || images+videos+audio == 0 || images+videos+audio > 50 {
			return fmt.Errorf("model %s allows at most 30 images, 10 videos, 10 audio files, and 50 references total", capability.Name)
		}
	case "zhenzhen-video":
		switch capability.Name {
		case "zhenzhen-video-gk-v15":
			if images > 7 || videos != 0 || audio != 0 {
				return fmt.Errorf("model %s allows at most 7 images", capability.Name)
			}
		case "zhenzhen-video-v31-fast":
			if images > 3 || videos != 0 || audio != 0 {
				return fmt.Errorf("model %s allows at most 3 images", capability.Name)
			}
		case "zhenzhen-video-v31-quality":
			if images > 2 || videos != 0 || audio != 0 {
				return fmt.Errorf("model %s does not accept the 3-image reference mode", capability.Name)
			}
		case "zhenzhen-video-g-omni-flash":
			if images > 16 || videos > 1 || audio != 0 {
				return fmt.Errorf("model %s allows at most 16 images and one video", capability.Name)
			}
			hasExtend := req.Metadata != nil && req.Metadata.ExtendFromTaskID != nil
			if hasExtend && !strings.HasPrefix(*req.Metadata.ExtendFromTaskID, "task_") {
				return fmt.Errorf("extend_from_task_id must be a local task_* ID")
			}
			if hasExtend && videos != 0 {
				return fmt.Errorf("extend_from_task_id and video_url are mutually exclusive")
			}
			if prompt == "" && images+videos == 0 && !hasExtend {
				return fmt.Errorf("model %s requires prompt or reference media", capability.Name)
			}
		}
	default:
		if images+videos+audio == 0 && capability.Kind == TaskKindMulti {
			return fmt.Errorf("model %s requires reference media", capability.Name)
		}
	}
	return nil
}

func validateSeconds(req *VideoRequest, capability ModelCapability) error {
	seconds, provided, err := requestedSeconds(req)
	if err != nil {
		return err
	}
	if !provided {
		return nil
	}
	if seconds > relaycommon.MaxTaskDurationSeconds {
		return fmt.Errorf("seconds must not exceed %d", relaycommon.MaxTaskDurationSeconds)
	}
	if seconds == -1 {
		durations := supportedDurations(capability)
		if durations.SmartValue != nil && *durations.SmartValue == seconds {
			return nil
		}
		return fmt.Errorf("model %s does not support smart duration", capability.Name)
	}
	if seconds <= 0 {
		return fmt.Errorf("seconds must be positive")
	}

	durations := supportedDurations(capability)
	if !durations.Supported {
		return fmt.Errorf("model %s does not accept seconds", capability.Name)
	}
	if len(durations.Values) > 0 {
		for _, value := range durations.Values {
			if seconds == value {
				return nil
			}
		}
		return fmt.Errorf("model %s seconds must be one of %v", capability.Name, durations.Values)
	}
	if seconds < durations.Min || seconds > durations.Max {
		return fmt.Errorf("model %s seconds must be between %d and %d", capability.Name, durations.Min, durations.Max)
	}
	return nil
}

func requestedSeconds(req *VideoRequest) (int, bool, error) {
	if req.Seconds != nil {
		seconds, err := strconv.Atoi(*req.Seconds)
		if err != nil {
			return 0, true, fmt.Errorf("seconds must be an integer encoded as a string")
		}
		return seconds, true, nil
	}
	if req.Duration != nil {
		return *req.Duration, true, nil
	}
	if req.Metadata != nil && req.Metadata.Duration != nil {
		return *req.Metadata.Duration, true, nil
	}
	return 0, false, nil
}

func validateResolutionAndRatio(req *VideoRequest, capability ModelCapability) error {
	if req.Metadata == nil {
		if capability.Kind == TaskKindContextText {
			return fmt.Errorf("metadata.ratio is required for model %s", capability.Name)
		}
		return nil
	}
	resolution := stringValue(req.Metadata.Resolution)
	ratio := stringValue(req.Metadata.Ratio)
	if capability.Kind == TaskKindContextText && ratio == "" {
		return fmt.Errorf("metadata.ratio is required for model %s", capability.Name)
	}

	if resolution != "" {
		allowed := supportedResolutions(capability)
		if len(allowed) == 0 || !contains(allowed, resolution) {
			return fmt.Errorf("resolution %q is not supported by model %s", resolution, capability.Name)
		}
	}

	if ratio != "" {
		allowed := supportedAspectRatios(capability)
		if len(allowed) == 0 || !contains(allowed, ratio) {
			return fmt.Errorf("ratio %q is not supported by model %s", ratio, capability.Name)
		}
	}
	return nil
}

func validateMetadataForCapability(req *VideoRequest, capability ModelCapability, fields map[string]struct{}) error {
	if req.Metadata == nil || len(fields) == 0 {
		return nil
	}
	allowed := stringSet("resolution", "ratio")
	switch capability.Family {
	case "seedance-2.0", "seedance-2.5":
		allowed = mergeSets(allowed, stringSet("seed", "generate_audio", "return_last_frame"))
		if capability.Family == "seedance-2.5" {
			allowed = mergeSets(allowed, stringSet("duration"))
		}
		if capability.Kind == TaskKindMulti {
			allowed = mergeSets(allowed, stringSet("content"))
		}
	case "wan-2.7-spicy":
		allowed = mergeSets(allowed, stringSet("audio_url", "negative_prompt", "prompt_extend"))
	case "happyhorse-1.1", "hailuo-2.3":
	case "hailuo-h3":
		if capability.Kind == TaskKindMulti {
			allowed = mergeSets(allowed, stringSet("content"))
		}
	case "flux-3-video":
		allowed = mergeSets(allowed, stringSet("draft", "generate_audio", "safety_tolerance"))
		if capability.Kind == TaskKindVideo {
			allowed = mergeSets(allowed, stringSet("video_url"))
		}
		if capability.Kind == TaskKindDraftEnhance {
			allowed = mergeSets(allowed, stringSet("draft_cache"))
		}
	case "minimax-h3-ow":
		if strings.Contains(capability.Name, "audio-drive") {
			allowed = mergeSets(allowed, stringSet("audio_url", "audio_urls"))
		}
	case "minimax-h3-context-ir":
		if capability.Kind == TaskKindContextMulti {
			allowed = mergeSets(allowed, stringSet("content", "video_url", "video_urls", "audio_url", "audio_urls"))
		}
	case "vidu-q3":
		if capability.Kind == TaskKindShortPlay {
			allowed = mergeSets(allowed, stringSet("script_name"))
		}
	case "zhenzhen-video":
		if capability.Name == "zhenzhen-video-v31-fast" {
			allowed = mergeSets(allowed, stringSet("type"))
		}
		if capability.Name == "zhenzhen-video-g-omni-flash" {
			allowed = mergeSets(allowed, stringSet("video_url", "extend_from_task_id"))
		}
	case "zhenzhen-upscaler":
		allowed = stringSet("resolution", "content")
	case "kling":
		switch capability.Kind {
		case TaskKindEdit, TaskKindMotion, TaskKindLipIdentify:
			allowed = mergeSets(allowed, stringSet("video_url"))
		case TaskKindLipTTS:
			allowed = mergeSets(allowed, stringSet("sessionId", "faceId"))
		case TaskKindLipVideo:
			allowed = mergeSets(allowed, stringSet("sessionId", "faceId", "audio_url", "audio_urls"))
		}
	}
	for field := range fields {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("metadata field %q is not supported by model %s", field, capability.Name)
		}
	}
	if req.Metadata.Seed != nil && (*req.Metadata.Seed < -1 || *req.Metadata.Seed > 2147483647) {
		return fmt.Errorf("metadata.seed must be between -1 and 2147483647")
	}
	if req.Metadata.SafetyTolerance != nil && (*req.Metadata.SafetyTolerance < 0 || *req.Metadata.SafetyTolerance > 4) {
		return fmt.Errorf("metadata.safety_tolerance must be between 0 and 4")
	}
	if req.Metadata.Type != nil && capability.Name == "zhenzhen-video-v31-fast" && *req.Metadata.Type != "reference" {
		return fmt.Errorf("metadata.type must be reference for model %s", capability.Name)
	}
	return nil
}

func validateMediaURLs(req *VideoRequest) error {
	for _, rawURL := range req.Images {
		if err := validateMediaURL(rawURL); err != nil {
			return fmt.Errorf("invalid image URL: %w", err)
		}
	}
	if req.Metadata == nil {
		return nil
	}
	for index, item := range req.Metadata.Content {
		var mediaURL string
		switch item.Type {
		case "image_url":
			if item.ImageURL == nil || item.VideoURL != nil || item.AudioURL != nil {
				return fmt.Errorf("metadata.content[%d] must contain exactly image_url", index)
			}
			mediaURL = item.ImageURL.URL
		case "video_url":
			if item.VideoURL == nil || item.ImageURL != nil || item.AudioURL != nil {
				return fmt.Errorf("metadata.content[%d] must contain exactly video_url", index)
			}
			mediaURL = item.VideoURL.URL
		case "audio_url":
			if item.AudioURL == nil || item.ImageURL != nil || item.VideoURL != nil {
				return fmt.Errorf("metadata.content[%d] must contain exactly audio_url", index)
			}
			mediaURL = item.AudioURL.URL
		default:
			return fmt.Errorf("metadata.content[%d].type is unsupported", index)
		}
		if err := validateMediaURL(mediaURL); err != nil {
			return fmt.Errorf("invalid metadata.content[%d] URL: %w", index, err)
		}
	}
	if req.Metadata.VideoURL != nil {
		if err := validateMediaURL(*req.Metadata.VideoURL); err != nil {
			return err
		}
	}
	if req.Metadata.AudioURL != nil {
		if err := validateMediaURL(*req.Metadata.AudioURL); err != nil {
			return err
		}
	}
	for _, values := range [][]string{req.Metadata.VideoURLs, req.Metadata.AudioURLs} {
		for _, rawURL := range values {
			if err := validateMediaURL(rawURL); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateMediaURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("only absolute http and https URLs are accepted")
	}
	return nil
}

func validateMidjourneyVideoRequest(req *MidjourneyVideoRequest) error {
	if req.Prompt != nil && utf8.RuneCountInString(*req.Prompt) > maxPromptLength {
		return fmt.Errorf("prompt must not exceed %d characters", maxPromptLength)
	}
	if req.TaskID != nil {
		return fmt.Errorf("task_id source mode is unavailable because Midjourney image tasks are not part of this integration")
	}
	if len(req.ImageURLs) != 1 {
		return fmt.Errorf("image_urls must contain exactly one start frame")
	}
	if err := validateMediaURL(req.ImageURLs[0]); err != nil {
		return fmt.Errorf("invalid start frame URL: %w", err)
	}
	if req.EndURL != nil {
		if err := validateMediaURL(*req.EndURL); err != nil {
			return fmt.Errorf("invalid end frame URL: %w", err)
		}
	}
	if req.Index != nil {
		return fmt.Errorf("index is only valid with the unsupported task_id source mode")
	}
	if req.AnimateMode != nil && *req.AnimateMode != "manual" {
		return fmt.Errorf("animate_mode must be manual when image_urls is used")
	}
	if req.Motion != nil && *req.Motion != "low" && *req.Motion != "high" {
		return fmt.Errorf("motion must be low or high")
	}
	if req.BatchSize != nil && *req.BatchSize != 1 && *req.BatchSize != 2 && *req.BatchSize != 4 {
		return fmt.Errorf("batch_size must be 1, 2, or 4")
	}
	videoType := "vid_1.1_i2v_480"
	if req.VideoType != nil {
		videoType = *req.VideoType
	}
	allowedTypes := stringSet("vid_1.1_i2v_480", "vid_1.1_i2v_720", "vid_1.1_i2v_start_end_480", "vid_1.1_i2v_start_end_720")
	if _, ok := allowedTypes[videoType]; !ok {
		return fmt.Errorf("unsupported video_type %q", videoType)
	}
	if req.EndURL == nil && strings.Contains(videoType, "start_end") {
		return fmt.Errorf("start_end video_type requires end_url")
	}
	if req.EndURL != nil && !strings.Contains(videoType, "start_end") {
		if strings.HasSuffix(videoType, "_720") {
			videoType = "vid_1.1_i2v_start_end_720"
		} else {
			videoType = "vid_1.1_i2v_start_end_480"
		}
	}
	req.VideoType = &videoType
	return nil
}

func countContent(metadata *VideoMetadata) (images, videos, audio int) {
	if metadata == nil {
		return 0, 0, 0
	}
	for _, item := range metadata.Content {
		switch item.Type {
		case "image_url":
			images++
		case "video_url":
			videos++
		case "audio_url":
			audio++
		}
	}
	return images, videos, audio
}

func metadataURLCount(metadata *VideoMetadata, mediaType string) int {
	if metadata == nil {
		return 0
	}
	switch mediaType {
	case "video":
		count := len(metadata.VideoURLs)
		if metadata.VideoURL != nil {
			count++
		}
		return count
	case "audio":
		count := len(metadata.AudioURLs)
		if metadata.AudioURL != nil {
			count++
		}
		return count
	default:
		return 0
	}
}

func isMidjourneyVideoPath(path string) bool {
	return strings.HasSuffix(strings.TrimSuffix(path, "/"), "/v1/midjourney/generations/video")
}

func getParsedRequest(c *gin.Context) (parsedTaskRequest, error) {
	value, ok := c.Get(requestContextKey)
	if !ok {
		return parsedTaskRequest{}, fmt.Errorf("validated Seedance request is missing")
	}
	request, ok := value.(parsedTaskRequest)
	if !ok {
		return parsedTaskRequest{}, fmt.Errorf("invalid Seedance request context")
	}
	return request, nil
}

// ResolveMappedModel follows a channel's model redirects to the final upstream model.
func ResolveMappedModel(originModel, mappingJSON string) (string, error) {
	mappingJSON = strings.TrimSpace(mappingJSON)
	if mappingJSON == "" || mappingJSON == "{}" {
		return originModel, nil
	}
	var mappings map[string]string
	if err := common.Unmarshal([]byte(mappingJSON), &mappings); err != nil {
		return "", fmt.Errorf("invalid channel model mapping: %w", err)
	}
	current := originModel
	visited := map[string]struct{}{current: {}}
	for {
		next := strings.TrimSpace(mappings[current])
		if next == "" || next == current {
			return current, nil
		}
		if _, exists := visited[next]; exists {
			return "", fmt.Errorf("channel model mapping contains a cycle")
		}
		visited[next] = struct{}{}
		current = next
	}
}

func localTaskError(message, code string) *dto.TaskError {
	return service.TaskErrorWrapperLocal(fmt.Errorf("%s", message), code, http.StatusBadRequest)
}

func stringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func mergeSets(destination, source map[string]struct{}) map[string]struct{} {
	for value := range source {
		destination[value] = struct{}{}
	}
	return destination
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
