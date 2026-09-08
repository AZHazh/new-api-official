package seedance

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaykitdto "github.com/QuantumNous/new-api/relaykit/dto"
)

const maxPublicTaskVideoResults = 4

// TaskResultDetails contains the public-safe data and private result material
// produced by a polling response. ResultURLs and ResultAssets must only be
// persisted in private task storage; SafeData may be returned to users.
type TaskResultDetails struct {
	Info         *relaycommon.TaskInfo
	SafeData     []byte
	ResultURLs   []string
	ResultAssets map[string]string
	Artifacts    map[string]string
}

func (a *TaskAdaptor) ParseTaskResult(responseBody []byte) (*relaycommon.TaskInfo, error) {
	details, err := a.ParseTaskResultDetails(responseBody, "")
	if err != nil {
		return nil, err
	}
	return details.Info, nil
}

// ParseTaskResultDetails is the lossless polling parser used by Seedance-aware
// polling. publicTaskID may be empty when only TaskInfo is needed.
func (a *TaskAdaptor) ParseTaskResultDetails(responseBody []byte, publicTaskID string) (*TaskResultDetails, error) {
	var shape map[string]any
	if err := common.Unmarshal(responseBody, &shape); err != nil {
		return nil, fmt.Errorf("unmarshal Seedance task result: %w", err)
	}
	if err := pollingEnvelopeError(shape); err != nil {
		return nil, err
	}
	if looksLikeContextIR(shape) {
		return parseContextIRResult(responseBody)
	}
	if looksLikeMidjourneyVideo(shape) {
		return parseMidjourneyResult(responseBody, publicTaskID)
	}
	return parseVideoResult(responseBody, publicTaskID)
}

func pollingEnvelopeError(shape map[string]any) error {
	if _, hasStatus := shape["status"]; hasStatus {
		return nil
	}
	if data, ok := shape["data"].(map[string]any); ok {
		if _, hasStatus := data["status"]; hasStatus {
			return nil
		}
	}
	errorObject, ok := shape["error"].(map[string]any)
	if !ok {
		return nil
	}
	message, _ := errorObject["message"].(string)
	return fmt.Errorf("Seedance polling error: %s", safeUpstreamMessage(message))
}

// ParseTaskResultDetailsForAction avoids shape inference when the polling
// caller already has the durable local task action.
func (a *TaskAdaptor) ParseTaskResultDetailsForAction(responseBody []byte, publicTaskID, action string) (*TaskResultDetails, error) {
	var shape map[string]any
	if err := common.Unmarshal(responseBody, &shape); err != nil {
		return nil, fmt.Errorf("unmarshal Seedance task result: %w", err)
	}
	if err := pollingEnvelopeError(shape); err != nil {
		return nil, err
	}
	switch action {
	case ActionVideo:
		return parseVideoResult(responseBody, publicTaskID)
	case ActionContextIR:
		return parseContextIRResult(responseBody)
	case ActionMidjourneyVideo:
		return parseMidjourneyResult(responseBody, publicTaskID)
	default:
		return nil, fmt.Errorf("unsupported Seedance polling action %q", action)
	}
}

// ParseTaskResultForTask exposes a package-neutral optional polling contract.
// service/task_polling.go can type-assert this exact method without importing
// the Seedance package (which would create an import cycle).
func (a *TaskAdaptor) ParseTaskResultForTask(responseBody []byte, task *model.Task) (
	info *relaycommon.TaskInfo,
	safeData []byte,
	resultURLs []string,
	resultAssets map[string]string,
	artifacts map[string]string,
	err error,
) {
	if task == nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("task is nil")
	}
	details, err := a.ParseTaskResultDetailsForAction(responseBody, task.TaskID, task.Action)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return details.Info, details.SafeData, details.ResultURLs, details.ResultAssets, details.Artifacts, nil
}

