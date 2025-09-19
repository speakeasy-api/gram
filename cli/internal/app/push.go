package app

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/env"
	"github.com/speakeasy-api/gram/cli/internal/o11y"
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
				Name:     "config",
				Usage:    "Path to the deployment file (relative locations resolve to the deployment file's directory)",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "idempotency-key",
				Usage:    "A unique key to identify this deployment request for idempotency",
				Required: false,
			},
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer cancel()

			logger := PullLogger(ctx)

			projectSlug := c.String("project")

			logger.InfoContext(ctx, "Deploying to project", slog.String("project", projectSlug), slog.String("config", c.String("config")))

			configFilename, err := filepath.Abs(c.String("config"))
			if err != nil {
				return fmt.Errorf("failed to resolve deployment file path: %w", err)
			}

			configFile, err := os.Open(filepath.Clean(configFilename))
			if err != nil {
				return fmt.Errorf("failed to open deployment file: %w", err)
			}
			defer o11y.LogDefer(ctx, logger, func() error {
				return configFile.Close()
			})

			config, err := deploy.NewConfig(configFile, filepath.Dir(configFilename))
			if err != nil {
				return fmt.Errorf("failed to parseread deployment config: %w", err)
			}

			result, err := deploy.CreateDeployment(ctx, logger, deploy.CreateDeploymentRequest{
				Config:         config,
				Project:        projectSlug,
				IdempotencyKey: c.String("idempotency-key"),
			})
			if err != nil {
				return fmt.Errorf("deployment failed: %w", err)
			}

			logger.InfoContext(ctx, "Deployment created successfully", slog.Any("id", result.Deployment.ID))

			return nil
		},
	}
}
