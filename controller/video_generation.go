package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/seedance"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

type videoGenerationModel struct {
	ID                string                          `json:"id"`
	Name              string                          `json:"name"`
	Family            string                          `json:"family"`
	Category          string                          `json:"category"`
	Description       string                          `json:"description"`
	Tags              []string                        `json:"tags"`
	Featured          bool                            `json:"featured"`
	IsNew             bool                            `json:"is_new"`
	PriceUSD          *float64                        `json:"price_usd"`
	GroupRatio        float64                         `json:"group_ratio"`
	EstimatedPriceUSD *float64                        `json:"estimated_price_usd"`
	PriceAvailable    bool                            `json:"price_available"`
	Capabilities      seedance.GenerationCapabilities `json:"capabilities"`
}

type videoGenerationCatalog struct {
	Group             string                 `json:"group"`
	Groups            []string               `json:"groups"`
	EstimateIsMaximum bool                   `json:"estimate_is_maximum"`
	Upload            videoGenerationUpload  `json:"upload"`
	Models            []videoGenerationModel `json:"models"`
}

type videoGenerationUpload struct {
	Path             string   `json:"path"`
	MaxBytes         int64    `json:"max_bytes"`
	AcceptedFormats  []string `json:"accepted_formats"`
	TemporaryResults bool     `json:"temporary_results"`
}

type videoGenerationCandidate struct {
	capability seedance.ModelCapability
	groups     map[string]struct{}
	ambiguous  bool
}

func GetVideoGenerationModels(c *gin.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	if err := service.WriteVideoContentCookie(c, identity); err != nil {
		common.ApiError(c, err)
		return
	}

	user, err := model.GetUserCache(identity.UserID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	requestedGroup := strings.TrimSpace(c.Query("group"))
	if requestedGroup == "" {
		requestedGroup = user.Group
	}
	groups, err := videoGenerationGroups(user.Group, requestedGroup)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": err.Error(), "data": nil})
		return
	}
	catalog, err := buildVideoGenerationCatalog(user, requestedGroup, groups)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": catalog})
}

func videoGenerationGroups(userGroup, requestedGroup string) ([]string, error) {
	if requestedGroup == "auto" {
		groups := service.GetUserAutoGroup(userGroup)
		if len(groups) == 0 {
			return nil, fmt.Errorf("no usable automatic groups are configured")
		}
		return groups, nil
	}
	if !service.GroupInUserUsableGroups(userGroup, requestedGroup) {
		return nil, fmt.Errorf("group %s is not available to this user", requestedGroup)
	}
	return []string{requestedGroup}, nil
}

