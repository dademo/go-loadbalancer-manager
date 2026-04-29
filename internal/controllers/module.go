package controllers

import (
	"go.uber.org/fx"
)

var Module = fx.Module("controller",
	fx.Provide(newGrpcServer),
	// Invoke the server to ensure it is instantiated and the lifecycle hooks are registered
	fx.Invoke(func(*GrpcServerService) {}),
)
