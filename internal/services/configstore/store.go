// Package configstore provides a generic Store interface and its backend implementations
// (in-memory and Redis). Callers instantiate a Store[T] for any JSON-serializable type T.
package configstore

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"dademo.fr/loadbalancer-manager/internal/repositories"
	"github.com/rs/zerolog"
)

const (
	BackendMemory = "memory"
	BackendRedis  = "redis"

	backendType = "haproxy"
)

var namespaceSanitizer = regexp.MustCompile(`[^a-z0-9_-]`)

// Store persists managed configurations of type T.
// T must be JSON-serializable.
type Store[T any] interface {
	List(context.Context) ([]T, error)
	Save(context.Context, []T) error
	Type() string
	Namespace() string
}

// NewConfigStore creates a Store[T] using the backend declared in the application configuration.
// Auto-detection: if backend is empty and a Redis address is set, Redis is used;
// otherwise the in-memory backend is used.
func NewConfigStore[T any](
	logger zerolog.Logger,
	configurationService repositories.AppConfigurationService,
) (Store[T], error) {
	configuration, err := configurationService.GetConfiguration()
	if err != nil {
		return nil, fmt.Errorf("unable to load configuration for managed configuration store provider: %w", err)
	}

	instanceName := strings.TrimSpace(configuration.Haproxy.InstanceName)
	if instanceName == "" {
		return nil, fmt.Errorf("invalid haproxy.instance_name: value is required")
	}

	ns := Namespace(instanceName)
	storeConfiguration := configuration.Haproxy.ManagedConfigurationsStore
	backend := strings.ToLower(strings.TrimSpace(storeConfiguration.Backend))
	redisAddress := strings.TrimSpace(storeConfiguration.Redis.Address)

	switch backend {
	case "", BackendMemory, BackendRedis:
		// Allowed values. Empty means auto mode.
	default:
		return nil, fmt.Errorf("invalid haproxy.managed_configurations_store.backend %q: supported values are memory and redis", storeConfiguration.Backend)
	}

	useRedis := backend == BackendRedis || (backend == "" && redisAddress != "")
	if useRedis {
		return newRedisStore[T](logger, ns, storeConfiguration.Redis)
	}

	return newInMemoryStore[T](logger, ns), nil
}

// Namespace computes the store namespace key for a given HAProxy instance name.
func Namespace(instanceName string) string {
	normalized := strings.ToLower(strings.TrimSpace(instanceName))
	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = namespaceSanitizer.ReplaceAllString(normalized, "_")
	normalized = strings.Trim(normalized, "_")
	if normalized == "" {
		normalized = "default"
	}

	return fmt.Sprintf("%s:%s", backendType, normalized)
}
