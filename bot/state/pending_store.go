package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"telegram-bot/config"
)

type PendingRequest struct {
	Kind     string        `json:"kind"`
	Payload  string        `json:"payload"`
	Platform string        `json:"platform"`
	HasAudio bool          `json:"has_audio"`
	Options  []MediaOption `json:"options,omitempty"`
}

type MediaOption struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Mode     string `json:"mode"`
	Selector string `json:"selector,omitempty"`
}

type PendingStore struct {
	redisClient *redis.Client
	ttl         time.Duration

	ramMu sync.Mutex
	ram   map[pendingKey]ramPendingEntry

	rlMu sync.Mutex
	rl   map[string]ramRateEntry
}

// pendingKey scopes a pending request to one user in one chat, so in a group
// users neither overwrite each other's request nor act on someone else's.
type pendingKey struct {
	chatID int64
	userID int64
}

type ramPendingEntry struct {
	req       PendingRequest
	expiresAt time.Time
}

type ramRateEntry struct {
	count     int
	expiresAt time.Time
}

func NewPendingStoreFromEnv() *PendingStore {
	store := &PendingStore{
		ttl: 15 * time.Minute,
		ram: make(map[pendingKey]ramPendingEntry),
		rl:  make(map[string]ramRateEntry),
	}

	ttlMinutes := strings.TrimSpace(config.GetEnv("REDIS_PENDING_TTL_MINUTES", ""))
	if ttlMinutes != "" {
		if minutes, err := strconv.Atoi(ttlMinutes); err == nil && minutes > 0 {
			store.ttl = time.Duration(minutes) * time.Minute
		}
	}

	addr := strings.TrimSpace(config.GetEnv("REDIS_ADDR", ""))
	if addr == "" {
		log.Println("Redis is not configured. Using RAM fallback for pending requests.")
		return store
	}

	dbIndex := 0
	if raw := strings.TrimSpace(config.GetEnv("REDIS_DB", "")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			dbIndex = parsed
		}
	}

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: config.GetEnv("REDIS_PASSWORD", ""),
		DB:       dbIndex,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("Redis connection failed (%v). Using RAM fallback for pending requests.", err)
		_ = client.Close()
		return store
	}

	log.Println("Redis connected for pending requests store")
	store.redisClient = client
	return store
}

func (s *PendingStore) Set(chatID, userID int64, req PendingRequest) {
	key := pendingKey{chatID: chatID, userID: userID}
	if s.tryRedisSet(key, req) {
		s.deleteRAM(key)
		return
	}

	s.setRAM(key, req)
}

func (s *PendingStore) Get(chatID, userID int64) (PendingRequest, bool) {
	key := pendingKey{chatID: chatID, userID: userID}
	if req, ok, usedRedis := s.tryRedisGet(key, false); usedRedis && ok {
		return req, true
	}
	return s.getRAM(key, false)
}

// Take returns the pending request and removes it in one step. When two
// callbacks race for the same request (a double tap), only one gets ok=true.
func (s *PendingStore) Take(chatID, userID int64) (PendingRequest, bool) {
	key := pendingKey{chatID: chatID, userID: userID}
	if req, ok, usedRedis := s.tryRedisGet(key, true); usedRedis && ok {
		s.deleteRAM(key)
		return req, true
	}
	return s.getRAM(key, true)
}

func (s *PendingStore) Delete(chatID, userID int64) {
	key := pendingKey{chatID: chatID, userID: userID}
	s.tryRedisDelete(key)
	s.deleteRAM(key)
}

