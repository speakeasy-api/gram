package app

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/urfave/cli/v2"
)

func newUploadCommand() *cli.Command {
	return &cli.Command{
		Name:   "upload",
		Action: doUpload,
		Usage:  "Upload an asset to Gram",
		Description: `
Example:
  gram upload --type openapiv3 \
    --location https://raw.githubusercontent.com/my/spec.yaml \
    --name "My API" \
    --slug my-api`[1:],
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
				Usage:    "The Gram project to upload to",
				EnvVars:  []string{"GRAM_PROJECT"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "type",
				Usage:    "The type of asset to upload",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "location",
				Usage:    "The location of the asset (file path or URL)",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "name",
				Usage:    "The human-readable name of the asset",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "slug",
				Usage:    "The URL-friendly slug for the asset",
				Required: true,
			},
		},
	}
}

func doUpload(c *cli.Context) error {
	ctx, cancel := signal.NotifyContext(
		c.Context,
		os.Interrupt,
		syscall.SIGTERM,
	)
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
	deploymentsClient := api.NewDeploymentsClient(
		&api.DeploymentsClientOptions{
			Scheme: apiURL.Scheme,
			Host:   apiURL.Host,
		},
	)

	logger.DebugContext(
		ctx,
		"Fetching active deployment",
		slog.String("project", projectSlug),
	)

	apiKey := secret.Secret(c.String("api-key"))
	active, err := deploymentsClient.GetActiveDeployment(
		ctx,
		apiKey,
		projectSlug,
	)
	if err != nil {
		return fmt.Errorf("failed to get active deployment: %w", err)
	}

	source := deploy.Source{
		Type:     deploy.SourceType(c.String("type")),
		Location: c.String("location"),
		Name:     c.String("name"),
		Slug:     c.String("slug"),
	}

	if err := source.Validate(); err != nil {
		return fmt.Errorf("invalid source: %w", err)
	}

	if active.Deployment == nil {
		createReq := deploy.CreateDeploymentRequest{
			APIKey:         apiKey,
			ProjectSlug:    projectSlug,
			Config:         deploy.NewConfigFromSources(source),
			IdempotencyKey: "",
		}
		_, err = deploy.CreateDeployment(
			ctx,
			logger,
			assetsClient,
			deploymentsClient,
			createReq,
		)
		if err != nil {
			return fmt.Errorf("failed to create deployment: %w", err)
		}

		return nil
	}

	activeDeploymentID := active.Deployment.ID
	logger.InfoContext(
		ctx,
		"Found active deployment",
		slog.String("deployment_id", activeDeploymentID),
	)
	addReq := deploy.AddAssetsRequest{
		APIKey:       apiKey,
		ProjectSlug:  projectSlug,
		DeploymentID: activeDeploymentID,
		Sources:      []deploy.Source{source},
	}
	addResult, err := deploy.AddAssets(
		ctx,
		logger,
		assetsClient,
		deploymentsClient,
		addReq,
	)

	if err != nil {
		return fmt.Errorf("failed to evolve deployment: %w", err)
	}

	if addResult.Deployment == nil {
		return fmt.Errorf("deployment evolution returned no deployment")
	}

	logger.InfoContext(
		ctx,
		"Uploaded successfully",
		slog.String("deployment_id", addResult.Deployment.ID),
	)
	return nil
}
