package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedCode     int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, newAPIError.StatusCode)
		})
	}
}

func TestRelayErrorHandlerTruncatesInvalidJSONBodyInLog(t *testing.T) {
	withDebugEnabled(t, false)

	body := strings.Repeat("b", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, "bad response status code 500", newAPIError.Error())
	require.Contains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), fmt.Sprintf("original_length=%d", len(body)))
	require.NotContains(t, logBuffer.String(), strings.Repeat("b", common.LocalLogContentLimit+1))
}

func TestRelayErrorHandlerKeepsStructuredErrorMessage(t *testing.T) {
	message := strings.Repeat("c", common.LocalLogContentLimit+256)
	body := `{"message":"` + message + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsOpenAIErrorMessage(t *testing.T) {
	message := strings.Repeat("d", common.LocalLogContentLimit+256)
	body := `{"error":{"message":"` + message + `","type":"server_error","code":"server_error"}}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsInvalidJSONBodyInDebugLog(t *testing.T) {
	withDebugEnabled(t, true)

	body := strings.Repeat("e", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.NotContains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), body)
}

// TestRelayErrorHandlerMasksUpstreamQuotaError 锁定对外错误文案契约：当上游本身也是
// new-api 实例时，其额度不足报文里带有上游中转账号的余额，绝不能透传给最终用户。
func TestRelayErrorHandlerMasksUpstreamQuotaError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		body             string
		showBodyWhenFail bool
		expectedMessage  string
	}{
		{
			name:            "mask pre-consume failure in openai shaped error",
			body:            `{"error":{"message":"预扣费额度失败, 用户剩余额度: $0.01, 需要预扣费额度: $0.50","type":"insufficient_user_quota"}}`,
			expectedMessage: upstreamGroupUnavailableMessage,
		},
		{
			name:            "mask insufficient quota in openai shaped error",
			body:            `{"error":{"type":"insufficient_user_quota","message":"用户额度不足, 剩余额度: ¥-0.043204 (request id: 20260802012231449650236)"},"type":"error"}`,
			expectedMessage: upstreamGroupUnavailableMessage,
		},
		{
			name:            "mask insufficient quota in flat message body",
			body:            `{"message":"用户额度不足, 剩余额度: ¥-0.043204"}`,
			expectedMessage: upstreamGroupUnavailableMessage,
		},
		{
			name:            "mask insufficient account balance in openai shaped error",
			body:            `{"error":{"message":"Insufficient account balance","type":"insufficient_quota"}}`,
			expectedMessage: upstreamGroupUnavailableMessage,
		},
		{
			name:            "leave unrelated upstream message untouched",
			body:            `{"error":{"message":"model not found","type":"invalid_request_error"}}`,
			expectedMessage: "model not found",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resp := &http.Response{
				StatusCode: http.StatusForbidden,
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}

			newAPIError := RelayErrorHandler(context.Background(), resp, false)

			require.NotNil(t, newAPIError)
			assert.Equal(t, tc.expectedMessage, newAPIError.Error())
			assert.NotContains(t, newAPIError.Error(), "0.043204")
			assert.NotContains(t, newAPIError.Error(), "剩余额度")
		})
	}
}

// TestRelayErrorHandlerKeepsUpstreamQuotaErrorForChannelTest 渠道测试场景下管理员需要看到
// 上游的真实失败原因，改写必须让路。
func TestRelayErrorHandlerKeepsUpstreamQuotaErrorForChannelTest(t *testing.T) {
	t.Parallel()

	body := `{"error":{"type":"insufficient_user_quota","message":"用户额度不足, 剩余额度: ¥-0.043204"}}`
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, true)

	require.NotNil(t, newAPIError)
	assert.Contains(t, newAPIError.Error(), "用户额度不足")
	assert.Contains(t, newAPIError.Error(), "¥-0.043204")
	assert.NotContains(t, newAPIError.Error(), upstreamGroupUnavailableMessage)
}

func withDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()

	oldDebug := common.DebugEnabled
	common.DebugEnabled = enabled
	t.Cleanup(func() {
		common.DebugEnabled = oldDebug
	})
}
