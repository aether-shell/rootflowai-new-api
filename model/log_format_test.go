package model

import (
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
			"original_error": "provider-specific policy metadata",
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
