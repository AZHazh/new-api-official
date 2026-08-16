package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type TaskBillingStatus string

const (
	TaskBillingStatusReservePending TaskBillingStatus = "reserve_pending"
	TaskBillingStatusReserved       TaskBillingStatus = "reserved"
	TaskBillingStatusSettled        TaskBillingStatus = "settled"
	TaskBillingStatusRefundPending  TaskBillingStatus = "refund_pending"
	TaskBillingStatusRefunded       TaskBillingStatus = "refunded"
)

type TaskBillingEventType string
type TaskBillingEventStatus string

const (
	TaskBillingEventReserve TaskBillingEventType = "reserve"
	TaskBillingEventSettle  TaskBillingEventType = "settle"
	TaskBillingEventRefund  TaskBillingEventType = "refund"

	TaskBillingEventPrepared   TaskBillingEventStatus = "prepared"
	TaskBillingEventLogPending TaskBillingEventStatus = "log_pending"
	TaskBillingEventLogging    TaskBillingEventStatus = "logging"
	TaskBillingEventCompleted  TaskBillingEventStatus = "completed"

	TaskBillingSourceWallet       = "wallet"
	TaskBillingSourceSubscription = "subscription"

	// Seedance task submission only waits this long for the upstream to return
	// an asynchronous task ID. Recovery waits an additional minute before
	// treating a reserved SUBMITTING row as an unknown submission.
	TaskSubmissionTimeoutSeconds       int64 = 2 * 60
	TaskSubmissionRecoveryDelaySeconds int64 = 3 * 60
)

var (
	ErrTaskBillingInvalidState           = errors.New("invalid task billing state")
	ErrTaskBillingInsufficientUserQuota  = errors.New("insufficient user quota")
	ErrTaskBillingInsufficientTokenQuota = errors.New("insufficient token quota")
)

// TaskBillingEvent is the durable idempotency record for one financial
// transition. EventKey is stable across process restarts and uniquely binds a
// task to reserve, settle, or refund exactly once.
type TaskBillingEvent struct {
	ID           int64                  `json:"id" gorm:"primaryKey"`
	EventKey     string                 `json:"event_key" gorm:"type:varchar(64);uniqueIndex"`
	TaskID       int64                  `json:"task_id" gorm:"index"`
	TaskPublicID string                 `json:"task_public_id" gorm:"type:varchar(191);index"`
	Type         TaskBillingEventType   `json:"type" gorm:"type:varchar(20);index"`
	Status       TaskBillingEventStatus `json:"status" gorm:"type:varchar(20);index"`
	Quota        int                    `json:"quota"`
	Reason       string                 `json:"reason" gorm:"type:text"`
	LastError    string                 `json:"last_error" gorm:"type:text"`
	Attempts     int                    `json:"attempts"`
	LockedUntil  int64                  `json:"locked_until" gorm:"bigint;index"`
	CreatedAt    int64                  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt    int64                  `json:"updated_at" gorm:"bigint;index"`
}

func (event *TaskBillingEvent) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if event.CreatedAt == 0 {
		event.CreatedAt = now
	}
	if event.UpdatedAt == 0 {
		event.UpdatedAt = now
	}
	return nil
}

func (event *TaskBillingEvent) BeforeUpdate(_ *gorm.DB) error {
	event.UpdatedAt = common.GetTimestamp()
	return nil
}

type TaskBillingMutation struct {
	Task    *Task
	Event   *TaskBillingEvent
	Applied bool
}

func taskBillingEventKey(taskID int64, eventType TaskBillingEventType) string {
	return fmt.Sprintf("task:%d:%s", taskID, eventType)
}

