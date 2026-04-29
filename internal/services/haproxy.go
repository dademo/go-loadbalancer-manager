package services

import (
	"context"
	"fmt"
	"sync"

	"dademo.fr/loadbalancer-manager/internal/repositories"
	"github.com/haproxytech/client-native/v6/models"
	"github.com/haproxytech/client-native/v6/runtime"
	runtimeOptions "github.com/haproxytech/client-native/v6/runtime/options"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

type HaproxyStatus struct {
	Frontends []HaproxyProxyStatus `json:"frontends"`
	Backends  []HaproxyProxyStatus `json:"backends"`
}

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

type HaproxyService struct {
	logger               zerolog.Logger
	configurationService repositories.AppConfigurationService
	mu                   sync.RWMutex
	client               runtime.Runtime
}

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

func (s *HaproxyService) onStart(ctx context.Context) error {
	_, err := s.getOrCreateClient(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Unable to initialize HAProxy runtime client")
		return err
	}

	s.logger.Info().Msg("HAProxy runtime client initialized")
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

	client, err := runtime.New(ctx, runtimeOptions.Socket(configuration.Haproxy.Socket.Address))
	if err != nil {
		return nil, fmt.Errorf("unable to create HAProxy runtime client: %w", err)
	}

	s.client = client
	return s.client, nil
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
