package configstore

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
)

// InMemoryStore stores managed configurations of type T in-process.
type InMemoryStore[T any] struct {
	mu             sync.RWMutex
	logger         zerolog.Logger
	namespace      string
	configurations []T
}

func newInMemoryStore[T any](logger zerolog.Logger, namespace string) Store[T] {
	return &InMemoryStore[T]{
		logger:    logger.With().Str("component", "in_memory_managed_configuration_store").Logger(),
		namespace: namespace,
	}
}

// List returns a copy of the currently stored configurations.
func (s *InMemoryStore[T]) List(_ context.Context) ([]T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]T, len(s.configurations))
	copy(result, s.configurations)
	return result, nil
}

// Save replaces the currently stored configurations.
func (s *InMemoryStore[T]) Save(_ context.Context, configurations []T) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := make([]T, len(configurations))
	copy(next, configurations)
	s.configurations = next
	return nil
}

// Type returns the backend type identifier.
func (s *InMemoryStore[T]) Type() string {
	return BackendMemory
}

// Namespace returns the store namespace.
func (s *InMemoryStore[T]) Namespace() string {
	return s.namespace
}