func getTaskBillingEventTx(tx *gorm.DB, eventKey string) (*TaskBillingEvent, error) {
	var event TaskBillingEvent
	err := tx.Where("event_key = ?", eventKey).First(&event).Error
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func getVerifiedTaskReserveEventTx(tx *gorm.DB, task *Task) (*TaskBillingEvent, error) {
	if task == nil {
		return nil, errors.New("task is nil")
	}
	event, err := getTaskBillingEventTx(tx, taskBillingEventKey(task.ID, TaskBillingEventReserve))
	if err != nil {
		return nil, err
	}
	if event.Type != TaskBillingEventReserve || event.Quota < 0 || event.Quota > common.MaxQuota || event.Quota != task.ReservedQuota {
		return nil, fmt.Errorf("%w: task reservation snapshot does not match its billing event", ErrTaskBillingInvalidState)
	}
	return event, nil
}

func newTaskBillingEvent(task *Task, eventType TaskBillingEventType, quota int, reason string) *TaskBillingEvent {
	return &TaskBillingEvent{
		EventKey:     taskBillingEventKey(task.ID, eventType),
		TaskID:       task.ID,
		TaskPublicID: task.TaskID,
		Type:         eventType,
		Status:       TaskBillingEventPrepared,
		Quota:        quota,
		Reason:       reason,
	}
}

// ReserveTaskBilling atomically reserves the maximum task charge, updates
// user/token/subscription balances, records request statistics once, and
// advances the task from reserve_pending to reserved. It always writes the
// primary database directly, regardless of BATCH_UPDATE.
func ReserveTaskBilling(taskID int64, quota int, billingPreference string) (*TaskBillingMutation, error) {
	if taskID <= 0 {
		return nil, errors.New("invalid task id")
	}
	if quota < 0 || quota > common.MaxQuota {
		return nil, fmt.Errorf("invalid reserved quota: %d", quota)
	}

	mutation := &TaskBillingMutation{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).Where("id = ?", taskID).First(&task).Error; err != nil {
			return err
		}
		eventKey := taskBillingEventKey(task.ID, TaskBillingEventReserve)
		if task.BillingStatus == TaskBillingStatusReserved || task.BillingStatus == TaskBillingStatusSettled {
			if task.ReservedQuota != quota {
				return fmt.Errorf("%w: reserved quota is %d, requested %d", ErrTaskBillingInvalidState, task.ReservedQuota, quota)
			}
			event, err := getVerifiedTaskReserveEventTx(tx, &task)
			if err != nil {
				return err
			}
			mutation.Task = &task
			mutation.Event = event
			return nil
		}
		if task.BillingStatus != TaskBillingStatusReservePending {
			return fmt.Errorf("%w: cannot reserve from %q", ErrTaskBillingInvalidState, task.BillingStatus)
		}
		if task.Status != TaskStatusSubmitting {
			return fmt.Errorf("%w: cannot reserve task status %q", ErrTaskBillingInvalidState, task.Status)
		}

		event := newTaskBillingEvent(&task, TaskBillingEventReserve, quota, "maximum task price reserve")
		if err := tx.Create(event).Error; err != nil {
			return err
		}

		var user User
		if err := lockForUpdate(tx.Unscoped()).Where("id = ?", task.UserId).First(&user).Error; err != nil {
			return err
		}

		preference := common.NormalizeBillingPreference(billingPreference)
		if task.PrivateData.BillingSource == TaskBillingSourceWallet {
			preference = "wallet_only"
		} else if task.PrivateData.BillingSource == TaskBillingSourceSubscription {
			preference = "subscription_only"
		}

		fundingSource := ""
		subscriptionID := 0
		reserveWallet := func() error {
			if user.Quota < quota {
				return fmt.Errorf("%w: have=%d need=%d", ErrTaskBillingInsufficientUserQuota, user.Quota, quota)
			}
			fundingSource = TaskBillingSourceWallet
			return nil
		}
		reserveSubscription := func() error {
			result, err := PreConsumeUserSubscriptionTx(
				tx,
				eventKey,
				task.UserId,
				task.Properties.OriginModelName,
				0,
				int64(quota),
			)
			if err != nil {
				return err
			}
			fundingSource = TaskBillingSourceSubscription
			subscriptionID = result.UserSubscriptionId
			return nil
		}
		activeSubscriptionsAllowWallet := func() (bool, error) {
			var strictCount int64
			err := tx.Model(&UserSubscription{}).
				Where("user_id = ? AND status = ? AND end_time > ? AND allow_wallet_overflow = ?",
					task.UserId, "active", getDBTimestamp(tx), false).
				Count(&strictCount).Error
			return strictCount == 0, err
		}

		if quota == 0 {
			fundingSource = TaskBillingSourceWallet
		} else {
			switch preference {
			case "wallet_only":
				if err := reserveWallet(); err != nil {
					return err
				}
			case "subscription_only":
				if err := reserveSubscription(); err != nil {
					return err
				}
			case "wallet_first":
				if err := reserveWallet(); err != nil {
					if !errors.Is(err, ErrTaskBillingInsufficientUserQuota) {
						return err
					}
					if subErr := reserveSubscription(); subErr != nil {
						return subErr
					}
				}
			default: // subscription_first
				if err := reserveSubscription(); err != nil {
					if !errors.Is(err, ErrNoActiveSubscription) && !errors.Is(err, ErrSubscriptionQuotaInsufficient) {
						return err
					}
					if errors.Is(err, ErrSubscriptionQuotaInsufficient) {
						allowed, allowErr := activeSubscriptionsAllowWallet()
						if allowErr != nil {
							return allowErr
						}
						if !allowed {
							return err
						}
					}
					if walletErr := reserveWallet(); walletErr != nil {
						return walletErr
					}
				}
			}
		}

		if task.PrivateData.TokenId > 0 && quota > 0 {
			var token Token
			if err := lockForUpdate(tx.Unscoped()).
				Where("id = ? AND user_id = ?", task.PrivateData.TokenId, task.UserId).
				First(&token).Error; err != nil {
				return err
			}
			if !token.UnlimitedQuota {
				if token.RemainQuota < quota {
					return fmt.Errorf("%w: have=%d need=%d", ErrTaskBillingInsufficientTokenQuota, token.RemainQuota, quota)
				}
				if err := tx.Unscoped().Model(&Token{}).Where("id = ?", token.Id).Updates(map[string]any{
					"remain_quota":  gorm.Expr("remain_quota - ?", quota),
					"used_quota":    gorm.Expr("used_quota + ?", quota),
					"accessed_time": common.GetTimestamp(),
				}).Error; err != nil {
					return err
				}
			}
		}

		userUpdates := map[string]any{
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"request_count": gorm.Expr("request_count + ?", 1),
		}
		if fundingSource == TaskBillingSourceWallet {
			userUpdates["quota"] = gorm.Expr("quota - ?", quota)
		}
		if err := tx.Unscoped().Model(&User{}).Where("id = ?", task.UserId).Updates(userUpdates).Error; err != nil {
			return err
		}
		if task.ChannelId > 0 {
			if err := tx.Model(&Channel{}).Where("id = ?", task.ChannelId).
				Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error; err != nil {
				return err
			}
		}

		task.PrivateData.BillingSource = fundingSource
		task.PrivateData.SubscriptionId = subscriptionID
		task.ReservedQuota = quota
		task.Quota = quota
		task.BillingStatus = TaskBillingStatusReserved
		task.UpdatedAt = common.GetTimestamp()
		if err := tx.Model(&Task{}).Where("id = ?", task.ID).Updates(map[string]any{
			"private_data":   task.PrivateData,
			"reserved_quota": task.ReservedQuota,
			"quota":          task.Quota,
			"billing_status": task.BillingStatus,
			"updated_at":     task.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		event.Status = TaskBillingEventLogPending
		event.UpdatedAt = common.GetTimestamp()
		if err := tx.Model(&TaskBillingEvent{}).Where("id = ?", event.ID).Updates(map[string]any{
			"status":     event.Status,
			"updated_at": event.UpdatedAt,
		}).Error; err != nil {
			return err
		}

		mutation.Task = &task
		mutation.Event = event
		mutation.Applied = true
		return nil
	})
	return mutation, err
}

// SettleTaskBilling records successful completion. The charge remains the
// immutable reservation; upstream cost fields never alter it.
func SettleTaskBilling(taskID int64) (*TaskBillingMutation, error) {
	if taskID <= 0 {
		return nil, errors.New("invalid task id")
	}
	mutation := &TaskBillingMutation{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).Where("id = ?", taskID).First(&task).Error; err != nil {
			return err
		}
		eventKey := taskBillingEventKey(task.ID, TaskBillingEventSettle)
		if task.BillingStatus == TaskBillingStatusSettled {
			event, err := getTaskBillingEventTx(tx, eventKey)
			if err != nil {
				return err
			}
			mutation.Task = &task
			mutation.Event = event
			return nil
		}
		if task.Status != TaskStatusSuccess || task.BillingStatus != TaskBillingStatusReserved {
			return fmt.Errorf("%w: cannot settle task status=%q billing_status=%q", ErrTaskBillingInvalidState, task.Status, task.BillingStatus)
		}
		if _, err := getVerifiedTaskReserveEventTx(tx, &task); err != nil {
			return err
		}
		event := newTaskBillingEvent(&task, TaskBillingEventSettle, task.ReservedQuota, "task completed")
		event.Status = TaskBillingEventLogPending
		if err := tx.Create(event).Error; err != nil {
			return err
		}
		task.BillingStatus = TaskBillingStatusSettled
		task.NextPollAt = 0
		task.UpdatedAt = common.GetTimestamp()
		if err := tx.Model(&Task{}).Where("id = ?", task.ID).Updates(map[string]any{
			"billing_status": task.BillingStatus,
			"next_poll_at":   task.NextPollAt,
			"updated_at":     task.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		mutation.Task = &task
		mutation.Event = event
		mutation.Applied = true
		return nil
	})
	return mutation, err
}

// RefundTaskBilling atomically returns the full immutable reservation and
// clears the legacy current quota. A unique refund event makes retries safe.
func RefundTaskBilling(taskID int64, reason string) (*TaskBillingMutation, error) {
	if taskID <= 0 {
		return nil, errors.New("invalid task id")
	}
	mutation := &TaskBillingMutation{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).Where("id = ?", taskID).First(&task).Error; err != nil {
			return err
		}
		eventKey := taskBillingEventKey(task.ID, TaskBillingEventRefund)
		if task.BillingStatus == TaskBillingStatusRefunded {
			event, err := getTaskBillingEventTx(tx, eventKey)
			if err != nil {
				return err
			}
			mutation.Task = &task
			mutation.Event = event
			return nil
		}
		if task.BillingStatus == TaskBillingStatusSettled {
			return fmt.Errorf("%w: settled task cannot be refunded", ErrTaskBillingInvalidState)
		}
		if task.BillingStatus != TaskBillingStatusRefundPending {
			return fmt.Errorf("%w: cannot refund from %q", ErrTaskBillingInvalidState, task.BillingStatus)
		}
		reserveEvent, err := getVerifiedTaskReserveEventTx(tx, &task)
		if err != nil {
			return err
		}
		quota := reserveEvent.Quota
		if reason == "" {
			reason = task.FailReason
		}
		event := newTaskBillingEvent(&task, TaskBillingEventRefund, quota, reason)
		if err := tx.Create(event).Error; err != nil {
			return err
		}

		var user User
		if err := lockForUpdate(tx.Unscoped()).Where("id = ?", task.UserId).First(&user).Error; err != nil {
			return err
		}
		userUpdates := map[string]any{
			"used_quota": gorm.Expr("CASE WHEN used_quota >= ? THEN used_quota - ? ELSE 0 END", quota, quota),
		}
		switch task.PrivateData.BillingSource {
		case TaskBillingSourceSubscription:
			if err := RefundSubscriptionPreConsumeTx(tx, taskBillingEventKey(task.ID, TaskBillingEventReserve)); err != nil {
				return err
			}
		case TaskBillingSourceWallet:
			userUpdates["quota"] = gorm.Expr("quota + ?", quota)
		default:
			return fmt.Errorf("%w: unknown funding source %q", ErrTaskBillingInvalidState, task.PrivateData.BillingSource)
		}

		if task.PrivateData.TokenId > 0 && quota > 0 {
			var token Token
			err := lockForUpdate(tx.Unscoped()).Where("id = ? AND user_id = ?", task.PrivateData.TokenId, task.UserId).First(&token).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && !token.UnlimitedQuota {
				if err := tx.Unscoped().Model(&Token{}).Where("id = ?", token.Id).Updates(map[string]any{
					"remain_quota":  gorm.Expr("remain_quota + ?", quota),
					"used_quota":    gorm.Expr("CASE WHEN used_quota >= ? THEN used_quota - ? ELSE 0 END", quota, quota),
					"accessed_time": common.GetTimestamp(),
				}).Error; err != nil {
					return err
				}
			}
		}

		if err := tx.Unscoped().Model(&User{}).Where("id = ?", task.UserId).Updates(userUpdates).Error; err != nil {
			return err
		}
		if task.ChannelId > 0 {
			if err := tx.Model(&Channel{}).Where("id = ?", task.ChannelId).
				Update("used_quota", gorm.Expr("CASE WHEN used_quota >= ? THEN used_quota - ? ELSE 0 END", quota, quota)).Error; err != nil {
				return err
			}
		}

		task.Quota = 0
		task.BillingStatus = TaskBillingStatusRefunded
		task.NextPollAt = 0
		task.UpdatedAt = common.GetTimestamp()
		if err := tx.Model(&Task{}).Where("id = ?", task.ID).Updates(map[string]any{
			"quota":          task.Quota,
			"billing_status": task.BillingStatus,
			"next_poll_at":   task.NextPollAt,
			"updated_at":     task.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		event.Status = TaskBillingEventLogPending
		event.UpdatedAt = common.GetTimestamp()
		if err := tx.Model(&TaskBillingEvent{}).Where("id = ?", event.ID).Updates(map[string]any{
			"status":     event.Status,
			"updated_at": event.UpdatedAt,
		}).Error; err != nil {
			return err
		}

		mutation.Task = &task
		mutation.Event = event
		mutation.Applied = true
		return nil
	})
	return mutation, err
}

// TransitionTaskToRefundPending atomically records the failed/unknown submit
// state together with refund_pending. The refund worker can therefore recover
// even if the process exits immediately after this update.
func TransitionTaskToRefundPending(taskID int64, fromStatus TaskStatus, terminalStatus TaskStatus, reason string) (bool, error) {
	if taskID <= 0 {
		return false, errors.New("invalid task id")
	}
	if terminalStatus != TaskStatusFailure && terminalStatus != TaskStatusSubmitUnknown {
		return false, fmt.Errorf("invalid refund terminal status: %s", terminalStatus)
	}
	if fromStatus == TaskStatusSuccess {
		return false, nil
	}
	now := common.GetTimestamp()
	result := DB.Model(&Task{}).
		Where("id = ? AND status = ? AND billing_status = ?", taskID, fromStatus, TaskBillingStatusReserved).
		Updates(map[string]any{
			"status":         terminalStatus,
			"billing_status": TaskBillingStatusRefundPending,
			"fail_reason":    reason,
			"progress":       "100%",
			"finish_time":    now,
			"next_poll_at":   0,
			"poll_failures":  0,
			"updated_at":     now,
		})
	return result.RowsAffected > 0, result.Error
}

// AcceptTaskSubmission binds the private upstream ID only after the durable
// reservation has committed. It is idempotent for the same upstream ID.
func AcceptTaskSubmission(taskID int64, upstreamTaskID string, submittedStatus TaskStatus, data json.RawMessage) (*Task, bool, error) {
	if taskID <= 0 {
		return nil, false, errors.New("invalid task id")
	}
	if upstreamTaskID == "" {
		return nil, false, errors.New("upstream task id is empty")
	}
	switch submittedStatus {
	case TaskStatusSubmitted, TaskStatusQueued, TaskStatusInProgress:
	default:
		return nil, false, fmt.Errorf("invalid submitted task status: %s", submittedStatus)
	}

	var accepted Task
	applied := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", taskID).First(&accepted).Error; err != nil {
			return err
		}
		if accepted.BillingStatus != TaskBillingStatusReserved {
			return fmt.Errorf("%w: cannot accept submission from billing status %q", ErrTaskBillingInvalidState, accepted.BillingStatus)
		}
		if accepted.Status != TaskStatusSubmitting {
			if accepted.PrivateData.UpstreamTaskID == upstreamTaskID && accepted.Status == submittedStatus {
				return nil
			}
			return fmt.Errorf("%w: cannot accept submission from task status %q", ErrTaskBillingInvalidState, accepted.Status)
		}
		accepted.PrivateData.UpstreamTaskID = upstreamTaskID
		accepted.Status = submittedStatus
		if accepted.Progress == "" || accepted.Progress == "0%" {
			accepted.Progress = "0%"
		}
		if len(data) > 0 {
			accepted.Data = data
		}
		accepted.UpdatedAt = common.GetTimestamp()
		updates := map[string]any{
			"private_data": accepted.PrivateData,
			"status":       accepted.Status,
			"progress":     accepted.Progress,
			"updated_at":   accepted.UpdatedAt,
		}
		if len(data) > 0 {
			updates["data"] = accepted.Data
		}
		if err := tx.Model(&Task{}).Where("id = ?", accepted.ID).Updates(updates).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	return &accepted, applied, err
}

