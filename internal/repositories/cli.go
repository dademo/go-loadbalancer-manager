package repositories

import (
	"flag"
	"fmt"
	"os"
	"path"
)

const (
	configurationFileName = "configuration.yaml"
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
	flag.StringVar(&cliArgs.confFilePath, "config", progDefaultConfPath(), "Configuration file path")

	flag.Parse()

	return &cliArgs
}

func progDefaultConfPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to determine current path: %s\n", err)
		os.Exit(1)
	}

	return path.Join(cwd, configurationFileName)
}
