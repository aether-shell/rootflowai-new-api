package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryStopsAfterStreamResponseStarted(t *testing.T) {
	c := &gin.Context{}
	c.Set(common.StreamResponseStartedKey, true)
	err := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponse, 503)

	require.False(t, shouldRetry(c, err, 2))
}

func TestShouldRetryStopsForContentAudit(t *testing.T) {
	c := &gin.Context{}
	auditErr := types.WithOpenAIError(types.OpenAIError{
		Message: common.ContentAuditUserMessage,
	}, http.StatusForbidden)
	require.False(t, shouldRetry(c, auditErr, 2))
}

func TestShouldRetryAllowsUnclassifiedForbidden(t *testing.T) {
	previous := operation_setting.AutomaticRetryStatusCodeRanges
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 403, End: 403}}
	t.Cleanup(func() { operation_setting.AutomaticRetryStatusCodeRanges = previous })

	c := &gin.Context{}
	err := types.WithOpenAIError(types.OpenAIError{
		Message: `The current group does not support the requested model "gpt-test"`,
	}, http.StatusForbidden)

	require.True(t, shouldRetry(c, err, 2))
}

func TestShouldRetryKeepsConfiguredBadRequestRetry(t *testing.T) {
	previous := operation_setting.AutomaticRetryStatusCodeRanges
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 400, End: 400}}
	t.Cleanup(func() { operation_setting.AutomaticRetryStatusCodeRanges = previous })

	c := &gin.Context{}
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "Thinking level MINIMAL is not supported",
	}, http.StatusBadRequest)

	require.True(t, shouldRetry(c, err, 2))
}

func TestSelectFinalChannelErrorPrefersActionableBadRequest(t *testing.T) {
	badRequest := types.WithOpenAIError(types.OpenAIError{
		Message: "Thinking level MINIMAL is not supported",
		Code:    "unsupported_thinking_level",
	}, http.StatusBadRequest)
	serviceUnavailable := types.WithOpenAIError(types.OpenAIError{
		Message: "service temporarily unavailable",
	}, http.StatusServiceUnavailable)

	require.Same(t, badRequest, preferredChannelErrorForUser(serviceUnavailable, badRequest))
}

func TestSelectFinalChannelErrorDoesNotOverrideContentAudit(t *testing.T) {
	badRequest := types.WithOpenAIError(types.OpenAIError{
		Message: "invalid parameter",
	}, http.StatusBadRequest)
	auditErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Request blocked by content policy",
		Code:    "content_policy",
	}, http.StatusForbidden)

	require.Nil(t, preferredChannelErrorForUser(auditErr, badRequest))
}
