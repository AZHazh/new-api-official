package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceRefundUnfinishedSeedanceTasksRestoresReservedQuota(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 401, 401, 401
	const userQuota, tokenQuota, reservedQuota = 10_000, 8_000, 3_000
	seedUser(t, userID, userQuota)
	seedToken(t, tokenID, userID, "sk-force-refund", tokenQuota)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:     channelID,
		Name:   "Seedance",
		Type:   constant.ChannelTypeSeedance,
		Key:    "seedance-key",
		Status: common.ChannelStatusEnabled,
	}).Error)
	task := makeDurableTask(userID, channelID, tokenID)
	task.Platform = constant.TaskPlatformSeedance
	require.NoError(t, task.Insert())

	_, err := ReserveTaskQuotaDurably(context.Background(), task.ID, reservedQuota, "wallet_only")
	require.NoError(t, err)
	_, won, err := AcceptTaskSubmissionDurably(
		task.ID,
		"upstream-private-id",
		model.TaskStatusQueued,
		json.RawMessage(`{"status":"queued"}`),
	)
	require.NoError(t, err)
	require.True(t, won)

	summary, err := ForceRefundUnfinishedSeedanceTasks(
		context.Background(),
		[]int{channelID},
		"administrator forced channel change",
	)
	require.NoError(t, err)
	assert.Equal(t, []string{task.TaskID}, summary.AffectedTaskIDs)
	assert.Equal(t, 1, summary.Refunded)

	stored := getDurableTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), stored.Status)
	assert.Equal(t, model.TaskBillingStatusRefunded, stored.BillingStatus)
	assert.Zero(t, stored.Quota)
	actualUserQuota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, userQuota, actualUserQuota)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, tokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(1), countTaskBillingEvents(t, task.ID, model.TaskBillingEventRefund))

	second, err := ForceRefundUnfinishedSeedanceTasks(context.Background(), []int{channelID}, "retry")
	require.NoError(t, err)
	assert.Empty(t, second.AffectedTaskIDs)
	assert.Zero(t, second.Refunded)
	assert.Equal(t, int64(1), countTaskBillingEvents(t, task.ID, model.TaskBillingEventRefund))
}

func TestForceRefundUnfinishedSeedanceTasksClosesUnreservedTaskWithoutCredit(t *testing.T) {
	truncate(t)
	const userID, channelID = 402, 402
	seedUser(t, userID, 5_000)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:     channelID,
		Name:   "Seedance",
		Type:   constant.ChannelTypeSeedance,
		Key:    "seedance-key",
		Status: common.ChannelStatusEnabled,
	}).Error)
	task := makeDurableTask(userID, channelID, 0)
	task.Platform = constant.TaskPlatformSeedance
	require.NoError(t, task.Insert())

	summary, err := ForceRefundUnfinishedSeedanceTasks(context.Background(), []int{channelID}, "forced before reserve")
	require.NoError(t, err)
	assert.Equal(t, []string{task.TaskID}, summary.AffectedTaskIDs)
	assert.Zero(t, summary.Refunded)
	stored := getDurableTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), stored.Status)
	assert.Equal(t, model.TaskBillingStatusRefunded, stored.BillingStatus)
	quota, usedQuota, requestCount := getUserBillingStats(t, userID)
	assert.Equal(t, 5_000, quota)
	assert.Zero(t, usedQuota)
	assert.Zero(t, requestCount)
	assert.Zero(t, countTaskBillingEvents(t, task.ID, model.TaskBillingEventRefund))
}
