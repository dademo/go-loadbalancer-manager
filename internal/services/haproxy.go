package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"

	"dademo.fr/loadbalancer-manager/internal/repositories"
	"github.com/haproxytech/client-native/v6/models"
	"github.com/haproxytech/client-native/v6/runtime"
	runtimeOptions "github.com/haproxytech/client-native/v6/runtime/options"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

// HaproxyStatus contains grouped HAProxy runtime stats.
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

// HaproxyService manages HAProxy runtime operations and managed configurations.
type HaproxyService struct {
	logger               zerolog.Logger
	configurationService repositories.AppConfigurationService
	mu                   sync.RWMutex
	client               runtime.Runtime
	configurationFile    string
}

var (
	// ErrConfigurationNotFound indicates a requested managed configuration does not exist.
	ErrConfigurationNotFound = errors.New("haproxy configuration not found")
	// ErrConfigurationExists indicates a configuration with the same name already exists.
	ErrConfigurationExists = errors.New("haproxy configuration already exists")
	// ErrInvalidConfiguration indicates the provided configuration content is invalid.
	ErrInvalidConfiguration = errors.New("invalid haproxy configuration")
	// ErrInvalidConfigurationKey indicates the provided configuration key or name is invalid.
	ErrInvalidConfigurationKey = errors.New("invalid haproxy configuration key")
)

// HaproxyConfiguration defines one managed frontend/backend HAProxy configuration.
type HaproxyConfiguration struct {
	Name                string                   `json:"name"`
	FrontendName        string                   `json:"frontend_name"`
	FrontendBindAddress string                   `json:"frontend_bind_address"`
	FrontendBindPort    uint32                   `json:"frontend_bind_port"`
	URL                 string                   `json:"url"`
	LoadBalancing       string                   `json:"load_balancing_strategy"`
	BackendName         string                   `json:"backend_name"`
	Backends            []HaproxyBackendTarget   `json:"backends"`
	TrafficMode         string                   `json:"traffic_mode"`
	AutoHTTPSRedirect   bool                     `json:"auto_https_redirect"`
	TLS                 *HaproxyTLSConfiguration `json:"tls,omitempty"`
}

// HaproxyBackendTarget defines one backend server target.
type HaproxyBackendTarget struct {
	Name                string `json:"name"`
	Address             string `json:"address"`
	Port                uint32 `json:"port"`
	CheckIntervalSecond int64  `json:"check_interval_seconds"`
}

// HaproxyTLSConfiguration defines TLS options for frontend and backend handling.
type HaproxyTLSConfiguration struct {
	Enabled              bool   `json:"enabled"`
	CertificatePath      string `json:"certificate_path"`
	PrivateKeyPath       string `json:"private_key_path"`
	CertificatePEM       string `json:"certificate_pem"`
	PrivateKeyPEM        string `json:"private_key_pem"`
	SkipBackendTLSVerify bool   `json:"skip_backend_tls_verify"`
}

var allowedLoadBalancingStrategies = map[string]struct{}{
	"roundrobin": {},
	"leastconn":  {},
	"source":     {},
}

var allowedTrafficModes = map[string]struct{}{
	"http": {},
	"tcp":  {},
}

const (
	managedBlockStart = "# BEGIN LBM MANAGED CONFIGURATIONS"
	managedBlockEnd   = "# END LBM MANAGED CONFIGURATIONS"
	managedConfigLine = "# LBM_CONFIG "
)

