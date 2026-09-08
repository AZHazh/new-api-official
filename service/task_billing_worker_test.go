package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeDurableTask(userID, channelID, tokenID int) *model.Task {
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	task.Status = model.TaskStatusSubmitting
	task.BillingStatus = model.TaskBillingStatusReservePending
	task.Quota = 0
	task.ReservedQuota = 0
	return task
}

func getDurableTask(t *testing.T, taskID int64) *model.Task {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.Where("id = ?", taskID).First(&task).Error)
	return &task
}

func getUserBillingStats(t *testing.T, userID int) (quota, usedQuota, requestCount int) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota", "used_quota", "request_count").Where("id = ?", userID).First(&user).Error)
	return user.Quota, user.UsedQuota, user.RequestCount
}

func getChannelUsedQuota(t *testing.T, channelID int) int64 {
	t.Helper()
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", channelID).First(&channel).Error)
	return channel.UsedQuota
}

func countTaskBillingEvents(t *testing.T, taskID int64, eventType model.TaskBillingEventType) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.TaskBillingEvent{}).
		Where("task_id = ? AND type = ?", taskID, eventType).
		Count(&count).Error)
	return count
}

func TestDurableTaskBillingWalletRefundIsIdempotent(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID = 101, 101, 101
	const initialUserQuota, initialTokenQuota, reservedQuota = 10_000, 8_000, 3_000

	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-durable-wallet", initialTokenQuota)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, tokenID)
	require.NoError(t, task.Insert())

	first, err := ReserveTaskQuotaDurably(ctx, task.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)
	require.True(t, first.Applied)
	second, err := ReserveTaskQuotaDurably(ctx, task.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)
	assert.False(t, second.Applied)

	userQuota, userUsed, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, initialUserQuota-reservedQuota, userQuota)
	assert.Equal(t, reservedQuota, userUsed)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, initialTokenQuota-reservedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, reservedQuota, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, int64(reservedQuota), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(1), countTaskBillingEvents(t, task.ID, model.TaskBillingEventReserve))

	accepted, won, err := AcceptTaskSubmissionDurably(task.ID, "upstream-private-id", model.TaskStatusQueued, json.RawMessage(`{"status":"queued"}`))
	require.NoError(t, err)
	require.True(t, won)
	assert.Equal(t, "upstream-private-id", accepted.PrivateData.UpstreamTaskID)

	won, err = MarkTaskRefundPendingDurably(task.ID, model.TaskStatusQueued, model.TaskStatusFailure, "upstream failed")
	require.NoError(t, err)
	require.True(t, won)
	summary := RunTaskBillingWorkerOnce(ctx, 20)
	assert.Equal(t, 1, summary.Refunded)

	refunded := getDurableTask(t, task.ID)
	assert.Equal(t, model.TaskBillingStatusRefunded, refunded.BillingStatus)
	assert.Zero(t, refunded.Quota)
	assert.Equal(t, reservedQuota, refunded.ReservedQuota)
	userQuota, userUsed, requestCount = getUserBillingStats(t, userID)
	assert.Equal(t, initialUserQuota, userQuota)
	assert.Zero(t, userUsed)
	assert.Equal(t, 1, requestCount, "refund must not decrement or duplicate request_count")
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(1), countTaskBillingEvents(t, task.ID, model.TaskBillingEventRefund))

	secondPass := RunTaskBillingWorkerOnce(ctx, 20)
	assert.Zero(t, secondPass.Refunded)
	userQuota, userUsed, requestCount = getUserBillingStats(t, userID)
	assert.Equal(t, initialUserQuota, userQuota)
	assert.Zero(t, userUsed)
	assert.Equal(t, 1, requestCount)
}

