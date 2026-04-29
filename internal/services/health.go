package services

import (
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type HealthService struct {
	logger zerolog.Logger
	health *health.Server
}

func newHealthService(logger zerolog.Logger) HealthService {
	return HealthService{
		logger: logger.With().Str("component", "health_service").Logger(),
		health: health.NewServer(),
	}
}

func (m *HealthService) Configure(s *grpc.Server) {
	grpc_health_v1.RegisterHealthServer(s, m.health)
}

func (m *HealthService) SetServingStatus(service string, status grpc_health_v1.HealthCheckResponse_ServingStatus) {
	m.logger.Info().Str("service", service).Int32("status", int32(status)).Msg("Setting health status")
	m.health.SetServingStatus(service, status)
}
