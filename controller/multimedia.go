package controller

import (
	"archive/zip"
	"fmt"
	"math"
	"mime"
	"net/http"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

type multimediaImageModel struct {
	Name           string                    `json:"name"`
	Capabilities   service.ImageCapabilities `json:"capabilities"`
	PriceUSD       *float64                  `json:"price_usd"`
	routeGroup     string
	routeChannelID int
	pinChannel     bool
}

type multimediaCatalog struct {
	Models         []multimediaImageModel `json:"models"`
	MaxUploadBytes int64                  `json:"max_upload_bytes"`
	RetentionDays  int                    `json:"retention_days"`
}

func RequireMultimediaSession(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if _, ok := requireBrowserSession(c); !ok {
		c.Abort()
	}
}

func buildMultimediaCatalog(user *model.UserBase, group string) (*multimediaCatalog, error) {
	usableGroups := service.GetUserUsableGroups(user.Group)
	groups := make([]string, 0, len(usableGroups))
	for name := range usableGroups {
		if service.IsUserSelectableGroup(user.Group, name) {
			groups = append(groups, name)
		}
	}
	sort.Strings(groups)
	groupPriority := make(map[string]int, len(groups))
	for _, name := range append(append([]string{user.Group}, service.GetUserAutoGroup(user.Group)...), groups...) {
		if _, found := groupPriority[name]; !found {
			groupPriority[name] = len(groupPriority)
		}
	}
	if group != "" {
		var err error
		groups, err = videoGenerationGroups(user.Group, group)
		if err != nil {
			return nil, err
		}
	}
	abilities, err := model.GetAllEnableAbilityWithChannels()
	if err != nil {
		return nil, err
	}
	allowedGroups := make(map[string]bool, len(groups))
	for _, name := range groups {
		allowedGroups[name] = service.IsUserSelectableGroup(user.Group, name)
	}
	abilities = slices.DeleteFunc(abilities, func(ability model.AbilityWithChannel) bool { return !allowedGroups[ability.Group] })
	var metadata []model.Model
	if err := model.DB.Where("status = ? AND endpoints <> ?", 1, "").Order("id ASC").Find(&metadata).Error; err != nil {
		return nil, err
	}
	imageRules := make([]model.Model, 0, len(metadata))
	for _, item := range metadata {
		var endpoints map[string]any
		if common.UnmarshalJsonStr(item.Endpoints, &endpoints) == nil && endpoints[string(constant.EndpointTypeImageGeneration)] != nil {
			imageRules = append(imageRules, item)
		}
	}
	sort.SliceStable(abilities, func(i, j int) bool {
		if abilities[i].Group != abilities[j].Group {
			return groupPriority[abilities[i].Group] < groupPriority[abilities[j].Group]
		}
		left, right := int64(0), int64(0)
		if abilities[i].Priority != nil {
			left = *abilities[i].Priority
		}
		if abilities[j].Priority != nil {
			right = *abilities[j].Priority
		}
		if left != right {
			return left > right
		}
		return abilities[i].ChannelId < abilities[j].ChannelId
	})
	catalog := &multimediaCatalog{Models: []multimediaImageModel{}, MaxUploadBytes: service.MaxMediaImageBytes, RetentionDays: service.MediaRetentionDays()}
	models := make(map[string]*multimediaImageModel)
	compatibleChannels := make(map[string]map[int]bool)
	channels := make(map[int]*model.Channel)
	for _, ability := range abilities {
		channel, cached := channels[ability.ChannelId]
		if !cached {
			channel, _ = model.CacheGetChannel(ability.ChannelId)
			channels[ability.ChannelId] = channel
		}
		if channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		upstream := ability.Model
		mapping := make(map[string]string)
		if raw := channel.GetModelMapping(); raw != "" && common.UnmarshalJsonStr(raw, &mapping) != nil {
			continue
		}
		if mapped := mapping[upstream]; mapped != "" {
			upstream = mapped
		}
		capability, supported := service.ImageCapabilitiesForModel(upstream)
		generic := false
		if !supported {
			for _, rule := range imageRules {
				for _, name := range []string{ability.Model, upstream} {
					matches := rule.NameRule == model.NameRuleExact && name == rule.ModelName ||
						rule.NameRule == model.NameRulePrefix && strings.HasPrefix(name, rule.ModelName) ||
						rule.NameRule == model.NameRuleSuffix && strings.HasSuffix(name, rule.ModelName) ||
						rule.NameRule == model.NameRuleContains && strings.Contains(name, rule.ModelName)
					if matches {
						capability, supported = service.CompatibleImageCapabilities(), true
						generic = true
					}
				}
			}
		}
		endpoints := common.GetEndpointTypesByChannelType(channel.Type, upstream)
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			config := channel.GetOtherSettings().AdvancedCustom
			if config == nil {
				continue
			}
			endpoints = config.SupportedEndpointTypesForModel(ability.Model)
			if !supported && slices.Contains(endpoints, constant.EndpointTypeImageGeneration) {
				capability, supported = service.CompatibleImageCapabilities(), true
			}
		}
		if !supported {
			continue
		}
		if generic && (channel.Type == constant.ChannelTypeGemini || channel.Type == constant.ChannelTypeVertexAi) {
			capability = service.GeminiImageCapabilities(upstream)
		}
		if capability.Protocol == service.ImageProtocolGemini && !slices.Contains(endpoints, constant.EndpointTypeGemini) {
			capability.Protocol = service.ImageProtocolChat
		}
		if channel.Type == constant.ChannelTypeSeedance || channel.Type == constant.ChannelTypeSora {
			continue
		}
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			path := "/v1/images/generations"
			if capability.Protocol == service.ImageProtocolGemini {
				path = "/v1beta/models/" + ability.Model + ":generateContent"
			}
			if capability.Protocol == service.ImageProtocolChat {
				path = "/v1/chat/completions"
			}
			if !channel.GetOtherSettings().AdvancedCustom.SupportsPathForModel(path, ability.Model) {
				continue
			}
		}
		if existing, found := models[ability.Model]; found {
			if existing.routeGroup == ability.Group && reflect.DeepEqual(existing.Capabilities, capability) {
				compatibleChannels[ability.Model][channel.Id] = true
			}
			continue
		}
		models[ability.Model] = &multimediaImageModel{Name: ability.Model, Capabilities: capability, routeGroup: ability.Group, routeChannelID: channel.Id}
		compatibleChannels[ability.Model] = map[int]bool{channel.Id: true}
	}
	for _, ability := range abilities {
		if item := models[ability.Model]; item != nil && item.routeGroup == ability.Group && !compatibleChannels[ability.Model][ability.ChannelId] {
			item.pinChannel = true
		}
	}
	for name, item := range models {
		if value, found := ratio_setting.GetModelPrice(name, false); found {
			value *= service.GetUserGroupRatio(user.Group, item.routeGroup)
			if value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
				item.PriceUSD = &value
			}
		}
		catalog.Models = append(catalog.Models, *item)
	}
	sort.Slice(catalog.Models, func(i, j int) bool { return catalog.Models[i].Name < catalog.Models[j].Name })
	return catalog, nil
}