func parseVideoResult(responseBody []byte, publicTaskID string) (*TaskResultDetails, error) {
	var response videoTaskResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("unmarshal Seedance video result: %w", err)
	}
	info := &relaycommon.TaskInfo{TaskID: firstNonEmpty(response.ID, response.TaskID)}
	setMappedStatus(info, response.Status, response.Error)
	setProgress(info, response.Progress)

	details := &TaskResultDetails{Info: info}
	stored := StoredTaskData{Protocol: ProtocolVideos, Model: response.Model, Status: publicStatus(info.Status), Progress: response.Progress}
	if info.Status == string(model.TaskStatusSuccess) {
		if response.Metadata == nil {
			return nil, fmt.Errorf("completed Seedance video response is missing metadata")
		}
		if response.Metadata.URL == "" {
			return nil, fmt.Errorf("completed Seedance video response is missing metadata.url")
		}
		if err := validateMediaURL(response.Metadata.URL); err != nil {
			return nil, fmt.Errorf("invalid Seedance result URL: %w", err)
		}
		info.Url = response.Metadata.URL
		details.ResultURLs = []string{response.Metadata.URL}
		stored.VideoCount = 1
		if response.Metadata.LastFrameURL != "" {
			if err := validateMediaURL(response.Metadata.LastFrameURL); err != nil {
				return nil, fmt.Errorf("invalid Seedance last-frame URL: %w", err)
			}
			details.ResultAssets = map[string]string{"last_frame": response.Metadata.LastFrameURL}
		}
		if response.Metadata.DraftCache != "" {
			details.Artifacts = map[string]string{"flux_draft_cache": response.Metadata.DraftCache}
		}
		if response.Metadata.SessionID != "" {
			if details.Artifacts == nil {
				details.Artifacts = map[string]string{}
			}
			details.Artifacts["kling_lip_session"] = response.Metadata.SessionID
		}
		if response.Metadata.FaceID != "" {
			if details.Artifacts == nil {
				details.Artifacts = map[string]string{}
			}
			details.Artifacts["kling_lip_face"] = response.Metadata.FaceID
		}
		if publicTaskID != "" {
			stored.VideoURLs = []string{taskcommon.BuildProxyURL(publicTaskID)}
		}
	}
	if info.Status == string(model.TaskStatusFailure) {
		stored.Error = &publicTaskError{Code: "video_generation_failed", Message: info.Reason}
		if response.Error != nil {
			stored.Error.Code = firstNonEmpty(stringifyCode(response.Error.Code), response.Error.Type, stored.Error.Code)
		}
	}
	data, err := common.Marshal(stored)
	if err != nil {
		return nil, err
	}
	if publicTaskID != "" && len(details.ResultAssets) > 0 {
		var safeData map[string]any
		if err := common.Unmarshal(data, &safeData); err != nil {
			return nil, err
		}
		safeData["last_frame_url"] = taskcommon.BuildProxyURL(publicTaskID) + "?asset=last_frame"
		data, err = common.Marshal(safeData)
		if err != nil {
			return nil, err
		}
	}
	details.SafeData = data
	return details, nil
}

func parseContextIRResult(responseBody []byte) (*TaskResultDetails, error) {
	var response contextIRTaskResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("unmarshal Context IR result: %w", err)
	}
	if response.Data != nil {
		response = *response.Data
	}
	info := &relaycommon.TaskInfo{TaskID: firstNonEmpty(response.TaskID, response.ID)}
	setMappedStatus(info, response.Status, response.Error)
	setProgress(info, response.Progress)
	if info.Status == string(model.TaskStatusSuccess) && strings.TrimSpace(response.ResultText) == "" {
		return nil, fmt.Errorf("successful Context IR response is missing result_text")
	}
	stored := StoredTaskData{
		Protocol:   ProtocolContextIR,
		Status:     publicStatus(info.Status),
		Progress:   response.Progress,
		ResultText: response.ResultText,
	}
	if info.Status == string(model.TaskStatusFailure) {
		stored.Error = &publicTaskError{Code: "context_ir_failed", Message: info.Reason}
	}
	data, err := common.Marshal(stored)
	if err != nil {
		return nil, err
	}
	return &TaskResultDetails{Info: info, SafeData: data}, nil
}

