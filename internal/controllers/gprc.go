package controllers

import (
	"context"
	"net"

	"dademo.fr/loadbalancer-manager/internal/repositories"
	"dademo.fr/loadbalancer-manager/internal/services"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

type GrpcServerService struct {
	logger               zerolog.Logger
	configurationService repositories.AppConfigurationService
	healthService        services.HealthService
	grpc                 *grpc.Server
}

func newGrpcServer(
	logger zerolog.Logger,
	configurationService repositories.AppConfigurationService,
	lifecycle fx.Lifecycle,
	healthService services.HealthService,
	grpcServerOptionsProviders struct {
		fx.In
		Options []services.GrpcServerOptionsProvider `group:"grpc_options"`
	}) (*GrpcServerService, error) {

	grpcServerOptions, err := getGrpcServerOptions(grpcServerOptionsProviders.Options)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get gRPC server options")
		return nil, err
	}

	service := GrpcServerService{
		logger:               logger,
		configurationService: configurationService,
		healthService:        healthService,
		grpc:                 grpc.NewServer(grpcServerOptions...),
	}
	if err := service.configure(lifecycle); err != nil {
		return nil, err
	}

	return &service, nil
}

func (g *GrpcServerService) configure(lifecycle fx.Lifecycle) error {
	// Integrate health service with gRPC server
	g.healthService.Configure(g.grpc)

	// Optional: Enable reflection to allow tools like Postman or grpcurl to inspect services
	reflection.Register(g.grpc)

	// Define the application lifecycle using Fx hooks
	lifecycle.Append(g.getInitializationFxLifecycleHook())

	return nil
}

func (g *GrpcServerService) getInitializationFxLifecycleHook() fx.Hook {

	return fx.Hook{
		OnStart: g.onStart,
		OnStop:  g.onStop,
	}
}

func (g *GrpcServerService) onStart(ctx context.Context) error {
	ln, err := net.Listen("tcp", ":50051")
	if err != nil {
		return err
	}

	// TODO
	g.logger.Info().Str("address", ":50051").Msg("Starting gRPC server")

	// Set the global health status to SERVING
	g.healthService.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Run the server in a separate goroutine to avoid blocking Fx startup
	go func() {
		if err := g.grpc.Serve(ln); err != nil {
			g.logger.Error().Err(err).Msg("Server execution failed")
		}
	}()
	return nil
}

func (g *GrpcServerService) onStop(ctx context.Context) error {
	g.logger.Info().Msg("Gracefully stopping gRPC server")
	// Update health status to NOT_SERVING before shutting down
	g.healthService.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	g.grpc.GracefulStop()
	return nil
}

func getGrpcServerOptions(providers []services.GrpcServerOptionsProvider) ([]grpc.ServerOption, error) {
	var options []grpc.ServerOption
	for _, provider := range providers {
		option, err := provider.GetOption()
		if err != nil {
			return nil, err
		}
		options = append(options, option)
	}
	return options, nil
}
