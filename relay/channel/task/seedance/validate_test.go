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
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubDependencyResolver struct{}

func (stubDependencyResolver) ResolveTaskReference(_ context.Context, _, _ int, localTaskID, _ string) (string, error) {
	return "upstream-" + localTaskID, nil
}

func (stubDependencyResolver) ResolveArtifact(_ context.Context, _, _ int, localHandle, _ string) (string, error) {
	return "upstream-" + localHandle, nil
}

func newTaskContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context, recorder
}

func validateRequest(t *testing.T, path, body string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	context, _ := newTaskContext(http.MethodPost, path, body)
	info := &relaycommon.RelayInfo{}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
	return context, info
}

func TestValidateVideoFamilies(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		action string
	}{
		{
			name:   "Seedance 2.0 text",
			body:   `{"model":"seedance-2.0-mini-t2v","prompt":"a lighthouse","seconds":"15","metadata":{"resolution":"720p","generate_audio":false}}`,
			action: ActionVideo,
		},
		{
			name:   "Seedance 2.5 image prompt optional",
			body:   `{"model":"seedance-2.5-standard-i2v","images":["https://cdn.example/a.png"],"seconds":"30"}`,
			action: ActionVideo,
		},
		{
			name:   "Seedance 2.5 multimodal maximum classes",
			body:   `{"model":"seedance-2.5-standard-multi","prompt":"replace the sky","metadata":{"content":[{"type":"image_url","image_url":{"url":"https://cdn.example/a.png"}},{"type":"video_url","video_url":{"url":"https://cdn.example/a.mp4"}},{"type":"audio_url","audio_url":{"url":"https://cdn.example/a.mp3"}}]}}`,
			action: ActionVideo,
		},
		{
			name:   "Wan i2v",
			body:   `{"model":"wan-2.7-spicy-i2v","prompt":"move","images":["https://cdn.example/a.png"],"seconds":"2","metadata":{"resolution":"1080p","prompt_extend":false}}`,
			action: ActionVideo,
		},
		{
			name:   "HappyHorse r2v",
			body:   `{"model":"happyhorse-1.1-r2v","prompt":"characters wave","images":["https://cdn.example/a.png","https://cdn.example/b.png"],"seconds":"3"}`,
			action: ActionVideo,
		},
		{
			name:   "Kling text",
			body:   `{"model":"kling-v3.0-std-t2v","prompt":"city timelapse"}`,
			action: ActionVideo,
		},
		{
			name:   "Hailuo H3 multi",
			body:   `{"model":"hailuo-h3-multi","prompt":"replace @Video 1 with @Image 1","seconds":"5","metadata":{"resolution":"768P","ratio":"16:9","content":[{"type":"image_url","image_url":{"url":"https://cdn.example/a.png"}},{"type":"video_url","video_url":{"url":"https://cdn.example/a.mp4"}}]}}`,
			action: ActionVideo,
		},
		{
			name:   "Flux video",
			body:   `{"model":"flux-3-video-v2v","prompt":"restyle","seconds":"20","metadata":{"resolution":"fhd","video_url":"https://cdn.example/a.mp4","safety_tolerance":0}}`,
			action: ActionVideo,
		},
		{
			name:   "MiniMax audio drive",
			body:   `{"model":"minimax-h3-ow-fl2va-audio-drive-fast","prompt":"sing","images":["https://cdn.example/a.png"],"seconds":"10","metadata":{"resolution":"480p","audio_urls":["https://cdn.example/a.mp3"]}}`,
			action: ActionVideo,
		},
		{
			name:   "Vidu start end",
			body:   `{"model":"vidu-q3-pro-start-end","prompt":"transition","images":["https://cdn.example/a.png","https://cdn.example/b.png"]}`,
			action: ActionVideo,
		},
		{
			name:   "Zhenzhen omni prompt only",
			body:   `{"model":"zhenzhen-video-g-omni-flash","prompt":"a butterfly","metadata":{"resolution":"720p","ratio":"16:9"}}`,
			action: ActionVideo,
		},
		{
			name:   "Upscaler",
			body:   `{"model":"zhenzhen-upscaler","metadata":{"resolution":"4k","content":[{"type":"video_url","video_url":{"url":"https://cdn.example/a.mp4"}}]}}`,
			action: ActionVideo,
		},
		{
			name:   "Context IR text",
			body:   `{"model":"minmax-h3-context-ir-text","prompt":"a crane flies","seconds":"4","metadata":{"ratio":"16:9"}}`,
			action: ActionContextIR,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, info := validateRequest(t, "/v1/videos", test.body)
			assert.Equal(t, test.action, info.Action)
		})
	}
}

