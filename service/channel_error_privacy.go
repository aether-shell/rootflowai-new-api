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

var privateChannelErrorCodeSignals = []string{
	"insufficient_balance", "account_balance", "billing", "payment_required",
	"insufficient_quota", "quota_exceeded", "api_key", "invalid_key",
	"invalid_token", "authentication", "subscription",
}

var privateChannelErrorMessageSignals = []string{
	"insufficient account balance", "insufficient balance", "insufficient funds",
	"account balance", "billing error", "payment required", "credit balance",
	"quota exceeded", "insufficient quota", "invalid api key", "authentication failed",
	"余额", "计费", "账单", "欠费", "充值", "额度不足", "配额",
	"密钥无效", "无效密钥", "鉴权失败", "认证失败", "订阅失效",
}

var contentAuditCodeSignals = []string{
	"content_policy", "content_filter", "content_audit", "moderation",
	"prompt_blocked", "risk_control", "safety", "sensitive_words",
}

var contentAuditMessageSignals = []string{
	"content policy", "content moderation", "content filter", "content audit",
	"prompt blocked", "safety system", "safety policy", "risk control",
	"内容审计", "内容审核", "内容安全", "风险规则", "风险控制", "风控",
	"敏感词", "提示词被拦截", "提示词违规",
}

func containsAnySignal(text string, signals []string) bool {
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
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
	if containsAnySignal(structured, privateChannelErrorCodeSignals) ||
		containsAnySignal(message, privateChannelErrorMessageSignals) {
		return false
	}
	if containsAnySignal(structured, contentAuditCodeSignals) ||
		containsAnySignal(message, contentAuditMessageSignals) {
		return true
	}
	// A provider can localize or rewrite a bare 403. Treat it as a public-safe
	// rejection, but return only our fixed notice and never the upstream text.
	return err.StatusCode == http.StatusForbidden
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
