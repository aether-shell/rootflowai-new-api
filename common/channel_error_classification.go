package common

import (
	"net/http"
	"regexp"
	"strings"
)

var (
	channelErrorStatusPrefix = regexp.MustCompile(`(?i)^\s*status_code\s*=\s*\d{3}\s*,?\s*`)
	channelRequestID         = regexp.MustCompile(`(?i)\s*[\(\[]?\s*["']?(?:(?:x|upstream|client)[-_ ]*)?request[-_ ]*id["']?\s*[:=]\s*["']?[a-z0-9][a-z0-9._:-]{5,127}["']?\s*[\)\]]?`)
)

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
	"content_policy", "content_filter", "content_audit", "content_blocked", "moderation",
	"prompt_blocked", "risk_control", "safety", "sensitive_words", "cyber_policy",
	"violation_fee.grok.csam",
}

var contentAuditMessageSignals = []string{
	"content policy", "content moderation", "content filter", "content audit",
	"prompt blocked", "safety system", "safety policy", "risk control",
	"cyber policy", "cyber-security policy", "cybersecurity policy", "cybersecurity risk",
	"内容审计", "内容审核", "内容安全", "风险规则", "风险控制", "风控",
	"敏感词", "提示词被拦截", "提示词违规", "网络安全策略",
	"content violates usage guidelines",
}

type PublicChannelErrorContract struct {
	Message    string
	Type       string
	Code       string
	StatusCode int
	StopRetry  bool
}

var claudeOfficialClientMessages = []string{
	"request blocked: this endpoint only accepts requests from the official claude code cli.",
	"当前分组仅限 claude code 客户端接入。the current group is only accessible to claude code clients.",
}

