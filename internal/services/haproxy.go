package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"dademo.fr/loadbalancer-manager/internal/repositories"
	"dademo.fr/loadbalancer-manager/internal/services/configstore"
	"dademo.fr/loadbalancer-manager/internal/services/haproxycfg"
	"github.com/haproxytech/client-native/v6/models"
	"github.com/haproxytech/client-native/v6/runtime"
	runtimeOptions "github.com/haproxytech/client-native/v6/runtime/options"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

// HaproxyStatus contains grouped HAProxy runtime stats.

// ManagedConfigurationStore is the configuration store type used by HaproxyService.
type ManagedConfigurationStore = configstore.Store[haproxycfg.HaproxyConfiguration]
type HaproxyStatus struct {
	Frontends []HaproxyProxyStatus `json:"frontends"`
	Backends  []HaproxyProxyStatus `json:"backends"`
}

// HaproxyProxyStatus represents runtime metrics for a frontend or backend.
type HaproxyProxyStatus struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Current    int64  `json:"current"`
	Max        int64  `json:"max"`
	Total      int64  `json:"total"`
	Rate       int64  `json:"rate"`
	BytesIn    int64  `json:"bytes_in"`
	BytesOut   int64  `json:"bytes_out"`
	LastChange int64  `json:"last_change"`
}

// Type aliases — allow callers to keep using services.HaproxyConfiguration etc.
type HaproxyConfiguration = haproxycfg.HaproxyConfiguration
type HaproxyBackendTarget = haproxycfg.HaproxyBackendTarget
type HaproxyTLSConfiguration = haproxycfg.HaproxyTLSConfiguration

// Error re-exports for backward compatibility.
var (
	ErrConfigurationNotFound   = haproxycfg.ErrConfigurationNotFound
	ErrConfigurationExists     = haproxycfg.ErrConfigurationExists
	ErrInvalidConfiguration    = haproxycfg.ErrInvalidConfiguration
	ErrInvalidConfigurationKey = haproxycfg.ErrInvalidConfigurationKey
)

// HaproxyService manages HAProxy runtime operations and managed configurations.
type HaproxyService struct {
	logger               zerolog.Logger
	configurationService repositories.AppConfigurationService
	configurationStore   ManagedConfigurationStore
	mu                   sync.RWMutex
	client               runtime.Runtime
	configurationFile    string
	certificatesDir      string
}

func newHaproxyService(
	logger zerolog.Logger,
	configurationService repositories.AppConfigurationService,
	configurationStore ManagedConfigurationStore,
	lifecycle fx.Lifecycle,
) *HaproxyService {
	service := &HaproxyService{
		logger:               logger.With().Str("component", "haproxy_service").Logger(),
		configurationService: configurationService,
		configurationStore:   configurationStore,
		mu:                   sync.RWMutex{},
	}

	lifecycle.Append(fx.Hook{
		OnStart: service.onStart,
		OnStop:  service.onStop,
	})

	return service
}

// GetStatus returns runtime status for HAProxy frontends and backends.
func (s *HaproxyService) GetStatus(ctx context.Context) (*HaproxyStatus, error) {
	client, err := s.getOrCreateClient(ctx)
	if err != nil {
		return nil, err
	}

	nativeStats := client.GetStats()
	if nativeStats.Error != "" {
		return nil, fmt.Errorf("unable to query HAProxy runtime stats: %s", nativeStats.Error)
	}

	status := &HaproxyStatus{
		Frontends: make([]HaproxyProxyStatus, 0),
		Backends:  make([]HaproxyProxyStatus, 0),
	}

	for _, stat := range nativeStats.Stats {
		if stat == nil || stat.Stats == nil {
			continue
		}

		mapped := mapNativeStat(stat)
		switch stat.Type {
		case "frontend":
			status.Frontends = append(status.Frontends, mapped)
		case "backend":
			status.Backends = append(status.Backends, mapped)
		}
	}

	return status, nil
}

