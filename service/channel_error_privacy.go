package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func SanitizeChannelErrorForUser(requestID string) *types.NewAPIError {
	message := common.ChannelErrorUserMessage
	if requestID != "" {
		message = common.MessageWithRequestId(message, requestID)
	}
	return types.NewServiceUnavailableError(message)
}

func channelErrorClassificationText(err *types.NewAPIError) (string, string) {
	structured := []string{string(err.GetErrorCode()), string(err.GetErrorType())}
	messages := []string{err.Error()}
	switch relayErr := err.RelayError.(type) {
	case types.OpenAIError:
		structured = append(structured, relayErr.Type, fmt.Sprint(relayErr.Code))
		messages = append(messages, relayErr.Message)
	case *types.OpenAIError:
		structured = append(structured, relayErr.Type, fmt.Sprint(relayErr.Code))
		messages = append(messages, relayErr.Message)
	case types.ClaudeError:
		structured = append(structured, relayErr.Type)
		messages = append(messages, relayErr.Message)
	case *types.ClaudeError:
		structured = append(structured, relayErr.Type)
		messages = append(messages, relayErr.Message)
	}
	return strings.ToLower(strings.Join(structured, " ")), strings.ToLower(strings.Join(messages, " "))
}

func IsPublicContentAuditError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	structured, message := channelErrorClassificationText(err)
	return common.IsExplicitContentAuditError(structured, message)
}

func ChannelErrorForUser(err *types.NewAPIError, requestID string) *types.NewAPIError {
	if !IsPublicContentAuditError(err) {
		return SanitizeChannelErrorForUser(requestID)
	}
	message := common.ContentAuditUserMessage
	if requestID != "" {
		message = common.MessageWithRequestId(message, requestID)
	}
	return types.NewOpenAIError(
		errors.New(message),
		types.ErrorCodeContentAuditBlocked,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
	)
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
