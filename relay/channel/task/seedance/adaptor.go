package seedance

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	ChannelName             = "Seedance"
	maximumResponseBodySize = 2 << 20
)

// DependencyResolver translates public, user-owned workflow references into
// private upstream values at the last possible moment. Implementations must
// enforce user ownership, channel affinity, artifact kind, success state, and
// expiry. A nil resolver makes dependent operations fail closed.
type DependencyResolver interface {
	ResolveTaskReference(ctx context.Context, userID, channelID int, localTaskID, purpose string) (string, error)
	ResolveArtifact(ctx context.Context, userID, channelID int, localHandle, kind string) (string, error)
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType        int
	DependencyResolver DependencyResolver
	apiKey             string
	baseURL            string
}

var _ channel.TaskAdaptor = (*TaskAdaptor)(nil)

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	return validateAndStoreRequest(c, info)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if a.baseURL == "" {
		return "", fmt.Errorf("Seedance base URL is empty")
	}
	switch info.Action {
	case ActionContextIR:
		return a.baseURL + "/v1/video/generations", nil
	case ActionMidjourneyVideo:
		return a.baseURL + "/v1/midjourney/generations/video", nil
	case ActionVideo:
		return a.baseURL + "/v1/videos", nil
	default:
		return "", fmt.Errorf("unsupported Seedance task action %q", info.Action)
	}
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, request *http.Request, _ *relaycommon.RelayInfo) error {
	request.Header.Set("Authorization", "Bearer "+a.apiKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	parsed, err := getParsedRequest(c)
	if err != nil {
		return nil, err
	}
	if parsed.Midjourney != nil {
		upstreamModel := strings.TrimSpace(info.UpstreamModelName)
		if upstreamModel == "" {
			upstreamModel = MidjourneyVideoCapability.Name
		}
		if upstreamModel != MidjourneyVideoCapability.Name {
			return nil, fmt.Errorf("mapped upstream model %q is not compatible with %s", upstreamModel, ProtocolMidjourneyVideo)
		}
		requestBody := *parsed.Midjourney
		requestBody.Model = upstreamModel
		if err := validateMidjourneyVideoRequest(&requestBody); err != nil {
			return nil, fmt.Errorf("mapped upstream model validation failed: %w", err)
		}
		body, err := common.Marshal(&requestBody)
		if err != nil {
			return nil, fmt.Errorf("marshal Midjourney Video request: %w", err)
		}
		return bytes.NewReader(body), nil
	}
	if parsed.Video == nil {
		return nil, fmt.Errorf("Seedance video request is missing")
	}

	requestBody, err := cloneVideoRequest(parsed.Video)
	if err != nil {
		return nil, err
	}
	upstreamModel := strings.TrimSpace(info.UpstreamModelName)
	if upstreamModel == "" {
		upstreamModel = parsed.Capability.Name
	}
	upstreamCapability, ok := CapabilityForModel(upstreamModel)
	if !ok || upstreamCapability.Protocol != parsed.Capability.Protocol {
		return nil, fmt.Errorf("mapped upstream model %q is not compatible with %s", upstreamModel, parsed.Capability.Protocol)
	}
	requestBody.Model = upstreamModel
	if err := validateModelRequest(requestBody, upstreamCapability, parsed.MetaFields); err != nil {
		return nil, fmt.Errorf("mapped upstream model validation failed: %w", err)
	}
	if err := a.resolveDependencies(c, info, requestBody, upstreamCapability); err != nil {
		return nil, err
	}

	body, err := common.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal Seedance request: %w", err)
	}
	return bytes.NewReader(body), nil
}

