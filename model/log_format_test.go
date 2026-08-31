package model

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 0.004,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}

func TestFormatUserLogsSanitizesHistoricalChannelErrors(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"error_type":   "openai_error",
		"error_code":   "insufficient_balance",
		"status_code":  402,
		"channel_id":   77,
		"channel_name": "vendor-a",
		"channel_type": 1,
		"admin_info": map[string]interface{}{
			"original_error": "balance exhausted",
			"use_channel":    []int{77, 88},
		},
	})
	logs := []*Log{{
		Type:              LogTypeError,
		Content:           "status_code=402, balance exhausted",
		ChannelId:         77,
		ChannelName:       "vendor-a",
		UpstreamRequestId: "upstream-secret",
		Other:             other,
	}}

	formatUserLogs(logs, 0)

	require.Equal(t, common.ChannelErrorUserMessage, logs[0].Content)
	require.Zero(t, logs[0].ChannelId)
	require.Empty(t, logs[0].ChannelName)
	require.Empty(t, logs[0].UpstreamRequestId)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "service_unavailable", parsed["error_type"])
	require.Equal(t, "service_unavailable", parsed["error_code"])
	require.Equal(t, float64(503), parsed["status_code"])
	require.NotContains(t, parsed, "admin_info")
	require.NotContains(t, parsed, "channel_id")
	require.NotContains(t, parsed, "channel_name")
	require.NotContains(t, parsed, "channel_type")
}

func TestFormatUserLogsPreservesPublicContentAuditNotice(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"error_type":  "content_audit_blocked",
		"error_code":  "content_audit_blocked",
		"status_code": 403,
		"admin_info": map[string]interface{}{
			"original_error":      "Request blocked by content policy",
			"original_error_code": "content_policy",
		},
	})
	logs := []*Log{{
		Type:              LogTypeError,
		Content:           common.ContentAuditUserMessage,
		ChannelId:         77,
		ChannelName:       "vendor-a",
		UpstreamRequestId: "upstream-secret",
		Other:             other,
	}}

	formatUserLogs(logs, 0)

	require.Equal(t, common.ContentAuditUserMessage, logs[0].Content)
	require.Zero(t, logs[0].ChannelId)
	require.Empty(t, logs[0].ChannelName)
	require.Empty(t, logs[0].UpstreamRequestId)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "content_audit_blocked", parsed["error_type"])
	require.Equal(t, "content_audit_blocked", parsed["error_code"])
	require.Equal(t, float64(403), parsed["status_code"])
	require.NotContains(t, parsed, "admin_info")
}

func TestFormatUserLogsPreservesCodexOfficialClientRestriction(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"error_type":  "service_unavailable",
		"error_code":  "service_unavailable",
		"status_code": 503,
		"admin_info": map[string]interface{}{
			"original_error":       "status_code=403, " + common.CodexOfficialClientForbiddenMessage,
			"original_error_type":  "openai_error",
			"original_error_code":  common.CodexOfficialClientForbiddenType,
			"original_status_code": 403,
		},
	})
	logs := []*Log{{
		Type:              LogTypeError,
		Content:           common.ChannelErrorUserMessage,
		ChannelId:         77,
		ChannelName:       "vendor-a",
		UpstreamRequestId: "upstream-secret",
		Other:             other,
	}}

	formatUserLogs(logs, 0)

	require.Equal(t, common.CodexOfficialClientForbiddenMessage, logs[0].Content)
	require.Zero(t, logs[0].ChannelId)
	require.Empty(t, logs[0].ChannelName)
	require.Empty(t, logs[0].UpstreamRequestId)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, common.CodexOfficialClientForbiddenType, parsed["error_type"])
	require.Equal(t, common.CodexOfficialClientForbiddenType, parsed["error_code"])
	require.Equal(t, float64(http.StatusForbidden), parsed["status_code"])
	require.NotContains(t, parsed, "admin_info")
}

