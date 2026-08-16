package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"
)

// TaskPollingAdaptor 定义轮询所需的最小适配器接口，避免 service -> relay 的循环依赖
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

// TaskPollingPrivateResultAdaptor is implemented by providers whose polling
// response contains signed media URLs or workflow secrets. Safe data is
// persisted publicly; result URLs, assets, and artifact values stay private.
type TaskPollingPrivateResultAdaptor interface {
	ParseTaskResultForTask(body []byte, task *model.Task) (
		info *relaycommon.TaskInfo,
		safeData []byte,
		resultURLs []string,
		resultAssets map[string]string,
		artifacts map[string]string,
		err error,
	)
}

type taskPollRetryError struct {
	statusCode        int
	retryAfterSeconds int64
}

func (e *taskPollRetryError) Error() string {
	return fmt.Sprintf("retryable upstream polling status %d", e.statusCode)
}

// GetTaskAdaptorFunc 由 main 包注入，用于获取指定平台的任务适配器。
// 打破 service -> relay -> relay/channel -> service 的循环依赖。
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
func sweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks := model.GetTimedOutUnfinishedTasks(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	reason := fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes)
	legacyReason := "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < model.TaskRefundLegacyCutoff
		if !isLegacy && task.Status == model.TaskStatusSubmitting &&
			(task.BillingStatus == model.TaskBillingStatusReservePending || task.BillingStatus == model.TaskBillingStatusReserved) {
			// Durable submission recovery owns these states. Closing them here can
			// race an in-flight reservation/upstream request when an unusually short
			// TASK_TIMEOUT_MINUTES is configured.
			continue
		}

		oldStatus := task.Status
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		if isLegacy {
			task.FailReason = legacyReason
			// 旧系统任务明确不退款，随终态 CAS 一并清掉 quota，
			// 避免留下可再次退款的计费状态。
			task.Quota = 0
		} else {
			task.FailReason = reason
		}

		won, err := task.UpdateWithStatus(oldStatus)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
		if !isLegacy && task.Quota != 0 {
			RefundTaskQuota(ctx, task, reason)
		}
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

// TaskPollSummary is the result recorded on an async_task_poll system task row,
// summarizing one polling pass.
type TaskPollSummary struct {
	UnfinishedTasks  int `json:"unfinished_tasks"`
	PlatformsScanned int `json:"platforms_scanned"`
	NullTasksFailed  int `json:"null_tasks_failed"`
}

// RunTaskPollingOnce performs one async-task (Suno/video) polling pass
// synchronously. It honors ctx cancellation (the system-task runner cancels it
// when the lease is lost) and, when report is non-nil, reports progress as
// (processedPlatforms, totalPlatforms). It returns immediately if the task
// adaptor factory has not been wired yet, to avoid a nil call during startup.
func RunTaskPollingOnce(ctx context.Context, report func(processed, total int)) TaskPollSummary {
	summary := TaskPollSummary{}
	if GetTaskAdaptorFunc == nil {
		return summary
	}
	if ctx == nil {
		ctx = context.Background()
	}

	common.SysLog("任务进度轮询开始")
	RunTaskBillingWorkerOnce(ctx, 100)
	sweepTimedOutTasks(ctx)
	allTasks := model.GetAllUnFinishSyncTasks(constant.TaskQueryLimit)
	summary.UnfinishedTasks = len(allTasks)
	platformTask := make(map[constant.TaskPlatform][]*model.Task)
	for _, t := range allTasks {
		platformTask[t.Platform] = append(platformTask[t.Platform], t)
	}

	totalPlatforms := len(platformTask)
	processedPlatforms := 0
	for platform, tasks := range platformTask {
		if ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedPlatforms, totalPlatforms)
		}
		processedPlatforms++
		if len(tasks) == 0 {
			continue
		}
		summary.PlatformsScanned++
		taskChannelM := make(map[int][]string)
		taskM := make(map[string]*model.Task)
		nullTaskIds := make([]int64, 0)
		for _, task := range tasks {
			upstreamID := task.GetUpstreamTaskID()
			if upstreamID == "" {
				// 统计失败的未完成任务
				nullTaskIds = append(nullTaskIds, task.ID)
				continue
			}
			taskM[pollingTaskKey(platform, task.ChannelId, upstreamID)] = task
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
		}
		if len(nullTaskIds) > 0 {
			summary.NullTasksFailed += len(nullTaskIds)
			err := model.TaskBulkUpdateByID(nullTaskIds, map[string]any{
				"status":   "FAILURE",
				"progress": "100%",
			})
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Fix null task_id task error: %v", err))
			} else {
				logger.LogInfo(ctx, fmt.Sprintf("Fix null task_id task success: %v", nullTaskIds))
			}
		}
		if len(taskChannelM) == 0 {
			continue
		}

		DispatchPlatformUpdate(ctx, platform, taskChannelM, taskM)
	}
	if report != nil && ctx.Err() == nil {
		report(totalPlatforms, totalPlatforms)
	}
	RunTaskBillingWorkerOnce(ctx, 100)
	common.SysLog("任务进度轮询完成")
	return summary
}

