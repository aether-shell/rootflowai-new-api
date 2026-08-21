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
