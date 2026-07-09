package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withOptionMap(t *testing.T, values map[string]string) {
	t.Helper()

	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = values
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		common.OptionMap = previous
	})
}

func TestGetOptionsReflectsBypassedPaymentCompliance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withOptionMap(t, map[string]string{
		"payment_setting.compliance_confirmed":     "false",
		"payment_setting.compliance_terms_version": "legacy",
		"payment_setting.compliance_confirmed_at":  "0",
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	GetOptions(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Success bool            `json:"success"`
		Data    []*model.Option `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)

	values := make(map[string]string, len(payload.Data))
	for _, option := range payload.Data {
		values[option.Key] = option.Value
	}

	assert.Equal(t, "true", values["payment_setting.compliance_confirmed"])
	assert.Equal(t, operation_setting.CurrentComplianceTermsVersion, values["payment_setting.compliance_terms_version"])
	assert.Equal(t, "0", values["payment_setting.compliance_confirmed_at"])
}