func parseMidjourneyResult(responseBody []byte, publicTaskID string) (*TaskResultDetails, error) {
	var response midjourneyTaskResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("unmarshal Midjourney Video result: %w", err)
	}
	if response.Data != nil {
		response = *response.Data
	}
	info := &relaycommon.TaskInfo{TaskID: firstNonEmpty(response.TaskID, response.ID)}
	setMappedStatus(info, response.Status, response.Error)
	setProgress(info, response.Progress)
	if info.Status == string(model.TaskStatusFailure) && info.Reason == "task failed" && response.FailReason != "" {
		info.Reason = safeUpstreamMessage(response.FailReason)
	}

	resultURLs := append([]string{}, response.VideoURLs...)
	if len(resultURLs) == 0 && response.VideoURL != "" {
		resultURLs = []string{response.VideoURL}
	}
	if len(resultURLs) > maxPublicTaskVideoResults {
		return nil, fmt.Errorf("Midjourney Video response has too many results")
	}
	if info.Status == string(model.TaskStatusSuccess) {
		if len(resultURLs) == 0 {
			return nil, fmt.Errorf("successful Midjourney Video response is missing video_urls")
		}
		for _, resultURL := range resultURLs {
			if err := validateMediaURL(resultURL); err != nil {
				return nil, fmt.Errorf("invalid Midjourney result URL: %w", err)
			}
		}
		info.Url = resultURLs[0]
	}

	stored := StoredTaskData{Protocol: ProtocolMidjourneyVideo, Status: publicStatus(info.Status), Progress: response.Progress, VideoCount: len(resultURLs)}
	if publicTaskID != "" && info.Status == string(model.TaskStatusSuccess) {
		stored.VideoURLs = make([]string, len(resultURLs))
		for index := range resultURLs {
			stored.VideoURLs[index] = taskcommon.BuildProxyURL(publicTaskID) + "?index=" + strconv.Itoa(index)
		}
	}
	if info.Status == string(model.TaskStatusFailure) {
		stored.Error = &publicTaskError{Code: "midjourney_video_failed", Message: info.Reason}
	}
	data, err := common.Marshal(stored)
	if err != nil {
		return nil, err
	}
	return &TaskResultDetails{Info: info, SafeData: data, ResultURLs: resultURLs}, nil
}

func looksLikeContextIR(shape map[string]any) bool {
	if _, ok := shape["result_text"]; ok {
		return true
	}
	if data, ok := shape["data"].(map[string]any); ok {
		if _, ok = data["result_text"]; ok {
			return true
		}
		_, hasStatus := data["status"]
		_, hasVideoURL := data["video_url"]
		_, hasVideoURLs := data["video_urls"]
		return hasStatus && !hasVideoURL && !hasVideoURLs
	}
	return false
}

func looksLikeMidjourneyVideo(shape map[string]any) bool {
	if _, ok := shape["video_url"]; ok {
		return true
	}
	if _, ok := shape["video_urls"]; ok {
		return true
	}
	if action, _ := shape["action"].(string); strings.EqualFold(action, "VIDEO") {
		return true
	}
	if data, ok := shape["data"].(map[string]any); ok {
		return looksLikeMidjourneyVideo(data)
	}
	return false
}

func setMappedStatus(info *relaycommon.TaskInfo, upstreamStatus string, upstreamError *upstreamError) {
	switch strings.ToLower(strings.TrimSpace(upstreamStatus)) {
	case "submitted":
		info.Status = string(model.TaskStatusSubmitted)
	case "queued", "queue", "pending", "waiting":
		info.Status = string(model.TaskStatusQueued)
	case "processing", "in_progress", "running":
		info.Status = string(model.TaskStatusInProgress)
	case "completed", "success", "succeeded":
		info.Status = string(model.TaskStatusSuccess)
	case "failed", "failure", "cancelled", "canceled":
		info.Status = string(model.TaskStatusFailure)
		info.Reason = "task failed"
		if upstreamError != nil {
			info.Reason = safeUpstreamMessage(upstreamError.Message)
		}
	default:
		info.Status = ""
	}
}