func TestValidateRequestRejectsBillingAndMediaBoundaryBypasses(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		body    string
		message string
	}{
		{
			name:    "unknown metadata multiplier",
			path:    "/v1/videos",
			body:    `{"model":"seedance-2.0-mini-t2v","prompt":"x","metadata":{"n":999999999}}`,
			message: "unsupported field",
		},
		{
			name:    "smart duration forbidden",
			path:    "/v1/videos",
			body:    `{"model":"happyhorse-1.1-t2v","prompt":"x","seconds":"-1"}`,
			message: "does not support smart duration",
		},
		{
			name:    "Seedance 2.5 duration overflow",
			path:    "/v1/videos",
			body:    `{"model":"seedance-2.5-standard-t2v","prompt":"x","seconds":"31"}`,
			message: "between 4 and 30",
		},
		{
			name:    "multimodal content count",
			path:    "/v1/videos",
			body:    `{"model":"seedance-2.0-mini-multi","prompt":"x","metadata":{"content":[]}}`,
			message: "requires 1 or more references",
		},
		{
			name:    "non HTTP media URL",
			path:    "/v1/videos",
			body:    `{"model":"seedance-2.0-mini-i2v","images":["data:image/png;base64,AA=="]}`,
			message: "only absolute http and https",
		},
		{
			name:    "upscaler prompt forbidden by shape",
			path:    "/v1/videos",
			body:    `{"model":"zhenzhen-upscaler","seconds":"5","metadata":{"resolution":"4k","content":[{"type":"video_url","video_url":{"url":"https://cdn.example/a.mp4"}}]}}`,
			message: "does not accept seconds",
		},
		{
			name:    "MJ batch zero",
			path:    "/v1/midjourney/generations/video",
			body:    `{"image_urls":["https://cdn.example/a.png"],"batch_size":0}`,
			message: "batch_size must be 1, 2, or 4",
		},
		{
			name:    "MJ huge unsigned count",
			path:    "/v1/midjourney/generations/video",
			body:    `{"image_urls":["https://cdn.example/a.png"],"batch_size":18446744073686646784}`,
			message: "batch_size must be 1, 2, or 4",
		},
		{
			name:    "MJ image task mode excluded",
			path:    "/v1/midjourney/generations/video",
			body:    `{"task_id":"task_local","index":0}`,
			message: "unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := newTaskContext(http.MethodPost, test.path, test.body)
			info := &relaycommon.RelayInfo{}
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(context, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Contains(t, taskErr.Message, test.message)
		})
	}
}

