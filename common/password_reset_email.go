package common

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"os"
	"strings"
	texttemplate "text/template"
)

//go:embed email_templates/password_reset.html email_templates/password_reset.txt
var passwordResetTemplates embed.FS

var passwordResetHTML = htmltemplate.Must(htmltemplate.ParseFS(passwordResetTemplates, "email_templates/password_reset.html"))
var passwordResetText = texttemplate.Must(texttemplate.ParseFS(passwordResetTemplates, "email_templates/password_reset.txt"))

// Set once at startup; never derive this destination from request headers.
var passwordResetPageURL *url.URL

func InitPasswordResetEmail() error {
	value := strings.TrimSpace(os.Getenv("PASSWORD_RESET_PAGE_URL"))
	if value == "" {
		passwordResetPageURL = nil
		return nil
	}
	page, err := url.Parse(value)
	if err != nil || page.Scheme != "https" || page.Hostname() == "" || page.User != nil || strings.ContainsAny(value, "?#\\") {
		return fmt.Errorf("PASSWORD_RESET_PAGE_URL must be an absolute HTTPS URL without credentials, query or fragment")
	}
	passwordResetPageURL = page
	return nil
}

// PasswordResetURL preserves the legacy destination when no dedicated page is
// configured. Query encoding must preserve addresses such as name+tag@example.com.
func PasswordResetURL(serverAddress, email, token string) (string, error) {
	page := passwordResetPageURL
	if page == nil {
		var err error
		page, err = url.Parse(serverAddress)
		if err != nil || (page.Scheme != "https" && page.Scheme != "http") || page.Hostname() == "" || page.User != nil || page.Fragment != "" || strings.Contains(serverAddress, "\\") {
			return "", fmt.Errorf("invalid password reset server address")
		}
		page.Path = strings.TrimRight(page.Path, "/") + "/user/reset"
		page.RawPath = ""
	}
	result := *page
	result.RawQuery = url.Values{"email": {email}, "token": {token}}.Encode()
	result.ForceQuery = false
	return result.String(), nil
}

type PasswordResetEmailData struct {
	Language   string
	SystemName string
	ResetURL   string
	Title      string
	Intro      string
	Action     string
	Button     string
	Validity   string
	Fallback   string
	Ignore     string
	Footer     string
}

func RenderPasswordResetEmail(data PasswordResetEmailData) (htmlBody, textBody string, err error) {
	var html, plain bytes.Buffer
	if err = passwordResetHTML.Execute(&html, data); err != nil {
		return "", "", err
	}
	if err = passwordResetText.Execute(&plain, data); err != nil {
		return "", "", err
	}
	return html.String(), plain.String(), nil
}
