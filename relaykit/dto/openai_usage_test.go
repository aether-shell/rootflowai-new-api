package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCacheWriteTokensPreservesCanonicalCreationValue(t *testing.T) {
	usage := &Usage{
		PromptTokensDetails: InputTokenDetails{CachedCreationTokens: 5},
		InputTokensDetails:  &InputTokenDetails{CacheWriteTokens: 6},
	}

	usage.NormalizeCacheWriteTokens()

	assert.Equal(t, 5, usage.PromptTokensDetails.CachedCreationTokens)
	assert.Equal(t, 6, usage.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, 6, usage.PromptTokensDetails.CacheCreationTokensTotal())

	legacy := &Usage{
		PromptTokensDetails: InputTokenDetails{CachedCreationTokens: 5},
		InputTokensDetails:  &InputTokenDetails{CachedCreationTokens: 7},
	}
	legacy.NormalizeCacheWriteTokens()
	assert.Equal(t, 7, legacy.PromptTokensDetails.CacheCreationTokensTotal())
	assert.False(t, legacy.CacheWriteTokensReported)
}

func TestNormalizeCacheWriteTokensBackfillsLegacyAlias(t *testing.T) {
	usage := &Usage{CacheWriteInputTokens: 200}

	usage.NormalizeCacheWriteTokens()

	assert.Equal(t, 200, usage.PromptTokensDetails.CachedCreationTokens)
	assert.Equal(t, 200, usage.PromptTokensDetails.CacheWriteTokens)
	assert.True(t, usage.CacheWriteTokensReported)
}

func TestNormalizeCacheWriteTokensCombinesAliasesWithTTLDetails(t *testing.T) {
	usage := &Usage{
		CacheCreationTokens:      7,
		CacheWriteTokens:         8,
		CacheCreationInputTokens: 9,
		CacheWriteInputTokens:    10,
		PromptTokensDetails: InputTokenDetails{
			CachedCreationTokens: 1, CacheCreationTokens: 2, CacheWriteTokens: 3,
			CacheCreationInputTokens: 4,
		},
		InputTokensDetails: &InputTokenDetails{
			CachedCreationTokens: 5, CacheCreationTokens: 6, CacheWriteTokens: 11,
			CacheCreationInputTokens: 12,
			CacheWriteTokensDetails:  &CacheWriteTokenDetails{TTL30mTokens: 3, TTL12hTokens: 9},
		},
	}
	usage.NormalizeCacheWriteTokens()
	usage.NormalizeCacheWriteTokens()

	assert.Equal(t, 12, usage.PromptTokensDetails.CacheCreationTokensTotal())
	assert.Equal(t, 9, usage.PromptTokensDetails.CacheWrite12hTokens())
	assert.True(t, usage.CacheWriteTokensReported)
	require.NotNil(t, usage.PromptTokensDetails.CacheWriteTokensDetails)
	usage.InputTokensDetails.CacheWriteTokensDetails.TTL12hTokens = 1
	assert.Equal(t, 9, usage.PromptTokensDetails.CacheWrite12hTokens())
}
