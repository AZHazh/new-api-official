package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultimediaExtractsImagesAcrossProviderResponseFormats(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want []GeneratedMediaImage
	}{
		{"Images base64", `{"data":[{"b64_json":"aW1hZ2U="}]}`, []GeneratedMediaImage{{Base64: "aW1hZ2U="}}},
		{"Images URL", `{"data":[{"url":"https://images.example/result.png"}]}`, []GeneratedMediaImage{{URL: "https://images.example/result.png"}}},
		{"Gemini camel case", `{"candidates":[{"content":{"parts":[{"text":"Here is the image"},{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`, []GeneratedMediaImage{{Base64: "aW1hZ2U="}}},
		{"Gemini snake case", `{"candidates":[{"content":{"parts":[{"inline_data":{"mime_type":"image/png","data":"aW1hZ2U="}}]}}]}`, []GeneratedMediaImage{{Base64: "aW1hZ2U="}}},
		{"chat images", `{"choices":[{"message":{"content":"done","images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,aW1hZ2U="}}]}}]}`, []GeneratedMediaImage{{Base64: "aW1hZ2U="}}},
		{"chat content parts", `{"choices":[{"message":{"content":[{"type":"image_url","image_url":"https://images.example/result.png"}]}}]}`, []GeneratedMediaImage{{URL: "https://images.example/result.png"}}},
		{"chat markdown", `{"choices":[{"message":{"content":"Here it is: ![result](data:image/png;base64,aW1hZ2U=)"}}]}`, []GeneratedMediaImage{{Base64: "aW1hZ2U="}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			images, err := ExtractMultimediaImages(strings.NewReader(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.want, images)
		})
	}
}

func TestMultimediaRejectsTextOnlyBlockedAndThoughtImageResults(t *testing.T) {
	for _, body := range []string{
		`{"data":[{}]}`,
		`{"promptFeedback":{"blockReason":"SAFETY"}}`,
		`{"candidates":[{"content":{"parts":[{"thought":true,"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`,
		`{"choices":[{"message":{"content":"See https://example.com/manual for help"}}]}`,
	} {
		_, err := ExtractMultimediaImages(strings.NewReader(body))
		require.Error(t, err)
	}
}