func TestDurableTaskBillingTokenInsufficientRollsBackEverything(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID = 102, 102, 102

	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-durable-insufficient", 500)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, tokenID)
	require.NoError(t, task.Insert())

	_, err := ReserveTaskQuotaDurably(ctx, task.ID, 1_000, "wallet_only")
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrTaskBillingInsufficientTokenQuota))
	userQuota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 10_000, userQuota)
	assert.Zero(t, usedQuota)
	assert.Zero(t, requestCount)
	assert.Equal(t, 500, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	assert.Zero(t, countTaskBillingEvents(t, task.ID, model.TaskBillingEventReserve))
	assert.Equal(t, model.TaskBillingStatusReservePending, getDurableTask(t, task.ID).BillingStatus)
}

func TestDurableTaskBillingRejectsReservationOutsideSubmittingState(t *testing.T) {
	truncate(t)
	const userID, channelID = 115, 115
	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, 0)
	task.Status = model.TaskStatusQueued
	require.NoError(t, task.Insert())

	_, err := ReserveTaskQuotaDurably(context.Background(), task.ID, 500, "wallet_only")
	require.ErrorIs(t, err, model.ErrTaskBillingInvalidState)
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000, quota)
	assert.Zero(t, usedQuota)
	assert.Zero(t, requestCount)
	assert.Zero(t, countTaskBillingEvents(t, task.ID, model.TaskBillingEventReserve))
}

func TestDurableTaskBillingSuccessKeepsFrozenReservation(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID = 103, 103, 103
	const reservedQuota = 2_000

	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-durable-success", 10_000)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, tokenID)
	require.NoError(t, task.Insert())
	_, err := ReserveTaskQuotaDurably(ctx, task.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)
	_, _, err = AcceptTaskSubmissionDurably(task.ID, "upstream-success", model.TaskStatusQueued, nil)
	require.NoError(t, err)

	queued := getDurableTask(t, task.ID)
	queued.Status = model.TaskStatusSuccess
	queued.Progress = "100%"
	queued.FinishTime = time.Now().Unix()
	won, err := queued.UpdateWithStatus(model.TaskStatusQueued)
	require.NoError(t, err)
	require.True(t, won)
	settled, err := SettleTaskQuotaDurably(ctx, task.ID)
	require.NoError(t, err)
	require.True(t, settled.Applied)
	settledAgain, err := SettleTaskQuotaDurably(ctx, task.ID)
	require.NoError(t, err)
	assert.False(t, settledAgain.Applied)

	completed := getDurableTask(t, task.ID)
	assert.Equal(t, model.TaskBillingStatusSettled, completed.BillingStatus)
	assert.Equal(t, reservedQuota, completed.ReservedQuota)
	assert.Equal(t, reservedQuota, completed.Quota)
	assert.Equal(t, int64(1), countTaskBillingEvents(t, task.ID, model.TaskBillingEventSettle))
	won, err = MarkTaskRefundPendingDurably(task.ID, model.TaskStatusSuccess, model.TaskStatusFailure, "late failure")
	require.NoError(t, err)
	assert.False(t, won, "settled tasks cannot be moved back to refund_pending")
}

func TestDurableTaskBillingSubscriptionRefund(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID, planID, subscriptionID = 104, 104, 104, 104, 104
	const reservedQuota int64 = 1_500

	seedUser(t, userID, 500)
	seedToken(t, tokenID, userID, "sk-durable-subscription", 10_000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:               planID,
		Title:            "durable test",
		DurationUnit:     model.SubscriptionDurationMonth,
		DurationValue:    1,
		QuotaResetPeriod: model.SubscriptionResetNever,
	}).Error)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:                  subscriptionID,
		UserId:              userID,
		PlanId:              planID,
		AmountTotal:         10_000,
		AmountUsed:          2_000,
		Status:              "active",
		StartTime:           time.Now().Add(-time.Hour).Unix(),
		EndTime:             time.Now().Add(24 * time.Hour).Unix(),
		AllowWalletOverflow: false,
	}).Error)
	task := makeDurableTask(userID, channelID, tokenID)
	require.NoError(t, task.Insert())

	reserved, err := ReserveTaskQuotaDurably(ctx, task.ID, int(reservedQuota), "subscription_only")
	require.NoError(t, err)
	assert.Equal(t, BillingSourceSubscription, reserved.Task.PrivateData.BillingSource)
	assert.Equal(t, subscriptionID, reserved.Task.PrivateData.SubscriptionId)
	assert.Equal(t, int64(3_500), getSubscriptionUsed(t, subscriptionID))
	userQuota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 500, userQuota, "subscription reserve must not debit wallet")
	assert.Equal(t, int(reservedQuota), usedQuota)
	assert.Equal(t, 1, requestCount)

	won, err := MarkTaskRefundPendingDurably(task.ID, model.TaskStatusSubmitting, model.TaskStatusSubmitUnknown, "submission result unknown")
	require.NoError(t, err)
	require.True(t, won)
	_, err = RefundTaskQuotaDurably(ctx, task.ID, "submission result unknown")
	require.NoError(t, err)
	assert.Equal(t, int64(2_000), getSubscriptionUsed(t, subscriptionID))
	userQuota, usedQuota, requestCount = getUserBillingStats(t, userID)
	assert.Equal(t, 500, userQuota)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)

	var record model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", reserved.Event.EventKey).First(&record).Error)
	assert.Equal(t, "refunded", record.Status)
}

