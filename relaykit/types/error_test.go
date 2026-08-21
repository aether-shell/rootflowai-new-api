package types

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewServiceUnavailableErrorDoesNotRetainRelayError(t *testing.T) {
	err := NewServiceUnavailableError("temporarily unavailable")

	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Equal(t, ErrorCodeServiceUnavailable, err.GetErrorCode())
	require.Nil(t, err.RelayError)
	require.Equal(t, "temporarily unavailable", err.ToOpenAIError().Message)
	require.Equal(t, "service_unavailable", err.ToOpenAIError().Type)
	require.Equal(t, ErrorCodeServiceUnavailable, err.ToOpenAIError().Code)
	require.Equal(t, "temporarily unavailable", err.ToClaudeError().Message)
	require.Equal(t, "service_unavailable", err.ToClaudeError().Type)
}
