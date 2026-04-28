package main

import (
	"dademo.fr/loadbalancer-manager/internal/controllers"
	"dademo.fr/loadbalancer-manager/internal/repositories"
	"dademo.fr/loadbalancer-manager/internal/services"
	"github.com/ipfans/fxlogger"
	"go.uber.org/fx"
)

// Version is injected at build time with -ldflags.
var Version = "dev"

func main() {
	services.NewLogger().Info().Str("version", Version).Msg("Starting application")

	fx.New(
		fx.WithLogger(fxlogger.WithZerolog(*services.NewLogger())),

		repositories.Module,
		services.Module,
		controllers.Module,
	).Run()
}