func FindRefundPendingTasks(limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}
	now := GetDBTimestamp()
	var tasks []*Task
	err := DB.Where("billing_status = ? AND (next_poll_at = 0 OR next_poll_at <= ?)", TaskBillingStatusRefundPending, now).
		Order("next_poll_at asc, id asc").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func FindStaleReservePendingTasks(cutoff int64, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}
	var tasks []*Task
	err := DB.Where("billing_status = ? AND status = ? AND updated_at < ?", TaskBillingStatusReservePending, TaskStatusSubmitting, cutoff).
		Order("id asc").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func AbandonStaleTaskReservation(taskID, cutoff int64, reason string) (bool, error) {
	if taskID <= 0 {
		return false, errors.New("invalid task id")
	}
	now := common.GetTimestamp()
	result := DB.Model(&Task{}).
		Where("id = ? AND billing_status = ? AND status = ? AND updated_at < ?", taskID, TaskBillingStatusReservePending, TaskStatusSubmitting, cutoff).
		Updates(map[string]any{
			"status":         TaskStatusFailure,
			"billing_status": TaskBillingStatusRefunded,
			"fail_reason":    reason,
			"progress":       "100%",
			"finish_time":    now,
			"next_poll_at":   0,
			"poll_failures":  0,
			"updated_at":     now,
		})
	return result.RowsAffected > 0, result.Error
}

