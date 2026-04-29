package repositories

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

type AppConfiguration struct {
}

type AppConfigurationService struct {
	logger              zerolog.Logger
	cli                 CLIRepository
	cachedConfiguration *AppConfiguration
}

func newConfigurationService(logger zerolog.Logger, cli CLIRepository) AppConfigurationService {
	return AppConfigurationService{
		logger:              logger.With().Str("component", "configuration_service").Logger(),
		cli:                 cli,
		cachedConfiguration: nil,
	}
}

func (a *AppConfigurationService) GetConfiguration() (*AppConfiguration, error) {

	if a.cachedConfiguration == nil {
		configurationFileContent, err := os.ReadFile(a.cli.GetConfFilePath())
		if err != nil {
			return nil, fmt.Errorf("unable to read configuration file: %w", err)
		}

		var configuration AppConfiguration

		err = yaml.Unmarshal(configurationFileContent, &configuration)
		if err != nil {
			return nil, fmt.Errorf("unable to unmarshal configuration; %w", err)
		}

		a.cachedConfiguration = &configuration
	}

	return a.cachedConfiguration, nil
}
