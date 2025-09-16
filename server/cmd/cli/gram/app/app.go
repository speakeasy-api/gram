package app

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/cmd/cli/gram/deploy"
	"github.com/speakeasy-api/gram/server/cmd/cli/gram/env"
	"github.com/speakeasy-api/gram/server/cmd/cli/gram/version"
	"github.com/urfave/cli/v2"
)

var (
	pushUsageDescription = `Push a deployment to Gram.

Sample deployment file
======================
{
  "schema_version": "1.0.0",
  "type": "deployment",
  "sources": [
    {
      "type": "openapiv3",
      "location": "/path/to/spec.yaml",
      "name": "My API",
      "slug": "my-api"
    }
  ]
}

NOTE: Names and slugs must be unique across all sources.
`
)

type CLI interface {
	Run(args []string) error
}

type cliApp struct {
	app *cli.App
}

func NewCLI() CLI {
	app := &cli.App{
		Name:    "gram",
		Usage:   "Remote MCP management",
		Version: version.Version,
		Commands: []*cli.Command{
			{
				Name:        "push",
				Usage:       "Push a deployment to Gram",
				Description: pushUsageDescription,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "file",
						Aliases:  []string{"f"},
						Usage:    "Path to the deployment file (relative locations resolve to the deployment file's directory)",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "project",
						Aliases: []string{"p"},
						EnvVars: []string{env.VarNameProjectSlug},
						Usage: fmt.Sprintf(
							"Project slug (falls back to %s environment variable)",
							env.VarNameProjectSlug),
						Required: true,
					},
				},
				Action: pushAction,
			},
		},
	}

	return &cliApp{app: app}
}

func (c *cliApp) Run(args []string) error {
	if err := c.app.Run(args); err != nil {
		// Extract the command name from args if available
		commandName := c.app.Name
		if len(args) > 1 {
			commandName = args[1]
		}
		return fmt.Errorf("%s failed: %w", commandName, err)
	}

	return nil
}

func pushAction(c *cli.Context) error {
	filePath := c.String("file")
	projectSlug := c.String("project")

	if env.APIKeyMissing() {
		return fmt.Errorf(
			"API key not set. Please set the %s environment variable and retry",
			env.VarNameProducerKey,
		)
	}

	fmt.Printf("Deploying to project: %s\n", projectSlug)

	req := deploy.CreateDeploymentFromFileRequest{
		FilePath: filePath,
		Project:  projectSlug,
	}
	result, err := deploy.CreateDeploymentFromFile(req)
	if err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	fmt.Printf("Deployment created successfully: %+v\n", result.Deployment)
	return nil
}
