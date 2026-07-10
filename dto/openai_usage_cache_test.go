package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUsageNormalizeCacheWriteTokens(t *testing.T) {
	tests := []struct {
		name       string
		payload    string
		wantTokens int
		reported   bool
	}{
		{
			name:       "chat cache write tokens",
			payload:    `{"prompt_tokens_details":{"cache_write_tokens":200}}`,
			wantTokens: 200,
			reported:   true,
		},
		{
			name:       "responses cache creation tokens",
			payload:    `{"input_tokens_details":{"cache_creation_tokens":190}}`,
			wantTokens: 190,
			reported:   true,
		},
		{
			name:       "top level cache creation input tokens",
			payload:    `{"cache_creation_input_tokens":180}`,
			wantTokens: 180,
			reported:   true,
		},
		{
			name:       "top level cache write input tokens",
			payload:    `{"cache_write_input_tokens":170}`,
			wantTokens: 170,
			reported:   true,
		},
		{
			name:       "legacy cached creation tokens",
			payload:    `{"prompt_tokens_details":{"cached_creation_tokens":160}}`,
			wantTokens: 160,
			reported:   false,
		},
		{
			name:       "aliases are alternatives",
			payload:    `{"cache_creation_input_tokens":180,"input_tokens_details":{"cache_creation_tokens":190,"cache_write_tokens":200}}`,
			wantTokens: 200,
			reported:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var usage Usage
			require.NoError(t, common.Unmarshal([]byte(test.payload), &usage))

			usage.NormalizeCacheWriteTokens()

			require.Equal(t, test.wantTokens, usage.PromptTokensDetails.CachedCreationTokens)
			require.Equal(t, test.reported, usage.CacheWriteTokensReported)
		})
	}
}
