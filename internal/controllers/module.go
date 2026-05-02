package controllers

import (
	"go.uber.org/fx"
)

// Module wires controller dependencies into the Fx application.
var Module = fx.Module("controller",
	fx.Provide(newHaproxyStatusController),
	fx.Invoke(func(*HaproxyStatusController) {}),
)
