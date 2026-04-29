package services

import (
	"go.uber.org/fx"
)

var Module = fx.Module("services",
	fx.Provide(NewLogger),
	fx.Provide(newHealthService),
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
			newKeepaliveEnforcementPolicyOptionService,
			fx.As(new(GrpcServerOptionsProvider)),
			fx.ResultTags(`group:"grpc_options"`),
		),
	),
	fx.Provide(
		fx.Annotate(
			newKeepaliveEnforcementPolicyOptionService,
			fx.As(new(GrpcServerOptionsProvider)),
			fx.ResultTags(`group:"grpc_options"`),
		),
	),
	// The GRPC server itself
	fx.Provide(newGrpcServer),
	// Invoke the server to ensure it is instantiated and the lifecycle hooks are registered.
	fx.Invoke(func(*GrpcServerService) {}),
)