func FindStaleSubmittingReservedTasks(cutoff int64, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}
	var tasks []*Task
	err := DB.Where("billing_status = ? AND status = ? AND submit_time > 0 AND submit_time < ?",
		TaskBillingStatusReserved, TaskStatusSubmitting, cutoff).
		Order("id asc").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

// ExpireStaleSubmittingReservedTask closes the crash window after quota was
// reserved but no upstream task ID was durably accepted. The timeout predicate
// stays in the atomic update so a concurrent successful submission wins safely.
func ExpireStaleSubmittingReservedTask(taskID, cutoff int64, reason string) (bool, error) {
	if taskID <= 0 {
		return false, errors.New("invalid task id")
	}
	now := GetDBTimestamp()
	result := DB.Model(&Task{}).
		Where("id = ? AND billing_status = ? AND status = ? AND submit_time > 0 AND submit_time < ?",
			taskID, TaskBillingStatusReserved, TaskStatusSubmitting, cutoff).
		Updates(map[string]any{
			"status":         TaskStatusSubmitUnknown,
			"billing_status": TaskBillingStatusRefundPending,
			"fail_reason":    reason,
			"progress":       "100%",
			"finish_time":    now,
			"next_poll_at":   0,
			"poll_failures":  0,
			"updated_at":     now,
		})
	return result.RowsAffected > 0, result.Error
}

