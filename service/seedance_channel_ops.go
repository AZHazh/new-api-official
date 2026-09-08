package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/model"
)

type SeedanceForceRefundSummary struct {
	AffectedTaskIDs []string
	Refunded        int
}

func ForceRefundUnfinishedSeedanceTasks(
	ctx context.Context,
	channelIDs []int,
	reason string,
) (*SeedanceForceRefundSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	summary := &SeedanceForceRefundSummary{AffectedTaskIDs: []string{}}
	seen := make(map[int64]struct{})

	for pass := 0; pass < 3; pass++ {
		tasks, err := model.GetUnfinishedSeedanceTasksForChannels(channelIDs)
		if err != nil {
			return summary, err
		}
		if len(tasks) == 0 {
			return summary, nil
		}
		for _, task := range tasks {
			closedTask, applied, err := model.ForceFailSeedanceTask(task.ID, int64(task.ChannelId), reason)
			if err != nil {
				return summary, err
			}
			if !applied {
				continue
			}
			if _, ok := seen[closedTask.ID]; !ok {
				seen[closedTask.ID] = struct{}{}
				summary.AffectedTaskIDs = append(summary.AffectedTaskIDs, closedTask.TaskID)
			}
			if closedTask.BillingStatus != model.TaskBillingStatusRefundPending {
				continue
			}
			mutation, err := RefundTaskQuotaDurably(ctx, closedTask.ID, reason)
			if err != nil {
				return summary, fmt.Errorf("refund forced Seedance task %s: %w", closedTask.TaskID, err)
			}
			if mutation.Applied {
				summary.Refunded++
			}
		}
	}

	remaining, err := model.GetUnfinishedSeedanceTasksForChannels(channelIDs)
	if err != nil {
		return summary, err
	}
	if len(remaining) > 0 {
		return summary, fmt.Errorf("%w: %d task(s) changed concurrently", model.ErrSeedanceChannelHasUnfinishedTasks, len(remaining))
	}
	return summary, nil
}