// BuildPublicTaskInput keeps reusable generation settings in task history
// while excluding every uploaded media URL and provider-facing identifier.
func (a *TaskAdaptor) BuildPublicTaskInput(c *gin.Context) (string, error) {
	parsed, err := getParsedRequest(c)
	if err != nil {
		return "", err
	}
	if parsed.Midjourney != nil {
		input := publicMidjourneyVideoTaskInput{
			Model:       parsed.Midjourney.Model,
			Prompt:      parsed.Midjourney.Prompt,
			VideoType:   parsed.Midjourney.VideoType,
			AnimateMode: parsed.Midjourney.AnimateMode,
			Motion:      parsed.Midjourney.Motion,
			BatchSize:   parsed.Midjourney.BatchSize,
		}
		body, marshalErr := common.Marshal(&input)
		if marshalErr != nil {
			return "", fmt.Errorf("marshal public Midjourney Video input: %w", marshalErr)
		}
		return string(body), nil
	}
	if parsed.Video == nil {
		return "", fmt.Errorf("Seedance video request is missing")
	}

	input := publicVideoTaskInput{
		Model:    parsed.Video.Model,
		Prompt:   parsed.Video.Prompt,
		Seconds:  parsed.Video.Seconds,
		Duration: parsed.Video.Duration,
	}
	if metadata := parsed.Video.Metadata; metadata != nil {
		input.Metadata = &publicVideoTaskMetadata{
			Resolution:       metadata.Resolution,
			Ratio:            metadata.Ratio,
			Seed:             metadata.Seed,
			GenerateAudio:    metadata.GenerateAudio,
			ReturnLastFrame:  metadata.ReturnLastFrame,
			Duration:         metadata.Duration,
			NegativePrompt:   metadata.NegativePrompt,
			PromptExtend:     metadata.PromptExtend,
			SafetyTolerance:  metadata.SafetyTolerance,
			Draft:            metadata.Draft,
			DraftCache:       metadata.DraftCache,
			ExtendFromTaskID: metadata.ExtendFromTaskID,
			Type:             metadata.Type,
			ScriptName:       metadata.ScriptName,
			SessionID:        metadata.SessionID,
			FaceID:           metadata.FaceID,
		}
	}
	body, err := common.Marshal(&input)
	if err != nil {
		return "", fmt.Errorf("marshal public Seedance video input: %w", err)
	}
	return string(body), nil
}

func cloneVideoRequest(request *VideoRequest) (*VideoRequest, error) {
	body, err := common.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("clone Seedance request: %w", err)
	}
	var clone VideoRequest
	if err := common.Unmarshal(body, &clone); err != nil {
		return nil, fmt.Errorf("clone Seedance request: %w", err)
	}
	return &clone, nil
}

func (a *TaskAdaptor) resolveDependencies(c *gin.Context, info *relaycommon.RelayInfo, request *VideoRequest, capability ModelCapability) error {
	if request.Metadata == nil {
		return nil
	}
	metadata := request.Metadata
	needsResolver := metadata.ExtendFromTaskID != nil || metadata.DraftCache != nil || metadata.SessionID != nil || metadata.FaceID != nil
	if !needsResolver {
		return nil
	}
	if a.DependencyResolver == nil {
		return fmt.Errorf("model %s requires the Seedance workflow dependency resolver", capability.Name)
	}
	ctx := c.Request.Context()
	if metadata.ExtendFromTaskID != nil {
		resolved, err := a.DependencyResolver.ResolveTaskReference(ctx, info.UserId, info.ChannelId, *metadata.ExtendFromTaskID, "zhenzhen_video_extend")
		if err != nil {
			return fmt.Errorf("resolve extend_from_task_id: %w", err)
		}
		metadata.ExtendFromTaskID = &resolved
	}
	if metadata.DraftCache != nil {
		resolved, err := a.DependencyResolver.ResolveArtifact(ctx, info.UserId, info.ChannelId, *metadata.DraftCache, "flux_draft_cache")
		if err != nil {
			return fmt.Errorf("resolve draft_cache: %w", err)
		}
		metadata.DraftCache = &resolved
	}
	if metadata.SessionID != nil {
		resolved, err := a.DependencyResolver.ResolveArtifact(ctx, info.UserId, info.ChannelId, *metadata.SessionID, "kling_lip_session")
		if err != nil {
			return fmt.Errorf("resolve sessionId: %w", err)
		}
		metadata.SessionID = &resolved
	}
	if metadata.FaceID != nil {
		resolved, err := a.DependencyResolver.ResolveArtifact(ctx, info.UserId, info.ChannelId, *metadata.FaceID, "kling_lip_face")
		if err != nil {
			return fmt.Errorf("resolve faceId: %w", err)
		}
		metadata.FaceID = &resolved
	}
	return nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse only parses the upstream response. It intentionally does not
// write gin.Context.Writer; the controller must persist the local task and its
// billing state before calling BuildSubmitResponse.
func (a *TaskAdaptor) DoResponse(_ *gin.Context, response *http.Response, info *relaycommon.RelayInfo) (string, []byte, *taskdto.TaskError) {
	if response == nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("empty Seedance response"), "empty_upstream_response", http.StatusBadGateway)
	}
	body, err := readLimitedBody(response.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusBadGateway)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", nil, upstreamTaskError(response.StatusCode, body)
	}

	var upstreamTaskID string
	switch info.Action {
	case ActionVideo:
		var task videoTaskResponse
		if err := common.Unmarshal(body, &task); err != nil {
			return "", nil, service.TaskErrorWrapper(err, "invalid_upstream_response", http.StatusBadGateway)
		}
		upstreamTaskID = firstNonEmpty(task.ID, task.TaskID)
	case ActionContextIR:
		var task contextIRTaskResponse
		if err := common.Unmarshal(body, &task); err != nil {
			return "", nil, service.TaskErrorWrapper(err, "invalid_upstream_response", http.StatusBadGateway)
		}
		if task.Data != nil {
			task = *task.Data
		}
		upstreamTaskID = firstNonEmpty(task.TaskID, task.ID)
	case ActionMidjourneyVideo:
		var task midjourneySubmitResponse
		if err := common.Unmarshal(body, &task); err != nil {
			return "", nil, service.TaskErrorWrapper(err, "invalid_upstream_response", http.StatusBadGateway)
		}
		if task.Error != nil {
			return "", nil, service.TaskErrorWrapper(fmt.Errorf("%s", safeUpstreamMessage(task.Error.Message)), "upstream_submit_failed", http.StatusBadGateway)
		}
		if len(task.Data) == 1 {
			upstreamTaskID = task.Data[0].TaskID
		}
	default:
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("unsupported Seedance task action %q", info.Action), "invalid_task_action", http.StatusInternalServerError)
	}
	if strings.TrimSpace(upstreamTaskID) == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("Seedance response did not contain a task ID"), "invalid_upstream_response", http.StatusBadGateway)
	}

	protocol := protocolForAction(info.Action)
	videoCount := 0
	if protocol == ProtocolMidjourneyVideo {
		videoCount = 1
		if ratios := info.PriceData.OtherRatios(); ratios != nil {
			if batchSize := int(ratios["batch_size"]); batchSize == 2 || batchSize == 4 {
				videoCount = batchSize
			}
		}
	}
	safeData, err := common.Marshal(StoredTaskData{
		Protocol:   protocol,
		Model:      info.OriginModelName,
		Status:     initialStatusForProtocol(protocol),
		VideoCount: videoCount,
	})
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "marshal_task_data_failed", http.StatusInternalServerError)
	}
	return upstreamTaskID, safeData, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	action, _ := body["action"].(string)
	path := "/v1/videos/"
	switch action {
	case ActionContextIR:
		path = "/v1/video/generations/"
	case ActionMidjourneyVideo:
		path = "/v1/midjourney/tasks/"
	case ActionVideo:
	default:
		return nil, fmt.Errorf("unsupported Seedance polling action %q", action)
	}
	requestContext, _ := body["context"].(context.Context)
	if requestContext == nil {
		requestContext = context.Background()
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, strings.TrimRight(baseURL, "/")+path+url.PathEscape(taskID), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("create proxy HTTP client: %w", err)
	}
	return client.Do(request)
}

