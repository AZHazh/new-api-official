package model

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetProviderRateLimitTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&ProviderRateLimitGuard{}, &ProviderRateLimitRequest{}))
	require.NoError(t, DB.Exec("DELETE FROM provider_rate_limit_requests").Error)
	require.NoError(t, DB.Exec("DELETE FROM provider_rate_limit_guards").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM provider_rate_limit_requests")
		DB.Exec("DELETE FROM provider_rate_limit_guards")
	})
}

func TestProviderRateLimitChecksRollingWindowsAndCleansExpiredRows(t *testing.T) {
	resetProviderRateLimitTables(t)
	const (
		provider    = "seedance"
		channelID   = 17
		initialTime = int64(1_000_000)
	)
	fingerprint := strings.Repeat("a", 64)
	windows := []ProviderRateLimitWindow{
		{Maximum: 2, DurationSeconds: 60},
		{Maximum: 3, DurationSeconds: 300},
	}
	require.NoError(t, DB.Create(&ProviderRateLimitRequest{
		Provider:              provider,
		ChannelID:             channelID,
		CredentialFingerprint: fingerprint,
		RequestedAt:           initialTime - 301,
	}).Error)

	for range 2 {
		allowed, retryAfter, err := takeProviderRateLimitAt(
			context.Background(), DB, provider, channelID, fingerprint, windows, initialTime,
		)
		require.NoError(t, err)
		assert.True(t, allowed)
		assert.Zero(t, retryAfter)
	}
	allowed, retryAfter, err := takeProviderRateLimitAt(
		context.Background(), DB, provider, channelID, fingerprint, windows, initialTime,
	)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, int64(60), retryAfter)

	allowed, retryAfter, err = takeProviderRateLimitAt(
		context.Background(), DB, provider, channelID, fingerprint, windows, initialTime+61,
	)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Zero(t, retryAfter)

	allowed, retryAfter, err = takeProviderRateLimitAt(
		context.Background(), DB, provider, channelID, fingerprint, windows, initialTime+62,
	)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, int64(238), retryAfter)

	var requestCount int64
	require.NoError(t, DB.Model(&ProviderRateLimitRequest{}).Count(&requestCount).Error)
	assert.Equal(t, int64(3), requestCount, "rejected attempts must not consume a provider slot")

	allowed, retryAfter, err = takeProviderRateLimitAt(
		context.Background(), DB, provider, channelID, fingerprint, windows, initialTime+301,
	)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Zero(t, retryAfter)
	require.NoError(t, DB.Model(&ProviderRateLimitRequest{}).
		Where("requested_at <= ?", initialTime+1).
		Count(&requestCount).Error)
	assert.Zero(t, requestCount, "records outside the longest rolling window must be removed")
}

func TestProviderRateLimitScopesByChannelAndFingerprint(t *testing.T) {
	resetProviderRateLimitTables(t)
	windows := []ProviderRateLimitWindow{{Maximum: 1, DurationSeconds: 60}}
	fingerprintA := strings.Repeat("a", 64)
	fingerprintB := strings.Repeat("b", 64)

	allowed, _, err := takeProviderRateLimitAt(context.Background(), DB, "seedance", 11, fingerprintA, windows, 1_000_000)
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, _, err = takeProviderRateLimitAt(context.Background(), DB, "seedance", 12, fingerprintA, windows, 1_000_000)
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, _, err = takeProviderRateLimitAt(context.Background(), DB, "seedance", 11, fingerprintB, windows, 1_000_000)
	require.NoError(t, err)
	assert.True(t, allowed)

	allowed, retryAfter, err := takeProviderRateLimitAt(context.Background(), DB, "seedance", 11, fingerprintA, windows, 1_000_000)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, int64(60), retryAfter)
}

func TestProviderRateLimitRejectsNonFingerprintCredentials(t *testing.T) {
	resetProviderRateLimitTables(t)
	allowed, retryAfter, err := takeProviderRateLimitAt(
		context.Background(),
		DB,
		"seedance",
		1,
		"raw-upstream-api-key",
		[]ProviderRateLimitWindow{{Maximum: 1, DurationSeconds: 60}},
		1_000_000,
	)
	assert.False(t, allowed)
	assert.Zero(t, retryAfter)
	assert.ErrorIs(t, err, ErrInvalidProviderRateLimit)

	var requestCount int64
	require.NoError(t, DB.Model(&ProviderRateLimitRequest{}).Count(&requestCount).Error)
	assert.Zero(t, requestCount)
	var guardCount int64
	require.NoError(t, DB.Model(&ProviderRateLimitGuard{}).Count(&guardCount).Error)
	assert.Zero(t, guardCount)
}
