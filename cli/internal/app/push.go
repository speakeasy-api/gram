package app

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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
				Name:     "project",
				Usage:    "The Gram project to push to",
				EnvVars:  []string{env.VarNameProjectSlug},
				Required: true,
			},
			&cli.PathFlag{
				Name:     "file",
				Usage:    "Path to the deployment file (relative locations resolve to the deployment file's directory)",
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer cancel()

			logger := PullLogger(ctx)

			projectSlug := c.String("project")
			filePath := c.Path("file")

			logger.InfoContext(ctx, "Deploying to project", slog.String("project", projectSlug), slog.String("file", filePath))

			result, err := deploy.CreateDeploymentFromFile(ctx, logger, deploy.CreateDeploymentFromFileRequest{
				FilePath: filePath,
				Project:  projectSlug,
			})
			if err != nil {
				return fmt.Errorf("deployment failed: %w", err)
			}

			logger.InfoContext(ctx, "Deployment created successfully", slog.Any("id", result.Deployment.ID))

			return nil
		},
	}
}
