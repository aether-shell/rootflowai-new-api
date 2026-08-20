package dto

import "testing"

func TestNormalizeCacheWriteTokensPreservesCanonicalCreationValue(t *testing.T) {
	usage := &Usage{
		PromptTokensDetails: InputTokenDetails{CachedCreationTokens: 5},
		InputTokensDetails:  &InputTokenDetails{CacheWriteTokens: 6},
	}

	usage.NormalizeCacheWriteTokens()

	if usage.PromptTokensDetails.CachedCreationTokens != 5 {
		t.Fatalf("canonical creation tokens changed: got %d, want 5", usage.PromptTokensDetails.CachedCreationTokens)
	}
	if usage.PromptTokensDetails.CacheWriteTokens != 6 {
		t.Fatalf("cache write tokens not preserved: got %d, want 6", usage.PromptTokensDetails.CacheWriteTokens)
	}
	if got := usage.PromptTokensDetails.CacheCreationTokensTotal(); got != 6 {
		t.Fatalf("billable cache creation tokens: got %d, want 6", got)
	}
}

func TestNormalizeCacheWriteTokensBackfillsLegacyAlias(t *testing.T) {
	usage := &Usage{CacheWriteInputTokens: 200}

	usage.NormalizeCacheWriteTokens()

	if usage.PromptTokensDetails.CachedCreationTokens != 200 {
		t.Fatalf("canonical creation tokens: got %d, want 200", usage.PromptTokensDetails.CachedCreationTokens)
	}
	if usage.PromptTokensDetails.CacheWriteTokens != 200 {
		t.Fatalf("cache write tokens: got %d, want 200", usage.PromptTokensDetails.CacheWriteTokens)
	}
	if !usage.CacheWriteTokensReported {
		t.Fatal("expected cache write tokens to be marked as reported")
	}
}
