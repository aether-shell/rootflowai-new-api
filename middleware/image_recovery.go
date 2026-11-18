package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const imageRecoveryHeader = "X-RootFlow-Image-Request"

var imageRecoveryID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

func ImageRecoveryCapabilities(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	// A storage outage must not silently downgrade an opted-in deployment to an
	// unprotected paid POST. Admission below rejects it before generation.
	enabled := os.Getenv("IMAGE_RECOVERY_ENABLED") == "true"
	c.JSON(http.StatusOK, gin.H{"version": 1, "enabled": enabled, "timeout_seconds": int(service.ImageRecoveryTimeout.Seconds())})
}

func ImageRecoveryResult(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := c.Param("id")
	if !imageRecoveryID.MatchString(id) || c.GetInt("id") <= 0 || c.GetInt("token_id") <= 0 {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"state": "unavailable"})
		return
	}
	if !common.RedisEnabled || common.RDB == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"state": "unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	if c.Request.Method == http.MethodDelete {
		if err := service.AcknowledgeImageRecovery(ctx, c.GetInt("id"), c.GetInt("token_id"), id); err != nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
		} else {
			c.Status(http.StatusNoContent)
		}
		return
	}
	record, err := service.GetImageRecovery(ctx, c.GetInt("id"), c.GetInt("token_id"), id)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"state": "unavailable"})
	} else if record == nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"state": "unavailable"})
	} else {
		c.JSON(http.StatusOK, record)
	}
}

// RecoverImageResponse runs after TokenAuth and before distribution/billing.
// It keeps the existing handler in this goroutine; no Gin context outlives it.
func RecoverImageResponse() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(imageRecoveryHeader)
		if id == "" {
			c.Next()
			return
		}
		c.Header("Cache-Control", "no-store")
		path := c.Request.URL.Path
		allowed := path == "/v1/images/generations" || path == "/v1/images/edits" || path == "/v1/chat/completions" || (strings.HasPrefix(path, "/v1beta/models/") && strings.HasSuffix(path, ":generateContent"))
		if c.Request.Method != http.MethodPost || !allowed || !imageRecoveryID.MatchString(id) || !strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_recovery_request", "message": "Image recovery requires a non-streaming JSON request and a UUID v4"}})
			return
		}
		userID, tokenID := c.GetInt("id"), c.GetInt("token_id")
		if userID <= 0 || tokenID <= 0 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if os.Getenv("IMAGE_RECOVERY_ENABLED") != "true" || !common.RedisEnabled || common.RDB == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "image_recovery_unavailable", "message": "Image recovery is unavailable; generation was not submitted"}})
			return
		}
		var request struct {
			Stream bool `json:"stream"`
		}
		if err := common.UnmarshalBodyReusable(c, &request); err != nil || request.Stream {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_recovery_request", "message": "Image recovery requires non-streaming JSON"}})
			return
		}
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		hash := sha256.New()
		fmt.Fprintf(hash, "%s\n%s\n", path, c.GetHeader("Content-Type"))
		if _, err = io.Copy(hash, storage); err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if err = common.RestoreRequestBody(c); err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		claimed, err := service.ClaimImageRecovery(ctx, userID, tokenID, id, fmt.Sprintf("%x", hash.Sum(nil)), c.GetString(common.RequestIdKey))
		cancel()
		if err != nil || claimed == -2 {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "image_recovery_unavailable", "message": "Image recovery capacity is unavailable; generation was not submitted"}})
			return
		}
		if claimed == -1 {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": gin.H{"code": "image_request_conflict", "message": "Request ID already belongs to different content"}})
			return
		}
		if claimed == 0 {
			// Clients retrieve the original response through the read-only endpoint.
			c.AbortWithStatusJSON(http.StatusAccepted, gin.H{"state": "pending"})
			return
		}
		// Do not send a gateway-only identifier to a provider or task plugin.
		c.Request.Header.Del(imageRecoveryHeader)
		c.Set("image_generation_request", true)
		runCtx, stop := context.WithTimeout(context.WithoutCancel(c.Request.Context()), service.ImageRecoveryTimeout)
		defer stop()
		c.Request = c.Request.WithContext(runCtx)
		writer := &imageRecoveryWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		finished := false
		defer func() {
			record := service.ImageRecoveryRecord{State: "unavailable", RequestID: c.GetString(common.RequestIdKey)}
			if finished && !writer.overflow && writer.body.Len() > 0 && !strings.Contains(writer.Header().Get("Content-Type"), "text/event-stream") {
				record.State, record.Status, record.Body, record.ContentType = "completed", writer.Status(), writer.body.String(), writer.Header().Get("Content-Type")
			}
			persistCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := service.CompleteImageRecovery(persistCtx, userID, tokenID, id, record); err != nil {
				common.SysError("image recovery result storage failed")
			}
		}()
		c.Next()
		finished = true
	}
}

type imageRecoveryWriter struct {
	gin.ResponseWriter
	body     bytes.Buffer
	overflow bool
}

func (w *imageRecoveryWriter) Write(data []byte) (int, error) {
	if !w.overflow {
		if w.body.Len()+len(data) <= service.ImageRecoveryMaxBytes {
			_, _ = w.body.Write(data)
		} else {
			w.body.Reset()
			w.overflow = true
		}
	}
	// A disconnected browser must not turn a successfully generated result into
	// a relay failure/refund or cause the handler to submit another generation.
	_, _ = w.ResponseWriter.Write(data)
	return len(data), nil
}

func (w *imageRecoveryWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }
