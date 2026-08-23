package common

import (
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
