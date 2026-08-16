package seedance

import (
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// GenerationPromptCapability describes the prompt control rendered by the
// dashboard video-generation form.
type GenerationPromptCapability struct {
	Supported bool `json:"supported"`
	Required  bool `json:"required"`
	MaxLength int  `json:"max_length"`
}

// GenerationMediaCapability tells the dashboard both which media may be
// uploaded and where the resulting URL belongs in the provider request.
type GenerationMediaCapability struct {
	Type     string `json:"type"`
	Field    string `json:"field"`
	Role     string `json:"role,omitempty"`
	MinItems int    `json:"min_items"`
	MaxItems int    `json:"max_items"`
}

// GenerationDurationCapability represents either a bounded integer range or
// a small explicit set. SmartValue is the provider's automatic-duration value.
type GenerationDurationCapability struct {
	Supported  bool  `json:"supported"`
	Min        int   `json:"min,omitempty"`
	Max        int   `json:"max,omitempty"`
	Values     []int `json:"values"`
	SmartValue *int  `json:"smart_value,omitempty"`
}

type GenerationDefaults struct {
	Duration        *int   `json:"duration,omitempty"`
	AspectRatio     string `json:"aspect_ratio,omitempty"`
	Resolution      string `json:"resolution,omitempty"`
	GenerateAudio   *bool  `json:"generate_audio,omitempty"`
	ReturnLastFrame *bool  `json:"return_last_frame,omitempty"`
	BatchSize       *int   `json:"batch_size,omitempty"`
}

type GenerationAdvancedField struct {
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	Widget       string   `json:"widget"`
	Required     bool     `json:"required"`
	Options      []string `json:"options,omitempty"`
	Minimum      *int64   `json:"minimum,omitempty"`
	Maximum      *int64   `json:"maximum,omitempty"`
	Step         *int64   `json:"step,omitempty"`
	DefaultValue any      `json:"default_value,omitempty"`
	Pattern      string   `json:"pattern,omitempty"`
}

// GenerationCapabilities is the public, key-free UI contract derived from the
// same local catalog and rule helpers used by request validation.
type GenerationCapabilities struct {
	Prompt            GenerationPromptCapability   `json:"prompt"`
	Media             []GenerationMediaCapability  `json:"media"`
	AspectRatios      []string                     `json:"aspect_ratios"`
	Resolutions       []string                     `json:"resolutions"`
	Durations         GenerationDurationCapability `json:"durations"`
	Defaults          GenerationDefaults           `json:"defaults"`
	SupportsAudio     bool                         `json:"supports_audio"`
	SupportsLastFrame bool                         `json:"supports_last_frame"`
	BatchSizes        []int                        `json:"batch_sizes"`
	AdvancedFields    []GenerationAdvancedField    `json:"advanced_fields"`
	Requirements      []string                     `json:"requirements"`
}

func GenerationCapabilitiesForModel(capability ModelCapability) GenerationCapabilities {
	promptRequired := promptRequiredForCapability(capability)
	promptSupported := capability.Kind != TaskKindUpscale
	result := GenerationCapabilities{
		Prompt: GenerationPromptCapability{
			Supported: promptSupported,
			Required:  promptSupported && promptRequired,
			MaxLength: maxPromptLength,
		},
		Media:          generationMediaCapabilities(capability),
		AspectRatios:   supportedAspectRatios(capability),
		Resolutions:    supportedResolutions(capability),
		Durations:      supportedDurations(capability),
		BatchSizes:     []int{},
		AdvancedFields: supportedAdvancedFields(capability),
		Requirements:   generationRequirements(capability),
	}
	if capability.Protocol == ProtocolContextIR {
		result.Prompt.MaxLength = maxContextPromptLength
	}
	if capability.Protocol == ProtocolMidjourneyVideo {
		result.BatchSizes = []int{1, 2, 4}
	}
	result.SupportsAudio = capability.Family == "seedance-2.0" || capability.Family == "seedance-2.5" || capability.Family == "flux-3-video"
	result.SupportsLastFrame = capability.Family == "seedance-2.0" || capability.Family == "seedance-2.5"
	result.Defaults = generationDefaults(capability, result)
	return result
}

func promptRequiredForCapability(capability ModelCapability) bool {
	required := true
	switch capability.Kind {
	case TaskKindImage:
		required = capability.Family != "seedance-2.0" && capability.Family != "seedance-2.5" && capability.Family != "happyhorse-1.1"
	case TaskKindUpscale, TaskKindLipIdentify, TaskKindLipVideo, TaskKindDraftEnhance:
		required = false
	case TaskKindMulti:
		if capability.Name == "zhenzhen-video-g-omni-flash" {
			required = false
		}
	case TaskKindMidjourneyVideo:
		required = false
	}
	if capability.Protocol == ProtocolContextIR {
		return true
	}
	return required
}

func generationMediaCapabilities(capability ModelCapability) []GenerationMediaCapability {
	media := make([]GenerationMediaCapability, 0, 3)
	add := func(mediaType, field, role string, minimum, maximum int) {
		media = append(media, GenerationMediaCapability{Type: mediaType, Field: field, Role: role, MinItems: minimum, MaxItems: maximum})
	}

	switch capability.Kind {
	case TaskKindImage, TaskKindContextImage:
		maxImages := 2
		if capability.Family == "flux-3-video" {
			maxImages = 10
		} else if capability.Family == "happyhorse-1.1" || capability.Family == "wan-2.7-spicy" || strings.Contains(capability.Name, "audio-drive") {
			maxImages = 1
		}
		add("image", "images", "reference", 1, maxImages)
		if strings.Contains(capability.Name, "audio-drive") {
			add("audio", "metadata.audio_urls", "driver", 1, 1)
		} else if capability.Family == "wan-2.7-spicy" {
			add("audio", "metadata.audio_url", "reference", 0, 1)
		}
	case TaskKindMulti, TaskKindContextMulti:
		switch capability.Family {
		case "seedance-2.0", "hailuo-h3", "minimax-h3-context-ir":
			add("image", "metadata.content", "reference", 0, 9)
			add("video", "metadata.content", "reference", 0, 3)
			add("audio", "metadata.content", "reference", 0, 3)
		case "seedance-2.5":
			add("image", "metadata.content", "reference", 0, 30)
			add("video", "metadata.content", "reference", 0, 10)
			add("audio", "metadata.content", "reference", 0, 10)
		case "zhenzhen-video":
			maxImages := 0
			switch capability.Name {
			case "zhenzhen-video-gk-v15":
				maxImages = 7
			case "zhenzhen-video-v31-fast":
				maxImages = 3
			case "zhenzhen-video-v31-quality":
				maxImages = 2
			case "zhenzhen-video-g-omni-flash":
				maxImages = 16
			}
			if maxImages > 0 {
				add("image", "images", "reference", 0, maxImages)
			}
			if capability.Name == "zhenzhen-video-g-omni-flash" {
				add("video", "metadata.video_url", "source", 0, 1)
			}
		default:
			add("image", "images", "reference", 1, 9)
		}
	case TaskKindReference:
		add("image", "images", "reference", 1, 9)
	case TaskKindStartEnd:
		add("image", "images", "start_end", 2, 2)
	case TaskKindVideo, TaskKindEdit, TaskKindMotion, TaskKindLipIdentify:
		add("video", "metadata.video_url", "source", 1, 1)
	case TaskKindElements:
		add("image", "images", "element", 1, 9)
	case TaskKindLipVideo:
		add("audio", "metadata.audio_url", "driver", 1, 1)
	case TaskKindUpscale:
		add("video", "metadata.content", "source", 1, 1)
	case TaskKindMidjourneyVideo:
		add("image", "image_urls", "start_frame", 1, 1)
		add("image", "end_url", "end_frame", 0, 1)
	}
	return media
}

func supportedDurations(capability ModelCapability) GenerationDurationCapability {
	result := GenerationDurationCapability{Values: []int{}}
	if capability.Protocol == ProtocolMidjourneyVideo || capability.Kind == TaskKindUpscale || capability.Name == "zhenzhen-video-g-omni-flash" {
		return result
	}
	result.Supported = true
	result.Min = 1
	result.Max = relaycommon.MaxTaskDurationSeconds
	switch capability.Family {
	case "seedance-2.0":
		result.Min, result.Max = 4, 15
		smart := -1
		result.SmartValue = &smart
	case "seedance-2.5":
		result.Min, result.Max = 4, 30
		smart := -1
		result.SmartValue = &smart
	case "wan-2.7-spicy":
		result.Min, result.Max = 2, 15
	case "happyhorse-1.1":
		result.Min, result.Max = 3, 15
	case "hailuo-2.3":
		result.Min, result.Max = 6, 10
		result.Values = []int{6, 10}
	case "hailuo-h3":
		result.Min, result.Max = 5, 15
	case "flux-3-video":
		result.Min, result.Max = 5, 20
	case "minimax-h3-ow":
		result.Min, result.Max = 5, 15
		result.Values = []int{5, 10, 15}
	case "minimax-h3-context-ir":
		result.Min, result.Max = 4, 15
	case "zhenzhen-video":
		switch capability.Name {
		case "zhenzhen-video-gk-v15":
			result.Min, result.Max = 6, 30
		case "zhenzhen-video-v31-fast", "zhenzhen-video-v31-quality", "zhenzhen-video-v31-lite":
			result.Min, result.Max = 8, 8
			result.Values = []int{8}
		}
	}
	return result
}

func supportedResolutions(capability ModelCapability) []string {
	switch capability.Family {
	case "seedance-2.0":
		values := []string{"480p", "720p", "1080p", "2k", "4k"}
		if strings.Contains(capability.Name, "standard") {
			values = append(values, "native1080p", "native4k")
		}
		return values
	case "seedance-2.5":
		return []string{"480p", "720p", "1080p", "2k", "4k"}
	case "wan-2.7-spicy", "happyhorse-1.1":
		return []string{"720p", "1080p"}
	case "hailuo-h3":
		return []string{"768P", "2K"}
	case "flux-3-video":
		return []string{"hd", "fhd"}
	case "minimax-h3-ow":
		return []string{"480p", "720p"}
	case "zhenzhen-upscaler":
		return []string{"720p", "1080p", "2k", "4k"}
	case "zhenzhen-video":
		switch capability.Name {
		case "zhenzhen-video-gk-v15":
			return []string{"480p", "720p"}
		case "zhenzhen-video-g-omni-flash":
			return []string{"720p"}
		default:
			return []string{"720p", "1080p", "4k"}
		}
	case "midjourney-video":
		return []string{"480p", "720p"}
	default:
		return []string{}
	}
}

func supportedAspectRatios(capability ModelCapability) []string {
	switch capability.Family {
	case "seedance-2.0", "seedance-2.5", "hailuo-h3":
		return []string{"adaptive", "16:9", "4:3", "1:1", "3:4", "9:16", "21:9"}
	case "happyhorse-1.1":
		return []string{"16:9", "4:3", "1:1", "3:4", "9:16", "21:9"}
	case "flux-3-video":
		return []string{"auto", "21:9", "2:1", "16:9", "4:3", "1:1", "3:4", "9:16"}
	case "minimax-h3-ow":
		return []string{"1:1", "2:3", "3:2", "3:4", "4:3", "9:16", "16:9", "21:9"}
	case "vidu-q3":
		return []string{"16:9", "9:16", "1:1", "4:3", "3:4"}
	case "minimax-h3-context-ir":
		values := []string{"21:9", "16:9", "4:3", "1:1", "3:4", "9:16"}
		if capability.Kind == TaskKindContextMulti {
			values = append(values, "adaptive")
		}
		return values
	case "zhenzhen-video":
		if capability.Name == "zhenzhen-video-gk-v15" {
			return []string{"16:9", "9:16", "1:1", "3:2", "2:3"}
		}
		return []string{"16:9", "9:16"}
	default:
		return []string{}
	}
}

func supportedAdvancedFields(capability ModelCapability) []GenerationAdvancedField {
	fields := []GenerationAdvancedField{}
	switch capability.Family {
	case "seedance-2.0", "seedance-2.5":
		minimum, maximum, step := int64(-1), int64(2147483647), int64(1)
		fields = append(fields, GenerationAdvancedField{
			Name: "seed", Path: "metadata.seed", Widget: "number", Minimum: &minimum, Maximum: &maximum, Step: &step,
		})
	case "wan-2.7-spicy":
		fields = append(fields,
			GenerationAdvancedField{Name: "negative_prompt", Path: "metadata.negative_prompt", Widget: "text"},
			GenerationAdvancedField{Name: "prompt_extend", Path: "metadata.prompt_extend", Widget: "boolean", DefaultValue: false},
		)
	case "flux-3-video":
		minimum, maximum, step := int64(0), int64(4), int64(1)
		fields = append(fields,
			GenerationAdvancedField{Name: "draft", Path: "metadata.draft", Widget: "boolean", DefaultValue: false},
			GenerationAdvancedField{
				Name: "safety_tolerance", Path: "metadata.safety_tolerance", Widget: "number",
				Minimum: &minimum, Maximum: &maximum, Step: &step,
			},
		)
	case "vidu-q3":
		if capability.Kind == TaskKindShortPlay {
			fields = append(fields, GenerationAdvancedField{Name: "script_name", Path: "metadata.script_name", Widget: "text"})
		}
	case "zhenzhen-video":
		if capability.Name == "zhenzhen-video-v31-fast" {
			fields = append(fields, GenerationAdvancedField{
				Name: "type", Path: "metadata.type", Widget: "select", Options: []string{"reference"},
			})
		}
		if capability.Name == "zhenzhen-video-g-omni-flash" {
			fields = append(fields, GenerationAdvancedField{
				Name: "extend_from_task_id", Path: "metadata.extend_from_task_id", Widget: "text", Pattern: "^task_",
			})
		}
	case "kling":
		if capability.Kind == TaskKindLipTTS || capability.Kind == TaskKindLipVideo {
			fields = append(fields,
				GenerationAdvancedField{Name: "sessionId", Path: "metadata.sessionId", Widget: "text", Required: true, Pattern: "^artifact_"},
				GenerationAdvancedField{Name: "faceId", Path: "metadata.faceId", Widget: "text", Required: true, Pattern: "^artifact_"},
			)
		}
	case "midjourney-video":
		return []GenerationAdvancedField{
			{
				Name: "video_type", Path: "video_type", Widget: "select",
				Options:      []string{"vid_1.1_i2v_480", "vid_1.1_i2v_720", "vid_1.1_i2v_start_end_480", "vid_1.1_i2v_start_end_720"},
				DefaultValue: "vid_1.1_i2v_480",
			},
			{Name: "animate_mode", Path: "animate_mode", Widget: "select", Options: []string{"manual"}, DefaultValue: "manual"},
			{Name: "motion", Path: "motion", Widget: "select", Options: []string{"low", "high"}, DefaultValue: "low"},
		}
	}
	if capability.Kind == TaskKindDraftEnhance {
		fields = append(fields, GenerationAdvancedField{
			Name: "draft_cache", Path: "metadata.draft_cache", Widget: "text", Required: true, Pattern: "^artifact_",
		})
	}
	return fields
}

func generationRequirements(capability ModelCapability) []string {
	requirements := []string{}
	switch capability.Kind {
	case TaskKindDraftEnhance:
		requirements = append(requirements, "local_draft_artifact")
	case TaskKindLipTTS:
		requirements = append(requirements, "local_lip_session", "local_lip_face")
	case TaskKindLipVideo:
		requirements = append(requirements, "local_lip_session", "local_lip_face")
	}
	if capability.Name == "zhenzhen-video-g-omni-flash" {
		requirements = append(requirements, "prompt_or_media_or_local_task")
	}
	if capability.Kind == TaskKindMulti || capability.Kind == TaskKindContextMulti {
		if capability.Family == "seedance-2.0" || capability.Family == "seedance-2.5" || capability.Family == "hailuo-h3" || capability.Protocol == ProtocolContextIR {
			requirements = append(requirements, "at_least_one_media")
		}
	}
	return requirements
}

func generationDefaults(capability ModelCapability, capabilities GenerationCapabilities) GenerationDefaults {
	defaults := GenerationDefaults{}
	if capabilities.Durations.Supported {
		value := 5
		if len(capabilities.Durations.Values) > 0 {
			value = capabilities.Durations.Values[0]
		} else if value < capabilities.Durations.Min || value > capabilities.Durations.Max {
			value = capabilities.Durations.Min
		}
		defaults.Duration = &value
	}
	if contains(capabilities.Resolutions, "720p") {
		defaults.Resolution = "720p"
	} else if len(capabilities.Resolutions) > 0 {
		defaults.Resolution = capabilities.Resolutions[0]
	}
	if contains(capabilities.AspectRatios, "adaptive") {
		defaults.AspectRatio = "adaptive"
	} else if contains(capabilities.AspectRatios, "16:9") {
		defaults.AspectRatio = "16:9"
	} else if len(capabilities.AspectRatios) > 0 {
		defaults.AspectRatio = capabilities.AspectRatios[0]
	}
	if capabilities.SupportsAudio {
		value := capability.Family == "seedance-2.0" || capability.Family == "seedance-2.5"
		defaults.GenerateAudio = &value
	}
	if capabilities.SupportsLastFrame {
		value := false
		defaults.ReturnLastFrame = &value
	}
	if len(capabilities.BatchSizes) > 0 {
		value := capabilities.BatchSizes[0]
		defaults.BatchSize = &value
	}
	return defaults
}
