package app

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/o11y"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
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
			flags.APIEndpoint(),
			flags.APIKey(),
			flags.Project(),
			flags.Org(),
			&cli.PathFlag{
				Name:     "config",
				Usage:    "Path to the deployment file",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "method",
				Usage: "When set to 'replace', the deployment replaces any existing deployment artifacts in Gram projects. When set to 'merge', the deployment merges with any existing deployment artifacts in Gram project.",
				Action: func(ctx *cli.Context, s string) error {
					if s != "replace" && s != "merge" {
						return fmt.Errorf("invalid method: %s (allowed values: replace, merge)", s)
					}
					return nil
				},
				Value: "merge",
			},
			&cli.StringFlag{
				Name:     "idempotency-key",
				Usage:    "A unique key to identify this deployment request for idempotency",
				Required: false,
			},
			&cli.BoolFlag{
				Name:  "skip-poll",
				Usage: "Skip polling for deployment completion and return immediately",
				Value: false,
			},
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer cancel()

			logger := logging.PullLogger(ctx)
			prof := profile.FromContext(ctx)

			workflowParams, err := workflow.ResolveParams(c, prof)
			if err != nil {
				return fmt.Errorf("failed to resolve workflow params: %w", err)
			}

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

			config, err := deploy.NewConfig(configFile, configFilename)
			if err != nil {
				return fmt.Errorf("failed to parseread deployment config: %w", err)
			}

			logger.InfoContext(
				ctx,
				"Deploying to project",
				slog.String("project", workflowParams.ProjectSlug),
				slog.String("config", c.String("config")),
			)

			result := workflow.New(ctx, logger, workflowParams).
				UploadAssets(ctx, config.Sources)
			if c.String("method") == "replace" {
				result = result.CreateDeployment(ctx, c.String("idempotency-key"))
			} else {
				result = result.EvolveDeployment(ctx)
			}

			if !c.Bool("skip-poll") {
				result.Poll(ctx)
			}

			if result.Failed() {
				if result.Deployment != nil {
					statusCommand := fmt.Sprintf(
						"gram status --id %s",
						result.Deployment.ID,
					)

					result.Logger.WarnContext(
						ctx,
						"Poll failed.",
						slog.String("command", statusCommand),
						slog.String("error", result.Err.Error()),
					)
					return nil
				}

				return fmt.Errorf("failed to push deploy: %w", result.Err)
			}

			slogID := slog.String("deployment_id", result.Deployment.ID)
			status := result.Deployment.Status

			deploymentLogsURL := fmt.Sprintf("https://localhost:5173/%s/%s/deployments/%s", workflowParams.OrgSlug, workflowParams.ProjectSlug, result.Deployment.ID)

			switch status {
			case "completed":
				logger.InfoContext(ctx, "Deployment succeeded", slogID, slog.String("logs_url", deploymentLogsURL))
				return nil
			case "failed":
				logger.ErrorContext(ctx, "Deployment failed", slogID)
				return fmt.Errorf("Deployment failed. Check the deployment logs for more information: %s", deploymentLogsURL)
			default:
				logger.InfoContext(
					ctx,
					"Deployment is still in progress",
					slogID,
					slog.String("status", status),
				)
			}

			return nil
		},
	}
}
