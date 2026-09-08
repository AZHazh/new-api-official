package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskQuerySupportsMultipleActions(t *testing.T) {
	truncateTables(t)
	tasks := []*Task{
		{TaskID: "task_video", UserId: 71, Platform: constant.TaskPlatformSeedance, Action: "seedance_video", SubmitTime: 100},
		{TaskID: "task_midjourney", UserId: 71, Platform: constant.TaskPlatformSeedance, Action: "seedance_midjourney_video", SubmitTime: 200},
		{TaskID: "task_context", UserId: 71, Platform: constant.TaskPlatformSeedance, Action: "seedance_context_ir", SubmitTime: 300},
		{TaskID: "task_other_user", UserId: 72, Platform: constant.TaskPlatformSeedance, Action: "seedance_video", SubmitTime: 400},
	}
	for _, task := range tasks {
		insertTask(t, task)
	}

	query := SyncTaskQueryParams{
		Platform: constant.TaskPlatformSeedance,
		Actions:  []string{"seedance_video", "seedance_midjourney_video"},
	}
	result := TaskGetAllUserTask(71, 0, 20, query)

	require.Len(t, result, 2)
	assert.Equal(t, "task_midjourney", result[0].TaskID)
	assert.Equal(t, "task_video", result[1].TaskID)
	assert.EqualValues(t, 2, TaskCountAllUserTask(71, query))

	singleAction := TaskGetAllUserTask(71, 0, 20, SyncTaskQueryParams{
		Platform: constant.TaskPlatformSeedance,
		Action:   "seedance_video",
	})
	require.Len(t, singleAction, 1)
	assert.Equal(t, "task_video", singleAction[0].TaskID)
}