// FindRefundTransitionPendingTasks finds durable tasks that reached a failure
// terminal status before refund_pending was persisted. This is a recovery
// fence for older polling code and process exits between those two writes.
func FindRefundTransitionPendingTasks(limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}
	now := GetDBTimestamp()
	var tasks []*Task
	err := DB.Where("billing_status = ? AND status IN ? AND (next_poll_at = 0 OR next_poll_at <= ?)",
		TaskBillingStatusReserved, []TaskStatus{TaskStatusFailure, TaskStatusSubmitUnknown}, now).
		Order("next_poll_at asc, id asc").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func FindSettlementPendingTasks(limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}
	now := GetDBTimestamp()
	var tasks []*Task
	err := DB.Where("billing_status = ? AND status = ? AND (next_poll_at = 0 OR next_poll_at <= ?)",
		TaskBillingStatusReserved, TaskStatusSuccess, now).
		Order("next_poll_at asc, id asc").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func DeferTaskBillingWork(taskID int64, billingStatus TaskBillingStatus, delay time.Duration) (bool, error) {
	if taskID <= 0 || billingStatus == "" {
		return false, errors.New("invalid task billing retry")
	}
	delaySeconds := int64(delay / time.Second)
	if delaySeconds < 1 {
		delaySeconds = 1
	}
	now := GetDBTimestamp()
	result := DB.Model(&Task{}).
		Where("id = ? AND billing_status = ?", taskID, billingStatus).
		Updates(map[string]any{
			"next_poll_at": now + delaySeconds,
			"updated_at":   now,
		})
	return result.RowsAffected > 0, result.Error
}

