package fx

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/fx"
)

type MainRunner interface {
	Run(context.Context) error
}

func fxConfigure(lifecycle fx.Lifecycle, shutdown fx.Shutdowner, mainRunner MainRunner) {
	var err error

	lifecycle.Append(fx.StartStopHook(
		func(ctx context.Context) error {
			r := func() {
				exitCode := 0

				err = mainRunner.Run(ctx)
				if err != nil {
					exitCode = 1
				}
				shutdown.Shutdown(fx.ExitCode(exitCode))
			}
			go r()
			return nil
		},
		func(ctx context.Context) error {
			return err
		},
	))

	lifecycle.Append(fx.StartHook(
		func(ctx context.Context) error {
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

			handler := func() {
				<-sigs
				shutdown.Shutdown(fx.ExitCode(1))
			}
			go handler()

			return nil
		},
	))
}

var Module = fx.Module("fx",
	fx.Invoke(fxConfigure),
)
