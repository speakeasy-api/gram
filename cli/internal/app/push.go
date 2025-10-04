package app

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/o11y"
	"github.com/speakeasy-api/gram/cli/internal/secret"
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
				Name:    "api-url",
				Usage:   "The base URL to use for API calls.",
				EnvVars: []string{"GRAM_API_URL"},
				Value:   "https://app.getgram.ai",
				Hidden:  true,
			},
			&cli.StringFlag{
				Name:     "api-key",
				Usage:    "Your Gram API key (must be scoped as a 'Provider')",
				EnvVars:  []string{"GRAM_API_KEY"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "project",
				Usage:    "The Gram project to push to",
				EnvVars:  []string{"GRAM_PROJECT"},
				Required: true,
			},
			&cli.PathFlag{
				Name:     "config",
				Usage:    "Path to the deployment file",
				Required: true,
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
			projectSlug := c.String("project")

			apiURLArg := c.String("api-url")
			apiURL, err := url.Parse(apiURLArg)
			if err != nil {
				return fmt.Errorf("failed to parse API URL '%s': %w", apiURLArg, err)
			}
			if apiURL.Scheme == "" || apiURL.Host == "" {
				return fmt.Errorf("API URL '%s' must include scheme and host", apiURLArg)
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
				slog.String("project", projectSlug),
				slog.String("config", c.String("config")),
			)

			params := deploy.WorkflowParams{
				APIKey:      secret.Secret(c.String("api-key")),
				APIURL:      apiURL,
				ProjectSlug: projectSlug,
			}
			result := deploy.NewWorkflow(ctx, params).
				UploadAssets(ctx, config.Sources).
				CreateDeployment(ctx, c.String("idempotency-key"))

			if !c.Bool("skip-poll") {
				result.Poll(ctx)
			}

			if result.Err != nil {
				if result.Deployment != nil {
					helpCmd := slog.String("command",
						fmt.Sprintf("gram status %s", result.Deployment.ID),
					)
					result.Logger.InfoContext(
						ctx,
						"You can check deployment status with",
						helpCmd,
					)
				}

				return fmt.Errorf("failed to push deploy: %w", result.Err)
			}

			deploymentResult := result.Deployment
			switch deploymentResult.Status {
			case "completed":
				logger.InfoContext(ctx, "Deployment completed successfully", slog.String("deployment_id", deploymentResult.ID))
			case "failed":
				logger.ErrorContext(ctx, "Deployment failed", slog.String("deployment_id", deploymentResult.ID))
				return fmt.Errorf("deployment failed")
			default:
				logger.InfoContext(ctx, "Deployment is still in progress", slog.String("status", deploymentResult.Status), slog.String("deployment_id", deploymentResult.ID))
			}

			return nil
		},
	}
}
