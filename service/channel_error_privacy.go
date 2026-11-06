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

func classifyPublicChannelError(err *types.NewAPIError) (common.PublicChannelErrorContract, bool) {
	if err == nil {
		return common.PublicChannelErrorContract{}, false
	}
	structured := []string{string(err.GetErrorCode()), string(err.GetErrorType())}
	message := err.Error()
	switch relayErr := err.RelayError.(type) {
	case types.OpenAIError:
		structured = append(structured, relayErr.Type, fmt.Sprint(relayErr.Code))
		message = relayErr.Message
	case *types.OpenAIError:
		structured = append(structured, relayErr.Type, fmt.Sprint(relayErr.Code))
		message = relayErr.Message
	case types.ClaudeError:
		structured = append(structured, relayErr.Type)
		message = relayErr.Message
	case *types.ClaudeError:
		structured = append(structured, relayErr.Type)
		message = relayErr.Message
	}
	return common.ClassifyPublicChannelError(err.StatusCode, strings.Join(structured, " "), message)
}

func ShouldPreferMappedChannelError(err *types.NewAPIError) bool {
	contract, ok := classifyPublicChannelError(err)
	return ok && contract.Code != common.GatewayTimeoutType
}

func ShouldStopRetryForPublicChannelError(err *types.NewAPIError) bool {
	contract, ok := classifyPublicChannelError(err)
	return ok && contract.StopRetry
}

func IsPublicChannelBadRequest(err *types.NewAPIError) bool {
	return err != nil && err.StatusCode == http.StatusBadRequest && !IsPublicContentAuditError(err)
}

func channelErrorMessageForUser(err *types.NewAPIError) string {
	if err == nil {
		return ""
	}
	var message string
	switch relayErr := err.RelayError.(type) {
	case types.OpenAIError:
		message = relayErr.Message
	case *types.OpenAIError:
		message = relayErr.Message
	case types.ClaudeError:
		message = relayErr.Message
	case *types.ClaudeError:
		message = relayErr.Message
	default:
		message = err.MaskSensitiveError()
	}
	if len(err.Metadata) > 0 {
		message = strings.TrimSuffix(message, fmt.Sprintf(" (%s)", err.Metadata))
	}
	return common.SanitizeChannelErrorMessageForUser(message)
}

func channelBadRequestForUser(err *types.NewAPIError, requestID string) *types.NewAPIError {
	message := channelErrorMessageForUser(err)
	if message == "" {
		message = common.ChannelBadRequestMessage
	}
	if requestID != "" {
		message = common.MessageWithRequestId(message, requestID)
	}

	switch relayErr := err.RelayError.(type) {
	case types.OpenAIError:
		relayErr.Message = message
		relayErr.Metadata = nil
		return types.WithOpenAIError(relayErr, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	case *types.OpenAIError:
		publicErr := *relayErr
		publicErr.Message = message
		publicErr.Metadata = nil
		return types.WithOpenAIError(publicErr, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	case types.ClaudeError:
		relayErr.Message = message
		return types.WithClaudeError(relayErr, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	case *types.ClaudeError:
		publicErr := *relayErr
		publicErr.Message = message
		return types.WithClaudeError(publicErr, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	default:
		errorCode := err.GetErrorCode()
		if errorCode == "" {
			errorCode = types.ErrorCodeInvalidRequest
		}
		return types.NewOpenAIError(errors.New(message), errorCode, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
}

func channelRateLimitForUser(requestID string) *types.NewAPIError {
	message := common.ChannelRateLimitMessage
	if requestID != "" {
		message = common.MessageWithRequestId(message, requestID)
	}
	return types.NewOpenAIError(
		errors.New(message),
		types.ErrorCodeRateLimitExceeded,
		http.StatusTooManyRequests,
		types.ErrOptionWithSkipRetry(),
	)
}

func ChannelErrorForUser(err *types.NewAPIError, requestID string) *types.NewAPIError {
	if contract, ok := classifyPublicChannelError(err); ok {
		message := contract.Message
		if requestID != "" {
			message = common.MessageWithRequestId(message, requestID)
		}
		options := make([]types.NewAPIErrorOptions, 0, 1)
		if contract.StopRetry {
			options = append(options, types.ErrOptionWithSkipRetry())
		}
		return types.NewOpenAIError(
			errors.New(message),
			types.ErrorCode(contract.Code),
			contract.StatusCode,
			options...,
		)
	}
	if IsPublicChannelBadRequest(err) {
		return channelBadRequestForUser(err, requestID)
	}
	if err != nil && err.StatusCode == http.StatusTooManyRequests {
		return channelRateLimitForUser(requestID)
	}
	return SanitizeChannelErrorForUser(requestID)
}

// ParseUpstreamStreamError recognizes explicit structured error fields without
// classifying arbitrary message text. The original payload is retained for the
// admin-only diagnostic log and is replaced before any client write.
func ParseUpstreamStreamError(data string) *types.NewAPIError {
	var payload struct {
		Type     string          `json:"type"`
		Code     any             `json:"code"`
		Error    json.RawMessage `json:"error"`
		Response *struct {
			Status string          `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if err := common.UnmarshalJsonStr(data, &payload); err != nil {
		return nil
	}
	typ := strings.ToLower(strings.TrimSpace(payload.Type))
	errorField := strings.TrimSpace(string(payload.Error))
	hasError := errorField != "" && errorField != "null"
	switch typ {
	case "error", "upstream_error", "response.error", "response.failed":
		hasError = true
	}
	if strings.HasPrefix(typ, "response.") && payload.Response != nil {
		nestedError := strings.TrimSpace(string(payload.Response.Error))
		hasError = hasError || payload.Response.Status == "failed" || (nestedError != "" && nestedError != "null")
	}
	if !hasError {
		return nil
	}
	// Preserve structured classification (including flat Responses SSE errors)
	// while keeping the full upstream event available only to diagnostics.
	details := struct {
		Type string `json:"type"`
		Code any    `json:"code"`
	}{Code: payload.Code}
	if payload.Response != nil && len(payload.Response.Error) > 0 {
		_ = common.Unmarshal(payload.Response.Error, &details)
	} else if len(payload.Error) > 0 {
		_ = common.Unmarshal(payload.Error, &details)
	}
	if details.Type == "" {
		details.Type = string(types.ErrorTypeUpstreamError)
	}
	if details.Code == nil {
		details.Code = string(types.ErrorCodeBadResponse)
	}
	return types.WithOpenAIError(types.OpenAIError{
		Message: fmt.Sprintf("upstream stream error: %s", data),
		Type:    details.Type,
		Code:    details.Code,
	}, http.StatusInternalServerError)
}
