package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type turnstileTestTransport func(*http.Request) (*http.Response, error)

func (f turnstileTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTurnstileEnforcementAndRollback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldEnabled, oldSecret := common.TurnstileCheckEnabled, common.TurnstileSecretKey
	t.Cleanup(func() { common.TurnstileCheckEnabled, common.TurnstileSecretKey = oldEnabled, oldSecret })
	common.TurnstileSecretKey = "test-only-secret"
	if turnstileHTTPClient.Timeout != 5*time.Second {
		t.Fatal("Siteverify needs a bounded 5-second timeout")
	}
	for _, tc := range []struct {
		name    string
		enabled bool
		token   string
		status  int
		body    string
		allow   bool
		calls   int
	}{
		{"disabled without token", false, "", 200, `{"success":true}`, true, 0},
		{"disabled with unusable token", false, "bad", 200, `{"success":false}`, true, 0},
		{"enabled missing token", true, "", 200, `{"success":true}`, false, 0},
		{"valid", true, "fresh", 200, `{"success":true}`, true, 1},
		{"expired or replayed", true, "used", 200, `{"success":false,"error-codes":["timeout-or-duplicate"]}`, false, 1},
		{"upstream HTTP failure", true, "fresh", 503, `{"success":true}`, false, 1},
		{"invalid response", true, "fresh", 200, `<html>upstream error</html>`, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.TurnstileCheckEnabled = tc.enabled
			calls, reached := 0, false
			client := &http.Client{Timeout: time.Second, Transport: turnstileTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Method != http.MethodPost || r.Form.Get("response") != tc.token || r.Form.Get("secret") != common.TurnstileSecretKey {
					t.Fatal("invalid Siteverify request")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			router := gin.New()
			router.POST("/register", turnstileCheckWithClient(client), func(c *gin.Context) { reached = true; c.Status(204) })
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("POST", "/register?turnstile="+tc.token, nil))
			if reached != tc.allow || calls != tc.calls {
				t.Fatalf("business reached=%v calls=%d", reached, calls)
			}
			if !tc.allow {
				var body map[string]any
				if err := common.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body["success"] != false {
					t.Fatal("verification failure must deny the request")
				}
			}
		})
	}
}

func TestTurnstileTimeoutAndRequestCancellation(t *testing.T) {
	old := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() { common.TurnstileCheckEnabled = old })
	for _, cancelRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "client deadline", true: "request cancellation"}[cancelRequest], func(t *testing.T) {
			client := &http.Client{Timeout: 30 * time.Millisecond, Transport: turnstileTestTransport(func(r *http.Request) (*http.Response, error) {
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			router := gin.New()
			router.POST("/login", turnstileCheckWithClient(client), func(c *gin.Context) { t.Error("verification outage must not reach login") })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelRequest {
				cancel()
			}
			req := httptest.NewRequest("POST", "/login?turnstile=valid-format", nil).WithContext(ctx)
			out := httptest.NewRecorder()
			start := time.Now()
			router.ServeHTTP(out, req)
			if time.Since(start) > time.Second {
				t.Fatal("verification did not stop promptly")
			}
			if !strings.Contains(out.Body.String(), `"success":false`) {
				t.Fatal("expected fail-closed response")
			}
			if strings.Contains(out.Body.String(), "context canceled") {
				t.Fatal("raw transport error exposed")
			}
		})
	}
}