func TestValidateModelSpecificVideoMetadataRules(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "Vidu accepts documented ratio",
			body:    `{"model":"vidu-q3-pro-t2v","prompt":"x","metadata":{"ratio":"16:9"}}`,
			message: "",
		},
		{
			name:    "quality rejects type",
			body:    `{"model":"zhenzhen-video-v31-quality","prompt":"x","metadata":{"type":"reference"}}`,
			message: "metadata field \"type\" is not supported",
		},
		{
			name:    "lite rejects type",
			body:    `{"model":"zhenzhen-video-v31-lite","prompt":"x","metadata":{"type":"reference"}}`,
			message: "metadata field \"type\" is not supported",
		},
		{
			name:    "fast only accepts reference type",
			body:    `{"model":"zhenzhen-video-v31-fast","prompt":"x","metadata":{"type":"normal"}}`,
			message: "metadata.type must be reference",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := newTaskContext(http.MethodPost, "/v1/videos", test.body)
			info := &relaycommon.RelayInfo{}
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(context, info)
			if test.message == "" {
				require.Nil(t, taskErr)
				return
			}
			require.NotNil(t, taskErr)
			assert.Contains(t, taskErr.Message, test.message)
		})
	}
}

func TestBuildRequestBodyPreservesExplicitZeroAndFalse(t *testing.T) {
	context, info := validateRequest(t, "/v1/videos", `{"model":"seedance-2.0-mini-t2v","prompt":"x","metadata":{"seed":0,"generate_audio":false,"return_last_frame":false}}`)
	info.UpstreamModelName = "seedance-2.0-mini-t2v"
	adaptor := &TaskAdaptor{}
	bodyReader, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)
	body, err := io.ReadAll(bodyReader)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, common.Unmarshal(body, &decoded))
	metadata, ok := decoded["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(0), metadata["seed"])
	assert.Equal(t, false, metadata["generate_audio"])
	assert.Equal(t, false, metadata["return_last_frame"])
}

func TestEstimateBillingOnlyMultipliesValidatedMidjourneyBatch(t *testing.T) {
	context, info := validateRequest(t, "/v1/midjourney/generations/video", `{"image_urls":["https://cdn.example/a.png"],"batch_size":4}`)
	ratios := (&TaskAdaptor{}).EstimateBilling(context, info)
	require.Equal(t, map[string]float64{"batch_size": 4}, ratios)

	videoContext, videoInfo := validateRequest(t, "/v1/videos", `{"model":"seedance-2.0-mini-t2v","prompt":"x","seconds":"15","metadata":{"resolution":"4k"}}`)
	assert.Nil(t, (&TaskAdaptor{}).EstimateBilling(videoContext, videoInfo))
}

func TestBuildRequestBodyRejectsIncompatibleMappedModelBeforeUpstream(t *testing.T) {
	context, info := validateRequest(t, "/v1/videos", `{"model":"seedance-2.0-mini-t2v","prompt":"x"}`)
	info.UpstreamModelName = "minmax-h3-context-ir-text"
	_, err := (&TaskAdaptor{}).BuildRequestBody(context, info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not compatible")
}

func TestBuildRequestBodyRejectsMappedMidjourneyModelBeforeUpstream(t *testing.T) {
	context, info := validateRequest(t, "/v1/midjourney/generations/video", `{"image_urls":["https://cdn.example/a.png"]}`)
	info.UpstreamModelName = "seedance-2.0-mini-t2v"

	_, err := (&TaskAdaptor{}).BuildRequestBody(context, info)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not compatible")
}

func TestValidationUsesChannelMappingForLocalAlias(t *testing.T) {
	context, _ := newTaskContext(http.MethodPost, "/v1/videos", `{"model":"my-video","prompt":"x","seconds":"15"}`)
	context.Set("model_mapping", `{"my-video":"seedance-2.0-mini-t2v"}`)
	info := &relaycommon.RelayInfo{}
	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(context, info))
	assert.Equal(t, "my-video", info.OriginModelName)
	assert.Equal(t, ActionVideo, info.Action)
}