func (a *TaskAdaptor) GetModelList() []string {
	models := make([]string, len(AllModelList))
	copy(models, AllModelList)
	return models
}

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func readLimitedBody(reader io.ReadCloser) ([]byte, error) {
	defer reader.Close()
	limited := io.LimitReader(reader, maximumResponseBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(body) > maximumResponseBodySize {
		return nil, fmt.Errorf("Seedance response exceeds %d bytes", maximumResponseBodySize)
	}
	return body, nil
}

func upstreamTaskError(status int, body []byte) *taskdto.TaskError {
	message := http.StatusText(status)
	code := "upstream_request_failed"
	var envelope upstreamErrorEnvelope
	if err := common.Unmarshal(body, &envelope); err == nil {
		if envelope.Error != nil {
			message = firstNonEmpty(envelope.Error.Message, message)
			code = firstNonEmpty(envelope.Error.Type, stringifyCode(envelope.Error.Code), code)
		} else {
			message = firstNonEmpty(envelope.Message, envelope.Msg, message)
			code = firstNonEmpty(stringifyCode(envelope.Code), code)
		}
	}
	message = safeUpstreamMessage(message)
	if status == http.StatusPaymentRequired {
		return service.TaskErrorWrapper(fmt.Errorf("Seedance upstream account balance is insufficient"), "upstream_balance_insufficient", http.StatusBadGateway)
	}
	return service.TaskErrorWrapper(fmt.Errorf("%s", message), code, status)
}

func stringifyCode(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func safeUpstreamMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "Seedance upstream request failed"
	}
	const maxLength = 512
	if len(message) > maxLength {
		message = message[:maxLength]
	}
	return common.MaskSensitiveInfo(message)
}

func protocolForAction(action string) Protocol {
	switch action {
	case ActionContextIR:
		return ProtocolContextIR
	case ActionMidjourneyVideo:
		return ProtocolMidjourneyVideo
	default:
		return ProtocolVideos
	}
}

func initialStatusForProtocol(protocol Protocol) string {
	if protocol == ProtocolMidjourneyVideo || protocol == ProtocolContextIR {
		return "SUBMITTED"
	}
	return "queued"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
