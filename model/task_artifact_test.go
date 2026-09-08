package model

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTaskArtifactFixture(t *testing.T) (*Task, *TaskArtifact) {
	t.Helper()
	truncateTables(t)
	now := time.Now().Unix()
	source := &Task{
		TaskID:          "task_artifact_source",
		Platform:        constant.TaskPlatformSeedance,
		UserId:          101,
		ChannelId:       202,
		Status:          TaskStatusSuccess,
		ResultExpiresAt: now + 3600,
	}
	insertTask(t, source)
	artifact := &TaskArtifact{
		ArtifactID:    taskArtifactIDPrefix + strings.Repeat("A", taskArtifactRandomPartLength),
		Kind:          TaskArtifactFluxDraftCache,
		UserId:        source.UserId,
		SourceTaskID:  source.ID,
		ChannelId:     source.ChannelId,
		UpstreamValue: "provider-private-value",
		ExpiresAt:     now + 3600,
		CreatedAt:     now,
	}
	require.NoError(t, DB.Create(artifact).Error)
	return source, artifact
}

func TestResolveSeedanceArtifactEnforcesEveryTrustBoundary(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*testing.T, *Task, *TaskArtifact)
		userID    int
		channelID int
		kind      string
		wantValue bool
	}{
		{name: "valid", userID: 101, channelID: 202, kind: TaskArtifactFluxDraftCache, wantValue: true},
		{name: "wrong owner", userID: 102, channelID: 202, kind: TaskArtifactFluxDraftCache},
		{name: "wrong channel", userID: 101, channelID: 203, kind: TaskArtifactFluxDraftCache},
		{name: "wrong kind", userID: 101, channelID: 202, kind: TaskArtifactKlingSession},
		{
			name: "expired artifact", userID: 101, channelID: 202, kind: TaskArtifactFluxDraftCache,
			mutate: func(t *testing.T, _ *Task, artifact *TaskArtifact) {
				require.NoError(t, DB.Model(artifact).Update("expires_at", time.Now().Unix()-1).Error)
			},
		},
		{
			name: "source task not successful", userID: 101, channelID: 202, kind: TaskArtifactFluxDraftCache,
			mutate: func(t *testing.T, source *Task, _ *TaskArtifact) {
				require.NoError(t, DB.Model(source).Update("status", TaskStatusFailure).Error)
			},
		},
		{
			name: "source task is not Seedance", userID: 101, channelID: 202, kind: TaskArtifactFluxDraftCache,
			mutate: func(t *testing.T, source *Task, _ *TaskArtifact) {
				require.NoError(t, DB.Model(source).Update("platform", constant.TaskPlatformSuno).Error)
			},
		},
		{
			name: "source result expired", userID: 101, channelID: 202, kind: TaskArtifactFluxDraftCache,
			mutate: func(t *testing.T, source *Task, _ *TaskArtifact) {
				require.NoError(t, DB.Model(source).Update("result_expires_at", time.Now().Unix()-1).Error)
			},
		},
		{
			name: "source owner differs from artifact", userID: 101, channelID: 202, kind: TaskArtifactFluxDraftCache,
			mutate: func(t *testing.T, source *Task, _ *TaskArtifact) {
				require.NoError(t, DB.Model(source).Update("user_id", 999).Error)
			},
		},
		{
			name: "source channel differs from artifact", userID: 101, channelID: 202, kind: TaskArtifactFluxDraftCache,
			mutate: func(t *testing.T, source *Task, _ *TaskArtifact) {
				require.NoError(t, DB.Model(source).Update("channel_id", 999).Error)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, artifact := createTaskArtifactFixture(t)
			if test.mutate != nil {
				test.mutate(t, source, artifact)
			}

			value, err := ResolveSeedanceArtifact(context.Background(), test.userID, test.channelID, artifact.ArtifactID, test.kind)
			if test.wantValue {
				require.NoError(t, err)
				assert.Equal(t, artifact.UpstreamValue, value)
				return
			}
			require.EqualError(t, err, "task artifact is invalid or unavailable")
			assert.Empty(t, value)
		})
	}
}

func TestUpdateTaskWithArtifactsUsesPersistedTaskIdentity(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:    "task_artifact_transition",
		Platform:  constant.TaskPlatformSeedance,
		UserId:    10,
		ChannelId: 20,
		Status:    TaskStatusInProgress,
	}
	insertTask(t, task)
	artifact, err := NewTaskArtifact(TaskArtifactKlingSession, "private-session", now+3600)
	require.NoError(t, err)
	task.Status = TaskStatusSuccess
	task.ResultExpiresAt = now + 3600

	won, err := UpdateTaskWithArtifacts(task, TaskStatusInProgress, []*TaskArtifact{artifact})
	require.NoError(t, err)
	require.True(t, won)
	assert.Equal(t, task.UserId, artifact.UserId)
	assert.Equal(t, task.ChannelId, artifact.ChannelId)
	assert.Equal(t, task.ID, artifact.SourceTaskID)
	value, err := ResolveSeedanceArtifact(context.Background(), task.UserId, task.ChannelId, artifact.ArtifactID, TaskArtifactKlingSession)
	require.NoError(t, err)
	assert.Equal(t, "private-session", value)
}

func TestUpdateTaskWithArtifactsRejectsForgedIdentityAndNonSuccess(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Task)
		error  string
	}{
		{name: "forged user", mutate: func(task *Task) { task.UserId++ }, error: "task identity changed"},
		{name: "forged channel", mutate: func(task *Task) { task.ChannelId++ }, error: "task identity changed"},
		{name: "non-success target", mutate: func(task *Task) { task.Status = TaskStatusQueued }, error: "require a successful task"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			now := time.Now().Unix()
			task := &Task{
				TaskID:    "task_rejected_artifact",
				Platform:  constant.TaskPlatformSeedance,
				UserId:    10,
				ChannelId: 20,
				Status:    TaskStatusInProgress,
			}
			insertTask(t, task)
			task.Status = TaskStatusSuccess
			test.mutate(task)
			artifact, err := NewTaskArtifact(TaskArtifactKlingFace, "private-face", now+3600)
			require.NoError(t, err)

			won, err := UpdateTaskWithArtifacts(task, TaskStatusInProgress, []*TaskArtifact{artifact})
			require.ErrorContains(t, err, test.error)
			assert.False(t, won)
			var artifactCount int64
			require.NoError(t, DB.Model(&TaskArtifact{}).Count(&artifactCount).Error)
			assert.Zero(t, artifactCount)
		})
	}
}