func FindPendingTaskBillingEventLogs(limit int) ([]*TaskBillingEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	now := GetDBTimestamp()
	var events []*TaskBillingEvent
	err := DB.Where("status = ? OR (status = ? AND locked_until < ?)", TaskBillingEventLogPending, TaskBillingEventLogging, now).
		Order("id asc").
		Limit(limit).
		Find(&events).Error
	return events, err
}

func HasPendingTaskBillingWork() bool {
	now := GetDBTimestamp()
	reserveCutoff := now - int64((2*time.Minute)/time.Second)
	submitCutoff := now - TaskSubmissionRecoveryDelaySeconds
	var taskID int64
	taskQuery := DB.Model(&Task{}).
		Where("(billing_status = ? AND status = ? AND updated_at < ?) OR (billing_status = ? AND (next_poll_at = 0 OR next_poll_at <= ?))",
			TaskBillingStatusReservePending, TaskStatusSubmitting, reserveCutoff, TaskBillingStatusRefundPending, now).
		Or("billing_status = ? AND status IN ? AND (next_poll_at = 0 OR next_poll_at <= ?)",
			TaskBillingStatusReserved, []TaskStatus{TaskStatusSuccess, TaskStatusFailure, TaskStatusSubmitUnknown}, now).
		Or("billing_status = ? AND status = ? AND submit_time > 0 AND submit_time < ?",
			TaskBillingStatusReserved, TaskStatusSubmitting, submitCutoff)
	err := taskQuery.
		Limit(1).
		Pluck("id", &taskID).Error
	if err == nil && taskID != 0 {
		return true
	}
	var eventID int64
	err = DB.Model(&TaskBillingEvent{}).
		Where("status = ? OR (status = ? AND locked_until < ?)", TaskBillingEventLogPending, TaskBillingEventLogging, now).
		Limit(1).
		Pluck("id", &eventID).Error
	return err == nil && eventID != 0
}

