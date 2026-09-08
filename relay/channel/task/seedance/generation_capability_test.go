package seedance

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerationCapabilitiesForSeedanceImageModel(t *testing.T) {
	capability, ok := CapabilityForModel("seedance-2.0-standard-i2v")
	require.True(t, ok)

	result := GenerationCapabilitiesForModel(capability)

	assert.True(t, result.Prompt.Supported)
	assert.False(t, result.Prompt.Required)
	require.Len(t, result.Media, 1)
	assert.Equal(t, GenerationMediaCapability{
		Type: "image", Field: "images", Role: "reference", MinItems: 1, MaxItems: 2,
	}, result.Media[0])
	assert.Equal(t, 4, result.Durations.Min)
	assert.Equal(t, 15, result.Durations.Max)
	require.NotNil(t, result.Durations.SmartValue)
	assert.Equal(t, -1, *result.Durations.SmartValue)
	assert.Contains(t, result.Resolutions, "native1080p")
	assert.Contains(t, result.Resolutions, "native4k")
	assert.Equal(t, "adaptive", result.Defaults.AspectRatio)
	assert.Equal(t, "720p", result.Defaults.Resolution)
	require.Len(t, result.AdvancedFields, 1)
	assert.Equal(t, "metadata.seed", result.AdvancedFields[0].Path)
	require.NotNil(t, result.AdvancedFields[0].Minimum)
	require.NotNil(t, result.AdvancedFields[0].Maximum)
	assert.Equal(t, int64(-1), *result.AdvancedFields[0].Minimum)
	assert.Equal(t, int64(2147483647), *result.AdvancedFields[0].Maximum)
}

func TestGenerationCapabilitiesForSeedanceMultiModel(t *testing.T) {
	capability, ok := CapabilityForModel("seedance-2.5-standard-multi")
	require.True(t, ok)

	result := GenerationCapabilitiesForModel(capability)

	require.Len(t, result.Media, 3)
	assert.Equal(t, GenerationMediaCapability{
		Type: "image", Field: "metadata.content", Role: "reference", MinItems: 0, MaxItems: 30,
	}, result.Media[0])
	assert.Equal(t, GenerationMediaCapability{
		Type: "video", Field: "metadata.content", Role: "reference", MinItems: 0, MaxItems: 10,
	}, result.Media[1])
	assert.Equal(t, GenerationMediaCapability{
		Type: "audio", Field: "metadata.content", Role: "reference", MinItems: 0, MaxItems: 10,
	}, result.Media[2])
	assert.Contains(t, result.Requirements, "at_least_one_media")
	assert.Equal(t, 4, result.Durations.Min)
	assert.Equal(t, 30, result.Durations.Max)
}

func TestGenerationCapabilitiesForHailuoMultiModel(t *testing.T) {
	capability, ok := CapabilityForModel("hailuo-h3-global-multi")
	require.True(t, ok)

	result := GenerationCapabilitiesForModel(capability)

	require.Len(t, result.Media, 3)
	assert.Equal(t, GenerationMediaCapability{
		Type: "image", Field: "metadata.content", Role: "reference", MinItems: 0, MaxItems: 9,
	}, result.Media[0])
	assert.Equal(t, GenerationMediaCapability{
		Type: "video", Field: "metadata.content", Role: "reference", MinItems: 0, MaxItems: 3,
	}, result.Media[1])
	assert.Equal(t, GenerationMediaCapability{
		Type: "audio", Field: "metadata.content", Role: "reference", MinItems: 0, MaxItems: 3,
	}, result.Media[2])
	assert.Equal(t, "adaptive", result.Defaults.AspectRatio)
	assert.Equal(t, "768P", result.Defaults.Resolution)
}

func TestGenerationCapabilitiesForUpscaler(t *testing.T) {
	capability, ok := CapabilityForModel("zhenzhen-upscaler")
	require.True(t, ok)

	result := GenerationCapabilitiesForModel(capability)

	assert.False(t, result.Prompt.Supported)
	assert.False(t, result.Prompt.Required)
	assert.False(t, result.Durations.Supported)
	require.Len(t, result.Media, 1)
	assert.Equal(t, GenerationMediaCapability{
		Type: "video", Field: "metadata.content", Role: "source", MinItems: 1, MaxItems: 1,
	}, result.Media[0])
	assert.Equal(t, []string{"720p", "1080p", "2k", "4k"}, result.Resolutions)
}

