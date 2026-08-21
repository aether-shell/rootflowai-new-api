package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func SanitizeChannelErrorForUser(requestID string) *types.NewAPIError {
	message := common.MessageWithRequestId(common.ChannelErrorUserMessage, requestID)
	return types.NewServiceUnavailableError(message)
}

// ParseUpstreamStreamError recognizes explicit structured error fields without
// classifying arbitrary message text. The original payload is retained for the
// admin-only diagnostic log and is replaced before any client write.
func ParseUpstreamStreamError(data string) *types.NewAPIError {
	var payload struct {
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if err := common.UnmarshalJsonStr(data, &payload); err != nil {
		return nil
	}
	typ := strings.ToLower(strings.TrimSpace(payload.Type))
	if len(payload.Error) == 0 && typ != "error" && typ != "upstream_error" {
		return nil
	}
	return types.NewOpenAIError(fmt.Errorf("upstream stream error: %s", data), types.ErrorCodeBadResponse, http.StatusInternalServerError)
}
