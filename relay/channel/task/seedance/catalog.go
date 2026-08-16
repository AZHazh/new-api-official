package seedance

// Protocol identifies the Seedance upstream API used by a catalog entry.
type Protocol string

const (
	ProtocolVideos          Protocol = "videos"
	ProtocolContextIR       Protocol = "context_ir"
	ProtocolMidjourneyVideo Protocol = "midjourney_video"
)

// TaskKind describes the input contract of a model. It is deliberately more
// specific than the public t2v/i2v suffix because several RunningHub models
// have distinct media and continuation requirements.
type TaskKind string

const (
	TaskKindText            TaskKind = "text"
	TaskKindImage           TaskKind = "image"
	TaskKindMulti           TaskKind = "multi"
	TaskKindReference       TaskKind = "reference"
	TaskKindVideo           TaskKind = "video"
	TaskKindDraftEnhance    TaskKind = "draft_enhance"
	TaskKindEdit            TaskKind = "edit"
	TaskKindMotion          TaskKind = "motion"
	TaskKindElements        TaskKind = "elements"
	TaskKindLipIdentify     TaskKind = "lip_identify"
	TaskKindLipTTS          TaskKind = "lip_tts"
	TaskKindLipVideo        TaskKind = "lip_video"
	TaskKindStartEnd        TaskKind = "start_end"
	TaskKindShortPlay       TaskKind = "short_play"
	TaskKindUpscale         TaskKind = "upscale"
	TaskKindContextText     TaskKind = "context_text"
	TaskKindContextImage    TaskKind = "context_image"
	TaskKindContextMulti    TaskKind = "context_multi"
	TaskKindMidjourneyVideo TaskKind = "midjourney_video"
)

// ModelCapability is the local routing and validation source of truth. Prices
// intentionally do not belong here; every exposed model uses the site's
// independently configured ModelPrice.
type ModelCapability struct {
	Name     string
	Family   string
	Protocol Protocol
	Kind     TaskKind
}

type DiscoveredModelCategory string

const (
	DiscoveredModelVideoOutput         DiscoveredModelCategory = "video_output"
	DiscoveredModelVideoPromptEnhancer DiscoveredModelCategory = "video_prompt_enhancer"
	DiscoveredModelMidjourneyVideo     DiscoveredModelCategory = "midjourney_video"
	DiscoveredModelNonVideo            DiscoveredModelCategory = "non_video"
	DiscoveredModelUnknown             DiscoveredModelCategory = "unknown"
)

type DiscoveredModel struct {
	ID         string                  `json:"id"`
	Category   DiscoveredModelCategory `json:"category"`
	Selectable bool                    `json:"selectable"`
}

func videoModel(name, family string, kind TaskKind) ModelCapability {
	return ModelCapability{Name: name, Family: family, Protocol: ProtocolVideos, Kind: kind}
}

func contextIRModel(name string, kind TaskKind) ModelCapability {
	return ModelCapability{Name: name, Family: "minimax-h3-context-ir", Protocol: ProtocolContextIR, Kind: kind}
}

