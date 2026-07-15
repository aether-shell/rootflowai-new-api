package smoketest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type apiResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type sessionState struct {
	Cookies []*http.Cookie
	UserID  int
}

func TestRC10StreamBillingSmoke(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "smoke.db")
	initSmokeEnv(t, dbPath)
	t.Cleanup(func() {
		_ = model.CloseDB()
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/chat/completions", r.URL.Path)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		defer r.Body.Close()

		var req struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.Unmarshal(body, &req))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		switch req.Model {
		case "mock-normal":
			_, err = io.WriteString(w, "data: {\"id\":\"chatcmpl-normal\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"mock-normal\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
			require.NoError(t, err)
			flusher.Flush()
			_, err = io.WriteString(w, "data: {\"id\":\"chatcmpl-normal\",\"object\":\"chat.completion.chunk\",\"created\":1710000001,\"model\":\"mock-normal\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"total_tokens\":18}}\n\n")
			require.NoError(t, err)
			flusher.Flush()
			_, err = io.WriteString(w, "data: [DONE]\n\n")
			require.NoError(t, err)
			flusher.Flush()
		case "mock-eof":
			_, err = io.WriteString(w, "data: {\"id\":\"chatcmpl-eof\",\"object\":\"chat.completion.chunk\",\"created\":1710000002,\"model\":\"mock-eof\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":0,\"total_tokens\":11}}\n\n")
			require.NoError(t, err)
			flusher.Flush()
		default:
			t.Fatalf("unexpected model: %s", req.Model)
		}
	}))
	defer upstream.Close()

	engine := newSmokeRouter()

	setupResp, _ := doJSONRequest(t, engine, http.MethodPost, "/api/setup", sessionState{}, map[string]any{
		"username":           "rootadmin",
		"password":           "Password123",
		"confirmPassword":    "Password123",
		"SelfUseModeEnabled": true,
		"DemoSiteEnabled":    false,
	})
	require.True(t, setupResp.Success, setupResp.Message)

	loginResp, loginRecorder := doJSONRequest(t, engine, http.MethodPost, "/api/user/login", sessionState{}, map[string]any{
		"username": "rootadmin",
		"password": "Password123",
	})
	require.True(t, loginResp.Success, loginResp.Message)
	var loginData struct {
		ID int `json:"id"`
	}
	require.NoError(t, json.Unmarshal(loginResp.Data, &loginData))
	session := sessionState{
		Cookies: loginRecorder.Result().Cookies(),
		UserID:  loginData.ID,
	}
	require.NotEmpty(t, session.Cookies)

	selfResp, _ := doRequest(t, engine, http.MethodGet, "/api/user/self", session, nil, "")
	require.True(t, selfResp.Success, selfResp.Message)

	baseURL := upstream.URL
	channelResp, _ := doJSONRequest(t, engine, http.MethodPost, "/api/channel/", session, map[string]any{
		"mode": "single",
		"channel": map[string]any{
			"name":     "smoke-openai",
			"type":     constant.ChannelTypeOpenAI,
			"key":      "dummy-key",
			"base_url": baseURL,
			"models":   "mock-normal,mock-eof",
			"group":    "default",
			"status":   common.ChannelStatusEnabled,
		},
	})
	require.True(t, channelResp.Success, channelResp.Message)

	addTokenResp, _ := doJSONRequest(t, engine, http.MethodPost, "/api/token/", session, map[string]any{
		"name":            "smoke-token",
		"unlimited_quota": true,
		"group":           "default",
	})
	require.True(t, addTokenResp.Success, addTokenResp.Message)

	tokenListResp, _ := doRequest(t, engine, http.MethodGet, "/api/token/?page_size=20", session, nil, "")
	require.True(t, tokenListResp.Success, tokenListResp.Message)
	var tokenPage struct {
		Items []struct {
			ID int `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(tokenListResp.Data, &tokenPage))
	require.NotEmpty(t, tokenPage.Items)

	tokenKeyResp, _ := doRequest(t, engine, http.MethodPost, "/api/token/"+itoa(tokenPage.Items[0].ID)+"/key", session, nil, "")
	require.True(t, tokenKeyResp.Success, tokenKeyResp.Message)
	var tokenKeyData struct {
		Key string `json:"key"`
	}
	require.NoError(t, json.Unmarshal(tokenKeyResp.Data, &tokenKeyData))
	require.NotEmpty(t, tokenKeyData.Key)

	normalBody := runRelayRequest(t, engine, tokenKeyData.Key, "mock-normal")
	require.Contains(t, normalBody, "[DONE]")

	eofBody := runRelayRequest(t, engine, tokenKeyData.Key, "mock-eof")
	require.Contains(t, eofBody, "mock-eof")

	logResp, _ := doRequest(t, engine, http.MethodGet, "/api/log/?type=2&page_size=20", session, nil, "")
	require.True(t, logResp.Success, logResp.Message)

	var logPage struct {
		Items []struct {
			ModelName        string `json:"model_name"`
			Quota            int    `json:"quota"`
			PromptTokens     int    `json:"prompt_tokens"`
			CompletionTokens int    `json:"completion_tokens"`
			Other            string `json:"other"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(logResp.Data, &logPage))

	var normalLogFound bool
	var eofLogFound bool
	for _, item := range logPage.Items {
		switch item.ModelName {
		case "mock-normal":
			normalLogFound = true
			require.Greater(t, item.Quota, 0)
			require.Equal(t, 11, item.PromptTokens)
			require.Equal(t, 7, item.CompletionTokens)
			require.NotContains(t, item.Other, "quota_suppressed")
		case "mock-eof":
			eofLogFound = true
			require.Equal(t, 0, item.Quota)
			require.Equal(t, 11, item.PromptTokens)
			require.Equal(t, 0, item.CompletionTokens)

			var other map[string]any
			require.NoError(t, json.Unmarshal([]byte(item.Other), &other))
			require.Equal(t, true, other["quota_suppressed"])
			require.Equal(t, "zero_completion_stream_eof", other["suppress_reason"])
		}
	}

	require.True(t, normalLogFound, "missing mock-normal consume log")
	require.True(t, eofLogFound, "missing mock-eof consume log")
	t.Logf("smoke ok: normal stream billed, abnormal EOF stream suppressed")
}

