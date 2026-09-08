package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"gorm.io/gorm"
)

const (
	taskArtifactIDPrefix         = "artifact_"
	taskArtifactRandomPartLength = 32
	TaskArtifactFluxDraftCache   = "flux_draft_cache"
	TaskArtifactKlingSession     = "kling_lip_session"
	TaskArtifactKlingFace        = "kling_lip_face"
)

var taskArtifactKinds = map[string]struct{}{
	TaskArtifactFluxDraftCache: {},
	TaskArtifactKlingSession:   {},
	TaskArtifactKlingFace:      {},
}

// IsTaskArtifactHandle validates the opaque public handle format.
func IsTaskArtifactHandle(artifactID string) bool {
	if len(artifactID) != len(taskArtifactIDPrefix)+taskArtifactRandomPartLength || !strings.HasPrefix(artifactID, taskArtifactIDPrefix) {
		return false
	}
	for _, character := range artifactID[len(taskArtifactIDPrefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') {
			return false
		}
	}
	return true
}

// TaskArtifact binds an opaque public handle to a provider-private workflow
// value. UpstreamValue is never serialized by task or admin list APIs.
type TaskArtifact struct {
	ID            int64  `json:"-" gorm:"primaryKey"`
	ArtifactID    string `json:"artifact_id" gorm:"type:varchar(64);uniqueIndex"`
	Kind          string `json:"kind" gorm:"type:varchar(64);index;uniqueIndex:idx_task_artifact_source_kind,priority:2"`
	UserId        int    `json:"-" gorm:"index"`
	SourceTaskID  int64  `json:"-" gorm:"index;uniqueIndex:idx_task_artifact_source_kind,priority:1"`
	ChannelId     int    `json:"-" gorm:"index"`
	UpstreamValue string `json:"-" gorm:"type:text"`
	ExpiresAt     int64  `json:"expires_at" gorm:"index"`
	CreatedAt     int64  `json:"created_at" gorm:"index"`
}

func NewTaskArtifact(kind, upstreamValue string, expiresAt int64) (*TaskArtifact, error) {
	if _, ok := taskArtifactKinds[kind]; !ok {
		return nil, fmt.Errorf("unsupported task artifact kind %q", kind)
	}
	if strings.TrimSpace(upstreamValue) == "" {
		return nil, errors.New("task artifact upstream value is empty")
	}
	if expiresAt <= common.GetTimestamp() {
		return nil, errors.New("task artifact expiry must be in the future")
	}
	key, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return nil, fmt.Errorf("generate task artifact id: %w", err)
	}
	return &TaskArtifact{
		ArtifactID:    taskArtifactIDPrefix + key,
		Kind:          kind,
		UpstreamValue: upstreamValue,
		ExpiresAt:     expiresAt,
		CreatedAt:     common.GetTimestamp(),
	}, nil
}

// UpdateTaskWithArtifacts persists one polling state transition and all
// workflow artifacts in a single primary-database transaction. The status
// guard prevents concurrent pollers from publishing duplicate handles.
func UpdateTaskWithArtifacts(task *Task, fromStatus TaskStatus, artifacts []*TaskArtifact) (bool, error) {
	if task == nil || task.ID <= 0 {
		return false, errors.New("task is invalid")
	}
	if len(artifacts) > 0 && task.Status != TaskStatusSuccess {
		return false, errors.New("task artifacts require a successful task")
	}
	won := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var current Task
		err := lockForUpdate(tx).
			Where("id = ? AND status = ?", task.ID, fromStatus).
			First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if task.UserId != current.UserId || task.ChannelId != current.ChannelId || task.TaskID != current.TaskID || task.Platform != current.Platform {
			return errors.New("task identity changed while persisting artifacts")
		}

		now := common.GetTimestamp()
		for _, artifact := range artifacts {
			if artifact == nil {
				return errors.New("task artifact is nil")
			}
			if _, ok := taskArtifactKinds[artifact.Kind]; !ok {
				return fmt.Errorf("unsupported task artifact kind %q", artifact.Kind)
			}
			if !IsTaskArtifactHandle(artifact.ArtifactID) || strings.TrimSpace(artifact.UpstreamValue) == "" {
				return errors.New("task artifact is invalid")
			}
			if artifact.ExpiresAt <= now {
				return errors.New("task artifact is expired")
			}
			artifact.UserId = current.UserId
			artifact.SourceTaskID = current.ID
			artifact.ChannelId = current.ChannelId
			if artifact.CreatedAt == 0 {
				artifact.CreatedAt = now
			}
		}

		result := tx.Model(&Task{}).
			Where("id = ? AND status = ?", task.ID, fromStatus).
			Select("*").
			Updates(task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		for _, artifact := range artifacts {
			if err := tx.Create(artifact).Error; err != nil {
				return err
			}
		}
		won = true
		return nil
	})
	return won, err
}