// VideoModelCatalog explicitly lists every /v1/videos model supported by this
// integration. Keep this list explicit so an unknown model discovered from
// /v1/models never becomes routable without an implementation review.
var VideoModelCatalog = []ModelCapability{
	videoModel("seedance-2.0-standard-t2v", "seedance-2.0", TaskKindText),
	videoModel("seedance-2.0-standard-i2v", "seedance-2.0", TaskKindImage),
	videoModel("seedance-2.0-standard-multi", "seedance-2.0", TaskKindMulti),
	videoModel("seedance-2.0-fast-t2v", "seedance-2.0", TaskKindText),
	videoModel("seedance-2.0-fast-i2v", "seedance-2.0", TaskKindImage),
	videoModel("seedance-2.0-fast-multi", "seedance-2.0", TaskKindMulti),
	videoModel("seedance-2.0-mini-t2v", "seedance-2.0", TaskKindText),
	videoModel("seedance-2.0-mini-i2v", "seedance-2.0", TaskKindImage),
	videoModel("seedance-2.0-mini-multi", "seedance-2.0", TaskKindMulti),
	videoModel("seedance-2.0-global-standard-t2v", "seedance-2.0", TaskKindText),
	videoModel("seedance-2.0-global-standard-i2v", "seedance-2.0", TaskKindImage),
	videoModel("seedance-2.0-global-standard-multi", "seedance-2.0", TaskKindMulti),
	videoModel("seedance-2.0-global-fast-t2v", "seedance-2.0", TaskKindText),
	videoModel("seedance-2.0-global-fast-i2v", "seedance-2.0", TaskKindImage),
	videoModel("seedance-2.0-global-fast-multi", "seedance-2.0", TaskKindMulti),
	videoModel("seedance-2.0-global-mini-t2v", "seedance-2.0", TaskKindText),
	videoModel("seedance-2.0-global-mini-i2v", "seedance-2.0", TaskKindImage),
	videoModel("seedance-2.0-global-mini-multi", "seedance-2.0", TaskKindMulti),

	videoModel("seedance-2.5-standard-t2v", "seedance-2.5", TaskKindText),
	videoModel("seedance-2.5-standard-i2v", "seedance-2.5", TaskKindImage),
	videoModel("seedance-2.5-standard-multi", "seedance-2.5", TaskKindMulti),
	videoModel("seedance-2.5-global-standard-t2v", "seedance-2.5", TaskKindText),
	videoModel("seedance-2.5-global-standard-i2v", "seedance-2.5", TaskKindImage),
	videoModel("seedance-2.5-global-standard-multi", "seedance-2.5", TaskKindMulti),

	videoModel("wan-2.7-spicy-i2v", "wan-2.7-spicy", TaskKindImage),
	videoModel("zhenzhen-upscaler", "zhenzhen-upscaler", TaskKindUpscale),
	videoModel("happyhorse-1.1-t2v", "happyhorse-1.1", TaskKindText),
	videoModel("happyhorse-1.1-i2v", "happyhorse-1.1", TaskKindImage),
	videoModel("happyhorse-1.1-r2v", "happyhorse-1.1", TaskKindReference),

	videoModel("kling-v3.0-std-t2v", "kling", TaskKindText),
	videoModel("kling-v3.0-pro-t2v", "kling", TaskKindText),
	videoModel("kling-v3.0-std-i2v", "kling", TaskKindImage),
	videoModel("kling-v3.0-pro-i2v", "kling", TaskKindImage),
	videoModel("kling-v3-turbo-std-t2v", "kling", TaskKindText),
	videoModel("kling-v3-turbo-pro-t2v", "kling", TaskKindText),
	videoModel("kling-v3-turbo-std-i2v", "kling", TaskKindImage),
	videoModel("kling-v3-turbo-pro-i2v", "kling", TaskKindImage),
	videoModel("kling-v3-4k-t2v", "kling", TaskKindText),
	videoModel("kling-v3-4k-i2v", "kling", TaskKindImage),
	videoModel("kling-o3-std-t2v", "kling", TaskKindText),
	videoModel("kling-o3-pro-t2v", "kling", TaskKindText),
	videoModel("kling-o3-std-i2v", "kling", TaskKindImage),
	videoModel("kling-o3-pro-i2v", "kling", TaskKindImage),
	videoModel("kling-o3-std-r2v", "kling", TaskKindReference),
	videoModel("kling-o3-pro-r2v", "kling", TaskKindReference),
	videoModel("kling-o3-std-edit", "kling", TaskKindEdit),
	videoModel("kling-o3-pro-edit", "kling", TaskKindEdit),
	videoModel("kling-o3-4k-t2v", "kling", TaskKindText),
	videoModel("kling-o3-4k-i2v", "kling", TaskKindImage),
	videoModel("kling-o3-4k-r2v", "kling", TaskKindReference),
	videoModel("kling-v3.0-std-motion", "kling", TaskKindMotion),
	videoModel("kling-v3.0-pro-motion", "kling", TaskKindMotion),
	videoModel("kling-v3.0-4k-motion", "kling", TaskKindMotion),
	videoModel("kling-elements-advanced", "kling", TaskKindElements),
	videoModel("kling-lip-sync-identify-face", "kling", TaskKindLipIdentify),
	videoModel("kling-lip-sync-tts", "kling", TaskKindLipTTS),
	videoModel("kling-lip-sync-video", "kling", TaskKindLipVideo),

	videoModel("hailuo-2.3-t2v-standard", "hailuo-2.3", TaskKindText),
	videoModel("hailuo-2.3-t2v-pro", "hailuo-2.3", TaskKindText),
	videoModel("hailuo-2.3-i2v-standard", "hailuo-2.3", TaskKindImage),
	videoModel("hailuo-2.3-i2v-pro", "hailuo-2.3", TaskKindImage),
	videoModel("hailuo-2.3-fast-i2v", "hailuo-2.3", TaskKindImage),
	videoModel("hailuo-2.3-fast-pro-i2v", "hailuo-2.3", TaskKindImage),
	videoModel("hailuo-h3-t2v", "hailuo-h3", TaskKindText),
	videoModel("hailuo-h3-i2v", "hailuo-h3", TaskKindImage),
	videoModel("hailuo-h3-multi", "hailuo-h3", TaskKindMulti),
	videoModel("hailuo-h3-global-t2v", "hailuo-h3", TaskKindText),
	videoModel("hailuo-h3-global-i2v", "hailuo-h3", TaskKindImage),
	videoModel("hailuo-h3-global-multi", "hailuo-h3", TaskKindMulti),

	videoModel("flux-3-video-t2v", "flux-3-video", TaskKindText),
	videoModel("flux-3-video-i2v", "flux-3-video", TaskKindImage),
	videoModel("flux-3-video-v2v", "flux-3-video", TaskKindVideo),
	videoModel("flux-3-video-draft-enhance", "flux-3-video", TaskKindDraftEnhance),
	videoModel("flux-3-video-global-t2v", "flux-3-video", TaskKindText),
	videoModel("flux-3-video-global-i2v", "flux-3-video", TaskKindImage),
	videoModel("flux-3-video-global-v2v", "flux-3-video", TaskKindVideo),
	videoModel("flux-3-video-global-draft-enhance", "flux-3-video", TaskKindDraftEnhance),

	videoModel("minimax-h3-ow-t2v", "minimax-h3-ow", TaskKindText),
	videoModel("minimax-h3-ow-r2v", "minimax-h3-ow", TaskKindReference),
	videoModel("minimax-h3-ow-i2v", "minimax-h3-ow", TaskKindImage),
	videoModel("minimax-h3-ow-r2v-fast", "minimax-h3-ow", TaskKindReference),
	videoModel("minimax-h3-ow-i2v-fast", "minimax-h3-ow", TaskKindImage),
	videoModel("minimax-h3-ow-fl2va-audio-drive-fast", "minimax-h3-ow", TaskKindImage),
	videoModel("minimax-h3-ow-ref2va-audio-drive-fast", "minimax-h3-ow", TaskKindImage),
	videoModel("minimax-h3-ow-t2v-fast", "minimax-h3-ow", TaskKindText),

	videoModel("vidu-q3-pro-t2v", "vidu-q3", TaskKindText),
	videoModel("vidu-q3-turbo-t2v", "vidu-q3", TaskKindText),
	videoModel("vidu-q3-pro-fast-t2v", "vidu-q3", TaskKindText),
	videoModel("vidu-q3-pro-i2v", "vidu-q3", TaskKindImage),
	videoModel("vidu-q3-turbo-i2v", "vidu-q3", TaskKindImage),
	videoModel("vidu-q3-pro-fast-i2v", "vidu-q3", TaskKindImage),
	videoModel("vidu-q3-pro-start-end", "vidu-q3", TaskKindStartEnd),
	videoModel("vidu-q3-turbo-start-end", "vidu-q3", TaskKindStartEnd),
	videoModel("vidu-q3-pro-fast-start-end", "vidu-q3", TaskKindStartEnd),
	videoModel("vidu-q3-r2v", "vidu-q3", TaskKindReference),
	videoModel("vidu-q3-mix-r2v", "vidu-q3", TaskKindReference),
	videoModel("vidu-q3-ad-r2v", "vidu-q3", TaskKindReference),
	videoModel("vidu-q3-drama-r2v", "vidu-q3", TaskKindReference),
	videoModel("vidu-q3-drama-short-play", "vidu-q3", TaskKindShortPlay),
	videoModel("vidu-q3-ad-short-play", "vidu-q3", TaskKindShortPlay),

	videoModel("zhenzhen-video-gk-v15", "zhenzhen-video", TaskKindMulti),
	videoModel("zhenzhen-video-v31-fast", "zhenzhen-video", TaskKindMulti),
	videoModel("zhenzhen-video-v31-quality", "zhenzhen-video", TaskKindMulti),
	videoModel("zhenzhen-video-v31-lite", "zhenzhen-video", TaskKindText),
	videoModel("zhenzhen-video-g-omni-flash", "zhenzhen-video", TaskKindMulti),
}

