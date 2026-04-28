package controllers

import (
	"context"
	"net"

	"dademo.fr/loadbalancer-manager/internal/repositories"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

type GRPCServerService struct {
	logger               *zerolog.Logger
	configurationService *repositories.AppConfigurationService
	grpc                 *grpc.Server
}

func newGRPCServer(
	logger *zerolog.Logger,
	configurationService *repositories.AppConfigurationService,
	lifecycle fx.Lifecycle,
) (*GRPCServerService, error) {

	service := GRPCServerService{
		logger:               logger,
		configurationService: configurationService,
		grpc:                 nil,
	}
	if err := service.configure(lifecycle); err != nil {
		return nil, err
	}

	return &service, nil
}

func (g *GRPCServerService) configure(lifecycle fx.Lifecycle) error {
	// configuration, err := m.configurationService.GetConfiguration()
	_, err := g.configurationService.GetConfiguration()
	if err != nil {
		g.logger.Error().Err(err).Msg("Unable to get configuration")
		return err
	}

	// Create a new gRPC server instance
	g.grpc = grpc.NewServer()

	// Configure the standard gRPC Health Service
	healthCheck := health.NewServer()
	grpc_health_v1.RegisterHealthServer(g.grpc, healthCheck)

	// Optional: Enable reflection to allow tools like Postman or grpcurl to inspect services
	reflection.Register(g.grpc)

	// Define the application lifecycle using Fx hooks
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", ":50051")
			if err != nil {
				return err
			}

			g.logger.Info().Str("address", ":50051").Msg("Starting gRPC server")

			// Set the global health status to SERVING
			healthCheck.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

			// Run the server in a separate goroutine to avoid blocking Fx startup
			go func() {
				if err := g.grpc.Serve(ln); err != nil {
					g.logger.Error().Err(err).Msg("Server execution failed")
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			g.logger.Info().Msg("Gracefully stopping gRPC server")
			// Update health status to NOT_SERVING before shutting down
			healthCheck.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
			g.grpc.GracefulStop()
			return nil
		},
	})

	return nil
}
