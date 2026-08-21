package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"

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