func TestDurableTaskBillingWorkerRecoversTerminalRefundGap(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, channelID, reservedQuota = 105, 105, 1_000

	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, 0)
	require.NoError(t, task.Insert())
	_, err := ReserveTaskQuotaDurably(ctx, task.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)

	// Simulate a process exit after the execution status reached FAILURE but
	// before refund_pending was written.
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
		"status":      model.TaskStatusFailure,
		"fail_reason": "poll failed",
		"progress":    "100%",
	}).Error)
	summary := RunTaskBillingWorkerOnce(ctx, 20)
	assert.Equal(t, 1, summary.Refunded)
	assert.Equal(t, model.TaskBillingStatusRefunded, getDurableTask(t, task.ID).BillingStatus)
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000, quota)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
}

func TestDurableTaskBillingBypassesBatchUpdateAndSupportsZeroPrice(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	oldBatchUpdate := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() { common.BatchUpdateEnabled = oldBatchUpdate })
	const userID, channelID = 106, 106

	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, 0)
	require.NoError(t, task.Insert())
	mutation, err := ReserveTaskQuotaDurably(ctx, task.ID, 0, "subscription_only")
	require.NoError(t, err)
	assert.True(t, mutation.Applied)
	assert.Equal(t, BillingSourceWallet, mutation.Task.PrivateData.BillingSource)
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000, quota)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount, "durable accounting must write the primary DB immediately")

	won, err := MarkTaskRefundPendingDurably(task.ID, model.TaskStatusSubmitting, model.TaskStatusFailure, "free task failed")
	require.NoError(t, err)
	require.True(t, won)
	_, err = RefundTaskQuotaDurably(ctx, task.ID, "free task failed")
	require.NoError(t, err)
	assert.Equal(t, model.TaskBillingStatusRefunded, getDurableTask(t, task.ID).BillingStatus)
}

