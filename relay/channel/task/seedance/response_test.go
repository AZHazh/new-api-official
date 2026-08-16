package seedance

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func upstreamResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestDoResponseDefersClientWriteAndStoresOnlySafeData(t *testing.T) {
	context, recorder := newTaskContext(http.MethodPost, "/v1/videos", `{}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-2.0-mini-t2v",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: ActionVideo, PublicTaskID: "task_public"},
	}
	adaptor := &TaskAdaptor{}
	upstreamID, taskData, taskErr := adaptor.DoResponse(context, upstreamResponse(http.StatusAccepted, `{"id":"upstream-secret","status":"queued","cost":99}`), info)
	require.Nil(t, taskErr)
	assert.Equal(t, "upstream-secret", upstreamID)
	assert.Empty(t, recorder.Body.String())
	assert.NotContains(t, string(taskData), "upstream-secret")
	assert.NotContains(t, string(taskData), "cost")

	var stored StoredTaskData
	require.NoError(t, common.Unmarshal(taskData, &stored))
	assert.Equal(t, ProtocolVideos, stored.Protocol)
	assert.Equal(t, "seedance-2.0-mini-t2v", stored.Model)
}

func TestDoResponseMapsUpstream402ToGatewayFailure(t *testing.T) {
	context, _ := newTaskContext(http.MethodPost, "/v1/videos", `{}`)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: ActionVideo}}
	_, _, taskErr := (&TaskAdaptor{}).DoResponse(context, upstreamResponse(http.StatusPaymentRequired, `{"error":{"type":"payment_required","message":"account balance insufficient"}}`), info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadGateway, taskErr.StatusCode)
	assert.Equal(t, "upstream_balance_insufficient", taskErr.Code)
}

func TestDoResponseParsesAllSubmitProtocols(t *testing.T) {
	tests := []struct {
		name     string
		action   string
		body     string
		wantID   string
		protocol Protocol
	}{
		{
			name:     "main videos",
			action:   ActionVideo,
			body:     `{"id":"video-upstream","status":"queued"}`,
			wantID:   "video-upstream",
			protocol: ProtocolVideos,
		},
		{
			name:     "Context IR nested data",
			action:   ActionContextIR,
			body:     `{"code":true,"data":{"task_id":"context-upstream","status":"SUBMITTED"}}`,
			wantID:   "context-upstream",
			protocol: ProtocolContextIR,
		},
		{
			name:     "Midjourney Video",
			action:   ActionMidjourneyVideo,
			body:     `{"code":200,"data":[{"status":"submitted","task_id":"mj-upstream"}]}`,
			wantID:   "mj-upstream",
			protocol: ProtocolMidjourneyVideo,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				OriginModelName: "model",
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: test.action, PublicTaskID: "task_public"},
			}
			upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(nil, upstreamResponse(http.StatusCreated, test.body), info)
			require.Nil(t, taskErr)
			assert.Equal(t, test.wantID, upstreamID)
			var stored StoredTaskData
			require.NoError(t, common.Unmarshal(taskData, &stored))
			assert.Equal(t, test.protocol, stored.Protocol)
		})
	}
}

func TestParseVideoResultSeparatesSignedURLsAndArtifacts(t *testing.T) {
	body := `{
		"id":"upstream-id",
		"model":"flux-3-video-t2v",
		"status":"completed",
		"progress":100,
		"metadata":{
			"url":"https://cdn.example/video.mp4?X-Amz-Signature=secret",
			"last_frame_url":"https://cdn.example/frame.png?token=secret",
			"draft_cache":"private-draft",
			"sessionId":"private-session",
			"faceId":"private-face"
		},
		"cost":123
	}`
	details, err := (&TaskAdaptor{}).ParseTaskResultDetailsForAction([]byte(body), "task_public", ActionVideo)
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), details.Info.Status)
	assert.Equal(t, "https://cdn.example/video.mp4?X-Amz-Signature=secret", details.Info.Url)
	require.Equal(t, []string{"https://cdn.example/video.mp4?X-Amz-Signature=secret"}, details.ResultURLs)
	assert.Equal(t, "https://cdn.example/frame.png?token=secret", details.ResultAssets["last_frame"])
	assert.Equal(t, "private-draft", details.Artifacts["flux_draft_cache"])
	assert.Equal(t, "private-session", details.Artifacts["kling_lip_session"])
	assert.Equal(t, "private-face", details.Artifacts["kling_lip_face"])
	assert.NotContains(t, string(details.SafeData), "secret")
	assert.NotContains(t, string(details.SafeData), "cost")
	assert.NotContains(t, string(details.SafeData), "upstream-id")
	var stored StoredTaskData
	require.NoError(t, common.Unmarshal(details.SafeData, &stored))
	assert.Equal(t, []string{taskcommon.BuildProxyURL("task_public")}, stored.VideoURLs)
	var safeData map[string]any
	require.NoError(t, common.Unmarshal(details.SafeData, &safeData))
	assert.Equal(t, taskcommon.BuildProxyURL("task_public")+"?asset=last_frame", safeData["last_frame_url"])
}

func TestLocalTaskResponsesExposeCompleteOutputsWithoutPrivateValues(t *testing.T) {
	handles := map[string]string{
		model.TaskArtifactFluxDraftCache: "artifact_" + strings.Repeat("A", 32),
		model.TaskArtifactKlingSession:   "artifact_" + strings.Repeat("B", 32),
		model.TaskArtifactKlingFace:      "artifact_" + strings.Repeat("C", 32),
	}
	taskData := []byte(`{
		"protocol":"videos",
		"status":"completed",
		"video_count":2,
		"video_urls":["https://cdn.example/0.mp4?sig=private-zero","https://cdn.example/1.mp4?sig=private-one"],
		"last_frame_url":"https://cdn.example/frame.png?sig=private-frame",
		"result_assets":{"last_frame":"https://cdn.example/frame.png?sig=private-frame"},
		"artifacts":{
			"flux_draft_cache":"` + handles[model.TaskArtifactFluxDraftCache] + `",
			"kling_lip_session":"` + handles[model.TaskArtifactKlingSession] + `",
			"kling_lip_face":"` + handles[model.TaskArtifactKlingFace] + `"
		},
		"cost":99,
		"upstream_task_id":"upstream-secret"
	}`)
	task := &model.Task{
		TaskID:          "task_public",
		Action:          ActionVideo,
		Status:          model.TaskStatusSuccess,
		SubmitTime:      100,
		FinishTime:      200,
		ResultExpiresAt: 300,
		Data:            taskData,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-secret",
			ResultURLs:     []string{"https://cdn.example/0.mp4?sig=private-zero"},
		},
	}

	openAI, err := buildOpenAIVideoResponse(task)
	require.NoError(t, err)
	legacy, err := buildLegacyTaskResponse(task, false)
	require.NoError(t, err)
	for _, body := range [][]byte{openAI, legacy} {
		assert.NotContains(t, string(body), "cdn.example")
		assert.NotContains(t, string(body), "private-")
		assert.NotContains(t, string(body), "upstream-secret")
		assert.NotContains(t, string(body), `"cost"`)
		assert.Contains(t, string(body), "/v1/videos/task_public/content")
		assert.Contains(t, string(body), "?index=1")
		assert.Contains(t, string(body), "?asset=last_frame")
		assert.Contains(t, string(body), `"expires_at":300`)
		for _, handle := range handles {
			assert.Contains(t, string(body), handle)
		}
	}
}

func TestMidjourneyTaskResponseAlwaysRebuildsLocalResultURLs(t *testing.T) {
	task := &model.Task{
		TaskID:          "task_midjourney",
		Action:          ActionMidjourneyVideo,
		Status:          model.TaskStatusSuccess,
		ResultExpiresAt: 400,
		Data: []byte(`{
			"protocol":"midjourney_video",
			"status":"completed",
			"video_count":2,
			"video_urls":["https://cdn.example/0.mp4?sig=private-zero","https://cdn.example/1.mp4?sig=private-one"]
		}`),
	}

	body, err := buildMidjourneyTaskResponse(task)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "cdn.example")
	assert.NotContains(t, string(body), "private-")
	assert.Contains(t, string(body), "/v1/videos/task_midjourney/content?index=0")
	assert.Contains(t, string(body), "/v1/videos/task_midjourney/content?index=1")
	assert.Contains(t, string(body), `"expires_at":400`)
}

func TestParseContextIRAndMidjourneyResults(t *testing.T) {
	contextDetails, err := (&TaskAdaptor{}).ParseTaskResultDetailsForAction(
		[]byte(`{"data":{"task_id":"context-secret","status":"SUCCESS","progress":100,"result_text":"cinematic crane shot"}}`),
		"task_public",
		ActionContextIR,
	)
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), contextDetails.Info.Status)
	assert.Contains(t, string(contextDetails.SafeData), "cinematic crane shot")
	assert.NotContains(t, string(contextDetails.SafeData), "context-secret")

	midjourneyDetails, err := (&TaskAdaptor{}).ParseTaskResultDetailsForAction(
		[]byte(`{"id":"mj-secret","status":"SUCCESS","action":"VIDEO","video_url":"https://cdn.example/0.mp4?sig=a","video_urls":["https://cdn.example/0.mp4?sig=a","https://cdn.example/1.mp4?sig=b"]}`),
		"task_public",
		ActionMidjourneyVideo,
	)
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), midjourneyDetails.Info.Status)
	require.Len(t, midjourneyDetails.ResultURLs, 2)
	assert.NotContains(t, string(midjourneyDetails.SafeData), "sig=")
	assert.Contains(t, string(midjourneyDetails.SafeData), "index=1")
}

func TestParseTaskFailurePreservesSafeReason(t *testing.T) {
	details, err := (&TaskAdaptor{}).ParseTaskResultDetailsForAction(
		[]byte(`{"id":"secret","status":"failed","error":{"code":"moderation","message":"prompt rejected"}}`),
		"task_public",
		ActionVideo,
	)
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), details.Info.Status)
	assert.Equal(t, "prompt rejected", details.Info.Reason)
	assert.Contains(t, string(details.SafeData), "moderation")
	assert.NotContains(t, string(details.SafeData), "secret")
}

func TestPollingErrorEnvelopeDoesNotBecomeTerminalTaskFailure(t *testing.T) {
	_, err := (&TaskAdaptor{}).ParseTaskResultDetailsForAction(
		[]byte(`{"error":{"code":429,"message":"try later"}}`),
		"task_public",
		ActionVideo,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "try later")
}

func TestFetchTaskSelectsProtocolEndpoint(t *testing.T) {
	tests := []struct {
		action string
		path   string
	}{
		{ActionVideo, "/v1/videos/upstream-id"},
		{ActionContextIR, "/v1/video/generations/upstream-id"},
		{ActionMidjourneyVideo, "/v1/midjourney/tasks/upstream-id"},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				assert.Equal(t, test.path, request.URL.Path)
				assert.Equal(t, "Bearer secret-key", request.Header.Get("Authorization"))
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(`{"status":"queued"}`))
			}))
			defer server.Close()

			response, err := (&TaskAdaptor{}).FetchTask(server.URL, "secret-key", map[string]any{"task_id": "upstream-id", "action": test.action}, "")
			require.NoError(t, err)
			_ = response.Body.Close()
		})
	}
}

func TestDeferredResponseUsesOnlyPublicTaskID(t *testing.T) {
	task := &model.Task{
		TaskID:      "task_public",
		Action:      ActionVideo,
		Status:      model.TaskStatusNotStart,
		SubmitTime:  123,
		Properties:  model.Properties{OriginModelName: "seedance-2.0-mini-t2v"},
		PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-secret"},
	}
	status, body, err := (&TaskAdaptor{}).BuildSubmitResponse(task)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(body), "task_public")
	assert.Contains(t, string(body), `"status":"queued"`)
	assert.NotContains(t, string(body), "upstream-secret")
}

func TestUploadClientStreamsKnownAndUnknownSizeAssets(t *testing.T) {
	payload := []byte("video-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/v1/files/upload", request.URL.Path)
		assert.Equal(t, "Bearer upload-key", request.Header.Get("Authorization"))
		require.NoError(t, request.ParseMultipartForm(MaxUploadBytes))
		file, header, err := request.FormFile("file")
		require.NoError(t, err)
		defer file.Close()
		assert.Equal(t, "clip.mp4", header.Filename)
		got, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, payload, got)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"url":"` + serverURLFromRequest(request) + `/asset.mp4","file_type":"video","size":11,"expires_in":86400}`))
	}))
	defer server.Close()

	client := UploadClient{BaseURL: server.URL, APIKey: "upload-key"}
	for _, test := range []struct {
		name string
		size int64
	}{
		{name: "known size", size: int64(len(payload))},
		{name: "streamed unknown size", size: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := client.Upload(context.Background(), "clip.mp4", test.size, bytes.NewReader(payload))
			require.NoError(t, err)
			assert.Equal(t, "video", result.FileType)
			assert.Equal(t, int64(86400), result.ExpiresIn)
		})
	}
}

