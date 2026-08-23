package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/stretchr/testify/require"
)

func TestSanitizeChannelErrorForUserUsesStableContract(t *testing.T) {
	err := SanitizeChannelErrorForUser("rf_test")

	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Equal(t, types.ErrorCodeServiceUnavailable, err.GetErrorCode())
	require.Equal(t, common.ChannelErrorUserMessage+" (request id: rf_test)", err.Error())
	require.NotContains(t, err.Error(), "upstream")
	require.Equal(t, common.ChannelErrorUserMessage, SanitizeChannelErrorForUser("").Error())
}

func TestChannelErrorForUserClassifiesMultilingualContentAudit(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
		typeCode   string
		wantPublic bool
	}{
		{name: "Chinese audit", statusCode: 403, message: "内容审计命中风险规则，请调整输入后重试", wantPublic: true},
		{name: "Chinese safety variant", statusCode: 403, message: "提示词被风控拦截，请修改后重试", wantPublic: true},
		{name: "English content policy", statusCode: 502, message: "Request blocked by content policy", wantPublic: true},
		{name: "English structured safety", statusCode: 400, message: "request rejected", typeCode: "prompt_blocked", wantPublic: true},
		{name: "English structured cyber policy", statusCode: 403, message: "request rejected", typeCode: "session_blocked_by_cyber_policy", wantPublic: true},
		{name: "wrapped cyber policy", statusCode: 500, message: `responses stream error: {"error":{"code":"cyber_policy","message":"This content was flagged for possible cybersecurity risk"}}`, wantPublic: true},
		{name: "localized unknown forbidden", statusCode: 403, message: "forbidden by provider", wantPublic: false},
		{name: "unsupported model", statusCode: 403, message: `The current group does not support the requested model "gpt-test"`, wantPublic: false},
		{name: "session group conflict", statusCode: 403, message: "This session already belongs to another group and cannot switch to the current session-isolated group", wantPublic: false},
		{name: "English balance", statusCode: 403, message: "Insufficient account balance", wantPublic: false},
		{name: "Chinese balance", statusCode: 403, message: "账户余额不足，请充值", wantPublic: false},
		{name: "English structured billing", statusCode: 403, message: "request rejected", typeCode: "billing_error", wantPublic: false},
		{name: "Chinese authentication", statusCode: 403, message: "鉴权失败，请检查密钥", wantPublic: false},
		{name: "unknown server error", statusCode: 500, message: "provider failed", wantPublic: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := types.WithOpenAIError(types.OpenAIError{
				Message: test.message,
				Type:    test.typeCode,
				Code:    test.typeCode,
			}, test.statusCode)
			require.Equal(t, test.wantPublic, IsPublicContentAuditError(err))

			publicErr := ChannelErrorForUser(err, "rf_test")
			if test.wantPublic {
				require.Equal(t, http.StatusForbidden, publicErr.StatusCode)
				require.Equal(t, types.ErrorCodeContentAuditBlocked, publicErr.GetErrorCode())
				require.Equal(t, common.ContentAuditUserMessage+" (request id: rf_test)", publicErr.Error())
				if test.message != common.ContentAuditUserMessage {
					require.NotContains(t, publicErr.Error(), test.message)
				}
			} else {
				require.Equal(t, http.StatusServiceUnavailable, publicErr.StatusCode)
				require.Equal(t, types.ErrorCodeServiceUnavailable, publicErr.GetErrorCode())
				require.Equal(t, common.ChannelErrorUserMessage+" (request id: rf_test)", publicErr.Error())
				require.NotContains(t, publicErr.Error(), test.message)
			}
		})
	}
}

func TestShouldDisableChannelKeepsContentAuditChannelEnabled(t *testing.T) {
	previous := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	defer func() { common.AutomaticDisableChannelEnabled = previous }()

	auditErr := types.WithClaudeError(types.ClaudeError{
		Message: common.ContentAuditUserMessage,
		Type:    "permission_denied",
	}, http.StatusForbidden)
	require.False(t, ShouldDisableChannel(auditErr))
}

func TestParseUpstreamStreamErrorUsesStructuredFieldsOnly(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantError bool
	}{
		{name: "explicit error field", data: `{"error":{"message":"vendor balance exhausted"}}`, wantError: true},
		{name: "explicit error type", data: `{"type":"upstream_error","message":"internal route"}`, wantError: true},
		{name: "message text alone", data: `{"message":"error from upstream"}`, wantError: false},
		{name: "normal chunk", data: `{"type":"response.output_text.delta","delta":"ok"}`, wantError: false},
		{name: "invalid JSON", data: `not-json`, wantError: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ParseUpstreamStreamError(test.data)
			if test.wantError {
				require.NotNil(t, err)
				require.Contains(t, err.Error(), test.data)
				return
			}
			require.Nil(t, err)
		})
	}
}
