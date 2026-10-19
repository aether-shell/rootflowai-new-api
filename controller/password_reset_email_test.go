package controller

import (
	"bufio"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func TestPasswordResetDestinationAndEscaping(t *testing.T) {
	// Register before Setenv so configuration is restored after the environment.
	t.Cleanup(func() { require.NoError(t, common.InitPasswordResetEmail()) })
	for _, invalid := range []string{
		"/forgot-password", "http://rootflowai.com/reset", "https:///reset",
		"https://user:secret@rootflowai.com/reset", "https://rootflowai.com/reset?",
		"https://rootflowai.com/reset?next=https://example.com", "https://rootflowai.com/reset#fragment",
		"https://rootflowai.com/reset#", "https://rootflowai.com/\\example.com",
	} {
		t.Setenv("PASSWORD_RESET_PAGE_URL", invalid)
		require.Error(t, common.InitPasswordResetEmail(), invalid)
	}
	t.Setenv("PASSWORD_RESET_PAGE_URL", "https://rootflowai.com/forgot-password")
	require.NoError(t, common.InitPasswordResetEmail())
	email, token := "alice+tag@example.invalid", `token&"'<value>`
	link, err := common.PasswordResetURL("https://unused.invalid", email, token)
	require.NoError(t, err)
	u, err := url.Parse(link)
	require.NoError(t, err)
	assert.Equal(t, "rootflowai.com", u.Host)
	assert.Equal(t, "/forgot-password", u.Path)
	assert.Equal(t, email, u.Query().Get("email"))
	assert.Equal(t, token, u.Query().Get("token"))
	assert.Len(t, u.Query(), 2)
	markup := `<script>alert("unsafe")</script>`
	body, plain, err := common.RenderPasswordResetEmail(common.PasswordResetEmailData{
		Language: "zh-CN", SystemName: markup, Title: markup, ResetURL: link,
	})
	require.NoError(t, err)
	assert.NotContains(t, body, "<script>")
	assert.Contains(t, body, "&lt;script&gt;")
	assert.Equal(t, []string{link, link}, passwordResetMailLinks(t, body))
	assert.Contains(t, plain, link)

	t.Setenv("PASSWORD_RESET_PAGE_URL", "")
	require.NoError(t, common.InitPasswordResetEmail())
	legacy, err := common.PasswordResetURL("https://legacy.example/base/", email, token)
	require.NoError(t, err)
	u, err = url.Parse(legacy)
	require.NoError(t, err)
	assert.Equal(t, "legacy.example", u.Host)
	assert.Equal(t, "/base/user/reset", u.Path)
	assert.Equal(t, email, u.Query().Get("email"))
}

// Capture SMTP DATA from the real sender. No external SMTP or user account is used.
func passwordResetSMTP(t *testing.T) (string, int, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	messages := make(chan string, 16)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				r := textproto.NewReader(bufio.NewReader(conn))
				_, _ = fmt.Fprint(conn, "220 capture.local ESMTP\r\n")
				for {
					line, err := r.ReadLine()
					if err != nil {
						return
					}
					switch {
					case line == "DATA":
						_, _ = fmt.Fprint(conn, "354 Send message\r\n")
						data, err := r.ReadDotBytes()
						if err != nil {
							return
						}
						messages <- string(data)
						_, _ = fmt.Fprint(conn, "250 Accepted\r\n")
					case line == "QUIT":
						_, _ = fmt.Fprint(conn, "221 Bye\r\n")
						return
					default:
						_, _ = fmt.Fprint(conn, "250 OK\r\n")
					}
				}
			}()
		}
	}()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	n, err := strconv.Atoi(port)
	require.NoError(t, err)
	return host, n, messages
}

func passwordResetMailLinks(t *testing.T, body string) []string {
	t.Helper()
	var links []string
	z := html.NewTokenizer(strings.NewReader(body))
	for {
		switch z.Next() {
		case html.ErrorToken:
			require.ErrorIs(t, z.Err(), io.EOF)
			return links
		case html.StartTagToken:
			token := z.Token()
			if token.Data == "a" {
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						links = append(links, attr.Val)
					}
				}
			}
		}
	}
}

