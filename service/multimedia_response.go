package service

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type GeneratedMediaImage struct {
	URL    string `json:"url"`
	Base64 string `json:"b64_json"`
}

type multimediaContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	ImageURL json.RawMessage `json:"image_url"`
}

func (part multimediaContentPart) imageURL() string {
	var value string
	if common.Unmarshal(part.ImageURL, &value) == nil {
		return value
	}
	var object struct {
		URL string `json:"url"`
	}
	if common.Unmarshal(part.ImageURL, &object) == nil {
		return object.URL
	}
	return ""
}

var multimediaMarkdownImage = regexp.MustCompile(`!\[[^\]]*\]\((data:image/[^\s)]+|https?://[^\s)]+)\)`)

// ExtractMultimediaImages normalizes Images, Gemini generateContent, and chat
// image responses after the relay has handled usage and billing.
func ExtractMultimediaImages(reader io.Reader) ([]GeneratedMediaImage, error) {
	var response struct {
		Data       []GeneratedMediaImage     `json:"data"`
		Candidates []dto.GeminiChatCandidate `json:"candidates"`
		Choices    []struct {
			Message struct {
				Content json.RawMessage         `json:"content"`
				Images  []multimediaContentPart `json:"images"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := common.DecodeJson(reader, &response); err != nil {
		return nil, err
	}
	images := response.Data
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if !part.Thought && part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "image/") && part.InlineData.Data != "" {
				images = append(images, GeneratedMediaImage{Base64: part.InlineData.Data})
			}
		}
	}
	for _, choice := range response.Choices {
		parts := choice.Message.Images
		var text string
		if common.Unmarshal(choice.Message.Content, &text) == nil {
			parts = append(parts, multimediaContentPart{Type: "text", Text: text})
		} else if len(choice.Message.Content) > 0 {
			var content []multimediaContentPart
			if common.Unmarshal(choice.Message.Content, &content) == nil {
				parts = append(parts, content...)
			}
		}
		for _, part := range parts {
			if url := part.imageURL(); url != "" {
				images = append(images, GeneratedMediaImage{URL: url})
			}
			if strings.HasPrefix(part.Text, "data:image/") {
				images = append(images, GeneratedMediaImage{URL: strings.TrimSpace(part.Text)})
				continue
			}
			for _, match := range multimediaMarkdownImage.FindAllStringSubmatch(part.Text, -1) {
				images = append(images, GeneratedMediaImage{URL: match[1]})
			}
		}
	}
	normalized := make([]GeneratedMediaImage, 0, len(images))
	for _, item := range images {
		if strings.HasPrefix(item.URL, "data:image/") {
			parts := strings.SplitN(item.URL, ",", 2)
			if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
				continue
			}
			item.Base64, item.URL = parts[1], ""
		}
		if item.Base64 == "" && item.URL == "" {
			continue
		}
		normalized = append(normalized, item)
	}
	if len(normalized) == 0 || len(normalized) > dto.MaxImageN {
		return nil, errors.New("the image provider returned no usable images or too many images")
	}
	return normalized, nil
}