// RunSeedanceTaskPollingOnce performs one provider-isolated polling pass. It
// only reads due Seedance tasks and also drains durable billing work so a
// terminal task remains recoverable even after it leaves the polling set.
func RunSeedanceTaskPollingOnce(ctx context.Context, report func(processed, total int)) TaskPollSummary {
	summary := TaskPollSummary{}
	if ctx == nil {
		ctx = context.Background()
	}
	RunTaskBillingWorkerOnce(ctx, 100)
	if !constant.UpdateTask || GetTaskAdaptorFunc == nil || ctx.Err() != nil {
		return summary
	}
	sweepTimedOutTasks(ctx)
	tasks := model.GetUnfinishedSyncTasksByPlatform(constant.TaskPlatformSeedance, model.GetDBTimestamp(), constant.TaskQueryLimit)
	summary.UnfinishedTasks = len(tasks)
	if report != nil {
		report(0, 1)
	}

	taskChannelM := make(map[int][]string)
	taskM := make(map[string]*model.Task)
	for _, task := range tasks {
		if ctx.Err() != nil {
			break
		}
		upstreamID := strings.TrimSpace(task.PrivateData.UpstreamTaskID)
		if upstreamID == "" {
			summary.NullTasksFailed++
			reason := "accepted Seedance task is missing its upstream task ID"
			oldStatus := task.Status
			if task.BillingStatus == model.TaskBillingStatusReserved {
				won, err := MarkTaskRefundPendingDurably(task.ID, task.Status, model.TaskStatusFailure, reason)
				if err != nil {
					logger.LogWarn(ctx, fmt.Sprintf("mark missing-upstream task for refund failed (task=%s): %s", task.TaskID, err.Error()))
				} else if won {
					task.Status = model.TaskStatusFailure
					task.BillingStatus = model.TaskBillingStatusRefundPending
					task.FailReason = reason
					RefundTaskQuota(ctx, task, reason)
				}
			} else {
				task.Status = model.TaskStatusFailure
				task.Progress = taskcommon.ProgressComplete
				task.FailReason = reason
				_, _ = task.UpdateWithStatus(oldStatus)
			}
			continue
		}
		taskM[pollingTaskKey(constant.TaskPlatformSeedance, task.ChannelId, upstreamID)] = task
		taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
	}
	if len(taskChannelM) > 0 && ctx.Err() == nil {
		summary.PlatformsScanned = 1
		if err := UpdateVideoTasks(ctx, constant.TaskPlatformSeedance, taskChannelM, taskM); err != nil {
			logger.LogWarn(ctx, "Seedance polling pass failed: "+err.Error())
		}
	}
	RunTaskBillingWorkerOnce(ctx, 100)
	if report != nil && ctx.Err() == nil {
		report(1, 1)
	}
	return summary
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	switch platform {
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		_ = UpdateSunoTasks(ctx, taskChannelM, taskM)
	default:
		if err := UpdateVideoTasks(ctx, platform, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateSunoTasks 按渠道更新所有 Suno 任务
func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := updateSunoTasks(ctx, channelId, taskIds, taskM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateSunoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := model.CacheGetChannel(channelId)
	if err != nil {
		common.SysLog(fmt.Sprintf("CacheGetChannel: %v", err))
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		err = model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if err != nil {
			common.SysLog(fmt.Sprintf("UpdateSunoTask error: %v", err))
		}
		return err
	}
	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		return errors.New("adaptor not found")
	}
	proxy := ch.GetSetting().Proxy
	resp, err := adaptor.FetchTask(*ch.BaseURL, ch.Key, map[string]any{
		"ids": taskIds,
	}, proxy)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Task Do req error: %v", err))
		return err
	}
	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		return fmt.Errorf("Get Task status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		return err
	}
	var responseItems taskdto.TaskResponse[[]taskdto.SunoDataResponse]
	err = common.Unmarshal(responseBody, &responseItems)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Suno Task parse body error2: %v, body: %s", err, string(responseBody)))
		return err
	}
	if !responseItems.IsSuccess() {
		common.SysLog(fmt.Sprintf("渠道 #%d 未完成的任务有: %d, 成功获取到任务数: %s", channelId, len(taskIds), string(responseBody)))
		return err
	}

	for _, responseItem := range responseItems.Data {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task := taskM[responseItem.TaskID]
		if task == nil {
			logger.LogWarn(ctx, fmt.Sprintf("Suno task response ignored: unknown task_id=%s", responseItem.TaskID))
			continue
		}
		if !taskNeedsUpdate(task, responseItem) {
			continue
		}

		prevStatus := task.Status
		task.Status = lo.If(model.TaskStatus(responseItem.Status) != "", model.TaskStatus(responseItem.Status)).Else(task.Status)
		task.FailReason = lo.If(responseItem.FailReason != "", responseItem.FailReason).Else(task.FailReason)
		task.SubmitTime = lo.If(responseItem.SubmitTime != 0, responseItem.SubmitTime).Else(task.SubmitTime)
		task.StartTime = lo.If(responseItem.StartTime != 0, responseItem.StartTime).Else(task.StartTime)
		task.FinishTime = lo.If(responseItem.FinishTime != 0, responseItem.FinishTime).Else(task.FinishTime)
		isFailure := responseItem.FailReason != "" || task.Status == model.TaskStatusFailure
		if isFailure {
			logger.LogInfo(ctx, task.TaskID+" 构建失败，"+task.FailReason)
			task.Status = model.TaskStatusFailure
			task.Progress = "100%"
		}
		if responseItem.Status == model.TaskStatusSuccess {
			task.Progress = "100%"
		}
		task.Data = responseItem.Data

		// 持久化走 CAS，防止重叠轮询/sweep/多实例/持久化失败重试导致重复退款或覆盖终态。
		won, err := task.UpdateWithStatus(prevStatus)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("UpdateSunoTask task %s error: %v", task.TaskID, err))
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s CAS lost or no-op update, skip billing", task.TaskID))
		} else if isFailure && prevStatus != model.TaskStatusFailure && task.Quota != 0 {
			RefundTaskQuota(ctx, task, task.FailReason)
		}
	}
	return nil
}