func TestUploadClientPreservesSafeUpstreamRateLimitDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.Header().Set("Retry-After", "17")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"code":"rate_limit","message":"try later"}}`))
	}))
	defer server.Close()

	_, err := (UploadClient{BaseURL: server.URL, APIKey: "upload-key"}).Upload(
		context.Background(),
		"clip.mp4",
		4,
		strings.NewReader("data"),
	)

	var upstreamErr *UpstreamUploadError
	require.ErrorAs(t, err, &upstreamErr)
	assert.Equal(t, http.StatusTooManyRequests, upstreamErr.StatusCode)
	assert.Equal(t, "rate_limit", upstreamErr.Code)
	assert.Equal(t, "try later", upstreamErr.Message)
	assert.Equal(t, "17", upstreamErr.RetryAfter)
}

func TestUploadClientDoesNotHideTransportFailureWithClosedPipe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	server.Close()

	_, err := (UploadClient{BaseURL: baseURL, APIKey: "upload-key"}).Upload(
		context.Background(),
		"clip.mp4",
		0,
		io.LimitReader(strings.NewReader(strings.Repeat("data", 1024)), 4096),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload Seedance asset")
	assert.NotContains(t, err.Error(), "closed pipe")
}

func serverURLFromRequest(request *http.Request) string {
	return "http://" + request.Host
}

func TestResultExpiryParsesAbsoluteAndAWSExpiry(t *testing.T) {
	assert.Equal(t, int64(1893456000), ResultExpiry("https://cdn.example/a.mp4?Expires=1893456000"))
	assert.Equal(t, int64(1893456060), ResultExpiry("https://cdn.example/a.mp4?X-Amz-Date=20300101T000000Z&X-Amz-Expires=60"))
	assert.Zero(t, ResultExpiry("https://cdn.example/a.mp4?token=secret"))
}