func TestBuildRequestBodyUsesCommonJSONRoundTrip(t *testing.T) {
	context, info := validateRequest(t, "/v1/videos", `{"model":"seedance-2.0-mini-t2v","prompt":"x"}`)
	info.UpstreamModelName = "seedance-2.0-mini-t2v"
	reader, err := (&TaskAdaptor{}).BuildRequestBody(context, info)
	require.NoError(t, err)
	buffer := &bytes.Buffer{}
	_, err = buffer.ReadFrom(reader)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"seedance-2.0-mini-t2v","prompt":"x"}`, buffer.String())
}

func TestBuildRequestBodyPreservesHailuoMultiContent(t *testing.T) {
	context, info := validateRequest(t, "/v1/videos", `{"model":"hailuo-h3-global-multi","prompt":"replace @Video 1 with @Image 1","seconds":"5","metadata":{"resolution":"768P","ratio":"16:9","content":[{"type":"image_url","image_url":{"url":"https://cdn.example/image.png"}},{"type":"video_url","video_url":{"url":"https://cdn.example/video.mp4"}}]}}`)
	info.UpstreamModelName = "hailuo-h3-global-multi"

	reader, err := (&TaskAdaptor{}).BuildRequestBody(context, info)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.JSONEq(t, `{
		"model":"hailuo-h3-global-multi",
		"prompt":"replace @Video 1 with @Image 1",
		"seconds":"5",
		"metadata":{
			"resolution":"768P",
			"ratio":"16:9",
			"content":[
				{"type":"image_url","image_url":{"url":"https://cdn.example/image.png"}},
				{"type":"video_url","video_url":{"url":"https://cdn.example/video.mp4"}}
			]
		}
	}`, string(body))
}

func TestBuildRequestBodyResolvesPrivateWorkflowArtifactWithoutMutatingPublicRequest(t *testing.T) {
	context, info := validateRequest(t, "/v1/videos", `{"model":"flux-3-video-draft-enhance","metadata":{"draft_cache":"artifact_local"}}`)
	info.UpstreamModelName = "flux-3-video-draft-enhance"
	adaptor := &TaskAdaptor{DependencyResolver: stubDependencyResolver{}}

	for range 2 {
		reader, err := adaptor.BuildRequestBody(context, info)
		require.NoError(t, err)
		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		var decoded VideoRequest
		require.NoError(t, common.Unmarshal(body, &decoded))
		require.NotNil(t, decoded.Metadata)
		require.NotNil(t, decoded.Metadata.DraftCache)
		assert.Equal(t, "upstream-artifact_local", *decoded.Metadata.DraftCache)
	}
}

func TestBuildPublicTaskInputKeepsReusableSettingsWithoutMediaURLs(t *testing.T) {
	context, _ := validateRequest(t, "/v1/videos", `{"model":"seedance-2.0-fast-multi","prompt":"replace @Video 1 with @Image 1","seconds":"5","metadata":{"resolution":"480p","ratio":"21:9","generate_audio":false,"return_last_frame":true,"content":[{"type":"image_url","image_url":{"url":"https://signed.example/image.png?token=secret"}},{"type":"video_url","video_url":{"url":"https://signed.example/video.mp4?token=secret"}}]}}`)

	input, err := (&TaskAdaptor{}).BuildPublicTaskInput(context)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"seedance-2.0-fast-multi","prompt":"replace @Video 1 with @Image 1","seconds":"5","metadata":{"resolution":"480p","ratio":"21:9","generate_audio":false,"return_last_frame":true}}`, input)
	assert.NotContains(t, input, "signed.example")
	assert.NotContains(t, input, "secret")
}

func TestBuildPublicTaskInputOmitsMidjourneySourceURLs(t *testing.T) {
	context, _ := validateRequest(t, "/v1/midjourney/generations/video", `{"model":"midjourney-video","prompt":"slow orbit","image_urls":["https://signed.example/start.png?token=secret"],"end_url":"https://signed.example/end.png?token=secret","motion":"high","batch_size":4}`)

	input, err := (&TaskAdaptor{}).BuildPublicTaskInput(context)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"midjourney-video","prompt":"slow orbit","video_type":"vid_1.1_i2v_start_end_480","motion":"high","batch_size":4}`, input)
	assert.NotContains(t, input, "signed.example")
	assert.NotContains(t, input, "secret")
}
