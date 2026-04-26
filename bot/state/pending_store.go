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

	ramMu sync.RWMutex
	ram   map[int64]PendingRequest
}

func NewPendingStoreFromEnv() *PendingStore {
	store := &PendingStore{
		ttl: 15 * time.Minute,
		ram: make(map[int64]PendingRequest),
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
		log.Println("WARNING: RAM pending store has no TTL — pending requests will not expire and memory will grow over time.")
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

func (s *PendingStore) Set(chatID int64, req PendingRequest) {
	if s.tryRedisSet(chatID, req) {
		s.deleteRAM(chatID)
		return
	}

	s.setRAM(chatID, req)
}

func (s *PendingStore) Get(chatID int64) (PendingRequest, bool) {
	if req, ok, usedRedis := s.tryRedisGet(chatID); usedRedis {
		if ok {
			return req, true
		}
		if ramReq, ramOK := s.getRAM(chatID); ramOK {
			return ramReq, true
		}
		return PendingRequest{}, false
	}

	return s.getRAM(chatID)
}

func (s *PendingStore) Delete(chatID int64) {
	if s.tryRedisDelete(chatID) {
		s.deleteRAM(chatID)
		return
	}

	s.deleteRAM(chatID)
}

func (s *PendingStore) tryRedisSet(chatID int64, req PendingRequest) bool {
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

	if err := s.redisClient.Set(ctx, s.redisKey(chatID), payload, s.ttl).Err(); err != nil {
		log.Printf("Redis SET failed (%v). Using RAM fallback for pending requests.", err)
		return false
	}

	return true
}

func (s *PendingStore) tryRedisGet(chatID int64) (PendingRequest, bool, bool) {
	if s.redisClient == nil {
		return PendingRequest{}, false, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	value, err := s.redisClient.Get(ctx, s.redisKey(chatID)).Result()
	if err == redis.Nil {
		return PendingRequest{}, false, true
	}
	if err != nil {
		log.Printf("Redis GET failed (%v). Using RAM fallback for pending requests.", err)
		return PendingRequest{}, false, false
	}

	var req PendingRequest
	if err := json.Unmarshal([]byte(value), &req); err != nil {
		log.Printf("Redis payload unmarshal failed (%v). Using RAM fallback for pending requests.", err)
		return PendingRequest{}, false, false
	}

	return req, true, true
}

func (s *PendingStore) tryRedisDelete(chatID int64) bool {
	if s.redisClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.redisClient.Del(ctx, s.redisKey(chatID)).Err(); err != nil {
		log.Printf("Redis DEL failed (%v). Using RAM fallback for pending requests.", err)
		return false
	}
	return true
}

func (s *PendingStore) setRAM(chatID int64, req PendingRequest) {
	s.ramMu.Lock()
	defer s.ramMu.Unlock()
	s.ram[chatID] = req
}

func (s *PendingStore) getRAM(chatID int64) (PendingRequest, bool) {
	s.ramMu.RLock()
	defer s.ramMu.RUnlock()
	req, ok := s.ram[chatID]
	return req, ok
}

func (s *PendingStore) deleteRAM(chatID int64) {
	s.ramMu.Lock()
	defer s.ramMu.Unlock()
	delete(s.ram, chatID)
}

func (s *PendingStore) redisKey(chatID int64) string {
	return fmt.Sprintf("pending:chat:%d", chatID)
}
