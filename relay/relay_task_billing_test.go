package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestApplyTaskOtherRatiosTaskModeDoesNotMultiplySeconds(t *testing.T) {
	originalPatches := append([]string(nil), constant.TaskPricePatches...)
	constant.TaskPricePatches = nil
	require.NoError(t, ratio_setting.UpdateModelBillingModeByJSONString(`{"fixed-video-model":"task"}`))
	t.Cleanup(func() {
		constant.TaskPricePatches = originalPatches
		require.NoError(t, ratio_setting.UpdateModelBillingModeByJSONString(`{}`))
	})

	priceData := &types.PriceData{
		Quota:       1000,
		OtherRatios: map[string]float64{"seconds": 4},
	}
	ApplyTaskOtherRatios("fixed-video-model", priceData)

	require.Equal(t, 1000, priceData.Quota)
}

func TestApplyTaskOtherRatiosSecondModeMultipliesSeconds(t *testing.T) {
	originalPatches := append([]string(nil), constant.TaskPricePatches...)
	constant.TaskPricePatches = nil
	require.NoError(t, ratio_setting.UpdateModelBillingModeByJSONString(`{"metered-video-model":"second"}`))
	t.Cleanup(func() {
		constant.TaskPricePatches = originalPatches
		require.NoError(t, ratio_setting.UpdateModelBillingModeByJSONString(`{}`))
	})

	priceData := &types.PriceData{
		Quota:       1000,
		OtherRatios: map[string]float64{"seconds": 6},
	}
	ApplyTaskOtherRatios("metered-video-model", priceData)

	require.Equal(t, 6000, priceData.Quota)
}

func TestApplyTaskOtherRatiosFallsBackToTaskPricePatch(t *testing.T) {
	originalPatches := append([]string(nil), constant.TaskPricePatches...)
	constant.TaskPricePatches = []string{"legacy-fixed-video-model"}
	require.NoError(t, ratio_setting.UpdateModelBillingModeByJSONString(`{}`))
	t.Cleanup(func() {
		constant.TaskPricePatches = originalPatches
		require.NoError(t, ratio_setting.UpdateModelBillingModeByJSONString(`{}`))
	})

	taskPriceData := &types.PriceData{
		Quota:       1000,
		OtherRatios: map[string]float64{"seconds": 8},
	}
	ApplyTaskOtherRatios("legacy-fixed-video-model", taskPriceData)
	require.Equal(t, 1000, taskPriceData.Quota)

	secondPriceData := &types.PriceData{
		Quota:       1000,
		OtherRatios: map[string]float64{"seconds": 8},
	}
	ApplyTaskOtherRatios("unconfigured-video-model", secondPriceData)
	require.Equal(t, 8000, secondPriceData.Quota)
}
