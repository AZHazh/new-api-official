package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

type TaskBillingWorkerSummary struct {
	Refunded int `json:"refunded"`
	Settled  int `json:"settled"`
	Logged   int `json:"logged"`
	Failed   int `json:"failed"`
}

// ReserveTaskQuotaDurably is the Seedance/task-specific pre-consume entry
// point. billingPreference accepts the same normalized values as user
// settings. The token is resolved from task.PrivateData.TokenId, so callers
// must not persist or pass the token key.
func ReserveTaskQuotaDurably(ctx context.Context, taskID int64, reservedQuota int, billingPreference string) (*model.TaskBillingMutation, error) {
	mutation, err := model.ReserveTaskBilling(taskID, reservedQuota, billingPreference)
	if err != nil {
		return nil, err
	}
	invalidateDurableTaskBillingCaches(ctx, mutation.Task)
	tryWriteTaskBillingEventLog(ctx, mutation.Event)
	return mutation, nil
}

// MarkTaskRefundPendingDurably atomically couples the terminal submit state
// with refund_pending. Explicit upstream failures use FAILURE; an interrupted
// request whose upstream result is unknown uses SUBMIT_UNKNOWN.
func MarkTaskRefundPendingDurably(taskID int64, fromStatus model.TaskStatus, terminalStatus model.TaskStatus, reason string) (bool, error) {
	return model.TransitionTaskToRefundPending(taskID, fromStatus, terminalStatus, reason)
}

// AcceptTaskSubmissionDurably persists the private upstream ID and submitted
// status before the controller returns a successful submission response.
func AcceptTaskSubmissionDurably(taskID int64, upstreamTaskID string, submittedStatus model.TaskStatus, data json.RawMessage) (*model.Task, bool, error) {
	return model.AcceptTaskSubmission(taskID, upstreamTaskID, submittedStatus, data)
}

// RefundTaskQuotaDurably applies the full refund once. Normally the worker
// invokes it after MarkTaskRefundPendingDurably; exposing it also allows an
// immediate best-effort attempt without sacrificing restart recovery.
func RefundTaskQuotaDurably(ctx context.Context, taskID int64, reason string) (*model.TaskBillingMutation, error) {
	mutation, err := model.RefundTaskBilling(taskID, reason)
	if err != nil {
		return nil, err
	}
	invalidateDurableTaskBillingCaches(ctx, mutation.Task)
	tryWriteTaskBillingEventLog(ctx, mutation.Event)
	return mutation, nil
}

// SettleTaskQuotaDurably confirms that a successful fixed-price task retains
// its reservation. No upstream cost or token value participates.
func SettleTaskQuotaDurably(ctx context.Context, taskID int64) (*model.TaskBillingMutation, error) {
	mutation, err := model.SettleTaskBilling(taskID)
	if err != nil {
		return nil, err
	}
	tryWriteTaskBillingEventLog(ctx, mutation.Event)
	return mutation, nil
}

func invalidateDurableTaskBillingCaches(ctx context.Context, task *model.Task) {
	if task == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := model.InvalidateUserCache(task.UserId); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("invalidate user cache after durable task billing failed (task=%s): %s", task.TaskID, err.Error()))
	}
	if err := model.InvalidateUserTokensCache(task.UserId); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("invalidate token cache after durable task billing failed (task=%s): %s", task.TaskID, err.Error()))
	}
}

func tryWriteTaskBillingEventLog(ctx context.Context, event *model.TaskBillingEvent) bool {
	if event == nil || event.Status == model.TaskBillingEventCompleted {
		return event != nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	claimed, won, err := model.ClaimTaskBillingEventLog(event.ID, time.Minute)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("claim task billing event log failed (event=%s): %s", event.EventKey, err.Error()))
		return false
	}
	if !won {
		return false
	}
	task, err := model.GetTaskForBillingEvent(claimed.TaskID)
	if err == nil {
		err = model.EnsureTaskBillingEventLog(claimed, task)
	}
	if err != nil {
		if _, retryErr := model.RetryTaskBillingEventLog(claimed.ID, claimed.Attempts, err, 30*time.Second); retryErr != nil {
			common.SysError(fmt.Sprintf("reset task billing event log failed (event=%s): %s", claimed.EventKey, retryErr.Error()))
		}
		logger.LogWarn(ctx, fmt.Sprintf("write task billing event log failed (event=%s): %s", claimed.EventKey, err.Error()))
		return false
	}
	completed, err := model.CompleteTaskBillingEventLog(claimed.ID, claimed.Attempts)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("complete task billing event log failed (event=%s): %s", claimed.EventKey, err.Error()))
		return false
	}
	return completed
}

