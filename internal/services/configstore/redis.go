package configstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"dademo.fr/loadbalancer-manager/internal/repositories"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// RedisStore stores managed configurations of type T in Redis as a single JSON blob.
// It implements io.Closer; callers should call Close() when the store is no longer needed.
type RedisStore[T any] struct {
	logger    zerolog.Logger
	namespace string
	redisKey  string
	client    *redis.Client
	closeOnce sync.Once
}

func newRedisStore[T any](
	logger zerolog.Logger,
	namespace string,
	settings repositories.HaproxyManagedConfigurationsRedisSettings,
) (Store[T], error) {
	address := strings.TrimSpace(settings.Address)
	if address == "" {
		return nil, fmt.Errorf("haproxy.managed_configurations_store.redis.address is required when Redis backend is enabled")
	}

	client := redis.NewClient(&redis.Options{
		Addr:     address,
		Username: strings.TrimSpace(settings.Username),
		Password: settings.Password,
		DB:       settings.DB,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("unable to connect to Redis managed configuration store at %q: %w", address, err)
	}

	return &RedisStore[T]{
		logger:    logger.With().Str("component", "redis_managed_configuration_store").Logger(),
		namespace: namespace,
		redisKey:  fmt.Sprintf("%s:configurations", namespace),
		client:    client,
	}, nil
}

// List returns all stored configurations from Redis.
func (s *RedisStore[T]) List(ctx context.Context) ([]T, error) {
	payload, err := s.client.Get(ctx, s.redisKey).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return []T{}, nil
		}
		return nil, fmt.Errorf("unable to read managed configurations from redis key %q: %w", s.redisKey, err)
	}

	var items []T
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, fmt.Errorf("unable to decode managed configurations from redis key %q: %w", s.redisKey, err)
	}

	return items, nil
}

// Save writes all configurations to Redis.
func (s *RedisStore[T]) Save(ctx context.Context, configurations []T) error {
	payload, err := json.Marshal(configurations)
	if err != nil {
		return fmt.Errorf("unable to serialize managed configurations: %w", err)
	}

	if err := s.client.Set(ctx, s.redisKey, payload, 0).Err(); err != nil {
		return fmt.Errorf("unable to persist managed configurations to redis key %q: %w", s.redisKey, err)
	}

	return nil
}

// Type returns the backend type identifier.
func (s *RedisStore[T]) Type() string {
	return BackendRedis
}

// Namespace returns the store namespace.
func (s *RedisStore[T]) Namespace() string {
	return s.namespace
}

// Close closes the underlying Redis client.
func (s *RedisStore[T]) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		closeErr = s.client.Close()
	})
	return closeErr
}
