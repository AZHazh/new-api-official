package model

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProviderRateLimitGuard serializes requests that share one upstream
// credential. The raw credential must never be stored in this table.
type ProviderRateLimitGuard struct {
	Provider              string `gorm:"size:32;primaryKey"`
	ChannelID             int    `gorm:"primaryKey;autoIncrement:false"`
	CredentialFingerprint string `gorm:"size:64;primaryKey"`
	LastSeenAt            int64  `gorm:"bigint;not null"`
}

// ProviderRateLimitRequest is one accepted request in a provider's rolling
// rate-limit window. Rows are deleted after the longest configured window.
type ProviderRateLimitRequest struct {
	ID                    int64  `gorm:"primaryKey"`
	Provider              string `gorm:"size:32;not null;index:idx_provider_rate_limit_scope_time,priority:1"`
	ChannelID             int    `gorm:"not null;index:idx_provider_rate_limit_scope_time,priority:2"`
	CredentialFingerprint string `gorm:"size:64;not null;index:idx_provider_rate_limit_scope_time,priority:3"`
	RequestedAt           int64  `gorm:"bigint;not null;index:idx_provider_rate_limit_scope_time,priority:4"`
}

type ProviderRateLimitWindow struct {
	Maximum         int
	DurationSeconds int64
}

var ErrInvalidProviderRateLimit = errors.New("invalid provider rate limit")

// TakeProviderRateLimit atomically checks every rolling window and records the
// request only when all windows allow it. It is the cross-instance fallback
// for deployments without Redis.
func TakeProviderRateLimit(
	ctx context.Context,
	provider string,
	channelID int,
	credentialFingerprint string,
	windows []ProviderRateLimitWindow,
) (bool, int64, error) {
	return takeProviderRateLimitAt(ctx, DB, provider, channelID, credentialFingerprint, windows, 0)
}

func takeProviderRateLimitAt(
	ctx context.Context,
	db *gorm.DB,
	provider string,
	channelID int,
	credentialFingerprint string,
	windows []ProviderRateLimitWindow,
	now int64,
) (bool, int64, error) {
	provider = strings.TrimSpace(provider)
	if db == nil {
		return false, 0, errors.New("provider rate limit database is not initialized")
	}
	if provider == "" || len(provider) > 32 || channelID <= 0 {
		return false, 0, ErrInvalidProviderRateLimit
	}
	decodedFingerprint, err := hex.DecodeString(credentialFingerprint)
	if err != nil || len(decodedFingerprint) != 32 || credentialFingerprint != strings.ToLower(credentialFingerprint) {
		return false, 0, ErrInvalidProviderRateLimit
	}
	if len(windows) == 0 {
		return false, 0, ErrInvalidProviderRateLimit
	}
	var longestWindow int64
	for _, window := range windows {
		if window.Maximum <= 0 || window.DurationSeconds <= 0 {
			return false, 0, ErrInvalidProviderRateLimit
		}
		if window.DurationSeconds > longestWindow {
			longestWindow = window.DurationSeconds
		}
	}

	allowed := false
	var retryAfterSeconds int64
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		guard := ProviderRateLimitGuard{
			Provider:              provider,
			ChannelID:             channelID,
			CredentialFingerprint: credentialFingerprint,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&guard).Error; err != nil {
			return fmt.Errorf("create provider rate limit guard: %w", err)
		}
		if err := lockForUpdate(tx).
			Where("provider = ? AND channel_id = ? AND credential_fingerprint = ?", provider, channelID, credentialFingerprint).
			First(&guard).Error; err != nil {
			return fmt.Errorf("lock provider rate limit guard: %w", err)
		}

		transactionNow := now
		if transactionNow <= 0 {
			transactionNow = getDBTimestamp(tx)
		}
		if transactionNow <= 0 || longestWindow > transactionNow {
			return ErrInvalidProviderRateLimit
		}

		if err := tx.Where(
			"provider = ? AND channel_id = ? AND credential_fingerprint = ? AND requested_at <= ?",
			provider,
			channelID,
			credentialFingerprint,
			transactionNow-longestWindow,
		).
			Delete(&ProviderRateLimitRequest{}).Error; err != nil {
			return fmt.Errorf("clean provider rate limit requests: %w", err)
		}

		for _, window := range windows {
			windowStart := transactionNow - window.DurationSeconds
			var count int64
			if err := tx.Model(&ProviderRateLimitRequest{}).
				Where(
					"provider = ? AND channel_id = ? AND credential_fingerprint = ? AND requested_at > ?",
					provider,
					channelID,
					credentialFingerprint,
					windowStart,
				).
				Count(&count).Error; err != nil {
				return fmt.Errorf("count provider rate limit requests: %w", err)
			}
			if count < int64(window.Maximum) {
				continue
			}

			var oldest ProviderRateLimitRequest
			if err := tx.Model(&ProviderRateLimitRequest{}).
				Select("requested_at").
				Where(
					"provider = ? AND channel_id = ? AND credential_fingerprint = ? AND requested_at > ?",
					provider,
					channelID,
					credentialFingerprint,
					windowStart,
				).
				Order("requested_at ASC, id ASC").
				First(&oldest).Error; err != nil {
				return fmt.Errorf("find provider rate limit boundary: %w", err)
			}
			windowRetry := oldest.RequestedAt + window.DurationSeconds - transactionNow
			if windowRetry < 1 {
				windowRetry = 1
			}
			if windowRetry > retryAfterSeconds {
				retryAfterSeconds = windowRetry
			}
		}

		if err := tx.Model(&ProviderRateLimitGuard{}).
			Where("provider = ? AND channel_id = ? AND credential_fingerprint = ?", provider, channelID, credentialFingerprint).
			Update("last_seen_at", transactionNow).Error; err != nil {
			return fmt.Errorf("update provider rate limit guard: %w", err)
		}
		if retryAfterSeconds > 0 {
			return nil
		}

		request := ProviderRateLimitRequest{
			Provider:              provider,
			ChannelID:             channelID,
			CredentialFingerprint: credentialFingerprint,
			RequestedAt:           transactionNow,
		}
		if err := tx.Create(&request).Error; err != nil {
			return fmt.Errorf("record provider rate limit request: %w", err)
		}
		allowed = true
		return nil
	})
	if err != nil {
		return false, 0, err
	}
	return allowed, retryAfterSeconds, nil
}