func TestDurableTaskBillingConcurrentReserveAppliesOnce(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, channelID, reservedQuota = 107, 107, 1_250

	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, 0)
	require.NoError(t, task.Insert())

	type result struct {
		mutation *model.TaskBillingMutation
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	for range 2 {
		go func() {
			defer workers.Done()
			<-start
			mutation, err := ReserveTaskQuotaDurably(ctx, task.ID, reservedQuota, "wallet_only")
			results <- result{mutation: mutation, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	applied := 0
	for call := range results {
		require.NoError(t, call.err)
		require.NotNil(t, call.mutation)
		if call.mutation.Applied {
			applied++
		}
	}
	assert.Equal(t, 1, applied)
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000-reservedQuota, quota)
	assert.Equal(t, reservedQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(1), countTaskBillingEvents(t, task.ID, model.TaskBillingEventReserve))
}

func TestDurableTaskBillingConcurrentRefundAppliesOnce(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, channelID, reservedQuota = 116, 116, 1_100
	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, 0)
	require.NoError(t, task.Insert())
	_, err := ReserveTaskQuotaDurably(ctx, task.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)
	won, err := MarkTaskRefundPendingDurably(task.ID, model.TaskStatusSubmitting, model.TaskStatusFailure, "submit failed")
	require.NoError(t, err)
	require.True(t, won)

	type result struct {
		mutation *model.TaskBillingMutation
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	for range 2 {
		go func() {
			defer workers.Done()
			<-start
			mutation, err := RefundTaskQuotaDurably(ctx, task.ID, "submit failed")
			results <- result{mutation: mutation, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	applied := 0
	for call := range results {
		require.NoError(t, call.err)
		require.NotNil(t, call.mutation)
		if call.mutation.Applied {
			applied++
		}
	}
	assert.Equal(t, 1, applied)
	assert.Equal(t, int64(1), countTaskBillingEvents(t, task.ID, model.TaskBillingEventRefund))
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000, quota)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
}

func TestTaskBillingEventLogLeaseIsAttemptFenced(t *testing.T) {
	truncate(t)
	event := &model.TaskBillingEvent{
		EventKey:     "task:lease-fence:reserve",
		TaskID:       999,
		TaskPublicID: "task_lease_fence",
		Type:         model.TaskBillingEventReserve,
		Status:       model.TaskBillingEventLogPending,
		Quota:        100,
	}
	require.NoError(t, model.DB.Create(event).Error)

	first, won, err := model.ClaimTaskBillingEventLog(event.ID, time.Minute)
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, model.DB.Model(&model.TaskBillingEvent{}).Where("id = ?", event.ID).
		Update("locked_until", model.GetDBTimestamp()-1).Error)
	second, won, err := model.ClaimTaskBillingEventLog(event.ID, time.Minute)
	require.NoError(t, err)
	require.True(t, won)
	require.Greater(t, second.Attempts, first.Attempts)

	completed, err := model.CompleteTaskBillingEventLog(event.ID, first.Attempts)
	require.NoError(t, err)
	assert.False(t, completed, "an expired worker must not complete a newer lease")
	retried, err := model.RetryTaskBillingEventLog(event.ID, first.Attempts, errors.New("stale worker"), time.Minute)
	require.NoError(t, err)
	assert.False(t, retried, "an expired worker must not reset a newer lease")
	completed, err = model.CompleteTaskBillingEventLog(event.ID, second.Attempts)
	require.NoError(t, err)
	assert.True(t, completed)
}

func TestTaskBillingEventLogFailureBacksOff(t *testing.T) {
	truncate(t)
	event := &model.TaskBillingEvent{
		EventKey:     "task:log-backoff:reserve",
		TaskID:       998,
		TaskPublicID: "task_log_backoff",
		Type:         model.TaskBillingEventReserve,
		Status:       model.TaskBillingEventLogPending,
		Quota:        100,
	}
	require.NoError(t, model.DB.Create(event).Error)
	claimed, won, err := model.ClaimTaskBillingEventLog(event.ID, time.Minute)
	require.NoError(t, err)
	require.True(t, won)
	retried, err := model.RetryTaskBillingEventLog(event.ID, claimed.Attempts, errors.New("log database unavailable"), time.Minute)
	require.NoError(t, err)
	require.True(t, retried)

	due, err := model.FindPendingTaskBillingEventLogs(10)
	require.NoError(t, err)
	assert.Empty(t, due)
	assert.False(t, model.HasPendingTaskBillingWork(), "a leased retry should not keep scheduling empty workers")
	require.NoError(t, model.DB.Model(&model.TaskBillingEvent{}).Where("id = ?", event.ID).
		Update("locked_until", model.GetDBTimestamp()-1).Error)
	due, err = model.FindPendingTaskBillingEventLogs(10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, event.ID, due[0].ID)
}

func TestDurableTaskBillingWorkerAbandonsOnlyStaleUnreservedTask(t *testing.T) {
	truncate(t)
	const userID, channelID = 108, 108
	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)

	fresh := makeDurableTask(userID, channelID, 0)
	require.NoError(t, fresh.Insert())
	assert.False(t, model.HasPendingTaskBillingWork(), "a request still entering reservation is not recovery work")

	staleAt := model.GetDBTimestamp() - int64((3*time.Minute)/time.Second)
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", fresh.ID).
		Updates(map[string]any{"created_at": staleAt, "updated_at": staleAt}).Error)
	assert.True(t, model.HasPendingTaskBillingWork())

	summary := RunTaskBillingWorkerOnce(context.Background(), 20)
	assert.Zero(t, summary.Failed)
	abandoned := getDurableTask(t, fresh.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), abandoned.Status)
	assert.Equal(t, model.TaskBillingStatusRefunded, abandoned.BillingStatus)
	assert.Zero(t, abandoned.Quota)
	assert.Zero(t, abandoned.ReservedQuota)
	assert.Zero(t, countTaskBillingEvents(t, fresh.ID, model.TaskBillingEventReserve))
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000, quota)
	assert.Zero(t, usedQuota)
	assert.Zero(t, requestCount)
}