// RunTaskBillingWorkerOnce recovers durable refunds, successful settlements,
// and pending log writes. It is safe to call from every async polling pass and
// concurrently on multiple nodes.
func RunTaskBillingWorkerOnce(ctx context.Context, limit int) TaskBillingWorkerSummary {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 100
	}
	summary := TaskBillingWorkerSummary{}
	reserveCutoff := model.GetDBTimestamp() - int64((2*time.Minute)/time.Second)
	staleReservations, err := model.FindStaleReservePendingTasks(reserveCutoff, limit)
	if err != nil {
		logger.LogWarn(ctx, "find stale task reservations failed: "+err.Error())
		summary.Failed++
	} else {
		for _, task := range staleReservations {
			if ctx.Err() != nil {
				return summary
			}
			_, abandonErr := model.AbandonStaleTaskReservation(task.ID, reserveCutoff, "task submission stopped before quota reservation completed")
			if abandonErr != nil {
				logger.LogWarn(ctx, fmt.Sprintf("abandon stale task reservation failed (task=%s): %s", task.TaskID, abandonErr.Error()))
				summary.Failed++
			}
		}
	}
	submitCutoff := model.GetDBTimestamp() - model.TaskSubmissionRecoveryDelaySeconds
	staleSubmissions, findErr := model.FindStaleSubmittingReservedTasks(submitCutoff, limit)
	if findErr != nil {
		logger.LogWarn(ctx, "find stale reserved task submissions failed: "+findErr.Error())
		summary.Failed++
	} else {
		for _, task := range staleSubmissions {
			if ctx.Err() != nil {
				return summary
			}
			_, expireErr := model.ExpireStaleSubmittingReservedTask(
				task.ID,
				submitCutoff,
				"task submission result remained unknown until timeout",
			)
			if expireErr != nil {
				logger.LogWarn(ctx, fmt.Sprintf("expire stale reserved task submission failed (task=%s): %s", task.TaskID, expireErr.Error()))
				summary.Failed++
			}
		}
	}

	strandedTasks, err := model.FindRefundTransitionPendingTasks(limit)
	if err != nil {
		logger.LogWarn(ctx, "find stranded task refunds failed: "+err.Error())
		summary.Failed++
	} else {
		for _, task := range strandedTasks {
			if ctx.Err() != nil {
				return summary
			}
			won, transitionErr := MarkTaskRefundPendingDurably(task.ID, task.Status, task.Status, task.FailReason)
			if transitionErr != nil {
				logger.LogWarn(ctx, fmt.Sprintf("recover task refund transition failed (task=%s): %s", task.TaskID, transitionErr.Error()))
				if _, deferErr := model.DeferTaskBillingWork(task.ID, task.BillingStatus, 30*time.Second); deferErr != nil {
					logger.LogWarn(ctx, fmt.Sprintf("defer task refund transition retry failed (task=%s): %s", task.TaskID, deferErr.Error()))
				}
				summary.Failed++
				continue
			}
			if !won {
				continue
			}
		}
	}

	refundTasks, err := model.FindRefundPendingTasks(limit)
	if err != nil {
		logger.LogWarn(ctx, "find pending task refunds failed: "+err.Error())
		summary.Failed++
	} else {
		for _, task := range refundTasks {
			if ctx.Err() != nil {
				return summary
			}
			mutation, refundErr := RefundTaskQuotaDurably(ctx, task.ID, task.FailReason)
			if refundErr != nil {
				logger.LogWarn(ctx, fmt.Sprintf("durable task refund failed (task=%s): %s", task.TaskID, refundErr.Error()))
				if _, deferErr := model.DeferTaskBillingWork(task.ID, task.BillingStatus, 30*time.Second); deferErr != nil {
					logger.LogWarn(ctx, fmt.Sprintf("defer durable task refund retry failed (task=%s): %s", task.TaskID, deferErr.Error()))
				}
				summary.Failed++
				continue
			}
			if mutation.Applied {
				summary.Refunded++
			}
		}
	}

	settlementTasks, err := model.FindSettlementPendingTasks(limit)
	if err != nil {
		logger.LogWarn(ctx, "find pending task settlements failed: "+err.Error())
		summary.Failed++
	} else {
		for _, task := range settlementTasks {
			if ctx.Err() != nil {
				return summary
			}
			mutation, settleErr := SettleTaskQuotaDurably(ctx, task.ID)
			if settleErr != nil {
				logger.LogWarn(ctx, fmt.Sprintf("durable task settlement failed (task=%s): %s", task.TaskID, settleErr.Error()))
				if _, deferErr := model.DeferTaskBillingWork(task.ID, task.BillingStatus, 30*time.Second); deferErr != nil {
					logger.LogWarn(ctx, fmt.Sprintf("defer durable task settlement retry failed (task=%s): %s", task.TaskID, deferErr.Error()))
				}
				summary.Failed++
				continue
			}
			if mutation.Applied {
				summary.Settled++
			}
		}
	}

	events, err := model.FindPendingTaskBillingEventLogs(limit)
	if err != nil {
		logger.LogWarn(ctx, "find pending task billing logs failed: "+err.Error())
		summary.Failed++
		return summary
	}
	for _, event := range events {
		if ctx.Err() != nil {
			break
		}
		if tryWriteTaskBillingEventLog(ctx, event) {
			summary.Logged++
		}
	}
	return summary
}