// taskNeedsUpdate 检查 Suno 任务是否需要更新
func taskNeedsUpdate(oldTask *model.Task, newTask taskdto.SunoDataResponse) bool {
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if string(oldTask.Status) != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}

	if (oldTask.Status == model.TaskStatusFailure || oldTask.Status == model.TaskStatusSuccess) && oldTask.Progress != "100%" {
		return true
	}

	oldData, _ := common.Marshal(oldTask.Data)
	newData, _ := common.Marshal(newTask.Data)

	sort.Slice(oldData, func(i, j int) bool {
		return oldData[i] < oldData[j]
	})
	sort.Slice(newData, func(i, j int) bool {
		return newData[i] < newData[j]
	})

	if string(oldData) != string(newData) {
		return true
	}
	return false
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	channelIDs := make([]int, 0, len(taskChannelM))
	for channelID := range taskChannelM {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)

	var wg sync.WaitGroup
	for _, channelId := range channelIDs {
		taskIds := taskChannelM[channelId]
		if len(taskIds) == 0 {
			continue
		}
		taskIds = append([]string(nil), taskIds...)

		wg.Add(1)
		gopool.Go(func() {
			defer wg.Done()
			if err := updateVideoTasks(ctx, platform, channelId, taskIds, taskM); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
			}
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// pollingTaskKey keeps the in-memory polling batch collision-free when two
// channels expose the same upstream task ID. Non-Seedance callers retain the
// legacy key shape because their task maps are also used by older adapters.
func pollingTaskKey(platform constant.TaskPlatform, channelID int, upstreamID string) string {
	if platform != constant.TaskPlatformSeedance {
		return upstreamID
	}
	return strconv.Itoa(channelID) + "\x00" + upstreamID
}

func pollingTask(taskM map[string]*model.Task, platform constant.TaskPlatform, channelID int, upstreamID string) *model.Task {
	if taskM == nil {
		return nil
	}
	if task := taskM[pollingTaskKey(platform, channelID, upstreamID)]; task != nil {
		return task
	}
	// Keep compatibility with tests and callers that construct legacy maps by
	// hand; production Seedance batches always use the channel-qualified key.
	return taskM[upstreamID]
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		if platform == constant.TaskPlatformSeedance {
			for _, upstreamID := range taskIds {
				if task := pollingTask(taskM, platform, channelId, upstreamID); task != nil {
					if recordErr := model.RecordTaskPollFailure(task.ID, 60, 5*60); recordErr != nil {
						logger.LogWarn(ctx, fmt.Sprintf("Failed to defer Seedance task after channel lookup error %s: %s", task.TaskID, recordErr.Error()))
					}
				}
			}
			return fmt.Errorf("CacheGetChannel failed: %w", err)
		}
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t := pollingTask(taskM, platform, channelId, upstreamID); t != nil {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		errUpdate := model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("Failed to get channel info, channel ID: %d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if errUpdate != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTask error: %v", errUpdate))
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelBaseUrl: cacheGetChannel.GetBaseURL(),
	}
	info.ApiKey = cacheGetChannel.Key
	adaptor.Init(info)
	disablePollingSleep := cacheGetChannel.GetOtherSettings().DisableTaskPollingSleep || platform == constant.TaskPlatformSeedance
	for i, taskId := range taskIds {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, platform, taskId, taskM); err != nil {
			if task := pollingTask(taskM, platform, channelId, taskId); task != nil {
				logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", task.TaskID, err.Error()))
			} else {
				logger.LogError(ctx, fmt.Sprintf("Failed to update an unknown video task: %s", err.Error()))
			}
			if platform == constant.TaskPlatformSeedance {
				minimumDelay := int64(5)
				var retryErr *taskPollRetryError
				if errors.As(err, &retryErr) && retryErr.retryAfterSeconds > minimumDelay {
					minimumDelay = retryErr.retryAfterSeconds
				}
				if task := pollingTask(taskM, platform, channelId, taskId); task != nil {
					if recordErr := model.RecordTaskPollFailure(task.ID, minimumDelay, 5*60); recordErr != nil {
						logger.LogWarn(ctx, fmt.Sprintf("Failed to schedule Seedance task retry %s: %s", task.TaskID, recordErr.Error()))
					}
				}
			}
		}
		if disablePollingSleep || i == len(taskIds)-1 {
			continue
		}

		// sleep 1 second between tasks for this channel only.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, platform constant.TaskPlatform, taskId string, taskM map[string]*model.Task) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	baseURL := constant.ChannelBaseURLs[ch.Type]
	if ch.GetBaseURL() != "" {
		baseURL = ch.GetBaseURL()
	}
	proxy := ch.GetSetting().Proxy

	task := pollingTask(taskM, platform, ch.Id, taskId)
	if task == nil {
		return fmt.Errorf("task is not present in the polling batch")
	}
	privateResultAdaptor, hasPrivateResults := adaptor.(TaskPollingPrivateResultAdaptor)
	key := ch.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	if privateData.ChannelKeyFingerprint != "" &&
		privateData.ChannelKeyFingerprint != fmt.Sprintf("%x", common.Sha256Raw([]byte(key))) {
		return fmt.Errorf("channel credential changed while task %s is unfinished", task.TaskID)
	}
	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
		"context": ctx,
	}, proxy)
	if err != nil {
		return fmt.Errorf("fetchTask failed for task %s: %s", task.TaskID, common.MaskSensitiveInfo(err.Error()))
	}
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("fetchTask returned an empty response for task %s", task.TaskID)
	}
	defer resp.Body.Close()
	responseReader := io.Reader(resp.Body)
	if hasPrivateResults {
		responseReader = io.LimitReader(resp.Body, (2<<20)+1)
	}
	responseBody, err := io.ReadAll(responseReader)
	if err != nil {
		return fmt.Errorf("readAll failed for task %s: %w", task.TaskID, err)
	}
	if hasPrivateResults && len(responseBody) > 2<<20 {
		return fmt.Errorf("poll response exceeded 2 MiB for task %s", task.TaskID)
	}

	if hasPrivateResults && (resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices) {
		retryAfter := int64(5)
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			retryAfter = 15
		case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden:
			retryAfter = 60
		default:
			if resp.StatusCode >= http.StatusInternalServerError {
				retryAfter = 10
			}
		}
		if headerDelay := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); headerDelay > retryAfter {
			retryAfter = headerDelay
		}
		return &taskPollRetryError{statusCode: resp.StatusCode, retryAfterSeconds: retryAfter}
	}
	if !hasPrivateResults {
		logger.LogDebug(ctx, "updateVideoSingleTask response: %s", responseBody)
	}

	snap := task.Snapshot()

	taskResult := &relaycommon.TaskInfo{}
	var resultURLs []string
	var resultAssets map[string]string
	var rawArtifacts map[string]string
	if hasPrivateResults {
		var safeData []byte
		taskResult, safeData, resultURLs, resultAssets, rawArtifacts, err = privateResultAdaptor.ParseTaskResultForTask(responseBody, task)
		if err != nil {
			return fmt.Errorf("parse private task result failed for task %s: %s", task.TaskID, common.MaskSensitiveInfo(err.Error()))
		}
		task.Data = safeData
	} else {
		// try parse as New API response format
		var responseItems taskdto.TaskResponse[model.Task]
		if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
			logger.LogDebug(ctx, "updateVideoSingleTask parsed as new api response format: %+v", responseItems)
			t := responseItems.Data
			taskResult.TaskID = t.TaskID
			taskResult.Status = string(t.Status)
			taskResult.Url = t.GetResultURL()
			taskResult.Progress = t.Progress
			taskResult.Reason = t.FailReason
			task.Data = t.Data
		} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
			return fmt.Errorf("parseTaskResult failed for task %s: %w", task.TaskID, err)
		}
		task.Data = redactVideoResponseBody(responseBody)
	}
	if taskResult == nil {
		return fmt.Errorf("task result is empty for task %s", task.TaskID)
	}

	if hasPrivateResults {
		logger.LogDebug(ctx, "updateVideoSingleTask private result: action=%s status=%s progress=%s", task.Action, taskResult.Status, taskResult.Progress)
	} else {
		logger.LogDebug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)
	}

	now := model.GetDBTimestamp()
	if taskResult.Status == "" {
		if hasPrivateResults {
			return fmt.Errorf("upstream returned an unknown status for task %s", task.TaskID)
		}
		//taskResult = relaycommon.FailTaskInfo("upstream returned empty status")
		errorResult := &dto.GeneralErrorResponse{}
		if err = common.Unmarshal(responseBody, &errorResult); err == nil {
			openaiError := errorResult.TryToOpenAIError()
			if openaiError != nil {
				// 返回规范的 OpenAI 错误格式，提取错误信息，判断错误是否为任务失败
				if openaiError.Code == "429" {
					// 429 错误通常表示请求过多或速率限制，暂时不认为是任务失败，保持原状态等待下一轮轮询
					return nil
				}

				// 其他错误认为是任务失败，记录错误信息并更新任务状态
				taskResult = relaycommon.FailTaskInfo("upstream returned error")
			} else {
				// unknown error format, log original response
				logger.LogError(ctx, fmt.Sprintf("Task %s returned empty status with unrecognized error format, response: %s", task.TaskID, string(responseBody)))
				taskResult = relaycommon.FailTaskInfo("upstream returned unrecognized message")
			}
		}
	}

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota
	var artifactRecords []*model.TaskArtifact

	if taskStatusStage(model.TaskStatus(taskResult.Status)) < taskStatusStage(snap.Status) {
		logger.LogWarn(ctx, fmt.Sprintf("ignored regressive task status (task=%s current=%s upstream=%s)", task.TaskID, snap.Status, taskResult.Status))
		return nil
	}
	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
		if hasPrivateResults {
			task.PollFailures = 0
			task.NextPollAt = now + 5
		}
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
		if hasPrivateResults {
			task.PollFailures = 0
			task.NextPollAt = now + 5
		}
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if hasPrivateResults {
			task.PollFailures = 0
			task.NextPollAt = now + 5
		}
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		task.PollFailures = 0
		task.NextPollAt = 0
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if hasPrivateResults {
			if len(resultURLs) > 0 {
				task.PrivateData.ResultURLs = append([]string(nil), resultURLs...)
				task.PrivateData.ResultURL = resultURLs[0]
			}
			if len(resultAssets) > 0 {
				task.PrivateData.ResultAssets = make(map[string]string, len(resultAssets))
				for name, rawURL := range resultAssets {
					task.PrivateData.ResultAssets[name] = rawURL
				}
			}

			publicAssets := make(map[string]string, len(resultAssets))
			for name := range resultAssets {
				publicAssets[name] = taskcommon.BuildProxyURL(task.TaskID) + "?asset=" + url.QueryEscape(name)
			}
			publicArtifacts := make(map[string]string, len(rawArtifacts))
			resultExpiry := resultURLExpiry(resultURLs, resultAssets)
			if len(resultURLs) > 0 || len(resultAssets) > 0 || len(rawArtifacts) > 0 {
				task.ResultExpiresAt = resultExpiry
			}
			for kind, upstreamValue := range rawArtifacts {
				artifact, artifactErr := model.NewTaskArtifact(kind, upstreamValue, resultExpiry)
				if artifactErr != nil {
					return fmt.Errorf("prepare task artifact %s for task %s: %w", kind, task.TaskID, artifactErr)
				}
				artifactRecords = append(artifactRecords, artifact)
				publicArtifacts[kind] = artifact.ArtifactID
			}
			if len(publicAssets) > 0 || len(publicArtifacts) > 0 {
				task.Data, err = attachPublicTaskOutputs(task.Data, publicAssets, publicArtifacts)
				if err != nil {
					return fmt.Errorf("build safe task outputs for task %s: %w", task.TaskID, err)
				}
			}
		} else if strings.HasPrefix(taskResult.Url, "data:") {
			// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		} else if taskResult.Url != "" {
			// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.)
			task.PrivateData.ResultURL = taskResult.Url
		} else {
			// No URL from adaptor — construct proxy URL using public task ID
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
		shouldSettle = true
	case model.TaskStatusFailure:
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", task.TaskID), task)
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		task.PollFailures = 0
		task.NextPollAt = 0
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		if task.BillingStatus == model.TaskBillingStatusReserved {
			task.BillingStatus = model.TaskBillingStatusRefundPending
		}
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = taskcommon.ProgressComplete
		if quota != 0 {
			shouldRefund = true
		}
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, task.TaskID)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone && snap.Status != task.Status {
		var won bool
		if len(artifactRecords) > 0 {
			won, err = model.UpdateTaskWithArtifacts(task, snap.Status, artifactRecords)
		} else {
			won, err = task.UpdateWithStatus(snap.Status)
		}
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("UpdateWithStatus failed for task %s: %s", task.TaskID, err.Error()))
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s CAS lost or no-op update, skip billing", task.TaskID))
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		if _, err := task.UpdateWithStatus(snap.Status); err != nil {
			return fmt.Errorf("update task %s: %w", task.TaskID, err)
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, "No update needed for task %s", task.TaskID)
	}

	if shouldSettle {
		if task.BillingStatus == model.TaskBillingStatusReserved {
			if _, err := SettleTaskQuotaDurably(ctx, task.ID); err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("durable task settlement deferred (task=%s): %s", task.TaskID, err.Error()))
			}
		} else {
			settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
		}
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}

	return nil
}

