package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/seedance"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskModel2DtoRebuildsSeedanceVideoURLs(t *testing.T) {
	previousAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://dashboard.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previousAddress })

	task := &model.Task{
		TaskID:   "task_public",
		Platform: constant.TaskPlatformSeedance,
		Action:   seedance.ActionMidjourneyVideo,
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Data: []byte(`{
			"protocol":"midjourney_video",
			"video_count":2,
			"video_urls":[
				"https://upstream.example/0.mp4?signature=private-zero",
				"https://upstream.example/1.mp4?signature=private-one"
			]
		}`),
		PrivateData: model.TaskPrivateData{
			ResultURL:  "https://upstream.example/0.mp4?signature=private-zero",
			ResultURLs: []string{"https://upstream.example/0.mp4?signature=private-zero"},
		},
	}

	result := TaskModel2Dto(task)

	assert.Equal(t, "https://dashboard.example.com/v1/videos/task_public/content?index=0", result.ResultURL)
	assert.NotContains(t, string(result.Data), "upstream.example")
	assert.NotContains(t, string(result.Data), "signature")
	var data struct {
		VideoURLs []string `json:"video_urls"`
	}
	require.NoError(t, common.Unmarshal(result.Data, &data))
	assert.Equal(t, []string{
		"https://dashboard.example.com/v1/videos/task_public/content?index=0",
		"https://dashboard.example.com/v1/videos/task_public/content?index=1",
	}, data.VideoURLs)
}

func TestTaskModel2DtoFailsClosedForUnknownSeedanceAction(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_unknown_seedance",
		Platform: constant.TaskPlatformSeedance,
		Action:   "legacy_seedance_action",
		Status:   model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://upstream.example/video.mp4?signature=private",
		},
		Data: []byte(`{"result_url":"https://upstream.example/video.mp4?signature=private"}`),
	}

	result := TaskModel2Dto(task)

	assert.Empty(t, result.ResultURL)
	assert.Empty(t, result.Data)
}
