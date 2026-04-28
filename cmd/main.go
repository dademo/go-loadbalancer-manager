package main

import (
	"dademo.fr/loadbalancer-manager/controllers"
	"dademo.fr/loadbalancer-manager/repositories"
	"dademo.fr/loadbalancer-manager/services"
	"github.com/ipfans/fxlogger"
	"go.uber.org/fx"

	appFx "dademo.fr/loadbalancer-manager/fx"
)

func main() {

	fx.New(
		fx.WithLogger(fxlogger.WithZerolog(*services.NewLogger())),
		appFx.Module,

		repositories.Module,
		services.Module,
		controllers.Module,
	).Run()
}
