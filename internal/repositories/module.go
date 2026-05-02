package repositories

import (
	"go.uber.org/fx"
)

// Module wires repository dependencies into the Fx application.
var Module = fx.Module("repositories",
	fx.Provide(NewCliRepository),
	fx.Provide(newConfigurationService),
)