func ClaimTaskBillingEventLog(eventID int64, lease time.Duration) (*TaskBillingEvent, bool, error) {
	if lease <= 0 {
		lease = time.Minute
	}
	now := GetDBTimestamp()
	lockedUntil := now + int64(lease/time.Second)
	result := DB.Model(&TaskBillingEvent{}).
		Where("id = ? AND (status = ? OR (status = ? AND locked_until < ?))", eventID, TaskBillingEventLogPending, TaskBillingEventLogging, now).
		Updates(map[string]any{
			"status":       TaskBillingEventLogging,
			"attempts":     gorm.Expr("attempts + ?", 1),
			"locked_until": lockedUntil,
			"updated_at":   now,
		})
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, false, result.Error
	}
	var event TaskBillingEvent
	if err := DB.Where("id = ?", eventID).First(&event).Error; err != nil {
		return nil, false, err
	}
	return &event, true, nil
}

func CompleteTaskBillingEventLog(eventID int64, attempt int) (bool, error) {
	result := DB.Model(&TaskBillingEvent{}).
		Where("id = ? AND status = ? AND attempts = ?", eventID, TaskBillingEventLogging, attempt).
		Updates(map[string]any{
			"status":       TaskBillingEventCompleted,
			"last_error":   "",
			"locked_until": 0,
			"updated_at":   GetDBTimestamp(),
		})
	return result.RowsAffected > 0, result.Error
}