func setProgress(info *relaycommon.TaskInfo, progress int) {
	if progress < 0 || progress > 100 {
		return
	}
	if progress > 0 {
		info.Progress = strconv.Itoa(progress) + "%"
	}
}

func publicStatus(status string) string {
	switch model.TaskStatus(status) {
	case model.TaskStatusNotStart:
		return "queued"
	case model.TaskStatusSubmitted:
		return "submitted"
	case model.TaskStatusQueued:
		return "queued"
	case model.TaskStatusInProgress:
		return "in_progress"
	case model.TaskStatusSuccess:
		return "completed"
	case model.TaskStatusFailure:
		return "failed"
	default:
		return "unknown"
	}
}

// BuildSubmitResponse creates the response only after the controller has
// durably inserted the local task. It never includes the upstream task ID.
func (a *TaskAdaptor) BuildSubmitResponse(task *model.Task) (int, []byte, error) {
	if task == nil {
		return 0, nil, fmt.Errorf("task is nil")
	}
	switch task.Action {
	case ActionContextIR:
		body, err := common.Marshal(map[string]any{
			"code": "success", "message": "ok", "data": map[string]any{"task_id": task.TaskID, "status": "SUBMITTED"},
		})
		return http.StatusOK, body, err
	case ActionMidjourneyVideo:
		body, err := common.Marshal(map[string]any{
			"code": 200,
			"data": []map[string]any{{"status": "submitted", "task_id": task.TaskID}},
		})
		return http.StatusOK, body, err
	case ActionVideo:
		body, err := buildOpenAIVideoResponse(task)
		return http.StatusOK, body, err
	default:
		return 0, nil, fmt.Errorf("unsupported Seedance task action %q", task.Action)
	}
}

// ConvertTaskResponse returns a protocol-specific response from local task
// state. User polling never calls the Seedance upstream directly.
func (a *TaskAdaptor) ConvertTaskResponse(task *model.Task, requestPath string) ([]byte, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}
	switch task.Action {
	case ActionContextIR:
		return buildLegacyTaskResponse(task, true)
	case ActionMidjourneyVideo:
		return buildMidjourneyTaskResponse(task)
	case ActionVideo:
		if strings.HasPrefix(requestPath, "/v1/videos/") {
			return buildOpenAIVideoResponse(task)
		}
		return buildLegacyTaskResponse(task, false)
	default:
		return nil, fmt.Errorf("unsupported Seedance task action %q", task.Action)
	}
}

// ConvertToOpenAIVideo preserves compatibility with the existing query path.
func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	return buildOpenAIVideoResponse(task)
}

