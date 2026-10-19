package middleware

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

var turnstileHTTPClient = &http.Client{Timeout: 5 * time.Second}

func TurnstileCheck() gin.HandlerFunc {
	return turnstileCheckWithClient(turnstileHTTPClient)
}

func turnstileCheckWithClient(client *http.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.TurnstileCheckEnabled {
			response := c.Query("turnstile")
			if response == "" {
				common.ApiErrorT(c, "Turnstile token is empty")
				c.Abort()
				return
			}
			form := url.Values{
				"secret":   {common.TurnstileSecretKey},
				"response": {response},
				"remoteip": {c.ClientIP()},
			}
			req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost,
				"https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
			if err != nil {
				common.ApiErrorT(c, "Turnstile verification failed, please refresh and try again!")
				c.Abort()
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rawRes, err := client.Do(req)
			if err != nil {
				common.SysLog(err.Error())
				common.ApiErrorT(c, "Turnstile verification failed, please refresh and try again!")
				c.Abort()
				return
			}
			defer rawRes.Body.Close()
			if rawRes.StatusCode != http.StatusOK {
				common.ApiErrorT(c, "Turnstile verification failed, please refresh and try again!")
				c.Abort()
				return
			}
			var res turnstileCheckResponse
			err = common.DecodeJson(rawRes.Body, &res)
			if err != nil {
				common.SysLog(err.Error())
				common.ApiErrorT(c, "Turnstile verification failed, please refresh and try again!")
				c.Abort()
				return
			}
			if !res.Success {
				common.ApiErrorT(c, "Turnstile verification failed, please refresh and try again!")
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
