package seedance

// VideoRequest is the normalized request accepted by the Seedance main video
// and Context IR protocols. Optional scalar fields are pointers so explicit
// zero and false values survive the upstream JSON round trip.
type VideoRequest struct {
	Model    string         `json:"model"`
	Prompt   *string        `json:"prompt,omitempty"`
	Image    *string        `json:"image,omitempty"`
	Images   []string       `json:"images,omitempty"`
	Seconds  *string        `json:"seconds,omitempty"`
	Duration *int           `json:"duration,omitempty"`
	Metadata *VideoMetadata `json:"metadata,omitempty"`
}

type VideoMetadata struct {
	Resolution       *string        `json:"resolution,omitempty"`
	Ratio            *string        `json:"ratio,omitempty"`
	Seed             *int64         `json:"seed,omitempty"`
	GenerateAudio    *bool          `json:"generate_audio,omitempty"`
	ReturnLastFrame  *bool          `json:"return_last_frame,omitempty"`
	Duration         *int           `json:"duration,omitempty"`
	Content          []MediaContent `json:"content,omitempty"`
	VideoURL         *string        `json:"video_url,omitempty"`
	VideoURLs        []string       `json:"video_urls,omitempty"`
	AudioURL         *string        `json:"audio_url,omitempty"`
	AudioURLs        []string       `json:"audio_urls,omitempty"`
	NegativePrompt   *string        `json:"negative_prompt,omitempty"`
	PromptExtend     *bool          `json:"prompt_extend,omitempty"`
	SafetyTolerance  *int           `json:"safety_tolerance,omitempty"`
	Draft            *bool          `json:"draft,omitempty"`
	DraftCache       *string        `json:"draft_cache,omitempty"`
	ExtendFromTaskID *string        `json:"extend_from_task_id,omitempty"`
	Type             *string        `json:"type,omitempty"`
	ScriptName       *string        `json:"script_name,omitempty"`
	SessionID        *string        `json:"sessionId,omitempty"`
	FaceID           *string        `json:"faceId,omitempty"`
}

type MediaContent struct {
	Type     string    `json:"type"`
	ImageURL *MediaURL `json:"image_url,omitempty"`
	VideoURL *MediaURL `json:"video_url,omitempty"`
	AudioURL *MediaURL `json:"audio_url,omitempty"`
}

type MediaURL struct {
	URL string `json:"url"`
}

type MidjourneyVideoRequest struct {
	Model       string   `json:"model,omitempty"`
	Prompt      *string  `json:"prompt,omitempty"`
	ImageURLs   []string `json:"image_urls,omitempty"`
	TaskID      *string  `json:"task_id,omitempty"`
	Index       *int     `json:"index,omitempty"`
	VideoType   *string  `json:"video_type,omitempty"`
	AnimateMode *string  `json:"animate_mode,omitempty"`
	Motion      *string  `json:"motion,omitempty"`
	BatchSize   *uint    `json:"batch_size,omitempty"`
	EndURL      *string  `json:"end_url,omitempty"`
}

type publicVideoTaskInput struct {
	Model    string                   `json:"model"`
	Prompt   *string                  `json:"prompt,omitempty"`
	Seconds  *string                  `json:"seconds,omitempty"`
	Duration *int                     `json:"duration,omitempty"`
	Metadata *publicVideoTaskMetadata `json:"metadata,omitempty"`
}

type publicVideoTaskMetadata struct {
	Resolution       *string `json:"resolution,omitempty"`
	Ratio            *string `json:"ratio,omitempty"`
	Seed             *int64  `json:"seed,omitempty"`
	GenerateAudio    *bool   `json:"generate_audio,omitempty"`
	ReturnLastFrame  *bool   `json:"return_last_frame,omitempty"`
	Duration         *int    `json:"duration,omitempty"`
	NegativePrompt   *string `json:"negative_prompt,omitempty"`
	PromptExtend     *bool   `json:"prompt_extend,omitempty"`
	SafetyTolerance  *int    `json:"safety_tolerance,omitempty"`
	Draft            *bool   `json:"draft,omitempty"`
	DraftCache       *string `json:"draft_cache,omitempty"`
	ExtendFromTaskID *string `json:"extend_from_task_id,omitempty"`
	Type             *string `json:"type,omitempty"`
	ScriptName       *string `json:"script_name,omitempty"`
	SessionID        *string `json:"sessionId,omitempty"`
	FaceID           *string `json:"faceId,omitempty"`
}

