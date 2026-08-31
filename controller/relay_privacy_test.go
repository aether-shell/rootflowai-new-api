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

func TestShouldRetryStopsForCodexOfficialClientRestriction(t *testing.T) {
	c := &gin.Context{}
	err := types.WithOpenAIError(types.OpenAIError{
		Message: common.CodexOfficialClientForbiddenMessage,
		Type:    common.CodexOfficialClientForbiddenType,
		Code:    common.CodexOfficialClientForbiddenType,
	}, http.StatusForbidden)

	require.False(t, shouldRetry(c, err, 2))
}

func TestShouldRetryAllowsModelUnavailableAcrossChannels(t *testing.T) {
	previous := operation_setting.AutomaticRetryStatusCodeRanges
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 403, End: 403}}
	t.Cleanup(func() { operation_setting.AutomaticRetryStatusCodeRanges = previous })

	c := &gin.Context{}
	err := types.WithOpenAIError(types.OpenAIError{
		Message: `The current group does not support the requested model "gpt-test"`,
	}, http.StatusForbidden)

	require.True(t, shouldRetry(c, err, 2))
}

func TestShouldRetryStopsForDeterministicMappedErrors(t *testing.T) {
	previous := operation_setting.AutomaticRetryStatusCodeRanges
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 400, End: 499}}
	t.Cleanup(func() { operation_setting.AutomaticRetryStatusCodeRanges = previous })

	tests := []struct {
		name       string
		statusCode int
		message    string
		code       string
	}{
		{
			name:       "session group conflict",
			statusCode: http.StatusForbidden,
			message:    "This session already belongs to another group and cannot switch to the current session-isolated group",
		},
		{
			name:       "context too long",
			statusCode: http.StatusInternalServerError,
			message:    "Your input exceeds the context window of this model.",
			code:       common.ContextLengthExceededType,
		},
		{
			name:       "unprocessable entity",
			statusCode: http.StatusUnprocessableEntity,
			message:    "Unprocessable Entity",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &gin.Context{}
			err := types.WithOpenAIError(types.OpenAIError{
				Message: test.message,
				Code:    test.code,
			}, test.statusCode)
			require.False(t, shouldRetry(c, err, 2))
		})
	}
}

func TestShouldRetryKeepsGlobalTimeoutNoRetryPolicy(t *testing.T) {
	previous := operation_setting.AutomaticRetryStatusCodeRanges
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 504, End: 504}}
	t.Cleanup(func() { operation_setting.AutomaticRetryStatusCodeRanges = previous })

	c := &gin.Context{}
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "Request did not complete within 900 seconds and was aborted by the gateway",
	}, http.StatusGatewayTimeout)
	require.False(t, shouldRetry(c, err, 2))
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

func TestSelectFinalChannelErrorPrefersEarlierMappedActionableError(t *testing.T) {
	modelUnavailable := types.WithOpenAIError(types.OpenAIError{
		Message: `The current group does not support the requested model "gpt-test"`,
	}, http.StatusForbidden)
	serviceUnavailable := types.WithOpenAIError(types.OpenAIError{
		Message: "service temporarily unavailable",
	}, http.StatusServiceUnavailable)

	require.Same(t, modelUnavailable, preferredChannelErrorForUser(serviceUnavailable, modelUnavailable))
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

func TestSelectFinalChannelErrorDoesNotOverrideMappedError(t *testing.T) {
	badRequest := types.WithOpenAIError(types.OpenAIError{
		Message: "invalid parameter",
	}, http.StatusBadRequest)
	mappedErr := types.WithOpenAIError(types.OpenAIError{
		Message: "This session already belongs to another group and cannot switch to the current session-isolated group",
	}, http.StatusForbidden)

	require.Nil(t, preferredChannelErrorForUser(mappedErr, badRequest))
}

func TestSelectFinalChannelErrorPrefersBadRequestOverTimeout(t *testing.T) {
	badRequest := types.WithOpenAIError(types.OpenAIError{
		Message: "invalid parameter",
	}, http.StatusBadRequest)
	timeoutErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Request did not complete within 900 seconds and was aborted by the gateway",
	}, http.StatusGatewayTimeout)

	require.Same(t, badRequest, preferredChannelErrorForUser(timeoutErr, badRequest))
}
