package repositories

import (
	"flag"
)

type CLIRepository struct {
	cachedCLIArgs *CLIArgs
}

type CLIArgs struct {
	isDebug      bool
	confFilePath string
}

func NewCliRepository() *CLIRepository {
	return &CLIRepository{
		cachedCLIArgs: nil,
	}
}

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
