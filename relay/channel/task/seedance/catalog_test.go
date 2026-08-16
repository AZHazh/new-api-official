package seedance

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogHasExactVideoScope(t *testing.T) {
	require.Len(t, VideoModelCatalog, 105)
	require.Len(t, ContextIRModelCatalog, 3)
	require.Len(t, ModelList, 105)
	require.Len(t, ContextIRModelList, 3)
	require.Len(t, AllModelList, 108)

	seen := make(map[string]Protocol, 109)
	for _, capability := range append(append([]ModelCapability{}, VideoModelCatalog...), ContextIRModelCatalog...) {
		_, duplicate := seen[capability.Name]
		assert.Falsef(t, duplicate, "duplicate catalog model %s", capability.Name)
		seen[capability.Name] = capability.Protocol
		assert.NotEmpty(t, capability.Family)
		assert.NotEmpty(t, capability.Kind)
	}
	require.Len(t, seen, 108)

	for _, name := range ContextIRModelList {
		assert.True(t, IsContextIRModel(name))
	}
	assert.False(t, IsContextIRModel("seedance-2.0-mini-t2v"))
	_, routableAsMainModel := seen[MidjourneyVideoCapability.Name]
	assert.False(t, routableAsMainModel)
	assert.Equal(t, ProtocolMidjourneyVideo, MidjourneyVideoCapability.Protocol)
}

func TestClassifyDiscoveredModelFailsClosed(t *testing.T) {
	tests := []struct {
		model      string
		category   DiscoveredModelCategory
		selectable bool
	}{
		{model: "seedance-2.0-mini-t2v", category: DiscoveredModelVideoOutput, selectable: true},
		{model: "minmax-h3-context-ir-text", category: DiscoveredModelVideoPromptEnhancer, selectable: true},
		{model: "midjourney-video", category: DiscoveredModelMidjourneyVideo, selectable: true},
		{model: "seedream-v5-pro-t2i", category: DiscoveredModelNonVideo},
		{model: "seedance-3.0-future-model", category: DiscoveredModelUnknown},
	}

	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			discovered := ClassifyDiscoveredModel(test.model)
			assert.Equal(t, test.model, discovered.ID)
			assert.Equal(t, test.category, discovered.Category)
			assert.Equal(t, test.selectable, discovered.Selectable)
		})
	}
}
