package controller

import (
	"errors"
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
