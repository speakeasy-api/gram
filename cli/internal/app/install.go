package app

import (
	"github.com/urfave/cli/v2"
)

func newInstallCommand() *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "Install Gram toolsets as MCP servers in various clients",
		Subcommands: []*cli.Command{
			newInstallCursorCommand(),
			newInstallClaudeCodeCommand(),
		},
	}
}