func RetryTaskBillingEventLog(eventID int64, attempt int, eventErr error, delay time.Duration) (bool, error) {
	message := ""
	if eventErr != nil {
		message = eventErr.Error()
	}
	delaySeconds := int64(delay / time.Second)
	if delaySeconds < 1 {
		delaySeconds = 1
	}
	now := GetDBTimestamp()
	result := DB.Model(&TaskBillingEvent{}).
		Where("id = ? AND status = ? AND attempts = ?", eventID, TaskBillingEventLogging, attempt).
		Updates(map[string]any{
			"status":       TaskBillingEventLogging,
			"last_error":   message,
			"locked_until": now + delaySeconds,
			"updated_at":   now,
		})
	return result.RowsAffected > 0, result.Error
}

func GetTaskForBillingEvent(taskID int64) (*Task, error) {
	var task Task
	if err := DB.Where("id = ?", taskID).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// EnsureTaskBillingEventLog creates the log at most once by using the unique
// billing event key as the log request_id. This makes a retry after a process
// crash observe the already-written log before completing the event.
func EnsureTaskBillingEventLog(event *TaskBillingEvent, task *Task) error {
	if event == nil || task == nil {
		return errors.New("task billing event or task is nil")
	}
	if event.Type == TaskBillingEventReserve && !common.LogConsumeEnabled {
		return nil
	}
	var count int64
	if err := LOG_DB.Model(&Log{}).Where("request_id = ?", event.EventKey).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	username, _ := GetUsernameById(task.UserId, false)
	tokenName := ""
	if task.PrivateData.TokenId > 0 {
		if token, err := GetTokenById(task.PrivateData.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	logType := LogTypeSystem
	logQuota := 0
	content := "task billing settled"
	switch event.Type {
	case TaskBillingEventReserve:
		logType = LogTypeConsume
		logQuota = event.Quota
		content = "task maximum charge reserved"
	case TaskBillingEventRefund:
		logType = LogTypeRefund
		logQuota = event.Quota
		content = event.Reason
	}
	other := map[string]interface{}{
		"is_task":          true,
		"task_id":          task.TaskID,
		"billing_event_id": event.EventKey,
		"billing_event":    event.Type,
		"billing_source":   task.PrivateData.BillingSource,
		"reserved_quota":   task.ReservedQuota,
		"final_quota":      task.Quota,
	}
	if task.PrivateData.SubscriptionId > 0 {
		other["subscription_id"] = task.PrivateData.SubscriptionId
	}
	if task.PrivateData.NodeName != "" {
		other["node_name"] = task.PrivateData.NodeName
	}
	if context := task.PrivateData.BillingContext; context != nil {
		other["model_price"] = context.ModelPrice
		other["group_ratio"] = context.GroupRatio
		if len(context.OtherRatios) > 0 {
			other["other_ratios"] = context.OtherRatios
		}
		if context.PriceVersion != "" {
			other["price_version"] = context.PriceVersion
		}
		if context.QuoteBasis != "" {
			other["quote_basis"] = context.QuoteBasis
		}
		if context.QuotaClamp != nil {
			other["admin_info"] = map[string]interface{}{
				"quota_saturation": context.QuotaClamp.AuditMap(),
			}
		}
	}
	if task.Properties.UpstreamModelName != "" && task.Properties.UpstreamModelName != task.Properties.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = task.Properties.UpstreamModelName
	}
	log := &Log{
		UserId:    task.UserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
		TokenName: tokenName,
		ModelName: taskModelNameForBillingEvent(task),
		Quota:     logQuota,
		ChannelId: task.ChannelId,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		RequestId: event.EventKey,
		Other:     common.MapToJsonStr(other),
	}
	return createLog(log)
}

func taskModelNameForBillingEvent(task *Task) string {
	if context := task.PrivateData.BillingContext; context != nil && context.OriginModelName != "" {
		return context.OriginModelName
	}
	return task.Properties.OriginModelName
}