// CreateConfiguration validates and persists a new managed HAProxy configuration.
func (s *HaproxyService) CreateConfiguration(ctx context.Context, configuration HaproxyConfiguration) (HaproxyConfiguration, error) {
	if err := haproxycfg.ValidateConfiguration(configuration); err != nil {
		return HaproxyConfiguration{}, err
	}

	if _, err := s.getOrCreateClient(ctx); err != nil {
		return HaproxyConfiguration{}, err
	}

	key, err := haproxycfg.NormalizeAndValidateConfigurationName(configuration.Name)
	if err != nil {
		return HaproxyConfiguration{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	configurations, err := s.loadConfigurationsLocked(ctx)
	if err != nil {
		return HaproxyConfiguration{}, err
	}

	for _, existing := range configurations {
		if haproxycfg.NormalizeConfigurationName(existing.Name) == key {
			return HaproxyConfiguration{}, fmt.Errorf("configuration %q: %w", configuration.Name, ErrConfigurationExists)
		}
	}

	stored := haproxycfg.CloneConfiguration(configuration)
	stored.Name = key
	stored.LoadBalancing = haproxycfg.NormalizeLoadBalancingStrategy(stored.LoadBalancing)
	stored.TrafficMode = haproxycfg.NormalizeTrafficMode(stored.TrafficMode)
	configurations = append(configurations, stored)

	if err := s.persistAndReloadLocked(ctx, configurations); err != nil {
		return HaproxyConfiguration{}, err
	}

	return haproxycfg.CloneConfiguration(stored), nil
}

// ListConfigurations returns all managed HAProxy configurations ordered by name.
func (s *HaproxyService) ListConfigurations(ctx context.Context) []HaproxyConfiguration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configurations, err := s.loadConfigurationsLocked(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Unable to load HAProxy managed configurations")
		return []HaproxyConfiguration{}
	}

	sort.Slice(configurations, func(i, j int) bool {
		return configurations[i].Name < configurations[j].Name
	})

	for i := range configurations {
		configurations[i] = haproxycfg.CloneConfiguration(configurations[i])
	}

	return configurations
}

// GetConfiguration returns a managed HAProxy configuration by name.
func (s *HaproxyService) GetConfiguration(ctx context.Context, name string) (HaproxyConfiguration, error) {
	key, err := haproxycfg.NormalizeAndValidateConfigurationName(name)
	if err != nil {
		return HaproxyConfiguration{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	configurations, err := s.loadConfigurationsLocked(ctx)
	if err != nil {
		return HaproxyConfiguration{}, err
	}

	for _, configuration := range configurations {
		if haproxycfg.NormalizeConfigurationName(configuration.Name) == key {
			return haproxycfg.CloneConfiguration(configuration), nil
		}
	}

	return HaproxyConfiguration{}, fmt.Errorf("configuration %q: %w", name, ErrConfigurationNotFound)
}

// UpdateConfiguration validates and persists changes to an existing managed configuration.
func (s *HaproxyService) UpdateConfiguration(ctx context.Context, configuration HaproxyConfiguration) (HaproxyConfiguration, error) {
	if err := haproxycfg.ValidateConfiguration(configuration); err != nil {
		return HaproxyConfiguration{}, err
	}

	if _, err := s.getOrCreateClient(ctx); err != nil {
		return HaproxyConfiguration{}, err
	}

	key := haproxycfg.NormalizeConfigurationName(configuration.Name)

	s.mu.Lock()
	defer s.mu.Unlock()

	configurations, err := s.loadConfigurationsLocked(ctx)
	if err != nil {
		return HaproxyConfiguration{}, err
	}

	index := -1
	for i, current := range configurations {
		if haproxycfg.NormalizeConfigurationName(current.Name) == key {
			index = i
			break
		}
	}
	if index == -1 {
		return HaproxyConfiguration{}, fmt.Errorf("configuration %q: %w", configuration.Name, ErrConfigurationNotFound)
	}

	updated := haproxycfg.CloneConfiguration(configuration)
	updated.Name = key
	updated.LoadBalancing = haproxycfg.NormalizeLoadBalancingStrategy(updated.LoadBalancing)
	updated.TrafficMode = haproxycfg.NormalizeTrafficMode(updated.TrafficMode)
	configurations[index] = updated

	if err := s.persistAndReloadLocked(ctx, configurations); err != nil {
		return HaproxyConfiguration{}, err
	}

	return haproxycfg.CloneConfiguration(updated), nil
}

// DeleteConfiguration removes a managed HAProxy configuration by name.
func (s *HaproxyService) DeleteConfiguration(ctx context.Context, name string) error {
	if _, err := s.getOrCreateClient(ctx); err != nil {
		return err
	}

	key, err := haproxycfg.NormalizeAndValidateConfigurationName(name)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	configurations, err := s.loadConfigurationsLocked(ctx)
	if err != nil {
		return err
	}

	index := -1
	for i, configuration := range configurations {
		if haproxycfg.NormalizeConfigurationName(configuration.Name) == key {
			index = i
			break
		}
	}
	if index == -1 {
		return fmt.Errorf("configuration %q: %w", name, ErrConfigurationNotFound)
	}

	configurations = append(configurations[:index], configurations[index+1:]...)
	if err := s.persistAndReloadLocked(ctx, configurations); err != nil {
		return err
	}

	return nil
}

func (s *HaproxyService) onStart(ctx context.Context) error {
	_, err := s.getOrCreateClient(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Unable to initialize HAProxy runtime client")
		return err
	}

	if err := s.syncManagedConfigurationsFromStore(ctx); err != nil {
		s.logger.Error().Err(err).Msg("Unable to synchronize HAProxy managed configurations from cache")
		return err
	}

	s.logger.Info().Str("configuration_file", s.configurationFile).Msg("HAProxy runtime client initialized")
	return nil
}

func (s *HaproxyService) onStop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = nil
	if closer, ok := s.configurationStore.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	return nil
}

func (s *HaproxyService) getOrCreateClient(ctx context.Context) (runtime.Runtime, error) {
	s.mu.RLock()
	if s.client != nil {
		client := s.client
		s.mu.RUnlock()
		return client, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil {
		return s.client, nil
	}

	configuration, err := s.configurationService.GetConfiguration()
	if err != nil {
		return nil, fmt.Errorf("unable to load configuration: %w", err)
	}
	if configuration.Haproxy.Socket.Network != "unix" {
		return nil, fmt.Errorf("unsupported HAProxy socket network %q: client-native runtime only supports unix sockets", configuration.Haproxy.Socket.Network)
	}
	if strings.TrimSpace(configuration.Haproxy.Socket.Address) == "" {
		return nil, errors.New("invalid haproxy socket address: value is required")
	}
	instanceName := strings.TrimSpace(configuration.Haproxy.InstanceName)
	if instanceName == "" {
		return nil, errors.New("invalid haproxy.instance_name: value is required")
	}
	if strings.TrimSpace(configuration.Haproxy.ConfigurationFile) == "" {
		return nil, errors.New("invalid haproxy configuration_file: value is required")
	}

	client, err := runtime.New(ctx, runtimeOptions.MasterSocket(configuration.Haproxy.Socket.Address))
	if err != nil {
		return nil, fmt.Errorf("unable to create HAProxy runtime client: %w", err)
	}

	s.configurationFile = configuration.Haproxy.ConfigurationFile
	s.certificatesDir = haproxycfg.ComputeCertificatesDirectory(s.configurationFile)

	// Ensure certificates directory exists
	if err := os.MkdirAll(s.certificatesDir, 0700); err != nil {
		return nil, fmt.Errorf("unable to create certificates directory %q: %w", s.certificatesDir, err)
	}

	s.client = client
	return s.client, nil
}

func (s *HaproxyService) loadConfigurationsLocked(ctx context.Context) ([]HaproxyConfiguration, error) {
	configurations, err := s.configurationStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to load managed configurations from %s store (%s): %w", s.configurationStore.Type(), s.configurationStore.Namespace(), err)
	}
	if len(configurations) > 0 {
		return configurations, nil
	}

	fileConfigurations, err := s.loadConfigurationsFromFileLocked()
	if err != nil {
		return nil, err
	}
	if len(fileConfigurations) == 0 {
		return []HaproxyConfiguration{}, nil
	}

	if err := s.configurationStore.Save(ctx, fileConfigurations); err != nil {
		return nil, fmt.Errorf("unable to seed %s store (%s) from HAProxy managed block: %w", s.configurationStore.Type(), s.configurationStore.Namespace(), err)
	}

	s.logger.Info().
		Int("count", len(fileConfigurations)).
		Str("store", s.configurationStore.Type()).
		Str("namespace", s.configurationStore.Namespace()).
		Msg("Seeded managed configuration store from HAProxy managed block")

	return fileConfigurations, nil
}

func (s *HaproxyService) loadConfigurationsFromFileLocked() ([]HaproxyConfiguration, error) {
	fileContent, err := os.ReadFile(s.configurationFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read HAProxy configuration file %q: %w", s.configurationFile, err)
	}

	start, end, lines := haproxycfg.ManagedBlockBounds(string(fileContent))
	if start == -1 || end == -1 || start >= end {
		return []HaproxyConfiguration{}, nil
	}

	configurations := make([]HaproxyConfiguration, 0)
	for _, line := range lines[start+1 : end] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, haproxycfg.ManagedConfigLine) {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, haproxycfg.ManagedConfigLine))
		if payload == "" {
			continue
		}

		var configuration HaproxyConfiguration
		if err := json.Unmarshal([]byte(payload), &configuration); err != nil {
			return nil, fmt.Errorf("unable to parse managed configuration entry: %w", err)
		}

		configurations = append(configurations, configuration)
	}

	return configurations, nil
}