func containsAnyChannelErrorSignal(text string, signals []string) bool {
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

// IsExplicitContentAuditError classifies only errors with affirmative content
// safety signals. HTTP status alone is not evidence of content moderation.
func IsExplicitContentAuditError(structuredText string, messageText string) bool {
	structuredText = strings.ToLower(structuredText)
	messageText = strings.ToLower(messageText)
	if containsAnyChannelErrorSignal(structuredText, privateChannelErrorCodeSignals) ||
		containsAnyChannelErrorSignal(messageText, privateChannelErrorMessageSignals) {
		return false
	}
	return containsAnyChannelErrorSignal(structuredText, contentAuditCodeSignals) ||
		containsAnyChannelErrorSignal(messageText, contentAuditMessageSignals)
}

// IsCodexOfficialClientForbiddenError matches the one upstream authorization
// error that is safe and actionable for end users. Other 403 responses remain
// private channel diagnostics.
func IsCodexOfficialClientForbiddenError(structuredText string, messageText string) bool {
	structuredText = strings.ToLower(structuredText)
	messageText = channelErrorStatusPrefix.ReplaceAllString(strings.TrimSpace(messageText), "")
	return strings.Contains(structuredText, CodexOfficialClientForbiddenType) &&
		strings.EqualFold(messageText, CodexOfficialClientForbiddenMessage)
}

func ClassifyPublicChannelError(statusCode int, structuredText string, messageText string) (PublicChannelErrorContract, bool) {
	structuredText = strings.ToLower(structuredText)
	messageText = channelErrorStatusPrefix.ReplaceAllString(strings.TrimSpace(messageText), "")
	normalizedMessage := strings.ToLower(messageText)
	contract := func(message string, errorType string, publicStatus int, stopRetry bool) (PublicChannelErrorContract, bool) {
		return PublicChannelErrorContract{
			Message:    message,
			Type:       errorType,
			Code:       errorType,
			StatusCode: publicStatus,
			StopRetry:  stopRetry,
		}, true
	}

	if statusCode == http.StatusForbidden && IsCodexOfficialClientForbiddenError(structuredText, messageText) {
		return contract(CodexOfficialClientForbiddenMessage, CodexOfficialClientForbiddenType, http.StatusForbidden, true)
	}
	if (statusCode == http.StatusForbidden || statusCode == http.StatusBadRequest) &&
		(containsAnyChannelErrorSignal(normalizedMessage, claudeOfficialClientMessages) ||
			strings.HasPrefix(normalizedMessage, "我们检测到您的客户端存在异常，请使用标准 claude code 客户端请求。")) {
		return contract(OfficialClientRequiredMessage, OfficialClientRequiredType, http.StatusForbidden, true)
	}
	if IsExplicitContentAuditError(structuredText, normalizedMessage) {
		return contract(ContentAuditUserMessage, "content_audit_blocked", http.StatusForbidden, true)
	}
	if statusCode == http.StatusForbidden && strings.Contains(normalizedMessage, "the current group does not support the requested model") {
		return contract(ModelNotAvailableMessage, ModelNotAvailableType, http.StatusNotFound, false)
	}
	if statusCode == http.StatusForbidden && strings.Contains(normalizedMessage, "this session already belongs to another group") {
		return contract(SessionGroupConflictMessage, SessionGroupConflictType, http.StatusConflict, true)
	}
	if strings.Contains(structuredText, ContextLengthExceededType) ||
		strings.Contains(normalizedMessage, "does not leave enough room in the model's context window") ||
		strings.Contains(normalizedMessage, "input exceeds the context window") ||
		strings.Contains(normalizedMessage, "maximum prompt length") ||
		strings.Contains(normalizedMessage, "input token count exceeds") {
		return contract(ContextLengthExceededMessage, ContextLengthExceededType, http.StatusBadRequest, true)
	}
	if strings.Contains(normalizedMessage, "channel does not support /v1/") {
		return contract(UnsupportedEndpointMessage, UnsupportedEndpointType, http.StatusBadRequest, true)
	}
	if strings.Contains(structuredText, "insufficient_user_quota") ||
		strings.HasPrefix(normalizedMessage, "预扣费额度失败, 用户剩余额度:") {
		return contract(InsufficientQuotaMessage, InsufficientQuotaType, http.StatusForbidden, true)
	}
	if strings.Contains(normalizedMessage, "prompt_cache_breakpoint is not supported") {
		return contract(UnsupportedParameterMessage, UnsupportedParameterType, http.StatusBadRequest, true)
	}
	if strings.Contains(normalizedMessage, "only supports image generation and cannot process text conversation requests") {
		return contract(UnsupportedModelCapabilityMessage, UnsupportedModelCapabilityType, http.StatusBadRequest, true)
	}
	if (strings.Contains(normalizedMessage, "model '") || strings.Contains(normalizedMessage, "model \"")) &&
		(strings.Contains(normalizedMessage, " is not supported.") || strings.Contains(normalizedMessage, " is not supported by any configured account")) {
		return contract(ModelNotSupportedMessage, ModelNotSupportedType, http.StatusNotFound, false)
	}
	if statusCode == http.StatusUnprocessableEntity {
		return contract(UnprocessableEntityMessage, UnprocessableEntityType, http.StatusUnprocessableEntity, true)
	}
	if statusCode == http.StatusRequestEntityTooLarge {
		return contract(PayloadTooLargeMessage, PayloadTooLargeType, http.StatusRequestEntityTooLarge, true)
	}
	if statusCode == http.StatusRequestTimeout || statusCode == http.StatusGatewayTimeout || statusCode == 524 ||
		strings.Contains(normalizedMessage, "request did not complete within") ||
		strings.Contains(normalizedMessage, "request timeout") {
		return contract(GatewayTimeoutMessage, GatewayTimeoutType, http.StatusGatewayTimeout, false)
	}
	return PublicChannelErrorContract{}, false
}

// SanitizeChannelErrorMessageForUser keeps actionable error semantics while
// removing transport metadata that belongs only in administrator diagnostics.
func SanitizeChannelErrorMessageForUser(message string) string {
	message = strings.TrimSpace(message)
	// OpenAI-compatible errors append response metadata as a parenthesized JSON
	// suffix. Remove it before masking so historical logs cannot expose it.
	if strings.HasSuffix(message, ")") {
		if metadataStart := strings.LastIndex(message, " ("); metadataStart >= 0 {
			rawMetadata := message[metadataStart+2 : len(message)-1]
			var metadata any
			if Unmarshal([]byte(rawMetadata), &metadata) == nil {
				switch metadata.(type) {
				case map[string]any, []any:
					message = message[:metadataStart]
				}
			}
		}
	}
	message = MaskSensitiveInfo(message)
	message = channelErrorStatusPrefix.ReplaceAllString(message, "")
	message = channelRequestID.ReplaceAllString(message, "")
	message = strings.TrimSpace(strings.Trim(message, ",;"))
	if message == "{}" || message == "[]" {
		return ""
	}
	return message
}