func buildOpenAIVideoResponse(task *model.Task) ([]byte, error) {
	response := relaykitdto.NewOpenAIVideo()
	response.ID = task.TaskID
	response.TaskID = task.TaskID
	response.Model = firstNonEmpty(task.Properties.OriginModelName, task.Properties.UpstreamModelName)
	response.Status = task.Status.ToVideoStatus()
	if task.Status == model.TaskStatusNotStart || task.Status == model.TaskStatusSubmitted {
		response.Status = relaykitdto.VideoStatusQueued
	}
	response.CreatedAt = task.SubmitTime
	response.CompletedAt = task.FinishTime
	response.ExpiresAt = task.ResultExpiresAt
	response.SetProgressStr(task.Progress)
	if task.Status == model.TaskStatusSuccess {
		stored := storedTaskData(task)
		videoURLs := publicTaskVideoURLs(task.TaskID, stored, 1)
		response.SetMetadata("url", videoURLs[0])
		if len(videoURLs) > 1 {
			response.SetMetadata("video_urls", videoURLs)
		}
		if lastFrameURL := publicTaskLastFrameURL(task.TaskID, stored); lastFrameURL != "" {
			response.SetMetadata("last_frame_url", lastFrameURL)
		}
		artifacts := publicTaskArtifacts(stored)
		if draftCache := artifacts[model.TaskArtifactFluxDraftCache]; draftCache != "" {
			response.SetMetadata("draft_cache", draftCache)
		}
		if sessionID := artifacts[model.TaskArtifactKlingSession]; sessionID != "" {
			response.SetMetadata("sessionId", sessionID)
		}
		if faceID := artifacts[model.TaskArtifactKlingFace]; faceID != "" {
			response.SetMetadata("faceId", faceID)
		}
	}
	if task.Status == model.TaskStatusFailure {
		response.Error = &relaykitdto.OpenAIVideoError{Code: "video_generation_failed", Message: task.FailReason}
	}
	return common.Marshal(response)
}

func buildLegacyTaskResponse(task *model.Task, includeText bool) ([]byte, error) {
	stored := storedTaskData(task)
	data := map[string]any{
		"task_id":  task.TaskID,
		"status":   legacyStatus(task.Status),
		"progress": task.Progress,
	}
	if task.Status == model.TaskStatusSuccess {
		if includeText {
			data["result_text"] = stored.ResultText
		} else {
			videoURLs := publicTaskVideoURLs(task.TaskID, stored, 1)
			data["result_url"] = videoURLs[0]
			data["video_urls"] = videoURLs
			if lastFrameURL := publicTaskLastFrameURL(task.TaskID, stored); lastFrameURL != "" {
				data["last_frame_url"] = lastFrameURL
				data["result_assets"] = map[string]string{"last_frame": lastFrameURL}
			}
			artifacts := publicTaskArtifacts(stored)
			if len(artifacts) > 0 {
				data["artifacts"] = artifacts
				if draftCache := artifacts[model.TaskArtifactFluxDraftCache]; draftCache != "" {
					data["draft_cache"] = draftCache
				}
				if sessionID := artifacts[model.TaskArtifactKlingSession]; sessionID != "" {
					data["sessionId"] = sessionID
				}
				if faceID := artifacts[model.TaskArtifactKlingFace]; faceID != "" {
					data["faceId"] = faceID
				}
			}
		}
		if task.ResultExpiresAt > 0 {
			data["expires_at"] = task.ResultExpiresAt
		}
	}
	if task.Status == model.TaskStatusFailure {
		data["error"] = map[string]any{"code": "task_failed", "message": task.FailReason}
	}
	return common.Marshal(map[string]any{"code": "success", "data": data})
}

// BuildPublicVideoTaskHistory returns the dashboard-safe result URL and task
// data for a Seedance video task. It always rebuilds media URLs from the
// public task ID, including when legacy task data contains upstream URLs.
func BuildPublicVideoTaskHistory(task *model.Task) (string, []byte, error) {
	if task == nil {
		return "", nil, fmt.Errorf("task is nil")
	}
	stored := storedTaskData(task)
	data := map[string]any{
		"task_id":  task.TaskID,
		"status":   legacyStatus(task.Status),
		"progress": task.Progress,
	}
	resultURL := ""
	if task.Status == model.TaskStatusSuccess {
		videoURLs := publicTaskVideoURLs(task.TaskID, stored, 1)
		resultURL = videoURLs[0]
		data["result_url"] = resultURL
		data["video_urls"] = videoURLs
		if lastFrameURL := publicTaskLastFrameURL(task.TaskID, stored); lastFrameURL != "" {
			data["last_frame_url"] = lastFrameURL
		}
		if task.ResultExpiresAt > 0 {
			data["expires_at"] = task.ResultExpiresAt
		}
	}
	if task.Status == model.TaskStatusFailure {
		data["error"] = map[string]any{"code": "task_failed", "message": task.FailReason}
	}
	publicData, err := common.Marshal(data)
	if err != nil {
		return "", nil, err
	}
	return resultURL, publicData, nil
}