func TestDurableTaskBillingWorkerRefundsTimedOutReservedSubmission(t *testing.T) {
	truncate(t)
	const userID, channelID, reservedQuota = 112, 112, 700
	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, 0)
	require.NoError(t, task.Insert())
	_, err := ReserveTaskQuotaDurably(context.Background(), task.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)
	assert.False(t, model.HasPendingTaskBillingWork(), "an active upstream submission is not expired")

	staleSubmitTime := model.GetDBTimestamp() - model.TaskSubmissionRecoveryDelaySeconds - 1
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).
		Update("submit_time", staleSubmitTime).Error)
	assert.True(t, model.HasPendingTaskBillingWork())

	summary := RunTaskBillingWorkerOnce(context.Background(), 20)
	assert.Equal(t, 1, summary.Refunded)
	assert.Zero(t, summary.Failed)
	refunded := getDurableTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSubmitUnknown), refunded.Status)
	assert.Equal(t, model.TaskBillingStatusRefunded, refunded.BillingStatus)
	assert.Equal(t, "task submission result remained unknown until timeout", refunded.FailReason)
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000, quota)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
}

func TestSeedancePollingDrainsBillingWhenPollingDisabledAndAdaptorMissing(t *testing.T) {
	truncate(t)
	const userID, channelID, reservedQuota = 109, 109, 800
	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, 0)
	task.Platform = constant.TaskPlatformSeedance
	require.NoError(t, task.Insert())
	_, err := ReserveTaskQuotaDurably(context.Background(), task.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)
	won, err := MarkTaskRefundPendingDurably(task.ID, model.TaskStatusSubmitting, model.TaskStatusFailure, "submit failed")
	require.NoError(t, err)
	require.True(t, won)

	previousUpdateTask := constant.UpdateTask
	previousFactory := GetTaskAdaptorFunc
	constant.UpdateTask = false
	GetTaskAdaptorFunc = nil
	t.Cleanup(func() {
		constant.UpdateTask = previousUpdateTask
		GetTaskAdaptorFunc = previousFactory
	})

	RunSeedanceTaskPollingOnce(context.Background(), nil)
	refunded := getDurableTask(t, task.ID)
	assert.Equal(t, model.TaskBillingStatusRefunded, refunded.BillingStatus)
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000, quota)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
}

func TestTaskPollBackoffCapsRetryAfterAndDueSelection(t *testing.T) {
	truncate(t)
	const userID, channelID = 110, 110
	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, 0, "", 0)
	task.Platform = constant.TaskPlatformSeedance
	task.Status = model.TaskStatusQueued
	task.Progress = "0%"
	task.PrivateData.UpstreamTaskID = "upstream-backoff"
	require.NoError(t, task.Insert())

	before := model.GetDBTimestamp()
	require.NoError(t, model.RecordTaskPollFailure(task.ID, 86_400, 300))
	after := model.GetDBTimestamp()
	deferred := getDurableTask(t, task.ID)
	assert.Equal(t, 1, deferred.PollFailures)
	assert.GreaterOrEqual(t, deferred.NextPollAt, before+300)
	assert.LessOrEqual(t, deferred.NextPollAt, after+300)
	assert.False(t, model.HasUnfinishedSyncTasksByPlatform(constant.TaskPlatformSeedance), "backed-off tasks are not due")

	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).
		Update("next_poll_at", model.GetDBTimestamp()-1).Error)
	assert.True(t, model.HasUnfinishedSyncTasksByPlatform(constant.TaskPlatformSeedance))
}