func GetMultimediaModels(c *gin.Context) {
	user, err := model.GetUserCache(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	catalog, err := buildMultimediaCatalog(user, "")
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiSuccess(c, catalog)
}

func SubmitMultimediaImage(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	var request service.MultimediaImageRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid image generation request"})
		return
	}
	user, err := model.GetUserCache(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	catalog, err := buildMultimediaCatalog(user, "")
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": err.Error()})
		return
	}
	index := slices.IndexFunc(catalog.Models, func(item multimediaImageModel) bool { return item.Name == request.Model })
	if index < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "This image model is unavailable for your account"})
		return
	}
	selected := catalog.Models[index]
	request.Group = selected.routeGroup
	request.Protocol = selected.Capabilities.Protocol
	if err := service.ValidateMultimediaImageRequest(&request, catalog.Models[index].Capabilities); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := service.ValidateImageReferences(user.Id, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	parameters, err := common.Marshal(request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	job := &model.ImageGeneration{UserID: user.Id, Model: request.Model, Prompt: request.Prompt, Parameters: string(parameters), RequestKey: request.RequestKey, RequestIP: c.ClientIP(), Assets: []model.MediaAsset{}}
	created, err := model.CreateImageGeneration(job)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
		return
	}
	if !created && job.Parameters != string(parameters) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "This request key has already been used for different image parameters"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": job})
}

func GetMultimediaGenerations(c *gin.Context) {
	offset, size, ok := multimediaPagination(c)
	if !ok {
		return
	}
	items, total, err := model.ListImageGenerations(c.GetInt("id"), offset, size)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total})
}