// ContextIRModelCatalog lists the three video prompt enhancement SKUs. They
// use the legacy /v1/video/generations protocol and return text, not media.
var ContextIRModelCatalog = []ModelCapability{
	contextIRModel("minmax-h3-context-ir-text", TaskKindContextText),
	contextIRModel("minmax-h3-context-ir-image", TaskKindContextImage),
	contextIRModel("minmax-h3-context-ir-multimodal", TaskKindContextMulti),
}

var MidjourneyVideoCapability = ModelCapability{
	Name: "midjourney-video", Family: "midjourney-video", Protocol: ProtocolMidjourneyVideo, Kind: TaskKindMidjourneyVideo,
}

// KnownNonVideoModelList is intentionally limited to IDs documented by the
// same upstream. New IDs stay visible as unknown until their protocol and
// billing boundaries have been reviewed.
var KnownNonVideoModelList = []string{
	"qwen/qwen3.8-max",
	"zhenzhen/gk-4.6",
	"kimi-k3",
	"whisper-1",
	"seedream-v5-pro-t2i",
	"seedream-v5-pro-i2i",
	"seedream-v5-pro-layer-decomposition",
	"dola-seedream-5.0-pro-t2i",
	"dola-seedream-5.0-pro-i2i",
	"dola-seedream-5.0-pro-layer-decomposition",
	"zhenzhen-image-g2-t2i",
	"zhenzhen-image-g2-i2i",
	"qwen-image-3.0-t2i",
	"qwen-image-3.0-i2i",
	"qwen-image-3.0-pro-t2i",
	"qwen-image-3.0-pro-i2i",
	"qwen-image-3.0-global-t2i",
	"qwen-image-3.0-global-i2i",
	"qwen-image-3.0-global-pro-t2i",
	"qwen-image-3.0-global-pro-i2i",
	"wan-2.7-global-t2i",
	"wan-2.7-global-i2i",
	"wan-2.7-global-i2i-pro",
	"zhenzhen-image-g-v2-lowprice",
	"zhenzhen-image-gk-v15",
	"zhenzhen-image-gk-v15-edit",
	"zhenzhen-image-gk-v2",
	"zhenzhen-image-nb-flash",
	"zhenzhen-image-nb-2",
	"zhenzhen-image-nb-2-lite",
	"zhenzhen-image-nb-pro",
	"doubao-seed-audio-1.0",
	"mureka-v8-bgm",
	"mureka-v9-bgm",
	"minimax-speech-2.8-turbo",
	"minimax-speech-2.8-hd",
	"minimax-voice-clone",
	"minimax-music-2.6",
	"mureka-v9-song",
	"mureka-o2-song",
	"qwen3-tts-flash",
	"qwen3-tts-instruct-flash",
}

