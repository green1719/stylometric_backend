package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	SessionCookieName = "sessionId"
	SessionKeyPrefix  = "session:"
	SessionTTL        = time.Hour
)

// SessionStore хранит краткоживущую связь sessionId с ID пользователя.
// В Redis не помещаются пароль и профиль пользователя.
type SessionStore interface {
	Create(context.Context, uint) (string, error)
	UserID(context.Context, string) (uint, bool, error)
	Destroy(context.Context, string) error
}

type RedisSessionStore struct {
	client *redis.Client
}

func NewRedisSessionStoreFromEnv() (*RedisSessionStore, error) {
	address := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if address == "" {
		return nil, errors.New("REDIS_ADDR не задан")
	}
	database := 0
	if rawDatabase := strings.TrimSpace(os.Getenv("REDIS_DB")); rawDatabase != "" {
		parsedDatabase, err := strconv.Atoi(rawDatabase)
		if err != nil || parsedDatabase < 0 {
			return nil, errors.New("REDIS_DB должен быть неотрицательным числом")
		}
		database = parsedDatabase
	}
	client := redis.NewClient(&redis.Options{
		Addr:         address,
		Password:     os.Getenv("REDIS_PASSWORD"),
		DB:           database,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("Redis недоступен: %w", err)
	}
	return &RedisSessionStore{client: client}, nil
}

func (store *RedisSessionStore) Create(ctx context.Context, userID uint) (string, error) {
	if userID == 0 {
		return "", errors.New("нельзя создать сессию без ID пользователя")
	}
	for range 4 {
		var rawID [32]byte
		if _, err := rand.Read(rawID[:]); err != nil {
			return "", err
		}
		sessionID := hex.EncodeToString(rawID[:])
		created, err := store.client.SetNX(ctx, SessionKeyPrefix+sessionID, strconv.FormatUint(uint64(userID), 10), SessionTTL).Result()
		if err != nil {
			return "", err
		}
		if created {
			return sessionID, nil
		}
	}
	return "", errors.New("не удалось создать уникальный sessionId")
}

func (store *RedisSessionStore) UserID(ctx context.Context, sessionID string) (uint, bool, error) {
	value, err := store.client.Get(ctx, SessionKeyPrefix+sessionID).Result()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	userID, err := strconv.ParseUint(value, 10, strconv.IntSize)
	if err != nil || userID == 0 {
		return 0, false, errors.New("некорректный ID пользователя в сессии")
	}
	return uint(userID), true, nil
}

func (store *RedisSessionStore) Destroy(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return store.client.Del(ctx, SessionKeyPrefix+sessionID).Err()
}
