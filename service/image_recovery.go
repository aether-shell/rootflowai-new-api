package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const ImageRecoveryTTL = 30 * time.Minute
const ImageRecoveryTimeout = 20 * time.Minute
const ImageRecoveryMaxBytes = 32 << 20

// The hash stores only the public response, never credentials or input images.
// The sorted sets reserve capacity for both running and completed responses.
var claimImageRecovery = redis.NewScript(`
local existing = redis.call('HGET', KEYS[1], 'fingerprint')
if existing then
  if existing == ARGV[1] then return 0 else return -1 end
end
local now = tonumber(redis.call('TIME')[1])
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
redis.call('ZREMRANGEBYSCORE', KEYS[3], '-inf', now)
redis.call('ZREMRANGEBYSCORE', KEYS[4], '-inf', now)
redis.call('ZREMRANGEBYSCORE', KEYS[5], '-inf', now)
-- Eight 32 MiB bodies leave headroom in the shared 1 GiB production cache.
if redis.call('ZCARD', KEYS[2]) >= 8 or redis.call('ZCARD', KEYS[3]) >= 4 then return -2 end
if redis.call('ZCARD', KEYS[4]) >= 4096 or redis.call('ZCARD', KEYS[5]) >= 512 then return -2 end
local expiry = now + tonumber(ARGV[2])
redis.call('HSET', KEYS[1], 'fingerprint', ARGV[1], 'state', 'pending', 'started', now, 'request_id', ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[2])
redis.call('ZADD', KEYS[2], expiry, KEYS[1])
redis.call('ZADD', KEYS[3], expiry, KEYS[1])
redis.call('ZADD', KEYS[4], expiry, KEYS[1])
redis.call('ZADD', KEYS[5], expiry, KEYS[1])
redis.call('EXPIRE', KEYS[2], ARGV[2])
redis.call('EXPIRE', KEYS[3], ARGV[2])
redis.call('EXPIRE', KEYS[4], ARGV[2])
redis.call('EXPIRE', KEYS[5], ARGV[2])
return 1
`)

var finishImageRecovery = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'state') ~= 'pending' or redis.call('HGET', KEYS[1], 'request_id') ~= ARGV[5] then return 0 end
redis.call('HSET', KEYS[1], 'state', ARGV[1], 'status', ARGV[2], 'body', ARGV[3], 'content_type', ARGV[4])
if redis.call('HGET', KEYS[1], 'acknowledged') == '1' then
  redis.call('HSET', KEYS[1], 'state', 'delivered')
  redis.call('HDEL', KEYS[1], 'body', 'content_type')
  redis.call('ZREM', KEYS[2], KEYS[1])
  redis.call('ZREM', KEYS[3], KEYS[1])
end
return 1
`)

var acknowledgeImageRecovery = redis.NewScript(`
local state = redis.call('HGET', KEYS[1], 'state')
if not state then return 0 end
redis.call('HSET', KEYS[1], 'acknowledged', '1')
if state ~= 'pending' then
  redis.call('HSET', KEYS[1], 'state', 'delivered')
  redis.call('HDEL', KEYS[1], 'body', 'content_type')
  redis.call('ZREM', KEYS[2], KEYS[1])
  redis.call('ZREM', KEYS[3], KEYS[1])
end
return 1
`)

type ImageRecoveryRecord struct {
	State       string `json:"state"`
	Status      int    `json:"status,omitempty"`
	Body        string `json:"body,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
}

func imageRecoveryKeys(userID, tokenID int, id string) []string {
	// A shared hash tag also keeps the transaction on one Redis Cluster slot.
	owner := fmt.Sprintf("%d:%d", userID, tokenID)
	return []string{
		"{image-recovery}:request:" + owner + ":" + id,
		"{image-recovery}:capacity", "{image-recovery}:owner:" + owner,
		"{image-recovery}:records", "{image-recovery}:records:" + owner,
	}
}

// ClaimImageRecovery returns 1 for a new reservation, 0 for a replay, -1 for
// conflicting content, and -2 when the bounded cache is full.
func ClaimImageRecovery(ctx context.Context, userID, tokenID int, id, fingerprint, requestID string) (int, error) {
	return claimImageRecovery.Run(ctx, common.RDB, imageRecoveryKeys(userID, tokenID, id), fingerprint, int(ImageRecoveryTTL.Seconds()), requestID).Int()
}

func CompleteImageRecovery(ctx context.Context, userID, tokenID int, id string, record ImageRecoveryRecord) error {
	return finishImageRecovery.Run(ctx, common.RDB, imageRecoveryKeys(userID, tokenID, id), record.State, record.Status, record.Body, record.ContentType, record.RequestID).Err()
}

// AcknowledgeImageRecovery frees the response after the client has read it.
// A small tombstone keeps duplicate POSTs blocked for the original TTL. An ACK
// racing completion cannot release a running operation's capacity reservation.
func AcknowledgeImageRecovery(ctx context.Context, userID, tokenID int, id string) error {
	return acknowledgeImageRecovery.Run(ctx, common.RDB, imageRecoveryKeys(userID, tokenID, id)).Err()
}

func GetImageRecovery(ctx context.Context, userID, tokenID int, id string) (*ImageRecoveryRecord, error) {
	values, err := common.RDB.HGetAll(ctx, imageRecoveryKeys(userID, tokenID, id)[0]).Result()
	if err != nil || len(values) == 0 {
		return nil, err
	}
	status, _ := strconv.Atoi(values["status"])
	record := &ImageRecoveryRecord{State: values["state"], Status: status, Body: values["body"], ContentType: values["content_type"], RequestID: values["request_id"]}
	started, _ := strconv.ParseInt(values["started"], 10, 64)
	if record.State == "pending" && time.Now().Unix() > started+int64(ImageRecoveryTimeout.Seconds())+30 {
		record.State = "unavailable"
	}
	return record, nil
}
