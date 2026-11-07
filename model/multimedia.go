package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ImageGeneration records dashboard jobs separately from provider polling tasks.
// A request is claimed once; an interrupted upstream call is never replayed.
type ImageGeneration struct {
	ID           int64        `json:"-" gorm:"primaryKey"`
	GenerationID string       `json:"id" gorm:"type:varchar(64);uniqueIndex"`
	UserID       int          `json:"-" gorm:"index;uniqueIndex:idx_image_generation_request,priority:1"`
	RequestKey   string       `json:"-" gorm:"type:varchar(64);uniqueIndex:idx_image_generation_request,priority:2"`
	Model        string       `json:"model" gorm:"type:varchar(191)"`
	Prompt       string       `json:"prompt" gorm:"type:text"`
	Parameters   string       `json:"parameters" gorm:"type:text"`
	Status       string       `json:"status" gorm:"type:varchar(24);index"`
	Error        string       `json:"error,omitempty" gorm:"type:text"`
	RequestIP    string       `json:"-" gorm:"type:varchar(64)"`
	CreatedAt    int64        `json:"created_at" gorm:"index"`
	StartedAt    int64        `json:"started_at"`
	FinishedAt   int64        `json:"finished_at"`
	Assets       []MediaAsset `json:"assets" gorm:"-"`
}

type MediaAsset struct {
	ID           int64  `json:"-" gorm:"primaryKey"`
	AssetID      string `json:"id" gorm:"type:varchar(64);uniqueIndex"`
	UserID       int    `json:"-" gorm:"index"`
	GenerationID string `json:"generation_id,omitempty" gorm:"type:varchar(64);index"`
	Kind         string `json:"kind" gorm:"type:varchar(16);index"`
	Name         string `json:"name" gorm:"type:varchar(255)"`
	StorageKey   string `json:"-" gorm:"type:varchar(128)"`
	MimeType     string `json:"mime_type" gorm:"type:varchar(64)"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Bytes        int64  `json:"bytes"`
	CreatedAt    int64  `json:"created_at" gorm:"index"`
	ExpiresAt    int64  `json:"expires_at" gorm:"index"`
	RemovedAt    int64  `json:"-" gorm:"index"`
}

func CreateImageGeneration(job *ImageGeneration) (bool, error) {
	created := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Serialize submissions for this user, including the active-job limit.
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, job.UserID).Error; err != nil {
			return err
		}
		var existing ImageGeneration
		err := tx.Where("user_id = ? AND request_key = ?", job.UserID, job.RequestKey).First(&existing).Error
		if err == nil {
			*job = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var active int64
		if err := tx.Model(&ImageGeneration{}).Where("user_id = ? AND status IN ?", job.UserID, []string{"queued", "running"}).Count(&active).Error; err != nil {
			return err
		}
		if active >= 3 {
			return errors.New("too many active image generations")
		}
		job.GenerationID = "img_" + uuid.NewString()
		job.Status = "queued"
		job.CreatedAt = time.Now().Unix()
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}

func GetMediaAsset(userID int, assetID string) (*MediaAsset, error) {
	var asset MediaAsset
	err := DB.Where("user_id = ? AND asset_id = ? AND removed_at = 0 AND expires_at > ?", userID, assetID, time.Now().Unix()).First(&asset).Error
	return &asset, err
}

func ListMediaAssets(userID, offset, limit int, kind string) ([]MediaAsset, int64, error) {
	query := DB.Model(&MediaAsset{}).Where("user_id = ? AND removed_at = 0 AND expires_at > ?", userID, time.Now().Unix())
	query = query.Where("kind <> ?", "mask")
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]MediaAsset, 0)
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

func ListImageGenerations(userID, offset, limit int) ([]ImageGeneration, int64, error) {
	query := DB.Model(&ImageGeneration{}).Where("user_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	jobs := make([]ImageGeneration, 0)
	if err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&jobs).Error; err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.GenerationID)
	}
	assets := make([]MediaAsset, 0)
	if len(ids) > 0 {
		if err := DB.Where("user_id = ? AND generation_id IN ? AND removed_at = 0 AND expires_at > ?", userID, ids, time.Now().Unix()).Order("id ASC").Find(&assets).Error; err != nil {
			return nil, 0, err
		}
	}
	byGeneration := make(map[string][]MediaAsset)
	for _, asset := range assets {
		byGeneration[asset.GenerationID] = append(byGeneration[asset.GenerationID], asset)
	}
	for i := range jobs {
		jobs[i].Assets = byGeneration[jobs[i].GenerationID]
		if jobs[i].Assets == nil {
			jobs[i].Assets = []MediaAsset{}
		}
	}
	return jobs, total, nil
}
