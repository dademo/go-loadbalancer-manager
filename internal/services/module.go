package services

import (
	"dademo.fr/loadbalancer-manager/internal/services/configstore"
	"dademo.fr/loadbalancer-manager/internal/services/haproxycfg"
	"go.uber.org/fx"
)

// Module wires service dependencies into the Fx application.
var Module = fx.Module("services",
	fx.Provide(NewLogger),
	fx.Provide(configstore.NewConfigStore[haproxycfg.HaproxyConfiguration]),
	fx.Provide(newHealthService),
	fx.Provide(newHaproxyService),
	fx.Provide(newNetworkSocketsService),
	// GRPC server options providers
	fx.Provide(
		fx.Annotate(
			newMetricsService,
			fx.As(new(GrpcServerOptionsProvider)),
			fx.ResultTags(`group:"grpc_options"`),
		),
	),
	fx.Provide(
		fx.Annotate(
			newGrpcKeepaliveParamsOptionService,
			fx.As(new(GrpcServerOptionsProvider)),
			fx.ResultTags(`group:"grpc_options"`),
		),
	),
	fx.Provide(
		fx.Annotate(
			newGrpcKeepaliveEnforcementPolicyOptionService,
			fx.As(new(GrpcServerOptionsProvider)),
			fx.ResultTags(`group:"grpc_options"`),
		),
	),
	// The GRPC server itself
	fx.Provide(newGrpcServer),
	// Invoke the server to ensure it is instantiated and the lifecycle hooks are registered.
	fx.Invoke(func(*GrpcServerService) {}),
)