func (s *PendingStore) tryRedisSet(key pendingKey, req PendingRequest) bool {
	if s.redisClient == nil {
		return false
	}

	payload, err := json.Marshal(req)
	if err != nil {
		log.Printf("Pending request marshal failed (%v). Using RAM fallback.", err)
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.redisClient.Set(ctx, s.redisKey(key), payload, s.ttl).Err(); err != nil {
		log.Printf("Redis SET failed (%v). Using RAM fallback for pending requests.", err)
		return false
	}

	return true
}

// tryRedisGet reads (and with take=true atomically deletes, via GETDEL) the
// pending request. usedRedis is false when Redis is unavailable or failed.
func (s *PendingStore) tryRedisGet(key pendingKey, take bool) (req PendingRequest, ok bool, usedRedis bool) {
	if s.redisClient == nil {
		return PendingRequest{}, false, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var value string
	var err error
	if take {
		value, err = s.redisClient.GetDel(ctx, s.redisKey(key)).Result()
	} else {
		value, err = s.redisClient.Get(ctx, s.redisKey(key)).Result()
	}
	if err == redis.Nil {
		return PendingRequest{}, false, true
	}
	if err != nil {
		log.Printf("Redis GET failed (%v). Using RAM fallback for pending requests.", err)
		return PendingRequest{}, false, false
	}

	if err := json.Unmarshal([]byte(value), &req); err != nil {
		log.Printf("Redis payload unmarshal failed (%v). Using RAM fallback for pending requests.", err)
		return PendingRequest{}, false, false
	}

	return req, true, true
}

func (s *PendingStore) tryRedisDelete(key pendingKey) bool {
	if s.redisClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.redisClient.Del(ctx, s.redisKey(key)).Err(); err != nil {
		log.Printf("Redis DEL failed (%v).", err)
		return false
	}
	return true
}

func (s *PendingStore) setRAM(key pendingKey, req PendingRequest) {
	s.ramMu.Lock()
	defer s.ramMu.Unlock()

	now := time.Now()
	for k, v := range s.ram {
		if now.After(v.expiresAt) {
			delete(s.ram, k)
		}
	}
	s.ram[key] = ramPendingEntry{req: req, expiresAt: now.Add(s.ttl)}
}

func (s *PendingStore) getRAM(key pendingKey, take bool) (PendingRequest, bool) {
	s.ramMu.Lock()
	defer s.ramMu.Unlock()

	entry, ok := s.ram[key]
	if !ok {
		return PendingRequest{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.ram, key)
		return PendingRequest{}, false
	}
	if take {
		delete(s.ram, key)
	}
	return entry.req, true
}

func (s *PendingStore) deleteRAM(key pendingKey) {
	s.ramMu.Lock()
	defer s.ramMu.Unlock()
	delete(s.ram, key)
}

func (s *PendingStore) redisKey(key pendingKey) string {
	return fmt.Sprintf("pending:chat:%d:user:%d", key.chatID, key.userID)
}

// AllowRequest reports whether userID may make another request within the current
// hourly window given limit (max requests/hour). A non-positive limit means unlimited.
// It increments the counter as a side effect. Falls back to an in-RAM counter when
// Redis is unavailable.
func (s *PendingStore) AllowRequest(userID int64, limit int) bool {
	if limit <= 0 {
		return true
	}

	window := time.Now().UTC().Format("2006010215")
	key := fmt.Sprintf("rl:%d:%s", userID, window)

	if count, ok := s.tryRedisIncr(key, time.Hour); ok {
		return count <= int64(limit)
	}

	return s.ramIncrAllow(key, limit)
}

func (s *PendingStore) tryRedisIncr(key string, ttl time.Duration) (int64, bool) {
	if s.redisClient == nil {
		return 0, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	count, err := s.redisClient.Incr(ctx, key).Result()
	if err != nil {
		log.Printf("Redis INCR failed (%v). Using RAM fallback for rate limit.", err)
		return 0, false
	}

	if count == 1 {
		if err := s.redisClient.Expire(ctx, key, ttl).Err(); err != nil {
			log.Printf("Redis EXPIRE failed for rate-limit key (%v).", err)
		}
	}

	return count, true
}

func (s *PendingStore) ramIncrAllow(key string, limit int) bool {
	s.rlMu.Lock()
	defer s.rlMu.Unlock()

	now := time.Now()
	entry, ok := s.rl[key]
	if !ok || now.After(entry.expiresAt) {
		entry = ramRateEntry{count: 0, expiresAt: now.Add(time.Hour)}
		s.pruneRAMRateLocked(now)
	}

	entry.count++
	s.rl[key] = entry
	return entry.count <= limit
}

func (s *PendingStore) pruneRAMRateLocked(now time.Time) {
	for k, v := range s.rl {
		if now.After(v.expiresAt) {
			delete(s.rl, k)
		}
	}
}