func (s *HaproxyService) persistAndReloadLocked(ctx context.Context, configurations []HaproxyConfiguration) error {
	return s.persistAndReloadLockedWithStorePolicy(ctx, configurations, true)
}

func (s *HaproxyService) persistAndReloadLockedWithStorePolicy(ctx context.Context, configurations []HaproxyConfiguration, persistStore bool) error {
	// Ensure certificate PEM data is written to files and paths are updated
	for i := range configurations {
		if err := haproxycfg.EnsureCertificatePath(&configurations[i], s.certificatesDir); err != nil {
			return err
		}
	}

	before, err := os.ReadFile(s.configurationFile)
	if err != nil {
		return fmt.Errorf("unable to read HAProxy configuration file %q: %w", s.configurationFile, err)
	}

	updated, err := haproxycfg.MergeManagedBlock(string(before), configurations)
	if err != nil {
		return err
	}

	stat, err := os.Stat(s.configurationFile)
	if err != nil {
		return fmt.Errorf("unable to stat HAProxy configuration file %q: %w", s.configurationFile, err)
	}

	if err := os.WriteFile(s.configurationFile, []byte(updated), stat.Mode().Perm()); err != nil {
		return fmt.Errorf("unable to write HAProxy configuration file %q: %w", s.configurationFile, err)
	}

	if persistStore {
		if err := s.configurationStore.Save(ctx, configurations); err != nil {
			_ = os.WriteFile(s.configurationFile, before, stat.Mode().Perm())
			return fmt.Errorf("unable to persist managed configurations to %s store (%s): %w", s.configurationStore.Type(), s.configurationStore.Namespace(), err)
		}
	}

	logs, err := s.client.Reload()
	if err != nil {
		_ = os.WriteFile(s.configurationFile, before, stat.Mode().Perm())
		if rollbackLogs, rollbackErr := s.client.Reload(); rollbackErr != nil {
			s.logger.Error().Err(rollbackErr).Str("logs", rollbackLogs).Msg("Unable to reload HAProxy after rollback")
		}
		return fmt.Errorf("unable to reload HAProxy after configuration update: %w; logs: %s", err, logs)
	}

	if strings.TrimSpace(logs) != "" {
		s.logger.Debug().Str("logs", logs).Msg("HAProxy reload output")
	}

	return nil
}

