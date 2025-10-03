package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/o11y"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/types"
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

			logger := PullLogger(ctx)
			projectSlug := c.String("project")

			apiURLArg := c.String("api-url")
			apiURL, err := url.Parse(apiURLArg)
			if err != nil {
				return fmt.Errorf("failed to parse API URL '%s': %w", apiURLArg, err)
			}
			if apiURL.Scheme == "" || apiURL.Host == "" {
				return fmt.Errorf("API URL '%s' must include scheme and host", apiURLArg)
			}

			assetsClient := api.NewAssetsClient(&api.AssetsClientOptions{
				Scheme: apiURL.Scheme,
				Host:   apiURL.Host,
			})
			deploymentsClient := api.NewDeploymentsClient(&api.DeploymentsClientOptions{
				Scheme: apiURL.Scheme,
				Host:   apiURL.Host,
			})

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

			logger.InfoContext(ctx, "Deploying to project", slog.String("project", projectSlug), slog.String("config", c.String("config")))

			req := deploy.CreateDeploymentRequest{
				Config:         config,
				APIKey:         secret.Secret(c.String("api-key")),
				ProjectSlug:    projectSlug,
				IdempotencyKey: c.String("idempotency-key"),
			}
			result, err := deploy.CreateDeployment(ctx, logger, assetsClient, deploymentsClient, req)
			if err != nil {
				return fmt.Errorf("deployment failed: %w", err)
			}

			logger.InfoContext(ctx, "Deployment has begun processing", slog.Any("id", result.Deployment.ID))

			if c.Bool("skip-poll") {
				logger.InfoContext(ctx, "Skipping deployment status polling", slog.String("deployment_id", result.Deployment.ID))
				logger.InfoContext(ctx, "You can check deployment status with", slog.String("command", fmt.Sprintf("gram status --id %s", result.Deployment.ID)))
				return nil
			}

			deploymentResult, err := pollDeploymentStatus(ctx, logger, deploymentsClient, req.APIKey, req.ProjectSlug, result.Deployment.ID)
			if err != nil {
				logger.WarnContext(ctx, "Failed to poll deployment status", slog.String("error", err.Error()))
				logger.InfoContext(ctx, "You can check deployment status with", slog.String("command", fmt.Sprintf("gram status %s", result.Deployment.ID)))
				return nil
			}

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

// pollDeploymentStatus polls for deployment status until it reaches a terminal
// state or times out
func pollDeploymentStatus(
	ctx context.Context,
	logger *slog.Logger,
	client *api.DeploymentsClient,
	apiKey secret.Secret,
	projectSlug string,
	deploymentID string,
) (*types.Deployment, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	logger.InfoContext(ctx, "Polling deployment status...", slog.String("deployment_id", deploymentID))

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("deployment polling timed out after 2 minutes")
			}
			return nil, fmt.Errorf("deployment polling cancelled: %w", ctx.Err())

		case <-ticker.C:
			result, err := client.GetDeployment(ctx, apiKey, projectSlug, deploymentID)
			if err != nil {
				return nil, fmt.Errorf("failed to get deployment status: %w", err)
			}

			deployment := &types.Deployment{
				ID:                 result.ID,
				OrganizationID:     result.OrganizationID,
				ProjectID:          result.ProjectID,
				UserID:             result.UserID,
				CreatedAt:          result.CreatedAt,
				Status:             result.Status,
				IdempotencyKey:     result.IdempotencyKey,
				GithubRepo:         result.GithubRepo,
				GithubPr:           result.GithubPr,
				GithubSha:          result.GithubSha,
				ExternalID:         result.ExternalID,
				ExternalURL:        result.ExternalURL,
				ClonedFrom:         result.ClonedFrom,
				Openapiv3ToolCount: result.Openapiv3ToolCount,
				Openapiv3Assets:    result.Openapiv3Assets,
				FunctionsToolCount: result.FunctionsToolCount,
				FunctionsAssets:    result.FunctionsAssets,
				Packages:           result.Packages,
			}

			logger.DebugContext(ctx, "Deployment status check",
				slog.String("deployment_id", deploymentID),
				slog.String("status", deployment.Status))

			switch deployment.Status {
			case "completed", "failed":
				return deployment, nil
			case "pending":
				continue
			default:
				logger.WarnContext(ctx, "Unknown deployment status", slog.String("status", deployment.Status))
				continue
			}
		}
	}
}
