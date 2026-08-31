package common

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeChannelErrorMessageForUser(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name: "transport fields",
			message: "status_code=400, Unable to process https://vendor.example.com/v1/input " +
				"(X-Request-ID: channel-request-123456)",
			expected: "Unable to process https://***.com/***/***",
		},
		{
			name:     "quoted request id",
			message:  `Invalid request {"request_id":"channel-request-123456"}`,
			expected: "Invalid request {}",
		},
		{
			name:     "response metadata suffix",
			message:  `status_code=400, Thinking level MINIMAL is not supported ({"route":"internal","request_id":"channel-request-123456"})`,
			expected: "Thinking level MINIMAL is not supported",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sanitized := SanitizeChannelErrorMessageForUser(test.message)

			require.Equal(t, test.expected, sanitized)
			require.NotContains(t, sanitized, "channel-request-123456")
		})
	}
}

func TestIsCodexOfficialClientForbiddenError(t *testing.T) {
	tests := []struct {
		name       string
		structured string
		message    string
		expected   bool
	}{
		{
			name:       "exact upstream error",
			structured: "openai_error forbidden_error",
			message:    CodexOfficialClientForbiddenMessage,
			expected:   true,
		},
		{
			name:       "stored status prefix",
			structured: "openai_error forbidden_error",
			message:    "status_code=403, " + CodexOfficialClientForbiddenMessage,
			expected:   true,
		},
		{
			name:       "missing structured type",
			structured: "openai_error unknown_error",
			message:    CodexOfficialClientForbiddenMessage,
			expected:   false,
		},
		{
			name:       "different forbidden message",
			structured: "openai_error forbidden_error",
			message:    "Account is forbidden",
			expected:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, IsCodexOfficialClientForbiddenError(test.structured, test.message))
		})
	}
}

