package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultGPT56CacheRatios(t *testing.T) {
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		require.Equal(t, 0.1, defaultCacheRatio[model])
		require.Equal(t, 1.25, defaultCreateCacheRatio[model])
	}
}
