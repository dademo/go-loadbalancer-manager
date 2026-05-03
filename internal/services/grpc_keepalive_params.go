package services

import (
	"time"

	"dademo.fr/loadbalancer-manager/internal/repositories"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

type grpcKeepaliveParamsOptionService struct {
	logger               zerolog.Logger
	configurationService repositories.AppConfigurationService
}

func newGrpcKeepaliveParamsOptionService(
	logger zerolog.Logger,
	configurationService repositories.AppConfigurationService) GrpcServerOptionsProvider {

	return &grpcKeepaliveParamsOptionService{
		logger:               logger.With().Str("component", "grpc_keepalive_params_option_service").Logger(),
		configurationService: configurationService,
	}
}

func (s *grpcKeepaliveParamsOptionService) GetOption() (grpc.ServerOption, error) {
	// configuration, err := s.configurationService.GetConfiguration()
	_, err := s.configurationService.GetConfiguration()
	if err != nil {
		s.logger.Error().Err(err).Msg("Unable to get configuration")
		return nil, err
	}

	// Define Keepalive parameters
	kaep := keepalive.ServerParameters{
		MaxConnectionIdle:     15 * time.Second, // If idle for 15s, send GOAWAY
		MaxConnectionAge:      30 * time.Minute, // Force reconnect after 30m
		MaxConnectionAgeGrace: 5 * time.Second,  // Grace period before closing
		Time:                  10 * time.Second, // Ping the client every 10s to keep connection alive
		Timeout:               1 * time.Second,  // Wait 1s for ping ack
	}

	return grpc.KeepaliveParams(kaep), nil
}