func (s *HaproxyService) syncManagedConfigurationsFromStore(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	configurations, err := s.configurationStore.List(ctx)
	if err != nil {
		return fmt.Errorf("unable to list managed configurations from %s store (%s): %w", s.configurationStore.Type(), s.configurationStore.Namespace(), err)
	}
	if len(configurations) == 0 {
		s.logger.Debug().
			Str("store", s.configurationStore.Type()).
			Str("namespace", s.configurationStore.Namespace()).
			Msg("No managed configurations found in cache; skipping HAProxy cache synchronization")
		return nil
	}

	if err := s.persistAndReloadLockedWithStorePolicy(ctx, configurations, false); err != nil {
		return fmt.Errorf("unable to synchronize HAProxy managed configurations from cache: %w", err)
	}

	s.logger.Info().
		Int("count", len(configurations)).
		Str("store", s.configurationStore.Type()).
		Str("namespace", s.configurationStore.Namespace()).
		Msg("Synchronized HAProxy managed configurations from cache")

	return nil
}

func mapNativeStat(stat *models.NativeStat) HaproxyProxyStatus {
	return HaproxyProxyStatus{
		Name:       stat.Name,
		Status:     stat.Stats.Status,
		Current:    int64PointerValue(stat.Stats.Scur),
		Max:        int64PointerValue(stat.Stats.Smax),
		Total:      int64PointerValue(stat.Stats.Stot),
		Rate:       int64PointerValue(stat.Stats.Rate),
		BytesIn:    int64PointerValue(stat.Stats.Bin),
		BytesOut:   int64PointerValue(stat.Stats.Bout),
		LastChange: int64PointerValue(stat.Stats.Lastchg),
	}
}

func int64PointerValue(value *int64) int64 {
	if value == nil {
		return 0
	}

	return *value
}
