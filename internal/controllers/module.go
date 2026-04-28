package controllers

import (
	"go.uber.org/fx"
)

var Module = fx.Module("controller",
	fx.Provide(newMainRunner),
)