func buildVideoGenerationCatalog(user *model.UserBase, requestedGroup string, groups []string) (*videoGenerationCatalog, error) {
	abilities, err := model.GetAllEnableAbilityWithChannels()
	if err != nil {
		return nil, err
	}
	allowedGroups := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		allowedGroups[group] = struct{}{}
	}
	candidates := make(map[string]*videoGenerationCandidate)
	selectableGroupSet := make(map[string]struct{})
	channels := make(map[int]*model.Channel)
	for _, ability := range abilities {
		if ability.ChannelType != constant.ChannelTypeSeedance {
			continue
		}
		channel, cached := channels[ability.ChannelId]
		if !cached {
			channel, err = model.CacheGetChannel(ability.ChannelId)
			if err != nil {
				channels[ability.ChannelId] = nil
				continue
			}
			channels[ability.ChannelId] = channel
		}
		if channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		mappedModel, mappingErr := seedance.ResolveMappedModel(ability.Model, channel.GetModelMapping())
		if mappingErr != nil {
			continue
		}
		capability, supported := seedance.CapabilityForModel(mappedModel)
		if !supported || capability.Protocol == seedance.ProtocolContextIR {
			continue
		}
		if service.GroupInUserUsableGroups(user.Group, ability.Group) {
			selectableGroupSet[ability.Group] = struct{}{}
		}
		if _, ok := allowedGroups[ability.Group]; !ok {
			continue
		}
		candidate, exists := candidates[ability.Model]
		if !exists {
			candidates[ability.Model] = &videoGenerationCandidate{
				capability: capability,
				groups:     map[string]struct{}{ability.Group: {}},
			}
			continue
		}
		candidate.groups[ability.Group] = struct{}{}
		if !reflect.DeepEqual(
			seedance.GenerationCapabilitiesForModel(candidate.capability),
			seedance.GenerationCapabilitiesForModel(capability),
		) {
			candidate.ambiguous = true
		}
	}
	selectableGroups := make([]string, 0, len(selectableGroupSet)+1)
	for group := range selectableGroupSet {
		selectableGroups = append(selectableGroups, group)
	}
	sort.Strings(selectableGroups)
	for _, autoGroup := range service.GetUserAutoGroup(user.Group) {
		if _, ok := selectableGroupSet[autoGroup]; ok {
			selectableGroups = append(selectableGroups, "auto")
			break
		}
	}

	models := make([]videoGenerationModel, 0, len(candidates))
	for publicModel, candidate := range candidates {
		if candidate.ambiguous {
			continue
		}
		groupRatio := 0.0
		for group := range candidate.groups {
			ratio := service.GetUserGroupRatio(user.Group, group)
			if ratio > groupRatio && !math.IsNaN(ratio) && !math.IsInf(ratio, 0) {
				groupRatio = ratio
			}
		}
		price, priceAvailable := ratio_setting.GetModelPrice(publicModel, false)
		if !priceAvailable {
			price, priceAvailable = ratio_setting.GetDefaultModelPriceMap()[publicModel]
		}
		priceAvailable = priceAvailable && price > 0 && groupRatio > 0 && !math.IsNaN(price) && !math.IsInf(price, 0)
		var pricePointer, estimatedPointer *float64
		if priceAvailable {
			priceCopy := price
			estimated := price * groupRatio
			if !math.IsNaN(estimated) && !math.IsInf(estimated, 0) && estimated > 0 {
				pricePointer = &priceCopy
				estimatedPointer = &estimated
			} else {
				priceAvailable = false
			}
		}
		category, description := videoGenerationPresentation(candidate.capability)
		tags := []string{candidate.capability.Family, string(candidate.capability.Kind)}
		if strings.Contains(candidate.capability.Name, "global") {
			tags = append(tags, "global")
		}
		models = append(models, videoGenerationModel{
			ID:                publicModel,
			Name:              publicModel,
			Family:            candidate.capability.Family,
			Category:          category,
			Description:       description,
			Tags:              tags,
			Featured:          isFeaturedVideoModel(candidate.capability.Name),
			IsNew:             isNewVideoModel(candidate.capability.Family),
			PriceUSD:          pricePointer,
			GroupRatio:        groupRatio,
			EstimatedPriceUSD: estimatedPointer,
			PriceAvailable:    priceAvailable,
			Capabilities:      seedance.GenerationCapabilitiesForModel(candidate.capability),
		})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Featured != models[j].Featured {
			return models[i].Featured
		}
		if models[i].IsNew != models[j].IsNew {
			return models[i].IsNew
		}
		return models[i].ID < models[j].ID
	})
	return &videoGenerationCatalog{
		Group:             requestedGroup,
		Groups:            selectableGroups,
		EstimateIsMaximum: requestedGroup == "auto",
		Upload: videoGenerationUpload{
			Path:             "/pg/files/upload",
			MaxBytes:         seedance.MaxUploadBytes,
			AcceptedFormats:  []string{"jpg", "jpeg", "png", "webp", "mp3", "wav", "flac", "mp4", "avi", "mov", "mkv"},
			TemporaryResults: true,
		},
		Models: models,
	}, nil
}

