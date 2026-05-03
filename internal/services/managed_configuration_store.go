package services

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"dademo.fr/loadbalancer-manager/internal/repositories"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	managedConfigurationsStoreBackendMemory = "memory"
	managedConfigurationsStoreBackendRedis  = "redis"
	managedConfigurationsBackendType        = "haproxy"
)

var managedConfigurationsNamespaceSanitizer = regexp.MustCompile(`[^a-z0-9_-]`)

// ManagedConfigurationStore persists managed HAProxy configurations outside of the HAProxy config file.
type ManagedConfigurationStore interface {
	List(context.Context) ([]HaproxyConfiguration, error)
	Save(context.Context, []HaproxyConfiguration) error
	Type() string
	Namespace() string
}

// InMemoryManagedConfigurationStore stores managed configurations in-process.
type InMemoryManagedConfigurationStore struct {
	mu             sync.RWMutex
	logger         zerolog.Logger
	namespace      string
	configurations map[string]HaproxyConfiguration
}

// RedisManagedConfigurationStore stores managed configurations in Redis.
type RedisManagedConfigurationStore struct {
	logger    zerolog.Logger
	namespace string
	redisKey  string
	client    *redis.Client
}

func newManagedConfigurationStore(
	logger zerolog.Logger,
	configurationService repositories.AppConfigurationService,
) (ManagedConfigurationStore, error) {
	configuration, err := configurationService.GetConfiguration()
	if err != nil {
		return nil, fmt.Errorf("unable to load configuration for managed configuration store provider: %w", err)
	}

	instanceName := strings.TrimSpace(configuration.Haproxy.InstanceName)
	if instanceName == "" {
		return nil, fmt.Errorf("invalid haproxy.instance_name: value is required")
	}

	namespace := managedConfigurationsNamespace(instanceName)
	storeConfiguration := configuration.Haproxy.ManagedConfigurationsStore
	backend := strings.ToLower(strings.TrimSpace(storeConfiguration.Backend))
	redisAddress := strings.TrimSpace(storeConfiguration.Redis.Address)

	switch backend {
	case "", managedConfigurationsStoreBackendMemory, managedConfigurationsStoreBackendRedis:
		// Allowed values. Empty means auto mode.
	default:
		return nil, fmt.Errorf("invalid haproxy.managed_configurations_store.backend %q: supported values are memory and redis", storeConfiguration.Backend)
	}

	useRedis := backend == managedConfigurationsStoreBackendRedis || (backend == "" && redisAddress != "")
	if useRedis {
		if redisAddress == "" {
			return nil, fmt.Errorf("haproxy.managed_configurations_store.redis.address is required when Redis backend is enabled")
		}

		client := redis.NewClient(&redis.Options{
			Addr:     redisAddress,
			Username: strings.TrimSpace(storeConfiguration.Redis.Username),
			Password: storeConfiguration.Redis.Password,
			DB:       storeConfiguration.Redis.DB,
		})

		if err := client.Ping(context.Background()).Err(); err != nil {
			return nil, fmt.Errorf("unable to connect to Redis managed configuration store at %q: %w", redisAddress, err)
		}

		redisKey := fmt.Sprintf("%s:configurations", namespace)
		return &RedisManagedConfigurationStore{
			logger:    logger.With().Str("component", "redis_managed_configuration_store").Logger(),
			namespace: namespace,
			redisKey:  redisKey,
			client:    client,
		}, nil
	}

	return &InMemoryManagedConfigurationStore{
		mu:             sync.RWMutex{},
		logger:         logger.With().Str("component", "in_memory_managed_configuration_store").Logger(),
		namespace:      namespace,
		configurations: make(map[string]HaproxyConfiguration),
	}, nil
}

func (s *InMemoryManagedConfigurationStore) List(_ context.Context) ([]HaproxyConfiguration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]HaproxyConfiguration, 0, len(s.configurations))
	for _, configuration := range s.configurations {
		items = append(items, cloneConfiguration(configuration))
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	return items, nil
}

func (s *InMemoryManagedConfigurationStore) Save(_ context.Context, configurations []HaproxyConfiguration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := make(map[string]HaproxyConfiguration, len(configurations))
	for _, configuration := range configurations {
		cloned := cloneConfiguration(configuration)
		cloned.Name = normalizeConfigurationName(cloned.Name)
		next[cloned.Name] = cloned
	}

	s.configurations = next
	return nil
}

func (s *InMemoryManagedConfigurationStore) Type() string {
	return managedConfigurationsStoreBackendMemory
}

func (s *InMemoryManagedConfigurationStore) Namespace() string {
	return s.namespace
}

func (s *RedisManagedConfigurationStore) List(ctx context.Context) ([]HaproxyConfiguration, error) {
	entries, err := s.client.HGetAll(ctx, s.redisKey).Result()
	if err != nil {
		return nil, fmt.Errorf("unable to read managed configurations from redis key %q: %w", s.redisKey, err)
	}
	if len(entries) == 0 {
		return []HaproxyConfiguration{}, nil
	}

	items := make([]HaproxyConfiguration, 0, len(entries))
	for name, payload := range entries {
		var configuration HaproxyConfiguration
		if err := json.Unmarshal([]byte(payload), &configuration); err != nil {
			return nil, fmt.Errorf("unable to decode redis managed configuration %q from key %q: %w", name, s.redisKey, err)
		}
		configuration.Name = normalizeConfigurationName(configuration.Name)
		items = append(items, configuration)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	return items, nil
}

func (s *RedisManagedConfigurationStore) Save(ctx context.Context, configurations []HaproxyConfiguration) error {
	pipe := s.client.TxPipeline()
	pipe.Del(ctx, s.redisKey)

	if len(configurations) > 0 {
		values := make(map[string]any, len(configurations))
		for _, configuration := range configurations {
			cloned := cloneConfiguration(configuration)
			cloned.Name = normalizeConfigurationName(cloned.Name)

			payload, err := json.Marshal(cloned)
			if err != nil {
				return fmt.Errorf("unable to serialize managed configuration %q: %w", cloned.Name, err)
			}
			values[cloned.Name] = string(payload)
		}

		pipe.HSet(ctx, s.redisKey, values)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("unable to persist managed configurations to redis key %q: %w", s.redisKey, err)
	}

	return nil
}

func (s *RedisManagedConfigurationStore) Type() string {
	return managedConfigurationsStoreBackendRedis
}

func (s *RedisManagedConfigurationStore) Namespace() string {
	return s.namespace
}

func managedConfigurationsNamespace(instanceName string) string {
	normalizedInstance := strings.ToLower(strings.TrimSpace(instanceName))
	normalizedInstance = strings.ReplaceAll(normalizedInstance, " ", "_")
	normalizedInstance = managedConfigurationsNamespaceSanitizer.ReplaceAllString(normalizedInstance, "_")
	normalizedInstance = strings.Trim(normalizedInstance, "_")
	if normalizedInstance == "" {
		normalizedInstance = "default"
	}

	return fmt.Sprintf("%s:%s", managedConfigurationsBackendType, normalizedInstance)
}