func UploadMultimediaImage(c *gin.Context) {
	if c.GetInt("role") < common.FileUploadPermission {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Image uploads are disabled for this account"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxMediaImageBytes+(1<<20))
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid image upload or file exceeds 32 MB"})
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	files := c.Request.MultipartForm.File["file"]
	if len(files) != 1 || len(c.Request.MultipartForm.File) != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Upload exactly one image per request"})
		return
	}
	file, err := files[0].Open()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer file.Close()
	asset, err := service.StoreMediaImage(c.GetInt("id"), "", "reference", files[0].Filename, file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiSuccess(c, asset)
}

func GetMultimediaAssets(c *gin.Context) {
	offset, size, ok := multimediaPagination(c)
	if !ok {
		return
	}
	kind := c.Query("kind")
	if !slices.Contains([]string{"", "generated", "reference"}, kind) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid asset filter"})
		return
	}
	items, total, err := model.ListMediaAssets(c.GetInt("id"), offset, size, kind)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "retention_days": service.MediaRetentionDays()})
}

func GetMultimediaAssetContent(c *gin.Context) {
	asset, err := model.GetMediaAsset(c.GetInt("id"), c.Param("id"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	thumbnail := c.Query("thumbnail") == "true"
	path, err := service.MediaAssetPath(asset, thumbnail)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		c.Status(http.StatusGone)
		return
	}
	defer file.Close()
	c.Header("X-Content-Type-Options", "nosniff")
	contentType := asset.MimeType
	if thumbnail {
		contentType = "image/jpeg"
	}
	c.Header("Content-Type", contentType)
	if c.Query("download") == "true" {
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": asset.Name}))
	}
	http.ServeContent(c.Writer, c.Request, asset.Name, time.Unix(asset.CreatedAt, 0), file)
}

func multimediaSelectedAssets(c *gin.Context) ([]model.MediaAsset, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var request struct {
		IDs []string `json:"ids"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.IDs) < 1 || len(request.IDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Select between 1 and 100 assets"})
		return nil, false
	}
	sort.Strings(request.IDs)
	request.IDs = slices.Compact(request.IDs)
	var assets []model.MediaAsset
	err := model.DB.Where("user_id = ? AND asset_id IN ? AND removed_at = 0 AND expires_at > ?", c.GetInt("id"), request.IDs, time.Now().Unix()).Order("id ASC").Find(&assets).Error
	if err != nil || len(assets) != len(request.IDs) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "One or more selected assets are unavailable"})
		return nil, false
	}
	return assets, true
}

func RemoveMultimediaAssets(c *gin.Context) {
	assets, ok := multimediaSelectedAssets(c)
	if !ok {
		return
	}
	ids := make([]int64, 0, len(assets))
	for _, asset := range assets {
		ids = append(ids, asset.ID)
	}
	err := model.DB.Model(&model.MediaAsset{}).Where("user_id = ? AND id IN ?", c.GetInt("id"), ids).Update("removed_at", time.Now().Unix()).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"removed": len(ids)})
}

func DownloadMultimediaAssets(c *gin.Context) {
	assets, ok := multimediaSelectedAssets(c)
	if !ok {
		return
	}
	files := make([]*os.File, 0, len(assets))
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	for _, asset := range assets {
		path, err := service.MediaAssetPath(&asset, false)
		if err != nil {
			c.Status(http.StatusGone)
			return
		}
		file, err := os.Open(path)
		if err != nil {
			c.Status(http.StatusGone)
			return
		}
		files = append(files, file)
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", `attachment; filename="images.zip"`)
	archive := zip.NewWriter(c.Writer)
	for i, file := range files {
		entry, err := archive.Create(fmt.Sprintf("%03d-%s", i+1, assets[i].Name))
		if err == nil {
			_, err = file.WriteTo(entry)
		}
		if err != nil {
			common.SysError("multimedia asset archive: " + err.Error())
			break
		}
	}
	if err := archive.Close(); err != nil {
		common.SysError("multimedia asset archive: " + err.Error())
	}
}

func multimediaPagination(c *gin.Context) (int, int, bool) {
	page := common.GetPageQuery(c)
	number := max(1, page.GetPage())
	size := min(50, max(1, page.GetPageSize()))
	if number > math.MaxInt/size {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid page number"})
		return 0, 0, false
	}
	return (number - 1) * size, size, true
}
