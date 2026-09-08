package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/seedance"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	videoPromptOptimizerMaxPromptBytes = 80 << 10
	videoPromptOptimizerMaxResponse    = 2 << 20
	videoPromptOptimizerTimeout        = 30 * time.Second
)

var videoPromptOptimizerModels = []string{
	"qwen/qwen3.8-max",
	"zhenzhen/gk-4.6",
	"kimi-k3",
}

type videoPromptOptimizeRequest struct {
	Prompt string `json:"prompt"`
	Group  string `json:"group,omitempty"`
}

type videoPromptOptimizeResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// OptimizeVideoPrompt is intentionally separate from the general chat relay.
// Seedance channels only expose video task routes, so this endpoint selects an
// explicitly configured Seedance chat capability and proxies one bounded call.
func OptimizeVideoPrompt(c *gin.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	var request videoPromptOptimizeRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid JSON request"})
		return
	}
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "prompt is required"})
		return
	}
	if len([]byte(request.Prompt)) > videoPromptOptimizerMaxPromptBytes {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "prompt is too long"})
		return
	}

	user, err := model.GetUserCache(identity.UserID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	group := strings.TrimSpace(request.Group)
	if group == "" {
		group = user.Group
	}
	groups, err := videoGenerationGroups(user.Group, group)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": err.Error()})
		return
	}

	channel, upstreamModel, err := selectVideoPromptOptimizerChannel(user.Group, groups)
	if err != nil {
		c.JSON(http.StatusNotImplemented, gin.H{"success": false, "message": "video prompt optimization is not configured"})
		return
	}
	if err := proxyVideoPromptOptimization(c, channel, upstreamModel, request.Prompt); err != nil {
		common.SysError(fmt.Sprintf("video prompt optimization failed: %v", err))
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": "video prompt optimization is unavailable"})
	}
}

func selectVideoPromptOptimizerChannel(userGroup string, groups []string) (*model.Channel, string, error) {
	allowedGroups := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		allowedGroups[group] = struct{}{}
	}
	knownModels := make(map[string]struct{}, len(videoPromptOptimizerModels))
	for _, modelName := range videoPromptOptimizerModels {
		knownModels[modelName] = struct{}{}
	}
	abilities, err := model.GetAllEnableAbilityWithChannels()
	if err != nil {
		return nil, "", err
	}
	channels := make(map[int]*model.Channel)
	for _, ability := range abilities {
		if ability.ChannelType != constant.ChannelTypeSeedance {
			continue
		}
		if _, ok := allowedGroups[ability.Group]; !ok || !service.GroupInUserUsableGroups(userGroup, ability.Group) {
			continue
		}
		channel, ok := channels[ability.ChannelId]
		if !ok {
			channel, _ = model.CacheGetChannel(ability.ChannelId)
			channels[ability.ChannelId] = channel
		}
		if channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		mapped, mappingErr := seedance.ResolveMappedModel(ability.Model, channel.GetModelMapping())
		if mappingErr != nil {
			continue
		}
		if _, ok := knownModels[mapped]; ok && strings.TrimSpace(channel.Key) != "" && channel.GetBaseURL() != "" {
			return channel, mapped, nil
		}
	}
	return nil, "", fmt.Errorf("no configured Seedance chat model")
}

func proxyVideoPromptOptimization(c *gin.Context, channel *model.Channel, modelName, prompt string) error {
	payload, err := common.Marshal(map[string]any{
		"model":      modelName,
		"stream":     false,
		"max_tokens": 1400,
		"messages": []map[string]string{
			{"role": "system", "content": "You are an expert video prompt writer. Rewrite the user prompt into a concise, vivid, production-ready prompt for a text-to-video model. Preserve the user intent, named entities, language, and requested constraints. Add concrete subject action, setting, composition, camera movement, lighting, temporal progression, and audio cues only when they are implied or useful. Do not explain your changes, do not add a heading, and return only the final prompt."},
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoPromptOptimizerTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(channel.GetBaseURL(), "/")+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+channel.Key)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	client, err := service.GetHttpClientWithProxySettings(channel.GetSetting().Proxy, channel.GetSetting())
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, videoPromptOptimizerMaxResponse+1))
	if err != nil {
		return err
	}
	if len(body) > videoPromptOptimizerMaxResponse {
		return fmt.Errorf("optimizer response too large")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("optimizer upstream returned status %d", response.StatusCode)
	}
	var result videoPromptOptimizeResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return err
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return fmt.Errorf("optimizer response did not contain text")
	}
	optimized := strings.TrimSpace(result.Choices[0].Message.Content)
	if len([]byte(optimized)) > videoPromptOptimizerMaxPromptBytes {
		return fmt.Errorf("optimizer response is too long")
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"prompt": optimized}})
	return nil
}
