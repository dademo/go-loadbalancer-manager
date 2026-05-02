// Package repositories provides configuration and CLI repositories.
package repositories

import (
	"flag"
)

// CLIRepository reads and caches CLI arguments.
type CLIRepository struct {
	cachedCLIArgs *CLIArgs
}

// CLIArgs holds the parsed command line arguments.
type CLIArgs struct {
	isDebug      bool
	confFilePath string
}

// NewCliRepository builds a CLI repository instance.
func NewCliRepository() *CLIRepository {
	return &CLIRepository{
		cachedCLIArgs: nil,
	}
}

// GetConfFilePath returns the optional external configuration file path.
func (c *CLIRepository) GetConfFilePath() string {
	return c.getCliArgs().confFilePath
}

func (c *CLIRepository) getCliArgs() *CLIArgs {
	if c.cachedCLIArgs == nil {
		c.cachedCLIArgs = parseCliArgs()
	}

	return c.cachedCLIArgs
}

func parseCliArgs() *CLIArgs {
	var cliArgs CLIArgs

	flag.BoolVar(&cliArgs.isDebug, "debug", false, "Enable debugging")
	flag.StringVar(&cliArgs.confFilePath, "config", "", "Configuration file path (optional)")

	flag.Parse()

	return &cliArgs
}
