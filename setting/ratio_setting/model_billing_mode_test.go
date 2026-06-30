package ratio_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestModelBillingModeRejectsInvalidValue(t *testing.T) {
	require.NoError(t, UpdateModelBillingModeByJSONString(`{}`))
	t.Cleanup(func() {
		require.NoError(t, UpdateModelBillingModeByJSONString(`{}`))
	})

	err := UpdateModelBillingModeByJSONString(`{"video-model":"minute"}`)
	require.Error(t, err)

	_, ok := GetModelBillingMode("video-model")
	require.False(t, ok)
}

func TestIsTaskBillingModelUsesDatabaseModeBeforeEnvFallback(t *testing.T) {
	originalPatches := append([]string(nil), constant.TaskPricePatches...)
	constant.TaskPricePatches = []string{"env-task-model", "db-second-model"}
	require.NoError(t, UpdateModelBillingModeByJSONString(`{"db-task-model":"task","db-second-model":"second"}`))
	t.Cleanup(func() {
		constant.TaskPricePatches = originalPatches
		require.NoError(t, UpdateModelBillingModeByJSONString(`{}`))
	})

	require.True(t, IsTaskBillingModel("db-task-model"))
	require.False(t, IsTaskBillingModel("db-second-model"))
	require.True(t, IsTaskBillingModel("env-task-model"))
	require.False(t, IsTaskBillingModel("unconfigured-model"))
}