func attachPublicTaskOutputs(data []byte, resultAssets, artifacts map[string]string) ([]byte, error) {
	stored := make(map[string]any)
	if len(data) > 0 {
		if err := common.Unmarshal(data, &stored); err != nil {
			return nil, err
		}
	}
	if len(resultAssets) > 0 {
		stored["result_assets"] = resultAssets
	}
	if len(artifacts) > 0 {
		stored["artifacts"] = artifacts
	}
	return common.Marshal(stored)
}

func taskStatusStage(status model.TaskStatus) int {
	switch status {
	case model.TaskStatusNotStart:
		return 0
	case model.TaskStatusSubmitted:
		return 1
	case model.TaskStatusQueued:
		return 2
	case model.TaskStatusInProgress:
		return 3
	case model.TaskStatusSuccess, model.TaskStatusFailure:
		return 4
	default:
		return -1
	}
}

func parseRetryAfter(value string, now time.Time) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds > 0 {
			return seconds
		}
		return 0
	}
	retryAt, err := http.ParseTime(value)
	if err != nil || !retryAt.After(now) {
		return 0
	}
	seconds := int64(retryAt.Sub(now).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

func resultURLExpiry(resultURLs []string, resultAssets map[string]string) int64 {
	now := time.Now().Unix()
	expiresAt := int64(0)
	consider := func(rawURL string) {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return
		}
		query := parsed.Query()
		candidate := int64(0)
		for _, key := range []string{"Expires", "expires"} {
			value, parseErr := strconv.ParseInt(query.Get(key), 10, 64)
			if parseErr == nil && value > now {
				candidate = value
				break
			}
		}
		if candidate == 0 {
			for _, pair := range [][2]string{{"X-Amz-Date", "X-Amz-Expires"}, {"X-Tos-Date", "X-Tos-Expires"}} {
				issuedAt, dateErr := time.Parse("20060102T150405Z", query.Get(pair[0]))
				duration, durationErr := strconv.ParseInt(query.Get(pair[1]), 10, 64)
				if dateErr == nil && durationErr == nil && duration > 0 {
					candidate = issuedAt.Unix() + duration
					break
				}
			}
		}
		if candidate > now && (expiresAt == 0 || candidate < expiresAt) {
			expiresAt = candidate
		}
	}
	for _, rawURL := range resultURLs {
		consider(rawURL)
	}
	for _, rawURL := range resultAssets {
		consider(rawURL)
	}
	if expiresAt == 0 {
		expiresAt = now + int64((24*time.Hour)/time.Second)
	}
	return expiresAt
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	b, err := common.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}

// settleTaskBillingOnComplete 任务完成时的统一计费调整。
// 优先级：1. adaptor.AdjustBillingOnComplete 返回正数 → 使用 adaptor 计算的额度
//
//  2. taskResult.TotalTokens > 0 → 按 token 重算
//  3. 都不满足 → 保持预扣额度不变
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) {
	// 0. 按次计费的任务不做差额结算
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 按次计费，跳过差额结算", task.TaskID))
		return
	}
	// 1. 优先让 adaptor 决定最终额度
	if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "adaptor计费调整")
		return
	}
	// 2. 回退到 token 重算
	if taskResult.TotalTokens > 0 {
		RecalculateTaskQuotaByTokens(ctx, task, taskResult.TotalTokens)
		return
	}
	// 3. 无调整，保持预扣额度
}
