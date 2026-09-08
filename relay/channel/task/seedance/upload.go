package seedance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
)

const MaxUploadBytes int64 = 50 << 20

var (
	ErrUploadEmpty    = errors.New("Seedance upload is empty")
	ErrUploadTooLarge = errors.New("Seedance upload exceeds the maximum size")
)

type UploadResponse struct {
	URL       string `json:"url"`
	FileType  string `json:"file_type"`
	Size      int64  `json:"size"`
	ExpiresIn int64  `json:"expires_in"`
}

type UpstreamUploadError struct {
	StatusCode int
	Code       string
	Message    string
	RetryAfter string
}

func (err *UpstreamUploadError) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

type UploadClient struct {
	BaseURL string
	APIKey  string
	Proxy   string
}

// Upload streams one validated video-workflow asset to Seedance. A zero size
// means the caller cannot know the size until the stream is consumed.
// Aggregate
// per-key minute/day rate limiting belongs in the controller/service layer so
// every user and application instance shares the same counters.
func (client UploadClient) Upload(ctx context.Context, filename string, size int64, source io.Reader) (*UploadResponse, error) {
	if size < 0 || size > MaxUploadBytes {
		return nil, fmt.Errorf("upload size must be between 1 and %d bytes", MaxUploadBytes)
	}
	if strings.TrimSpace(filename) == "" {
		return nil, fmt.Errorf("upload filename is required")
	}
	if strings.TrimSpace(client.BaseURL) == "" || strings.TrimSpace(client.APIKey) == "" {
		return nil, fmt.Errorf("Seedance upload client is not configured")
	}

	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	writeDone := make(chan error, 1)
	go func() {
		part, err := multipartWriter.CreateFormFile("file", filename)
		if err == nil {
			limit := MaxUploadBytes + 1
			if size > 0 {
				limit = size + 1
			}
			var copied int64
			copied, err = io.Copy(part, io.LimitReader(source, limit))
			if err == nil {
				switch {
				case copied == 0:
					err = ErrUploadEmpty
				case copied > MaxUploadBytes:
					err = ErrUploadTooLarge
				case size > 0 && copied != size:
					err = fmt.Errorf("upload size changed while streaming: expected %d, got %d", size, copied)
				}
			}
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		_ = pipeWriter.CloseWithError(err)
		writeDone <- err
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(client.BaseURL, "/")+"/v1/files/upload", pipeReader)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+client.APIKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())

	httpClient, err := service.GetHttpClientWithProxy(client.Proxy)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		return nil, fmt.Errorf("create upload HTTP client: %w", err)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		if writeErr := <-writeDone; writeErr != nil && !errors.Is(writeErr, io.ErrClosedPipe) {
			return nil, writeErr
		}
		return nil, fmt.Errorf("upload Seedance asset: %w", err)
	}
	writeErr := <-writeDone
	if writeErr != nil {
		_ = response.Body.Close()
		return nil, writeErr
	}
	body, err := readLimitedBody(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		taskErr := upstreamTaskError(response.StatusCode, body)
		return nil, &UpstreamUploadError{
			StatusCode: response.StatusCode,
			Code:       taskErr.Code,
			Message:    taskErr.Message,
			RetryAfter: response.Header.Get("Retry-After"),
		}
	}
	var result UploadResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal Seedance upload response: %w", err)
	}
	if err := validateMediaURL(result.URL); err != nil {
		return nil, fmt.Errorf("invalid Seedance upload URL: %w", err)
	}
	if result.Size <= 0 || result.Size > MaxUploadBytes {
		return nil, fmt.Errorf("invalid Seedance upload response size %d", result.Size)
	}
	if result.ExpiresIn <= 0 {
		return nil, fmt.Errorf("invalid Seedance upload expiry %d", result.ExpiresIn)
	}
	switch result.FileType {
	case "image", "audio", "video":
	default:
		return nil, fmt.Errorf("invalid Seedance upload file_type %q", result.FileType)
	}
	return &result, nil
}