func TestSuccessfulReservedTaskCannotBeRefunded(t *testing.T) {
	truncate(t)
	const userID, channelID, reservedQuota = 111, 111, 900
	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, 0)
	require.NoError(t, task.Insert())
	_, err := ReserveTaskQuotaDurably(context.Background(), task.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)
	accepted, _, err := AcceptTaskSubmissionDurably(task.ID, "upstream-success-gap", model.TaskStatusQueued, nil)
	require.NoError(t, err)
	accepted.Status = model.TaskStatusSuccess
	accepted.Progress = "100%"
	won, err := accepted.UpdateWithStatus(model.TaskStatusQueued)
	require.NoError(t, err)
	require.True(t, won)

	assert.False(t, RefundTaskQuota(context.Background(), accepted, "late failure"))
	stored := getDurableTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
	assert.Equal(t, model.TaskBillingStatusReserved, stored.BillingStatus)
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000-reservedQuota, quota)
	assert.Equal(t, reservedQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
}

func TestDurableTaskRefundRejectsMismatchedReservationSnapshot(t *testing.T) {
	truncate(t)
	const userID, channelID, reservedQuota = 113, 113, 600
	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)
	task := makeDurableTask(userID, channelID, 0)
	require.NoError(t, task.Insert())
	_, err := ReserveTaskQuotaDurably(context.Background(), task.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)
	won, err := MarkTaskRefundPendingDurably(task.ID, model.TaskStatusSubmitting, model.TaskStatusFailure, "submit failed")
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).
		Update("reserved_quota", reservedQuota+1).Error)

	_, err = RefundTaskQuotaDurably(context.Background(), task.ID, "submit failed")
	require.ErrorIs(t, err, model.ErrTaskBillingInvalidState)
	stored := getDurableTask(t, task.ID)
	assert.Equal(t, model.TaskBillingStatusRefundPending, stored.BillingStatus)
	assert.Equal(t, int64(0), countTaskBillingEvents(t, task.ID, model.TaskBillingEventRefund))
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000-reservedQuota, quota)
	assert.Equal(t, reservedQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
}

func TestDurableTaskBillingRetryDoesNotStarveLaterRefunds(t *testing.T) {
	truncate(t)
	const userID, channelID = 114, 114
	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)

	poisoned := makeDurableTask(userID, channelID, 0)
	require.NoError(t, poisoned.Insert())
	_, err := ReserveTaskQuotaDurably(context.Background(), poisoned.ID, 600, "wallet_only")
	require.NoError(t, err)
	won, err := MarkTaskRefundPendingDurably(poisoned.ID, model.TaskStatusSubmitting, model.TaskStatusFailure, "poisoned refund")
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", poisoned.ID).
		Update("reserved_quota", 601).Error)

	healthy := makeDurableTask(userID, channelID, 0)
	require.NoError(t, healthy.Insert())
	_, err = ReserveTaskQuotaDurably(context.Background(), healthy.ID, 400, "wallet_only")
	require.NoError(t, err)
	won, err = MarkTaskRefundPendingDurably(healthy.ID, model.TaskStatusSubmitting, model.TaskStatusFailure, "healthy refund")
	require.NoError(t, err)
	require.True(t, won)

	firstPass := RunTaskBillingWorkerOnce(context.Background(), 1)
	assert.Equal(t, 1, firstPass.Failed)
	assert.Zero(t, firstPass.Refunded)
	deferred := getDurableTask(t, poisoned.ID)
	assert.Greater(t, deferred.NextPollAt, model.GetDBTimestamp())
	assert.Equal(t, model.TaskBillingStatusRefundPending, getDurableTask(t, healthy.ID).BillingStatus)

	secondPass := RunTaskBillingWorkerOnce(context.Background(), 1)
	assert.Equal(t, 1, secondPass.Refunded)
	assert.Equal(t, model.TaskBillingStatusRefunded, getDurableTask(t, healthy.ID).BillingStatus)
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 4_400, quota)
	assert.Equal(t, 600, usedQuota)
	assert.Equal(t, 2, requestCount)
}