func initSmokeEnv(t *testing.T, dbPath string) {
	t.Helper()

	logDir := filepath.Join(filepath.Dir(dbPath), "logs")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	*common.LogDir = logDir

	common.SessionSecret = "smoke-session-secret"
	common.CryptoSecret = "smoke-crypto-secret"
	common.SQLitePath = dbPath
	common.DebugEnabled = false
	common.MemoryCacheEnabled = false
	common.LogConsumeEnabled = true
	common.GlobalApiRateLimitEnable = false
	common.GlobalWebRateLimitEnable = false
	common.CriticalRateLimitEnable = false
	common.SearchRateLimitEnable = false
	common.IsMasterNode = true
	common.NodeName = "smoke"
	common.RelayTimeout = 15
	common.RelayMaxIdleConns = 20
	common.RelayMaxIdleConnsPerHost = 20
	common.RedisEnabled = false
	common.DataExportEnabled = false
	common.TurnstileCheckEnabled = false
	common.PasswordLoginEnabled = true
	common.PasswordRegisterEnabled = true
	common.RegisterEnabled = true
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	constant.StreamingTimeout = 300
	constant.StreamScannerMaxBufferMB = 128

	logger.SetupLogger()
	ratio_setting.InitRatioSettings()
	service.InitHttpClient()
	service.InitTokenEncoders()
	require.NoError(t, model.InitDB())
	model.CheckSetup()
	model.InitOptionMap()
	model.GetPricing()
	require.NoError(t, model.InitLogDB())
}

func newSmokeRouter() *gin.Engine {
	engine := gin.New()
	engine.Use(middleware.RequestId())
	engine.Use(middleware.Version())

	store := cookie.NewStore([]byte(common.SessionSecret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   2592000,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteStrictMode,
	})
	engine.Use(sessions.Sessions("session", store))

	router.SetApiRouter(engine)
	router.SetRelayRouter(engine)
	return engine
}

func doJSONRequest(t *testing.T, engine *gin.Engine, method string, target string, session sessionState, body any) (apiResponse, *httptest.ResponseRecorder) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(payload)
	}
	return doRequest(t, engine, method, target, session, reader, "application/json")
}

func doRequest(t *testing.T, engine *gin.Engine, method string, target string, session sessionState, body io.Reader, contentType string) (apiResponse, *httptest.ResponseRecorder) {
	t.Helper()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if session.UserID > 0 {
		req.Header.Set("New-Api-User", itoa(session.UserID))
	}
	for _, cookie := range session.Cookies {
		req.AddCookie(cookie)
	}

	engine.ServeHTTP(recorder, req)

	var resp apiResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	return resp, recorder
}

func runRelayRequest(t *testing.T, engine *gin.Engine, tokenKey string, modelName string) string {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"model":  modelName,
		"stream": true,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "hello",
			},
		},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenKey)
	engine.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, body, "\"error\"")
	require.True(t, strings.Contains(recorder.Header().Get("Content-Type"), "text/event-stream"))
	return body
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
