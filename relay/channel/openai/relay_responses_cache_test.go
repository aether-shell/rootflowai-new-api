package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestApplyUsagePostProcessingNormalizesChatCacheWriteTokens(t *testing.T) {
	var usage dto.Usage
	payload := []byte(`{"prompt_tokens":1000,"prompt_tokens_details":{"cached_tokens":100,"cache_write_tokens":200}}`)
	require.NoError(t, common.Unmarshal(payload, &usage))

	applyUsagePostProcessing(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
	}, &usage, payload)

	require.Equal(t, 100, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 200, usage.PromptTokensDetails.CachedCreationTokens)
	require.True(t, usage.CacheWriteTokensReported)
}

func TestOaiResponsesHandlerNormalizesCacheWriteTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{"id":"resp_1","output":[],"usage":{"input_tokens":1000,"output_tokens":50,"total_tokens":1050,"input_tokens_details":{"cached_tokens":100,"cache_write_tokens":200}}}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}

	usage, err := OaiResponsesHandler(ctx, nil, resp)

	require.Nil(t, err)
	require.Equal(t, 1000, usage.PromptTokens)
	require.Equal(t, 100, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 200, usage.PromptTokensDetails.CachedCreationTokens)
	require.True(t, usage.CacheWriteTokensReported)
}

func TestOaiResponsesStreamHandlerNormalizesCacheWriteTokens(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":1000,"output_tokens":50,"total_tokens":1050,"input_tokens_details":{"cached_tokens":100,"cache_write_tokens":200}}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
		DisablePing: true,
	}

	usage, err := OaiResponsesStreamHandler(ctx, info, resp)

	require.Nil(t, err)
	require.Equal(t, 1000, usage.PromptTokens)
	require.Equal(t, 100, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 200, usage.PromptTokensDetails.CachedCreationTokens)
	require.True(t, usage.CacheWriteTokensReported)
}

func TestOaiResponsesCompactionHandlerNormalizesCacheWriteTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	body := `{"usage":{"input_tokens":1000,"output_tokens":50,"total_tokens":1050,"cache_creation_input_tokens":200,"input_tokens_details":{"cached_tokens":100}}}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}

	usage, err := OaiResponsesCompactionHandler(ctx, resp)

	require.Nil(t, err)
	require.Equal(t, 100, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 200, usage.PromptTokensDetails.CachedCreationTokens)
	require.True(t, usage.CacheWriteTokensReported)
}
