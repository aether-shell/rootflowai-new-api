package common

import "strings"

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
}

var contentAuditMessageSignals = []string{
	"content policy", "content moderation", "content filter", "content audit",
	"prompt blocked", "safety system", "safety policy", "risk control",
	"cyber policy", "cyber-security policy", "cybersecurity policy", "cybersecurity risk",
	"内容审计", "内容审核", "内容安全", "风险规则", "风险控制", "风控",
	"敏感词", "提示词被拦截", "提示词违规", "网络安全策略",
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
