package middleware

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const RouteTagKey = "route_tag"

func RouteTag(tag string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(RouteTagKey, tag)
		c.Next()
	}
}

func SetUpLogger(server *gin.Engine) {
	server.Use(redactTaskArtifactAccessQuery())
	server.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Output: common.LogWriter,
		Formatter: func(param gin.LogFormatterParams) string {
			var requestID string
			if param.Keys != nil {
				requestID, _ = param.Keys[common.RequestIdKey].(string)
			}
			tag, _ := param.Keys[RouteTagKey].(string)
			if tag == "" {
				tag = "web"
			}
			path := param.Path
			// Authentication endpoints carry one-time tokens and email addresses.
			// Redact only the log value; handlers still need the original query.
			cleanPath := param.Request.URL.Path
			authQuery := cleanPath == "/api/user/login" || cleanPath == "/api/user/register" ||
				cleanPath == "/api/user/checkin" || cleanPath == "/api/verification" ||
				cleanPath == "/api/reset_password" || cleanPath == "/api/user/reset"
			if authQuery || strings.HasPrefix(cleanPath, "/api/oauth/") || strings.HasPrefix(cleanPath, "/oauth/") {
				path = cleanPath
			} else if param.Request.URL.RawQuery != "" {
				query := param.Request.URL.Query().Encode()
				path = param.Request.URL.EscapedPath() + relaycommon.SanitizeURLForLog("?"+query)
			} else {
				// param.Path retains credentials removed by handlers, such as ?key=.
				path = cleanPath
			}
			return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %7s %s\n",
				param.TimeStamp.Format("2006/01/02 - 15:04:05"),
				tag,
				requestID,
				param.StatusCode,
				param.Latency,
				param.ClientIP,
				param.Method,
				path,
			)
		},
	}))
}