func buildMidjourneyTaskResponse(task *model.Task) ([]byte, error) {
	stored := storedTaskData(task)
	response := map[string]any{
		"id":      task.TaskID,
		"task_id": task.TaskID,
		"status":  legacyStatus(task.Status),
		"action":  "VIDEO",
		"mode":    "FAST",
	}
	if task.Status == model.TaskStatusSuccess {
		urls := publicTaskVideoURLs(task.TaskID, stored, 1)
		response["video_url"] = urls[0]
		response["video_urls"] = urls
		if task.ResultExpiresAt > 0 {
			response["expires_at"] = task.ResultExpiresAt
		}
	}
	if task.Status == model.TaskStatusFailure {
		response["fail_reason"] = task.FailReason
	}
	return common.Marshal(response)
}

func storedTaskData(task *model.Task) StoredTaskData {
	var stored StoredTaskData
	if len(task.Data) != 0 {
		_ = common.Unmarshal(task.Data, &stored)
	}
	return stored
}

func publicTaskVideoURLs(taskID string, stored StoredTaskData, minimumCount int) []string {
	count := stored.VideoCount
	if len(stored.VideoURLs) > count {
		count = len(stored.VideoURLs)
	}
	if count < minimumCount {
		count = minimumCount
	}
	if count > maxPublicTaskVideoResults {
		count = maxPublicTaskVideoResults
	}
	urls := make([]string, count)
	indexed := stored.Protocol == ProtocolMidjourneyVideo || count > 1
	for index := range urls {
		urls[index] = taskcommon.BuildProxyURL(taskID)
		if indexed {
			urls[index] += "?index=" + strconv.Itoa(index)
		}
	}
	return urls
}

func publicTaskLastFrameURL(taskID string, stored StoredTaskData) string {
	if strings.TrimSpace(stored.ResultAssets["last_frame"]) == "" {
		return ""
	}
	return taskcommon.BuildProxyURL(taskID) + "?asset=last_frame"
}

func publicTaskArtifacts(stored StoredTaskData) map[string]string {
	public := make(map[string]string)
	for _, kind := range []string{model.TaskArtifactFluxDraftCache, model.TaskArtifactKlingSession, model.TaskArtifactKlingFace} {
		handle := stored.Artifacts[kind]
		if model.IsTaskArtifactHandle(handle) {
			public[kind] = handle
		}
	}
	return public
}

func legacyStatus(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusNotStart, model.TaskStatusSubmitted, model.TaskStatusQueued:
		return "SUBMITTED"
	case model.TaskStatusInProgress:
		return "PROCESSING"
	case model.TaskStatusSuccess:
		return "SUCCESS"
	case model.TaskStatusFailure, model.TaskStatusSubmitUnknown:
		return "FAILURE"
	default:
		return "UNKNOWN"
	}
}

// ResultExpiry returns an expiry estimate from common signed URL parameters.
// The caller should fall back to 24 hours when the result is zero.
func ResultExpiry(rawURL string) int64 {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	query := parsed.Query()
	for _, key := range []string{"Expires", "expires"} {
		value := query.Get(key)
		if value == "" {
			continue
		}
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err == nil && seconds > 1000000000 {
			return seconds
		}
	}
	for _, pair := range [][2]string{{"X-Amz-Date", "X-Amz-Expires"}, {"X-Tos-Date", "X-Tos-Expires"}} {
		issuedAt, dateErr := time.Parse("20060102T150405Z", query.Get(pair[0]))
		duration, durationErr := strconv.ParseInt(query.Get(pair[1]), 10, 64)
		if dateErr == nil && durationErr == nil && duration > 0 {
			return issuedAt.Unix() + duration
		}
	}
	return 0
}
