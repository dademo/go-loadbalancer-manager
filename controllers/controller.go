package controllers

import (
	"context"

	"dademo.fr/loadbalancer-manager/fx"
	"dademo.fr/loadbalancer-manager/repositories"
	"github.com/rs/zerolog"
)

type MainRunnerService struct {
	logger               *zerolog.Logger
	configurationService *repositories.AppConfigurationService
}

func newMainRunner(
	logger *zerolog.Logger,
	configurationService *repositories.AppConfigurationService,
) fx.MainRunner {
	return MainRunnerService{
		logger:               logger,
		configurationService: configurationService,
	}
}

func (m MainRunnerService) Run(ctx context.Context) error {
	// configuration, err := m.configurationService.GetConfiguration()
	_, err := m.configurationService.GetConfiguration()
	if err != nil {
		m.logger.Error().Err(err).Msg("Unable to get configuration")
		return err
	}

	// Main logic goes here
	return nil
}
