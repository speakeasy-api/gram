package app

import (
	"fmt"

	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/env"
	"github.com/urfave/cli/v2"
)

func newPushCommand() *cli.Command {
	return &cli.Command{
		Name:  "push",
		Usage: "Push a deployment to Gram",
		Description: `
Push a deployment to Gram.

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

NOTE: Names and slugs must be unique across all sources.`[1:],
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
		Action: func(c *cli.Context) error {
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
		},
	}
}