func TestTimeoutSweepLeavesDurableSubmissionRecoveryStatesAlone(t *testing.T) {
	truncate(t)
	const userID, channelID, reservedQuota = 117, 117, 700
	seedUser(t, userID, 5_000)
	seedChannel(t, channelID)

	previousTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = previousTimeout })

	unreserved := makeDurableTask(userID, channelID, 0)
	unreserved.Platform = constant.TaskPlatformSeedance
	unreserved.SubmitTime = model.GetDBTimestamp() - 70
	require.NoError(t, unreserved.Insert())
	sweepTimedOutTasks(context.Background())
	storedUnreserved := getDurableTask(t, unreserved.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSubmitting), storedUnreserved.Status)
	assert.Equal(t, model.TaskBillingStatusReservePending, storedUnreserved.BillingStatus)

	reserved := makeDurableTask(userID, channelID, 0)
	reserved.Platform = constant.TaskPlatformSeedance
	reserved.SubmitTime = model.GetDBTimestamp() - 70
	require.NoError(t, reserved.Insert())
	_, err := ReserveTaskQuotaDurably(context.Background(), reserved.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)
	sweepTimedOutTasks(context.Background())
	storedReserved := getDurableTask(t, reserved.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSubmitting), storedReserved.Status)
	assert.Equal(t, model.TaskBillingStatusReserved, storedReserved.BillingStatus)
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000-reservedQuota, quota)
	assert.Equal(t, reservedQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
}

func TestSubscriptionCleanupPreservesActiveDurableRefundRecord(t *testing.T) {
	truncate(t)
	const userID, channelID, planID, subscriptionID, reservedQuota = 118, 118, 118, 118, 900
	seedUser(t, userID, 0)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:               planID,
		Title:            "cleanup protection",
		DurationUnit:     model.SubscriptionDurationMonth,
		DurationValue:    1,
		QuotaResetPeriod: model.SubscriptionResetNever,
	}).Error)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:          subscriptionID,
		UserId:      userID,
		PlanId:      planID,
		AmountTotal: 5_000,
		Status:      "active",
		StartTime:   time.Now().Add(-time.Hour).Unix(),
		EndTime:     time.Now().Add(24 * time.Hour).Unix(),
	}).Error)
	task := makeDurableTask(userID, channelID, 0)
	require.NoError(t, task.Insert())
	mutation, err := ReserveTaskQuotaDurably(context.Background(), task.ID, reservedQuota, "subscription_only")
	require.NoError(t, err)

	staleAt := model.GetDBTimestamp() - int64((8*24*time.Hour)/time.Second)
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).
		Where("request_id = ?", mutation.Event.EventKey).
		UpdateColumn("updated_at", staleAt).Error)
	deleted, err := model.CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)
	require.NoError(t, err)
	assert.Zero(t, deleted)
	var count int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).
		Where("request_id = ?", mutation.Event.EventKey).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	won, err := MarkTaskRefundPendingDurably(task.ID, model.TaskStatusSubmitting, model.TaskStatusFailure, "cleanup test")
	require.NoError(t, err)
	require.True(t, won)
	_, err = RefundTaskQuotaDurably(context.Background(), task.ID, "cleanup test")
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).
		Where("request_id = ?", mutation.Event.EventKey).
		UpdateColumn("updated_at", staleAt).Error)
	deleted, err = model.CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
}