func TestFormatUserLogsRestoresHistoricalModelUnavailable(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"error_type":  "content_audit_blocked",
		"error_code":  "content_audit_blocked",
		"status_code": 403,
		"admin_info": map[string]interface{}{
			"original_error":      `The current group does not support the requested model "gpt-test"`,
			"original_error_type": "openai_error",
			"original_error_code": "unknown_error",
		},
	})
	logs := []*Log{{
		Type:      LogTypeError,
		Content:   common.ContentAuditUserMessage,
		ChannelId: 77,
		Other:     other,
	}}

	formatUserLogs(logs, 0)

	require.Equal(t, common.ModelNotAvailableMessage, logs[0].Content)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, common.ModelNotAvailableType, parsed["error_type"])
	require.Equal(t, common.ModelNotAvailableType, parsed["error_code"])
	require.Equal(t, float64(http.StatusNotFound), parsed["status_code"])
}

func TestFormatUserLogsRestoresMappedErrors(t *testing.T) {
	tests := []struct {
		name           string
		originalStatus int
		originalType   string
		originalCode   string
		originalError  string
		wantStatus     int
		wantCode       string
		wantMessage    string
	}{
		{
			name:           "timeout",
			originalStatus: http.StatusGatewayTimeout,
			originalError:  "status_code=504, Request did not complete within 900 seconds and was aborted by the gateway",
			wantStatus:     http.StatusGatewayTimeout,
			wantCode:       common.GatewayTimeoutType,
			wantMessage:    common.GatewayTimeoutMessage,
		},
		{
			name:           "context too long",
			originalStatus: http.StatusInternalServerError,
			originalCode:   common.ContextLengthExceededType,
			originalError:  "status_code=500, Your input exceeds the context window of this model",
			wantStatus:     http.StatusBadRequest,
			wantCode:       common.ContextLengthExceededType,
			wantMessage:    common.ContextLengthExceededMessage,
		},
		{
			name:           "user quota",
			originalStatus: http.StatusForbidden,
			originalCode:   "insufficient_user_quota",
			originalError:  "status_code=403, 用户额度不足, 剩余额度: -1",
			wantStatus:     http.StatusForbidden,
			wantCode:       common.InsufficientQuotaType,
			wantMessage:    common.InsufficientQuotaMessage,
		},
		{
			name:           "Claude official client",
			originalStatus: http.StatusForbidden,
			originalError:  "status_code=403, Request blocked: this endpoint only accepts requests from the official Claude Code CLI.",
			wantStatus:     http.StatusForbidden,
			wantCode:       common.OfficialClientRequiredType,
			wantMessage:    common.OfficialClientRequiredMessage,
		},
		{
			name:           "unprocessable entity",
			originalStatus: http.StatusUnprocessableEntity,
			originalError:  "status_code=422, Unprocessable Entity",
			wantStatus:     http.StatusUnprocessableEntity,
			wantCode:       common.UnprocessableEntityType,
			wantMessage:    common.UnprocessableEntityMessage,
		},
		{
			name:           "unsupported endpoint",
			originalStatus: http.StatusInternalServerError,
			originalError:  "status_code=500, channel does not support /v1/alpha/search",
			wantStatus:     http.StatusBadRequest,
			wantCode:       common.UnsupportedEndpointType,
			wantMessage:    common.UnsupportedEndpointMessage,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			other := common.MapToJsonStr(map[string]interface{}{
				"error_type":  "service_unavailable",
				"error_code":  "service_unavailable",
				"status_code": 503,
				"admin_info": map[string]interface{}{
					"original_error":       test.originalError,
					"original_error_type":  test.originalType,
					"original_error_code":  test.originalCode,
					"original_status_code": test.originalStatus,
				},
			})
			logs := []*Log{{Type: LogTypeError, Content: common.ChannelErrorUserMessage, ChannelId: 77, Other: other}}

			formatUserLogs(logs, 0)

			require.Equal(t, test.wantMessage, logs[0].Content)
			parsed, err := common.StrToMap(logs[0].Other)
			require.NoError(t, err)
			require.Equal(t, test.wantCode, parsed["error_type"])
			require.Equal(t, test.wantCode, parsed["error_code"])
			require.Equal(t, float64(test.wantStatus), parsed["status_code"])
			require.NotContains(t, parsed, "admin_info")
		})
	}
}