func TestPasswordResetEmailDeliveryAndConfirmation(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	email := "reset+mail@example.invalid"
	require.NoError(t, model.DB.Model(user).Update("email", email).Error)
	t.Cleanup(func() { common.DeleteKey(email, common.PasswordResetPurpose) })
	oldHost, oldPort, oldAccount, oldFrom, oldSecret := common.SMTPServer, common.SMTPPort, common.SMTPAccount, common.SMTPFrom, common.SMTPToken
	oldSSL, oldStartTLS := common.SMTPSSLEnabled, common.SMTPStartTLSEnabled
	oldSystem, oldAddress, oldTurnstile := common.SystemName, system_setting.ServerAddress, common.TurnstileCheckEnabled
	oldValidity := common.VerificationValidMinutes
	t.Cleanup(func() {
		common.SMTPServer, common.SMTPPort, common.SMTPAccount, common.SMTPFrom, common.SMTPToken = oldHost, oldPort, oldAccount, oldFrom, oldSecret
		common.SMTPSSLEnabled, common.SMTPStartTLSEnabled = oldSSL, oldStartTLS
		common.SystemName, system_setting.ServerAddress, common.TurnstileCheckEnabled = oldSystem, oldAddress, oldTurnstile
		common.VerificationValidMinutes = oldValidity
		require.NoError(t, common.InitPasswordResetEmail())
	})
	t.Setenv("PASSWORD_RESET_PAGE_URL", "https://rootflowai.com/forgot-password")
	require.NoError(t, common.InitPasswordResetEmail())
	host, port, messages := passwordResetSMTP(t)
	common.SMTPServer, common.SMTPPort = host, port
	common.SMTPFrom, common.SMTPAccount, common.SMTPToken = "noreply@example.invalid", "", ""
	common.SMTPSSLEnabled, common.SMTPStartTLSEnabled = false, false
	common.SystemName, system_setting.ServerAddress = "RootFlowAI", "https://api.example.invalid"
	common.TurnstileCheckEnabled = false
	r := gin.New()
	r.GET("/api/reset_password", middleware.TurnstileCheck(), SendPasswordResetEmail)
	r.POST("/api/user/reset", ResetPassword)

	request := func(method, path, body, language string) map[string]any {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "attacker.invalid"
		req.Header.Set("X-Forwarded-Host", "attacker.invalid")
		req.Header.Set("Accept-Language", language)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		r.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code)
		var result map[string]any
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		return result
	}
	var previousToken string
	for _, tc := range []struct{ saved, requested, expected string }{
		{"", "", "重置你的密码"}, {"en", "zh-CN", "Reset your password"},
		{"zhTW", "en", "重設你的密碼"}, {"", "en-US", "Reset your password"},
	} {
		setting, err := common.Marshal(dto.UserSetting{Language: tc.saved})
		require.NoError(t, err)
		require.NoError(t, model.DB.Model(user).Update("setting", string(setting)).Error)
		result := request("GET", "/api/reset_password?"+url.Values{"email": {email}}.Encode(), "", tc.requested)
		require.Equal(t, true, result["success"])
		var raw string
		select {
		case raw = <-messages:
		case <-time.After(2 * time.Second):
			t.Fatal("reset email did not reach local SMTP")
		}
		message, err := mail.ReadMessage(strings.NewReader(raw))
		require.NoError(t, err)
		assert.Equal(t, "1.0", message.Header.Get("MIME-Version"))
		subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
		require.NoError(t, err)
		assert.Contains(t, subject, "RootFlowAI")
		kind, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, "multipart/alternative", kind)
		reader := multipart.NewReader(message.Body, params["boundary"])
		parts := map[string]string{}
		for _, expectedType := range []string{"text/plain", "text/html"} {
			part, err := reader.NextPart() // Decodes quoted-printable, as an email client does.
			require.NoError(t, err)
			mediaType, attrs, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
			require.NoError(t, err)
			assert.Equal(t, expectedType, mediaType)
			assert.Equal(t, "UTF-8", attrs["charset"])
			body, err := io.ReadAll(part)
			require.NoError(t, err)
			assert.True(t, utf8.Valid(body))
			assert.Contains(t, string(body), tc.expected)
			parts[mediaType] = string(body)
		}
		_, err = reader.NextPart()
		require.ErrorIs(t, err, io.EOF)
		links := passwordResetMailLinks(t, parts["text/html"])
		require.Len(t, links, 2)
		assert.Equal(t, links[0], links[1])
		assert.Contains(t, parts["text/plain"], links[0])
		u, err := url.Parse(links[0])
		require.NoError(t, err)
		assert.Equal(t, "https", u.Scheme)
		assert.Equal(t, "rootflowai.com", u.Host)
		assert.Equal(t, "/forgot-password", u.Path)
		assert.Equal(t, email, u.Query().Get("email"))
		code := u.Query().Get("token")
		require.NotEmpty(t, code)
		assert.True(t, common.VerifyCodeWithKey(email, code, common.PasswordResetPurpose))
		if previousToken != "" {
			assert.False(t, common.VerifyCodeWithKey(email, previousToken, common.PasswordResetPurpose), "resending supersedes the old link")
		}
		previousToken = code
		if tc.saved == "" && tc.requested == "" {
			if dir := os.Getenv("TEST_PASSWORD_RESET_PREVIEW_DIR"); dir != "" {
				require.NoError(t, os.MkdirAll(dir, 0700))
				for name, content := range map[string]string{"reset.eml": raw, "email.html": parts["text/html"], "email.txt": parts["text/plain"]} {
					require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
				}
			}
		}
	}
	assert.Equal(t, true, request("GET", "/api/reset_password?email=unknown%40example.invalid", "", "")["success"])
	assert.Empty(t, messages, "unknown account must not send email")
	common.TurnstileCheckEnabled = true
	assert.Equal(t, false, request("GET", "/api/reset_password?"+url.Values{"email": {email}}.Encode(), "", "")["success"])
	assert.Empty(t, messages, "missing Turnstile must not send email")
	assert.Equal(t, false, request("POST", "/api/user/reset", "{}", "")["success"])
	encoded, err := common.Marshal(map[string]string{"email": email, "token": previousToken})
	require.NoError(t, err)
	common.VerificationValidMinutes = 0
	assert.Equal(t, false, request("POST", "/api/user/reset", string(encoded), "")["success"], "expired links fail")
	common.VerificationValidMinutes = oldValidity
	result := request("POST", "/api/user/reset", string(encoded), "")
	require.Equal(t, true, result["success"])
	password, ok := result["data"].(string)
	require.True(t, ok)
	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.True(t, common.ValidatePasswordAndHash(password, stored.Password))
	assert.Greater(t, stored.AuthVersion, identity.UserAuthVersion)
	assert.Equal(t, false, request("POST", "/api/user/reset", string(encoded), "")["success"], "successful reset consumes the code")
}