func newHaproxyService(
	logger zerolog.Logger,
	configurationService repositories.AppConfigurationService,
	lifecycle fx.Lifecycle,
) *HaproxyService {
	service := &HaproxyService{
		logger:               logger.With().Str("component", "haproxy_service").Logger(),
		configurationService: configurationService,
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
	if err := validateConfiguration(configuration); err != nil {
		return HaproxyConfiguration{}, err
	}

	if _, err := s.getOrCreateClient(ctx); err != nil {
		return HaproxyConfiguration{}, err
	}

	key, err := normalizeAndValidateConfigurationName(configuration.Name)
	if err != nil {
		return HaproxyConfiguration{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	configurations, err := s.loadConfigurationsLocked()
	if err != nil {
		return HaproxyConfiguration{}, err
	}

	for _, existing := range configurations {
		if normalizeConfigurationName(existing.Name) == key {
			return HaproxyConfiguration{}, fmt.Errorf("configuration %q: %w", configuration.Name, ErrConfigurationExists)
		}
	}

	stored := cloneConfiguration(configuration)
	stored.Name = key
	stored.LoadBalancing = normalizeLoadBalancingStrategy(stored.LoadBalancing)
	stored.TrafficMode = normalizeTrafficMode(stored.TrafficMode)
	configurations = append(configurations, stored)

	if err := s.persistAndReloadLocked(configurations); err != nil {
		return HaproxyConfiguration{}, err
	}

	return cloneConfiguration(stored), nil
}

// ListConfigurations returns all managed HAProxy configurations ordered by name.
func (s *HaproxyService) ListConfigurations(_ context.Context) []HaproxyConfiguration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configurations, err := s.loadConfigurationsLocked()
	if err != nil {
		s.logger.Error().Err(err).Msg("Unable to load HAProxy managed configurations")
		return []HaproxyConfiguration{}
	}

	sort.Slice(configurations, func(i, j int) bool {
		return configurations[i].Name < configurations[j].Name
	})

	for i := range configurations {
		configurations[i] = cloneConfiguration(configurations[i])
	}

	return configurations
}

// GetConfiguration returns a managed HAProxy configuration by name.
func (s *HaproxyService) GetConfiguration(_ context.Context, name string) (HaproxyConfiguration, error) {
	key, err := normalizeAndValidateConfigurationName(name)
	if err != nil {
		return HaproxyConfiguration{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	configurations, err := s.loadConfigurationsLocked()
	if err != nil {
		return HaproxyConfiguration{}, err
	}

	for _, configuration := range configurations {
		if normalizeConfigurationName(configuration.Name) == key {
			return cloneConfiguration(configuration), nil
		}
	}

	return HaproxyConfiguration{}, fmt.Errorf("configuration %q: %w", name, ErrConfigurationNotFound)
}

// UpdateConfiguration validates and persists changes to an existing managed configuration.
func (s *HaproxyService) UpdateConfiguration(ctx context.Context, configuration HaproxyConfiguration) (HaproxyConfiguration, error) {
	if err := validateConfiguration(configuration); err != nil {
		return HaproxyConfiguration{}, err
	}

	if _, err := s.getOrCreateClient(ctx); err != nil {
		return HaproxyConfiguration{}, err
	}

	key := normalizeConfigurationName(configuration.Name)

	s.mu.Lock()
	defer s.mu.Unlock()

	configurations, err := s.loadConfigurationsLocked()
	if err != nil {
		return HaproxyConfiguration{}, err
	}

	index := -1
	for i, current := range configurations {
		if normalizeConfigurationName(current.Name) == key {
			index = i
			break
		}
	}
	if index == -1 {
		return HaproxyConfiguration{}, fmt.Errorf("configuration %q: %w", configuration.Name, ErrConfigurationNotFound)
	}

	updated := cloneConfiguration(configuration)
	updated.Name = key
	updated.LoadBalancing = normalizeLoadBalancingStrategy(updated.LoadBalancing)
	updated.TrafficMode = normalizeTrafficMode(updated.TrafficMode)
	configurations[index] = updated

	if err := s.persistAndReloadLocked(configurations); err != nil {
		return HaproxyConfiguration{}, err
	}

	return cloneConfiguration(updated), nil
}

// DeleteConfiguration removes a managed HAProxy configuration by name.
func (s *HaproxyService) DeleteConfiguration(ctx context.Context, name string) error {
	if _, err := s.getOrCreateClient(ctx); err != nil {
		return err
	}

	key, err := normalizeAndValidateConfigurationName(name)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	configurations, err := s.loadConfigurationsLocked()
	if err != nil {
		return err
	}

	index := -1
	for i, configuration := range configurations {
		if normalizeConfigurationName(configuration.Name) == key {
			index = i
			break
		}
	}
	if index == -1 {
		return fmt.Errorf("configuration %q: %w", name, ErrConfigurationNotFound)
	}

	configurations = append(configurations[:index], configurations[index+1:]...)
	if err := s.persistAndReloadLocked(configurations); err != nil {
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

	s.logger.Info().Str("configuration_file", s.configurationFile).Msg("HAProxy runtime client initialized")
	return nil
}

func (s *HaproxyService) onStop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = nil
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
		return nil, fmt.Errorf("invalid haproxy socket address: value is required")
	}
	if strings.TrimSpace(configuration.Haproxy.ConfigurationFile) == "" {
		return nil, fmt.Errorf("invalid haproxy configuration_file: value is required")
	}

	client, err := runtime.New(ctx, runtimeOptions.MasterSocket(configuration.Haproxy.Socket.Address))
	if err != nil {
		return nil, fmt.Errorf("unable to create HAProxy runtime client: %w", err)
	}

	s.configurationFile = configuration.Haproxy.ConfigurationFile
	s.client = client
	return s.client, nil
}

func (s *HaproxyService) loadConfigurationsLocked() ([]HaproxyConfiguration, error) {
	fileContent, err := os.ReadFile(s.configurationFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read HAProxy configuration file %q: %w", s.configurationFile, err)
	}

	start, end, lines := managedBlockBounds(string(fileContent))
	if start == -1 || end == -1 || start >= end {
		return []HaproxyConfiguration{}, nil
	}

	configurations := make([]HaproxyConfiguration, 0)
	for _, line := range lines[start+1 : end] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, managedConfigLine) {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, managedConfigLine))
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

func (s *HaproxyService) persistAndReloadLocked(configurations []HaproxyConfiguration) error {
	before, err := os.ReadFile(s.configurationFile)
	if err != nil {
		return fmt.Errorf("unable to read HAProxy configuration file %q: %w", s.configurationFile, err)
	}

	updated, err := mergeManagedBlock(string(before), configurations)
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

func managedBlockBounds(content string) (int, int, []string) {
	lines := strings.Split(content, "\n")
	start := -1
	end := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case managedBlockStart:
			if start == -1 {
				start = i
			}
		case managedBlockEnd:
			if start != -1 && end == -1 {
				end = i
			}
		}
	}

	return start, end, lines
}

func mergeManagedBlock(base string, configurations []HaproxyConfiguration) (string, error) {
	block, err := renderManagedBlock(configurations)
	if err != nil {
		return "", err
	}

	start, end, lines := managedBlockBounds(base)
	if start != -1 && end != -1 && start < end {
		replaced := append([]string{}, lines[:start]...)
		replaced = append(replaced, strings.Split(block, "\n")...)
		replaced = append(replaced, lines[end+1:]...)
		return strings.Join(replaced, "\n"), nil
	}

	trimmed := strings.TrimRight(base, "\n")
	if trimmed == "" {
		return block + "\n", nil
	}

	return trimmed + "\n\n" + block + "\n", nil
}

func renderManagedBlock(configurations []HaproxyConfiguration) (string, error) {
	sorted := make([]HaproxyConfiguration, 0, len(configurations))
	for _, configuration := range configurations {
		cloned := cloneConfiguration(configuration)
		cloned.Name = normalizeConfigurationName(cloned.Name)
		sorted = append(sorted, cloned)
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	lines := []string{managedBlockStart}
	for _, configuration := range sorted {
		payload, err := json.Marshal(configuration)
		if err != nil {
			return "", fmt.Errorf("unable to serialize managed configuration %q: %w", configuration.Name, err)
		}
		lines = append(lines, managedConfigLine+string(payload))
	}

	for _, configuration := range sorted {
		rendered, err := renderConfigurationSections(configuration)
		if err != nil {
			return "", err
		}
		lines = append(lines, rendered...)
	}

	lines = append(lines, managedBlockEnd)
	return strings.Join(lines, "\n"), nil
}

func renderConfigurationSections(configuration HaproxyConfiguration) ([]string, error) {
	trafficMode := normalizeTrafficMode(configuration.TrafficMode)
	loadBalancing := normalizeLoadBalancingStrategy(configuration.LoadBalancing)

	bindLine := fmt.Sprintf("    bind %s:%d", strings.TrimSpace(configuration.FrontendBindAddress), configuration.FrontendBindPort)
	if configuration.TLS != nil && configuration.TLS.Enabled {
		certificatePath := strings.TrimSpace(configuration.TLS.CertificatePath)
		if certificatePath == "" {
			return nil, fmt.Errorf("tls.enabled=true requires tls.certificate_path when rendering HAProxy config: %w", ErrInvalidConfiguration)
		}
		bindLine = fmt.Sprintf("%s ssl crt %s", bindLine, certificatePath)
	}

	lines := []string{
		fmt.Sprintf("frontend %s", configuration.FrontendName),
		bindLine,
		fmt.Sprintf("    mode %s", trafficMode),
		fmt.Sprintf("    default_backend %s", configuration.BackendName),
	}
	if configuration.AutoHTTPSRedirect {
		lines = append(lines, "    http-request redirect scheme https code 301 if !{ ssl_fc }")
	}

	lines = append(lines,
		fmt.Sprintf("backend %s", configuration.BackendName),
		fmt.Sprintf("    mode %s", trafficMode),
		fmt.Sprintf("    balance %s", loadBalancing),
	)

	for _, backend := range configuration.Backends {
		serverLine := fmt.Sprintf(
			"    server %s %s:%d check inter %ds",
			backend.Name,
			strings.TrimSpace(backend.Address),
			backend.Port,
			backend.CheckIntervalSecond,
		)
		if configuration.TLS != nil && configuration.TLS.SkipBackendTLSVerify {
			serverLine += " ssl verify none"
		}
		lines = append(lines, serverLine)
	}

	return lines, nil
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

func validateConfiguration(configuration HaproxyConfiguration) error {
	key, err := normalizeAndValidateConfigurationName(configuration.Name)
	if err != nil {
		return err
	}
	if key == "" {
		return ErrInvalidConfigurationKey
	}

	if strings.TrimSpace(configuration.FrontendName) == "" {
		return fmt.Errorf("frontend_name is required: %w", ErrInvalidConfiguration)
	}

	if strings.TrimSpace(configuration.BackendName) == "" {
		return fmt.Errorf("backend_name is required: %w", ErrInvalidConfiguration)
	}

	trafficMode := normalizeTrafficMode(configuration.TrafficMode)
	if trafficMode == "" {
		return fmt.Errorf("traffic_mode is required: %w", ErrInvalidConfiguration)
	}
	if _, ok := allowedTrafficModes[trafficMode]; !ok {
		return fmt.Errorf("traffic_mode must be one of http, tcp: %w", ErrInvalidConfiguration)
	}

	if trafficMode == "http" {
		if strings.TrimSpace(configuration.URL) == "" {
			return fmt.Errorf("url is required in http mode: %w", ErrInvalidConfiguration)
		}

		parsedURL, err := url.ParseRequestURI(strings.TrimSpace(configuration.URL))
		if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
			return fmt.Errorf("url must be a valid absolute URL: %w", ErrInvalidConfiguration)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return fmt.Errorf("url must use http or https scheme in http mode: %w", ErrInvalidConfiguration)
		}
	}

	loadBalancing := strings.ToLower(strings.TrimSpace(configuration.LoadBalancing))
	if loadBalancing == "" {
		return fmt.Errorf("load_balancing_strategy is required: %w", ErrInvalidConfiguration)
	}
	if _, ok := allowedLoadBalancingStrategies[loadBalancing]; !ok {
		return fmt.Errorf("load_balancing_strategy must be one of roundrobin, leastconn, source: %w", ErrInvalidConfiguration)
	}

	if configuration.AutoHTTPSRedirect {
		if trafficMode != "http" {
			return fmt.Errorf("auto_https_redirect can only be enabled in http mode: %w", ErrInvalidConfiguration)
		}
		if configuration.TLS == nil || !configuration.TLS.Enabled {
			return fmt.Errorf("auto_https_redirect requires tls.enabled=true: %w", ErrInvalidConfiguration)
		}
	}

	if err := validateTLSConfiguration(configuration.TLS); err != nil {
		return err
	}

	if strings.TrimSpace(configuration.FrontendBindAddress) == "" {
		return fmt.Errorf("frontend_bind_address is required: %w", ErrInvalidConfiguration)
	}

	if configuration.FrontendBindPort == 0 || configuration.FrontendBindPort > 65535 {
		return fmt.Errorf("frontend_bind_port must be between 1 and 65535: %w", ErrInvalidConfiguration)
	}

	if len(configuration.Backends) == 0 {
		return fmt.Errorf("backends must contain at least one backend target: %w", ErrInvalidConfiguration)
	}

	backendNames := make(map[string]struct{}, len(configuration.Backends))
	for i, backend := range configuration.Backends {
		backendName := strings.TrimSpace(backend.Name)
		if backendName == "" {
			return fmt.Errorf("backends[%d].name is required: %w", i, ErrInvalidConfiguration)
		}

		normalizedBackendName := strings.ToLower(backendName)
		if _, exists := backendNames[normalizedBackendName]; exists {
			return fmt.Errorf("backends[%d].name duplicates an existing backend target: %w", i, ErrInvalidConfiguration)
		}
		backendNames[normalizedBackendName] = struct{}{}

		if strings.TrimSpace(backend.Address) == "" {
			return fmt.Errorf("backends[%d].address is required: %w", i, ErrInvalidConfiguration)
		}

		if backend.Port == 0 || backend.Port > 65535 {
			return fmt.Errorf("backends[%d].port must be between 1 and 65535: %w", i, ErrInvalidConfiguration)
		}

		if backend.CheckIntervalSecond <= 0 {
			return fmt.Errorf("backends[%d].check_interval_seconds must be > 0: %w", i, ErrInvalidConfiguration)
		}
	}

	return nil
}

func normalizeAndValidateConfigurationName(name string) (string, error) {
	normalized := normalizeConfigurationName(name)
	if normalized == "" {
		return "", fmt.Errorf("name is required: %w", ErrInvalidConfigurationKey)
	}

	return normalized, nil
}

func normalizeConfigurationName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeTrafficMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func normalizeLoadBalancingStrategy(strategy string) string {
	return strings.ToLower(strings.TrimSpace(strategy))
}

func validateTLSConfiguration(configuration *HaproxyTLSConfiguration) error {
	if configuration == nil {
		return nil
	}

	certificatePath := strings.TrimSpace(configuration.CertificatePath)
	privateKeyPath := strings.TrimSpace(configuration.PrivateKeyPath)
	certificatePEM := strings.TrimSpace(configuration.CertificatePEM)
	privateKeyPEM := strings.TrimSpace(configuration.PrivateKeyPEM)

	hasPathPair := certificatePath != "" || privateKeyPath != ""
	hasPEMPair := certificatePEM != "" || privateKeyPEM != ""

	if !configuration.Enabled {
		if hasPathPair || hasPEMPair {
			return fmt.Errorf("tls certificates cannot be provided when tls.enabled=false: %w", ErrInvalidConfiguration)
		}
		return nil
	}

	if hasPathPair {
		if certificatePath == "" || privateKeyPath == "" {
			return fmt.Errorf("tls.certificate_path and tls.private_key_path must both be provided: %w", ErrInvalidConfiguration)
		}
	}

	if hasPEMPair {
		if certificatePEM == "" || privateKeyPEM == "" {
			return fmt.Errorf("tls.certificate_pem and tls.private_key_pem must both be provided: %w", ErrInvalidConfiguration)
		}
	}

	if hasPathPair && hasPEMPair {
		return fmt.Errorf("provide either tls path pair or tls pem pair, not both: %w", ErrInvalidConfiguration)
	}

	if !hasPathPair && !hasPEMPair {
		return fmt.Errorf("tls.enabled=true requires certificate/key configuration: %w", ErrInvalidConfiguration)
	}

	return nil
}

func cloneConfiguration(configuration HaproxyConfiguration) HaproxyConfiguration {
	cloned := configuration
	if len(configuration.Backends) > 0 {
		cloned.Backends = append([]HaproxyBackendTarget(nil), configuration.Backends...)
	}
	if configuration.TLS != nil {
		tls := *configuration.TLS
		cloned.TLS = &tls
	}

	return cloned
}
