package middleware

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const recoveryTestID = "12345678-1234-4234-8234-123456789abc"

func recoveryTestRouter(tokenID int, handler gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(BodyStorageCleanup())
	router.Use(func(c *gin.Context) {
		c.Set("id", 42)
		c.Set("token_id", tokenID)
		c.Set(common.RequestIdKey, "server-request-id")
	})
	router.GET("/v1/images/recovery", ImageRecoveryCapabilities)
	router.GET("/v1/images/requests/:id", ImageRecoveryResult)
	router.DELETE("/v1/images/requests/:id", ImageRecoveryResult)
	router.POST("/v1/images/generations", RecoverImageResponse(), handler)
	return router
}

// Optional integration uses a dedicated local Redis Unix socket, never a
// deployment connection string. It exercises an actual disconnected HTTP client.
func TestImageRecoveryRealRedisDisconnectedHTTP(t *testing.T) {
	socket := os.Getenv("ROOTFLOW_TEST_REDIS_SOCKET")
	if socket == "" {
		t.Skip("set ROOTFLOW_TEST_REDIS_SOCKET to a dedicated local Redis socket")
	}
	require.True(t, strings.HasPrefix(socket, "/private/tmp/rootflow-"))
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socket, DB: 15})
	testID := uuid.NewString()
	require.NoError(t, client.Ping(context.Background()).Err())
	previousRedis, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = previousRedis, previousEnabled; _ = client.Close() })
	t.Setenv("IMAGE_RECOVERY_ENABLED", "true")
	started, release, completed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"original-result"}]}`)
	}))
	defer upstream.Close()
	router := recoveryTestRouter(999, func(c *gin.Context) {
		r, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, strings.NewReader(`{}`))
		require.NoError(t, err)
		response, err := http.DefaultClient.Do(r)
		require.NoError(t, err)
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		c.Data(response.StatusCode, "application/json", body)
	})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r)
		if r.Method == http.MethodPost {
			close(completed)
		}
	}))
	defer gateway.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1/images/generations", strings.NewReader(`{"model":"image"}`))
	require.NoError(t, err)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(imageRecoveryHeader, testID)
	clientDone := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(r)
		if resp != nil {
			_ = resp.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not start")
	}
	cancel()
	require.Error(t, <-clientDone)
	close(release)
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("disconnected request did not complete")
	}
	result, err := http.Get(gateway.URL + "/v1/images/requests/" + testID)
	require.NoError(t, err)
	defer result.Body.Close()
	var record service.ImageRecoveryRecord
	require.NoError(t, common.DecodeJson(result.Body, &record))
	assert.Equal(t, "completed", record.State)
	assert.JSONEq(t, `{"data":[{"b64_json":"original-result"}]}`, record.Body)
	assert.EqualValues(t, 1, calls.Load())
}

func recoveryTestRequest(body, id string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(imageRecoveryHeader, id)
	return r
}

func TestImageRecoveryDisconnectAndReplay(t *testing.T) {
	useRateLimitMiniRedis(t)
	t.Setenv("IMAGE_RECOVERY_ENABLED", "true")
	started, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var generations, settlements atomic.Int32
	router := recoveryTestRouter(10, func(c *gin.Context) {
		generations.Add(1)
		close(started)
		<-finish
		assert.NoError(t, c.Request.Context().Err(), "client cancellation must not cancel the accepted operation")
		assert.Empty(t, c.GetHeader(imageRecoveryHeader), "gateway identifier stays out of upstream requests")
		c.JSON(200, gin.H{"data": []gin.H{{"b64_json": "original-image"}}})
		settlements.Add(1)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer close(done)
		router.ServeHTTP(httptest.NewRecorder(), recoveryTestRequest(`{"model":"image"}`, recoveryTestID).WithContext(ctx))
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("generation did not start")
	}
	cancel()
	query := httptest.NewRecorder()
	router.ServeHTTP(query, httptest.NewRequest(http.MethodGet, "/v1/images/requests/"+recoveryTestID, nil))
	assert.Contains(t, query.Body.String(), `"state":"pending"`)
	duplicate := httptest.NewRecorder()
	router.ServeHTTP(duplicate, recoveryTestRequest(`{"model":"image"}`, recoveryTestID))
	assert.Equal(t, 202, duplicate.Code)
	conflict := httptest.NewRecorder()
	router.ServeHTTP(conflict, recoveryTestRequest(`{"model":"changed"}`, recoveryTestID))
	assert.Equal(t, 409, conflict.Code)
	close(finish)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("generation did not finish")
	}
	// Another handler instance shares Redis, as another gateway replica would.
	reader := recoveryTestRouter(10, func(c *gin.Context) { t.Error("query must not generate") })
	query = httptest.NewRecorder()
	reader.ServeHTTP(query, httptest.NewRequest(http.MethodGet, "/v1/images/requests/"+recoveryTestID, nil))
	var record service.ImageRecoveryRecord
	require.NoError(t, common.Unmarshal(query.Body.Bytes(), &record))
	assert.Equal(t, "completed", record.State)
	assert.Equal(t, 200, record.Status)
	assert.JSONEq(t, `{"data":[{"b64_json":"original-image"}]}`, record.Body)
	assert.Equal(t, "no-store", query.Header().Get("Cache-Control"))
	assert.EqualValues(t, 1, generations.Load())
	assert.EqualValues(t, 1, settlements.Load())
	other := httptest.NewRecorder()
	recoveryTestRouter(11, nil).ServeHTTP(other, httptest.NewRequest(http.MethodGet, "/v1/images/requests/"+recoveryTestID, nil))
	assert.Equal(t, 404, other.Code)
	assert.NotContains(t, other.Body.String(), "original-image")
	ack := httptest.NewRecorder()
	reader.ServeHTTP(ack, httptest.NewRequest(http.MethodDelete, "/v1/images/requests/"+recoveryTestID, nil))
	assert.Equal(t, 204, ack.Code)
	query = httptest.NewRecorder()
	reader.ServeHTTP(query, httptest.NewRequest(http.MethodGet, "/v1/images/requests/"+recoveryTestID, nil))
	assert.NotContains(t, query.Body.String(), "original-image")
	duplicate = httptest.NewRecorder()
	reader.ServeHTTP(duplicate, recoveryTestRequest(`{"model":"image"}`, recoveryTestID))
	assert.Equal(t, 202, duplicate.Code, "ACK must not permit another generation")
}

func TestImageRecoveryAdmissionAndExpiry(t *testing.T) {
	s, _ := useRateLimitMiniRedis(t)
	t.Setenv("IMAGE_RECOVERY_ENABLED", "true")
	calls := 0
	router := recoveryTestRouter(10, func(c *gin.Context) { calls++; c.JSON(403, gin.H{"error": gin.H{"code": "content_audit_blocked"}}) })
	for i := range 5 {
		id := fmt.Sprintf("12345678-1234-4234-8234-%012d", i)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, recoveryTestRequest(`{"model":"image"}`, id))
		if i < 4 {
			assert.Equal(t, 403, w.Code)
		} else {
			assert.Equal(t, 503, w.Code)
		}
	}
	assert.Equal(t, 4, calls)
	bad := httptest.NewRecorder()
	router.ServeHTTP(bad, recoveryTestRequest(`{"stream":true}`, recoveryTestID))
	assert.Equal(t, 400, bad.Code)
	unauthenticated := httptest.NewRecorder()
	recoveryTestRouter(0, nil).ServeHTTP(unauthenticated, recoveryTestRequest(`{}`, recoveryTestID))
	assert.Equal(t, 401, unauthenticated.Code)
	s.FastForward(service.ImageRecoveryTTL + time.Second)
	s.SetTime(time.Now().Add(service.ImageRecoveryTTL + time.Second))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, recoveryTestRequest(`{"model":"image"}`, recoveryTestID))
	assert.Equal(t, 403, w.Code, "expired reservations must release capacity")
	assert.Equal(t, 5, calls)
	t.Setenv("IMAGE_RECOVERY_ENABLED", "false")
	query := httptest.NewRecorder()
	router.ServeHTTP(query, httptest.NewRequest(http.MethodGet, "/v1/images/requests/"+recoveryTestID, nil))
	assert.Contains(t, query.Body.String(), `"state":"completed"`, "rollback keeps retrieval available")
	s.Close()
	t.Setenv("IMAGE_RECOVERY_ENABLED", "true")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, recoveryTestRequest(`{}`, "12345678-1234-4234-8234-123456789abd"))
	assert.Equal(t, 503, w.Code)
	assert.Equal(t, 5, calls, "Redis admission failure must not generate")
}

func TestImageRecoveryLimitsAndInterruptedOperation(t *testing.T) {
	_, client := useRateLimitMiniRedis(t)
	t.Setenv("IMAGE_RECOVERY_ENABLED", "true")
	router := recoveryTestRouter(10, func(c *gin.Context) {
		c.Data(200, "application/json", []byte(strings.Repeat("x", service.ImageRecoveryMaxBytes+1)))
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, recoveryTestRequest(`{}`, recoveryTestID))
	assert.Equal(t, service.ImageRecoveryMaxBytes+1, w.Body.Len(), "response still reaches the original client")
	query := httptest.NewRecorder()
	router.ServeHTTP(query, httptest.NewRequest(http.MethodGet, "/v1/images/requests/"+recoveryTestID, nil))
	assert.JSONEq(t, `{"state":"unavailable","request_id":"server-request-id"}`, query.Body.String())
	ctx := context.Background()
	claimed, err := service.ClaimImageRecovery(ctx, 42, 10, "12345678-1234-4234-8234-123456789abd", "digest", "request")
	require.NoError(t, err)
	require.Equal(t, 1, claimed)
	keys, err := client.Keys(ctx, "{image-recovery}:request:*123456789abd").Result()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.NoError(t, client.HSet(ctx, keys[0], "started", time.Now().Add(-21*time.Minute).Unix()).Err())
	record, err := service.GetImageRecovery(ctx, 42, 10, "12345678-1234-4234-8234-123456789abd")
	require.NoError(t, err)
	assert.Equal(t, "unavailable", record.State, "a killed worker cannot leave endless pending")
}

func TestImageRecoverySharedCapacityAndAcknowledgement(t *testing.T) {
	useRateLimitMiniRedis(t)
	t.Setenv("IMAGE_RECOVERY_ENABLED", "true")
	calls := 0
	for i := range 9 {
		router := recoveryTestRouter(100+i, func(c *gin.Context) {
			calls++
			c.JSON(http.StatusOK, gin.H{"data": "image"})
		})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, recoveryTestRequest(`{}`, recoveryTestID))
		if i < 8 {
			assert.Equal(t, http.StatusOK, w.Code)
		} else {
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		}
	}
	assert.Equal(t, 8, calls, "shared capacity must reject before another upstream call")
	ack := httptest.NewRecorder()
	recoveryTestRouter(100, nil).ServeHTTP(ack, httptest.NewRequest(http.MethodDelete, "/v1/images/requests/"+recoveryTestID, nil))
	require.Equal(t, http.StatusNoContent, ack.Code)
	w := httptest.NewRecorder()
	recoveryTestRouter(108, func(c *gin.Context) {
		calls++
		c.JSON(http.StatusOK, gin.H{"data": "next-image"})
	}).ServeHTTP(w, recoveryTestRequest(`{}`, recoveryTestID))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 9, calls, "acknowledging a result must release shared capacity")
}

func TestImageRecoveryAcknowledgementRacingCompletion(t *testing.T) {
	_, client := useRateLimitMiniRedis(t)
	ctx := context.Background()
	claimed, err := service.ClaimImageRecovery(ctx, 42, 10, recoveryTestID, "digest", "original-request")
	require.NoError(t, err)
	require.Equal(t, 1, claimed)
	require.NoError(t, service.AcknowledgeImageRecovery(ctx, 42, 10, recoveryTestID))
	assert.EqualValues(t, 1, client.ZCard(ctx, "{image-recovery}:capacity").Val(), "ACK must not release an in-flight reservation")
	require.NoError(t, service.CompleteImageRecovery(ctx, 42, 10, recoveryTestID, service.ImageRecoveryRecord{State: "completed", Status: 200, Body: "original", RequestID: "stale-worker"}))
	record, err := service.GetImageRecovery(ctx, 42, 10, recoveryTestID)
	require.NoError(t, err)
	assert.Equal(t, "pending", record.State, "a stale worker cannot overwrite another request")
	require.NoError(t, service.CompleteImageRecovery(ctx, 42, 10, recoveryTestID, service.ImageRecoveryRecord{State: "completed", Status: 200, Body: "original", RequestID: "original-request"}))
	record, err = service.GetImageRecovery(ctx, 42, 10, recoveryTestID)
	require.NoError(t, err)
	assert.Equal(t, "delivered", record.State)
	assert.Empty(t, record.Body)
	assert.Zero(t, client.ZCard(ctx, "{image-recovery}:capacity").Val())
	claimed, err = service.ClaimImageRecovery(ctx, 42, 10, recoveryTestID, "digest", "retry-request")
	require.NoError(t, err)
	assert.Zero(t, claimed, "the tombstone must still block duplicate generation")
}
