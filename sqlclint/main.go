// Command sqlclint checks that sqlc queries are bounded to a tenant.
//
// It parses each query with the same Postgres grammar sqlc uses, resolves which
// tenancy column every referenced table requires, and reports queries that never
// bind one. Queries that genuinely cannot be tenant-bounded carry an
// annotation naming the category that explains why; pre-existing violations are
// grandfathered in an ignore file that can only shrink.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

func main() {
	cmd := newCommand()
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "sqlclint:", err)
		os.Exit(1)
	}
}

func newCommand() *cli.Command {
	// configPath is resolved before the YAML value sources read from it, so
	// --config selects the file that supplies every other flag's default.
	var configPath string

	return &cli.Command{
		Name:  "sqlclint",
		Usage: "check that sqlc queries are bounded to a tenant",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Usage:       "path to a YAML config file supplying any of the flags below",
				Value:       "sqlclint.yaml",
				Destination: &configPath,
				Sources:     cli.EnvVars("SQLCLINT_CONFIG"),
			},
		},
		Commands: []*cli.Command{
			newRunCommand(&configPath),
			newRulesCommand(),
			newRuleCommand(),
		},
	}
}