func TestFormatUserLogsReclassifiesHistoricalCyberPolicyAsAudit(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"error_type":  "service_unavailable",
		"error_code":  "service_unavailable",
		"status_code": 503,
		"channel_id":  77,
		"admin_info": map[string]interface{}{
			"original_error":      `responses stream error: {"error":{"code":"cyber_policy","message":"This content was flagged for possible cybersecurity risk"}}`,
			"original_error_type": "new_api_error",
			"original_error_code": "bad_response",
		},
	})
	logs := []*Log{{
		Type:    LogTypeError,
		Content: common.ChannelErrorUserMessage,
		Other:   other,
	}}

	formatUserLogs(logs, 0)

	require.Equal(t, common.ContentAuditUserMessage, logs[0].Content)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "content_audit_blocked", parsed["error_type"])
	require.Equal(t, "content_audit_blocked", parsed["error_code"])
	require.Equal(t, float64(403), parsed["status_code"])
}

func TestFormatUserLogsRestoresSanitizedBadRequest(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"error_type":  "service_unavailable",
		"error_code":  "service_unavailable",
		"status_code": 503,
		"admin_info": map[string]interface{}{
			"original_error":       `status_code=400, Thinking level MINIMAL is not supported (request id: channel-123456) ({"route":"internal"})`,
			"original_error_type":  "invalid_request_error",
			"original_error_code":  "unsupported_thinking_level",
			"original_status_code": 400,
		},
	})
	logs := []*Log{{
		Type:              LogTypeError,
		Content:           common.ChannelErrorUserMessage,
		ChannelId:         77,
		ChannelName:       "vendor-a",
		UpstreamRequestId: "channel-123456",
		Other:             other,
	}}

	formatUserLogs(logs, 0)

	require.Equal(t, "Thinking level MINIMAL is not supported", logs[0].Content)
	require.Zero(t, logs[0].ChannelId)
	require.Empty(t, logs[0].ChannelName)
	require.Empty(t, logs[0].UpstreamRequestId)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "invalid_request_error", parsed["error_type"])
	require.Equal(t, "unsupported_thinking_level", parsed["error_code"])
	require.Equal(t, float64(400), parsed["status_code"])
	require.NotContains(t, parsed, "admin_info")
}

func TestFormatUserLogsNormalizesRateLimit(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"error_type":  "service_unavailable",
		"error_code":  "service_unavailable",
		"status_code": 503,
		"admin_info": map[string]interface{}{
			"original_error":       "status_code=429, vendor account pool exhausted",
			"original_error_type":  "rate_limit_error",
			"original_error_code":  "vendor_quota_exhausted",
			"original_status_code": 429,
		},
	})
	logs := []*Log{{
		Type:      LogTypeError,
		Content:   common.ChannelErrorUserMessage,
		ChannelId: 77,
		Other:     other,
	}}

	formatUserLogs(logs, 0)

	require.Equal(t, common.ChannelRateLimitMessage, logs[0].Content)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "rate_limit_exceeded", parsed["error_type"])
	require.Equal(t, "rate_limit_exceeded", parsed["error_code"])
	require.Equal(t, float64(429), parsed["status_code"])
	require.NotContains(t, parsed, "admin_info")
}

func TestFormatUserLogsDoesNotReturnMalformedOtherOrChannelFields(t *testing.T) {
	logs := []*Log{{
		Type:              LogTypeError,
		ChannelId:         9,
		ChannelName:       "vendor-b",
		UpstreamRequestId: "request-secret",
		Other:             "not-json",
	}}

	formatUserLogs(logs, 0)

	require.Zero(t, logs[0].ChannelId)
	require.Empty(t, logs[0].ChannelName)
	require.Empty(t, logs[0].UpstreamRequestId)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "service_unavailable", parsed["error_code"])
}