func TestClassifyPublicChannelError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		structured string
		message    string
		want       PublicChannelErrorContract
		wantPublic bool
	}{
		{
			name:       "Claude official client restriction",
			statusCode: http.StatusForbidden,
			message:    "Request blocked: this endpoint only accepts requests from the official Claude Code CLI.",
			want:       PublicChannelErrorContract{Message: OfficialClientRequiredMessage, Type: OfficialClientRequiredType, Code: OfficialClientRequiredType, StatusCode: http.StatusForbidden, StopRetry: true},
			wantPublic: true,
		},
		{
			name:       "Claude anomalous client bad request variant",
			statusCode: http.StatusBadRequest,
			message:    "我们检测到您的客户端存在异常，请使用标准 Claude Code 客户端请求。如您对此事不知情，请首先尝试升级客户端。",
			want:       PublicChannelErrorContract{Message: OfficialClientRequiredMessage, Type: OfficialClientRequiredType, Code: OfficialClientRequiredType, StatusCode: http.StatusForbidden, StopRetry: true},
			wantPublic: true,
		},
		{
			name:       "model unavailable for group",
			statusCode: http.StatusForbidden,
			message:    `The current group does not support the requested model "gpt-test". Available models: gpt-safe`,
			want:       PublicChannelErrorContract{Message: ModelNotAvailableMessage, Type: ModelNotAvailableType, Code: ModelNotAvailableType, StatusCode: http.StatusNotFound},
			wantPublic: true,
		},
		{
			name:       "session group conflict",
			statusCode: http.StatusForbidden,
			message:    "This session already belongs to another group and cannot switch to the current session-isolated group",
			want:       PublicChannelErrorContract{Message: SessionGroupConflictMessage, Type: SessionGroupConflictType, Code: SessionGroupConflictType, StatusCode: http.StatusConflict, StopRetry: true},
			wantPublic: true,
		},
		{
			name:       "context length wrapped as server error",
			statusCode: http.StatusInternalServerError,
			structured: "openai_error context_length_exceeded",
			message:    "The request does not leave enough room in the model's context window for a response.",
			want:       PublicChannelErrorContract{Message: ContextLengthExceededMessage, Type: ContextLengthExceededType, Code: ContextLengthExceededType, StatusCode: http.StatusBadRequest, StopRetry: true},
			wantPublic: true,
		},
		{
			name:       "unsupported endpoint",
			statusCode: http.StatusInternalServerError,
			message:    "channel does not support /v1/alpha/search",
			want:       PublicChannelErrorContract{Message: UnsupportedEndpointMessage, Type: UnsupportedEndpointType, Code: UnsupportedEndpointType, StatusCode: http.StatusBadRequest, StopRetry: true},
			wantPublic: true,
		},
		{
			name:       "user quota only",
			statusCode: http.StatusForbidden,
			structured: "openai_error insufficient_user_quota",
			message:    "用户额度不足, 剩余额度: -1",
			want:       PublicChannelErrorContract{Message: InsufficientQuotaMessage, Type: InsufficientQuotaType, Code: InsufficientQuotaType, StatusCode: http.StatusForbidden, StopRetry: true},
			wantPublic: true,
		},
		{
			name:       "model unsupported by account wording",
			statusCode: http.StatusNotFound,
			message:    `Model "gpt-test" is not supported by any configured account in this group`,
			want:       PublicChannelErrorContract{Message: ModelNotSupportedMessage, Type: ModelNotSupportedType, Code: ModelNotSupportedType, StatusCode: http.StatusNotFound},
			wantPublic: true,
		},
		{
			name:       "unprocessable entity",
			statusCode: http.StatusUnprocessableEntity,
			message:    "Unprocessable Entity",
			want:       PublicChannelErrorContract{Message: UnprocessableEntityMessage, Type: UnprocessableEntityType, Code: UnprocessableEntityType, StatusCode: http.StatusUnprocessableEntity, StopRetry: true},
			wantPublic: true,
		},
		{
			name:       "gateway timeout",
			statusCode: http.StatusGatewayTimeout,
			message:    "Request did not complete within 900 seconds and was aborted by the gateway",
			want:       PublicChannelErrorContract{Message: GatewayTimeoutMessage, Type: GatewayTimeoutType, Code: GatewayTimeoutType, StatusCode: http.StatusGatewayTimeout},
			wantPublic: true,
		},
		{
			name:       "CSAM classifier is normalized",
			statusCode: http.StatusForbidden,
			structured: "openai_error violation_fee.grok.csam",
			message:    "Content violates usage guidelines. Failed check: SAFETY_CHECK_TYPE_CSAM",
			want:       PublicChannelErrorContract{Message: ContentAuditUserMessage, Type: "content_audit_blocked", Code: "content_audit_blocked", StatusCode: http.StatusForbidden, StopRetry: true},
			wantPublic: true,
		},
		{
			name:       "unsupported parameter",
			statusCode: http.StatusInternalServerError,
			message:    "prompt_cache_breakpoint is not supported on this model",
			want:       PublicChannelErrorContract{Message: UnsupportedParameterMessage, Type: UnsupportedParameterType, Code: UnsupportedParameterType, StatusCode: http.StatusBadRequest, StopRetry: true},
			wantPublic: true,
		},
		{
			name:       "wrong model capability",
			statusCode: http.StatusBadGateway,
			message:    "This model only supports image generation and cannot process text conversation requests.",
			want:       PublicChannelErrorContract{Message: UnsupportedModelCapabilityMessage, Type: UnsupportedModelCapabilityType, Code: UnsupportedModelCapabilityType, StatusCode: http.StatusBadRequest, StopRetry: true},
			wantPublic: true,
		},
		{
			name:       "payload too large",
			statusCode: http.StatusRequestEntityTooLarge,
			message:    "openai_error",
			want:       PublicChannelErrorContract{Message: PayloadTooLargeMessage, Type: PayloadTooLargeType, Code: PayloadTooLargeType, StatusCode: http.StatusRequestEntityTooLarge, StopRetry: true},
			wantPublic: true,
		},
		{name: "upstream account balance stays private", statusCode: http.StatusForbidden, structured: "openai_error unknown_error", message: "Insufficient account balance"},
		{name: "bare forbidden stays private", statusCode: http.StatusForbidden, structured: "openai_error bad_response_status_code", message: "bad response status code 403"},
		{name: "upstream auth stays private", statusCode: http.StatusUnauthorized, structured: "openai_error 401", message: "Invalid OAuth token"},
		{name: "bare not found stays private", statusCode: http.StatusNotFound, structured: "openai_error bad_response_status_code", message: "bad response status code 404"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ClassifyPublicChannelError(test.statusCode, test.structured, test.message)
			require.Equal(t, test.wantPublic, ok)
			require.Equal(t, test.want, got)
		})
	}
}
