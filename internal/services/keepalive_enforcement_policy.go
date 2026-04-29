package services

import (
	"time"

	"dademo.fr/loadbalancer-manager/internal/repositories"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

type keepaliveEnforcementPolicyOptionService struct {
	logger               zerolog.Logger
	configurationService repositories.AppConfigurationService
}

func newKeepaliveEnforcementPolicyOptionService(
	logger zerolog.Logger,
	configurationService repositories.AppConfigurationService) GrpcServerOptionsProvider {

	return &keepaliveEnforcementPolicyOptionService{
		logger:               logger.With().Str("component", "keepalive_enforcement_policy_option_service").Logger(),
		configurationService: configurationService,
	}
}

func (s *keepaliveEnforcementPolicyOptionService) GetOption() (grpc.ServerOption, error) {
	// configuration, err := s.configurationService.GetConfiguration()
	_, err := s.configurationService.GetConfiguration()
	if err != nil {
		s.logger.Error().Err(err).Msg("Unable to get configuration")
		return nil, err
	}

	// Define Keepalive parameters
	kaep := keepalive.EnforcementPolicy{
		MinTime:             5 * time.Second, // Minimum time between client pings
		PermitWithoutStream: true,            // Allow pings even without active RPCs
	}

	return grpc.KeepaliveEnforcementPolicy(kaep), nil
}