// ResolveSeedanceTaskReference converts a user-owned local task ID into the
// private provider task ID used by the Zhenzhen continuation workflow.
func ResolveSeedanceTaskReference(ctx context.Context, userID, channelID int, localTaskID, purpose string) (string, error) {
	if purpose != "zhenzhen_video_extend" || userID <= 0 || channelID <= 0 || !strings.HasPrefix(localTaskID, "task_") {
		return "", errors.New("task dependency is invalid or unavailable")
	}
	var task Task
	err := DB.WithContext(ctx).
		Where("user_id = ? AND channel_id = ? AND task_id = ?", userID, channelID, localTaskID).
		First(&task).Error
	if err != nil {
		return "", errors.New("task dependency is invalid or unavailable")
	}
	if task.Status != TaskStatusSuccess || task.Platform != constant.TaskPlatformSeedance {
		return "", errors.New("task dependency is invalid or unavailable")
	}
	upstreamModel := task.Properties.UpstreamModelName
	if upstreamModel == "" {
		upstreamModel = task.Properties.OriginModelName
	}
	if upstreamModel != "zhenzhen-video-g-omni-flash" || strings.TrimSpace(task.PrivateData.UpstreamTaskID) == "" {
		return "", errors.New("task dependency is invalid or unavailable")
	}
	return task.PrivateData.UpstreamTaskID, nil
}

// ResolveSeedanceArtifact validates ownership, channel affinity, kind,
// source-task success, and expiry before returning the provider-private value.
func ResolveSeedanceArtifact(ctx context.Context, userID, channelID int, artifactID, kind string) (string, error) {
	if userID <= 0 || channelID <= 0 || !IsTaskArtifactHandle(artifactID) {
		return "", errors.New("task artifact is invalid or unavailable")
	}
	if _, ok := taskArtifactKinds[kind]; !ok {
		return "", errors.New("task artifact is invalid or unavailable")
	}
	var artifact TaskArtifact
	err := DB.WithContext(ctx).
		Where("artifact_id = ? AND user_id = ? AND channel_id = ? AND kind = ? AND expires_at > ?",
			artifactID, userID, channelID, kind, time.Now().Unix()).
		First(&artifact).Error
	if err != nil {
		return "", errors.New("task artifact is invalid or unavailable")
	}
	var source Task
	err = DB.WithContext(ctx).
		Select("id", "user_id", "channel_id", "platform", "status", "result_expires_at").
		Where("id = ?", artifact.SourceTaskID).
		First(&source).Error
	if err != nil || source.Status != TaskStatusSuccess || source.Platform != constant.TaskPlatformSeedance ||
		source.UserId != userID || source.ChannelId != channelID ||
		(source.ResultExpiresAt > 0 && source.ResultExpiresAt <= time.Now().Unix()) {
		return "", errors.New("task artifact is invalid or unavailable")
	}
	if strings.TrimSpace(artifact.UpstreamValue) == "" {
		return "", errors.New("task artifact is invalid or unavailable")
	}
	return artifact.UpstreamValue, nil
}