func TestGenerationCapabilitiesForMidjourneyVideo(t *testing.T) {
	result := GenerationCapabilitiesForModel(MidjourneyVideoCapability)

	assert.True(t, result.Prompt.Supported)
	assert.False(t, result.Prompt.Required)
	assert.False(t, result.Durations.Supported)
	require.Len(t, result.Media, 2)
	assert.Equal(t, GenerationMediaCapability{
		Type: "image", Field: "image_urls", Role: "start_frame", MinItems: 1, MaxItems: 1,
	}, result.Media[0])
	assert.Equal(t, GenerationMediaCapability{
		Type: "image", Field: "end_url", Role: "end_frame", MinItems: 0, MaxItems: 1,
	}, result.Media[1])
	assert.Equal(t, []string{"480p", "720p"}, result.Resolutions)
	assert.Equal(t, []int{1, 2, 4}, result.BatchSizes)
	require.NotNil(t, result.Defaults.BatchSize)
	assert.Equal(t, 1, *result.Defaults.BatchSize)
	require.Len(t, result.AdvancedFields, 3)
	assert.Equal(t, "video_type", result.AdvancedFields[0].Path)
	assert.Equal(t, []string{
		"vid_1.1_i2v_480", "vid_1.1_i2v_720", "vid_1.1_i2v_start_end_480", "vid_1.1_i2v_start_end_720",
	}, result.AdvancedFields[0].Options)
	assert.Equal(t, "animate_mode", result.AdvancedFields[1].Path)
	assert.Equal(t, "motion", result.AdvancedFields[2].Path)
}

func TestGenerationCapabilitiesExposeValidatedAdvancedBounds(t *testing.T) {
	capability, ok := CapabilityForModel("flux-3-video-t2v")
	require.True(t, ok)

	result := GenerationCapabilitiesForModel(capability)
	require.Len(t, result.AdvancedFields, 2)
	assert.Equal(t, "metadata.draft", result.AdvancedFields[0].Path)
	assert.Equal(t, false, result.AdvancedFields[0].DefaultValue)
	assert.Equal(t, "metadata.safety_tolerance", result.AdvancedFields[1].Path)
	require.NotNil(t, result.AdvancedFields[1].Minimum)
	require.NotNil(t, result.AdvancedFields[1].Maximum)
	assert.Equal(t, int64(0), *result.AdvancedFields[1].Minimum)
	assert.Equal(t, int64(4), *result.AdvancedFields[1].Maximum)
}

func TestGenerationCapabilitiesMatchFluxAndViduMediaRules(t *testing.T) {
	flux, ok := CapabilityForModel("flux-3-video-i2v")
	require.True(t, ok)
	fluxCapabilities := GenerationCapabilitiesForModel(flux)
	require.Len(t, fluxCapabilities.Media, 1)
	assert.Equal(t, 10, fluxCapabilities.Media[0].MaxItems)

	vidu, ok := CapabilityForModel("vidu-q3-pro-t2v")
	require.True(t, ok)
	viduCapabilities := GenerationCapabilitiesForModel(vidu)
	assert.Equal(t, []string{"16:9", "9:16", "1:1", "4:3", "3:4"}, viduCapabilities.AspectRatios)
}

func TestGenerationCapabilitiesOnlyExposeZhenzhenFastType(t *testing.T) {
	for _, name := range []string{"zhenzhen-video-v31-quality", "zhenzhen-video-v31-lite"} {
		capability, ok := CapabilityForModel(name)
		require.True(t, ok)
		for _, field := range GenerationCapabilitiesForModel(capability).AdvancedFields {
			assert.NotEqual(t, "metadata.type", field.Path)
		}
	}
	capability, ok := CapabilityForModel("zhenzhen-video-v31-fast")
	require.True(t, ok)
	fields := GenerationCapabilitiesForModel(capability).AdvancedFields
	require.Len(t, fields, 1)
	assert.Equal(t, "metadata.type", fields[0].Path)
	assert.Equal(t, []string{"reference"}, fields[0].Options)
}