func videoGenerationPresentation(capability seedance.ModelCapability) (string, string) {
	switch capability.Kind {
	case seedance.TaskKindText:
		return "video_generation", "Text to video"
	case seedance.TaskKindImage:
		return "video_generation", "Image to video"
	case seedance.TaskKindMulti:
		return "video_generation", "Multimodal video generation"
	case seedance.TaskKindReference, seedance.TaskKindElements:
		return "video_generation", "Reference images to video"
	case seedance.TaskKindStartEnd:
		return "video_generation", "Start and end frames to video"
	case seedance.TaskKindMidjourneyVideo:
		return "video_generation", "Midjourney image to video"
	case seedance.TaskKindVideo, seedance.TaskKindEdit, seedance.TaskKindMotion:
		return "video_processing", "Video transformation"
	case seedance.TaskKindUpscale:
		return "video_processing", "Video upscaling"
	case seedance.TaskKindDraftEnhance:
		return "video_workflow", "Enhance a generated video draft"
	case seedance.TaskKindLipIdentify, seedance.TaskKindLipTTS, seedance.TaskKindLipVideo:
		return "video_workflow", "Lip sync workflow"
	case seedance.TaskKindShortPlay:
		return "video_generation", "Script to short video"
	default:
		return "video_generation", "Video generation"
	}
}

func isFeaturedVideoModel(modelName string) bool {
	switch modelName {
	case "seedance-2.0-fast-multi", "seedance-2.0-global-fast-multi", "seedance-2.5-standard-multi", "seedance-2.5-global-standard-multi", "midjourney-video":
		return true
	default:
		return false
	}
}

func isNewVideoModel(family string) bool {
	switch family {
	case "seedance-2.5", "hailuo-h3", "flux-3-video", "minimax-h3-ow", "vidu-q3":
		return true
	default:
		return false
	}
}

func normalizeDashboardVideoGenerationRequest(body []byte, modelMapping string) ([]byte, seedance.ModelCapability, error) {
	var request map[string]json.RawMessage
	if err := common.Unmarshal(body, &request); err != nil || request == nil {
		return nil, seedance.ModelCapability{}, fmt.Errorf("request must be a JSON object")
	}
	var modelName string
	if rawModel, exists := request["model"]; exists {
		if err := common.Unmarshal(rawModel, &modelName); err != nil {
			return nil, seedance.ModelCapability{}, fmt.Errorf("model must be a string")
		}
	}
	modelName = strings.TrimSpace(modelName)
	mappedModel, err := seedance.ResolveMappedModel(modelName, modelMapping)
	if err != nil {
		return nil, seedance.ModelCapability{}, err
	}
	capability, ok := seedance.CapabilityForModel(mappedModel)
	if !ok || capability.Protocol == seedance.ProtocolContextIR {
		return nil, seedance.ModelCapability{}, fmt.Errorf("model is not a supported video-output model")
	}
	delete(request, "group")
	normalizedBody, err := common.Marshal(request)
	if err != nil {
		return nil, seedance.ModelCapability{}, err
	}
	return normalizedBody, capability, nil
}

// SubmitVideoGeneration provides dashboard-session access to the existing
// Seedance task relay. It removes the dashboard-only group selector before
// validation and creates an in-memory token context without exposing an API key.
func SubmitVideoGeneration(c *gin.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	if err := service.WriteVideoContentCookie(c, identity); err != nil {
		common.ApiError(c, err)
		return
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	normalizedBody, capability, err := normalizeDashboardVideoGenerationRequest(body, c.GetString("model_mapping"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	replacement, err := common.CreateBodyStorage(normalizedBody)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	_ = storage.Close()
	c.Set(common.KeyBodyStorage, replacement)
	c.Request.Body = io.NopCloser(replacement)
	c.Request.ContentLength = int64(len(normalizedBody))

	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	userID := c.GetInt("id")
	temporaryToken := &model.Token{
		UserId:         userID,
		Name:           "video-generation-session",
		Group:          usingGroup,
		UnlimitedQuota: true,
	}
	if err = middleware.SetupContextForToken(c, temporaryToken); err != nil {
		return
	}
	if capability.Protocol == seedance.ProtocolMidjourneyVideo {
		c.Request.URL.Path = "/v1/midjourney/generations/video"
		c.Set("relay_mode", relayconstant.RelayModeSeedanceMidjourneyVideoSubmit)
	} else {
		c.Request.URL.Path = "/v1/videos"
		c.Set("relay_mode", relayconstant.RelayModeVideoSubmit)
	}
	RelayTask(c)
}
