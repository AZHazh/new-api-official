package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"gorm.io/gorm"
)

func GetUnfinishedSeedanceTasksForChannels(channelIDs []int) ([]*Task, error) {
	if len(channelIDs) == 0 {
		return []*Task{}, nil
	}
	var seedanceChannelIDs []int
	if err := DB.Model(&Channel{}).
		Where("id IN ? AND type = ?", channelIDs, constant.ChannelTypeSeedance).
		Pluck("id", &seedanceChannelIDs).Error; err != nil {
		return nil, err
	}
	if len(seedanceChannelIDs) == 0 {
		return []*Task{}, nil
	}

	var tasks []*Task
	err := DB.Where("channel_id IN ?", seedanceChannelIDs).
		Where(
			"status NOT IN ? OR (status = ? AND billing_status IN ?)",
			[]TaskStatus{TaskStatusFailure, TaskStatusSuccess},
			TaskStatusFailure,
			[]TaskBillingStatus{TaskBillingStatusReservePending, TaskBillingStatusReserved, TaskBillingStatusRefundPending},
		).
		Order("id ASC").
		Find(&tasks).Error
	return tasks, err
}

// ForceFailSeedanceTask closes one unfinished task without creating a credit.
// A committed reservation moves to refund_pending; a reservation that never
// committed is closed as refunded because no balance mutation occurred.
func ForceFailSeedanceTask(taskID, channelID int64, reason string) (*Task, bool, error) {
	if taskID <= 0 || channelID <= 0 {
		return nil, false, errors.New("invalid task or channel id")
	}
	if reason == "" {
		reason = "Seedance channel connection was forcibly changed"
	}

	var task Task
	applied := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ? AND channel_id = ?", taskID, channelID).First(&task).Error; err != nil {
			return err
		}
		if task.Status == TaskStatusSuccess {
			return nil
		}
		if task.Status == TaskStatusFailure &&
			(task.BillingStatus == "" || task.BillingStatus == TaskBillingStatusRefunded) {
			return nil
		}

		nextBillingStatus := task.BillingStatus
		switch task.BillingStatus {
		case TaskBillingStatusReservePending:
			nextBillingStatus = TaskBillingStatusRefunded
		case TaskBillingStatusReserved:
			if _, err := getVerifiedTaskReserveEventTx(tx, &task); err != nil {
				return err
			}
			nextBillingStatus = TaskBillingStatusRefundPending
		case TaskBillingStatusRefundPending, TaskBillingStatusRefunded:
		case TaskBillingStatusSettled:
			return fmt.Errorf("%w: unfinished settled task %s cannot be force-refunded", ErrTaskBillingInvalidState, task.TaskID)
		case "":
			if task.Quota != 0 || task.ReservedQuota != 0 {
				return fmt.Errorf("%w: legacy task %s has untracked quota", ErrTaskBillingInvalidState, task.TaskID)
			}
			nextBillingStatus = TaskBillingStatusRefunded
		default:
			return fmt.Errorf("%w: unsupported billing status %q", ErrTaskBillingInvalidState, task.BillingStatus)
		}

		now := common.GetTimestamp()
		task.Status = TaskStatusFailure
		task.BillingStatus = nextBillingStatus
		task.FailReason = reason
		task.Progress = "100%"
		task.FinishTime = now
		task.NextPollAt = 0
		task.PollFailures = 0
		task.UpdatedAt = now
		if nextBillingStatus == TaskBillingStatusRefunded {
			task.Quota = 0
		}
		if err := tx.Model(&Task{}).Where("id = ?", task.ID).Updates(map[string]any{
			"status":         task.Status,
			"billing_status": task.BillingStatus,
			"quota":          task.Quota,
			"fail_reason":    task.FailReason,
			"progress":       task.Progress,
			"finish_time":    task.FinishTime,
			"next_poll_at":   task.NextPollAt,
			"poll_failures":  task.PollFailures,
			"updated_at":     task.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	return &task, applied, err
}