type publicMidjourneyVideoTaskInput struct {
	Model       string  `json:"model,omitempty"`
	Prompt      *string `json:"prompt,omitempty"`
	VideoType   *string `json:"video_type,omitempty"`
	AnimateMode *string `json:"animate_mode,omitempty"`
	Motion      *string `json:"motion,omitempty"`
	BatchSize   *uint   `json:"batch_size,omitempty"`
}

type upstreamErrorEnvelope struct {
	Error   *upstreamError `json:"error,omitempty"`
	Code    any            `json:"code,omitempty"`
	Msg     string         `json:"msg,omitempty"`
	Message string         `json:"message,omitempty"`
}

type upstreamError struct {
	Code    any    `json:"code,omitempty"`
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

type videoTaskResponse struct {
	ID          string             `json:"id,omitempty"`
	TaskID      string             `json:"task_id,omitempty"`
	Object      string             `json:"object,omitempty"`
	Model       string             `json:"model,omitempty"`
	Status      string             `json:"status,omitempty"`
	Progress    int                `json:"progress,omitempty"`
	CreatedAt   int64              `json:"created_at,omitempty"`
	CompletedAt int64              `json:"completed_at,omitempty"`
	ExpiresAt   int64              `json:"expires_at,omitempty"`
	Seconds     string             `json:"seconds,omitempty"`
	Metadata    *videoTaskMetadata `json:"metadata,omitempty"`
	Error       *upstreamError     `json:"error,omitempty"`
}

type videoTaskMetadata struct {
	URL          string `json:"url,omitempty"`
	LastFrameURL string `json:"last_frame_url,omitempty"`
	DraftCache   string `json:"draft_cache,omitempty"`
	SessionID    string `json:"sessionId,omitempty"`
	FaceID       string `json:"faceId,omitempty"`
}

type contextIRTaskResponse struct {
	ID         string                 `json:"id,omitempty"`
	TaskID     string                 `json:"task_id,omitempty"`
	Status     string                 `json:"status,omitempty"`
	Progress   int                    `json:"progress,omitempty"`
	ResultText string                 `json:"result_text,omitempty"`
	Error      *upstreamError         `json:"error,omitempty"`
	Data       *contextIRTaskResponse `json:"data,omitempty"`
}

type midjourneySubmitResponse struct {
	Code    any                     `json:"code,omitempty"`
	Message string                  `json:"message,omitempty"`
	Data    []midjourneySubmitEntry `json:"data,omitempty"`
	Error   *upstreamError          `json:"error,omitempty"`
}

type midjourneySubmitEntry struct {
	Status string `json:"status,omitempty"`
	TaskID string `json:"task_id,omitempty"`
}

type midjourneyTaskResponse struct {
	ID         string                  `json:"id,omitempty"`
	TaskID     string                  `json:"task_id,omitempty"`
	Status     string                  `json:"status,omitempty"`
	Progress   int                     `json:"progress,omitempty"`
	Action     string                  `json:"action,omitempty"`
	Mode       string                  `json:"mode,omitempty"`
	VideoURL   string                  `json:"video_url,omitempty"`
	VideoURLs  []string                `json:"video_urls,omitempty"`
	FailReason string                  `json:"fail_reason,omitempty"`
	Error      *upstreamError          `json:"error,omitempty"`
	Data       *midjourneyTaskResponse `json:"data,omitempty"`
}

// StoredTaskData is safe for the public Task.Data column. It never stores an
// upstream task ID, signed result URL, provider cost, or provider quota.
type StoredTaskData struct {
	Protocol     Protocol          `json:"protocol"`
	Model        string            `json:"model"`
	Status       string            `json:"status"`
	Progress     int               `json:"progress,omitempty"`
	ResultText   string            `json:"result_text,omitempty"`
	VideoCount   int               `json:"video_count,omitempty"`
	VideoURLs    []string          `json:"video_urls,omitempty"`
	ResultAssets map[string]string `json:"result_assets,omitempty"`
	Artifacts    map[string]string `json:"artifacts,omitempty"`
	Error        *publicTaskError  `json:"error,omitempty"`
}

type publicTaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