// ModelList is the stable list exposed through TaskAdaptor.GetModelList.
// Midjourney Video is route-selected and is deliberately not mixed into the
// model-distributed /v1/videos list.
var ModelList = catalogNames(VideoModelCatalog)

var ContextIRModelList = catalogNames(ContextIRModelCatalog)

var AllModelList = append(append([]string{}, ModelList...), ContextIRModelList...)

var capabilityByName = buildCapabilityIndex()

var knownNonVideoModelByName = func() map[string]struct{} {
	models := make(map[string]struct{}, len(KnownNonVideoModelList))
	for _, name := range KnownNonVideoModelList {
		models[name] = struct{}{}
	}
	return models
}()

func catalogNames(catalog []ModelCapability) []string {
	names := make([]string, 0, len(catalog))
	for _, capability := range catalog {
		names = append(names, capability.Name)
	}
	return names
}

func buildCapabilityIndex() map[string]ModelCapability {
	index := make(map[string]ModelCapability, len(VideoModelCatalog)+len(ContextIRModelCatalog)+1)
	for _, capability := range VideoModelCatalog {
		index[capability.Name] = capability
	}
	for _, capability := range ContextIRModelCatalog {
		index[capability.Name] = capability
	}
	index[MidjourneyVideoCapability.Name] = MidjourneyVideoCapability
	return index
}

func CapabilityForModel(model string) (ModelCapability, bool) {
	capability, ok := capabilityByName[model]
	return capability, ok
}

func IsContextIRModel(model string) bool {
	capability, ok := CapabilityForModel(model)
	return ok && capability.Protocol == ProtocolContextIR
}

func ClassifyDiscoveredModel(model string) DiscoveredModel {
	if model == MidjourneyVideoCapability.Name {
		return DiscoveredModel{ID: model, Category: DiscoveredModelMidjourneyVideo, Selectable: true}
	}
	if capability, ok := CapabilityForModel(model); ok {
		category := DiscoveredModelVideoOutput
		if capability.Protocol == ProtocolContextIR {
			category = DiscoveredModelVideoPromptEnhancer
		}
		return DiscoveredModel{ID: model, Category: category, Selectable: true}
	}
	if _, ok := knownNonVideoModelByName[model]; ok {
		return DiscoveredModel{ID: model, Category: DiscoveredModelNonVideo}
	}
	return DiscoveredModel{ID: model, Category: DiscoveredModelUnknown}
}
