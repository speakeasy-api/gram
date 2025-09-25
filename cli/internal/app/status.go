package app

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/urfave/cli/v2"
)

func newStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Check the status of a deployment",
		Description: `
Check the status of a deployment.

If no deployment ID is provided, shows the status of the latest deployment.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "api-url",
				Usage:   "The base URL to use for API calls.",
				EnvVars: []string{"GRAM_API_URL"},
				Value:   "https://app.getgram.ai",
			},
			&cli.StringFlag{
				Name:     "api-key",
				Usage:    "Your Gram API key (must be scoped as a 'Provider')",
				EnvVars:  []string{"GRAM_API_KEY"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "project",
				Usage:    "The Gram project to check status for",
				EnvVars:  []string{"GRAM_PROJECT"},
				Required: true,
			},
			&cli.StringFlag{
				Name:  "id",
				Usage: "The deployment ID to check status for (if not provided, shows latest deployment)",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "Output deployment status as JSON",
			},
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer cancel()

			logger := PullLogger(ctx)
			projectSlug := c.String("project")
			deploymentID := c.String("id")
			jsonOutput := c.Bool("json")

			apiURLArg := c.String("api-url")
			apiURL, err := url.Parse(apiURLArg)
			if err != nil {
				return fmt.Errorf("failed to parse API URL '%s': %w", apiURLArg, err)
			}
			if apiURL.Scheme == "" || apiURL.Host == "" {
				return fmt.Errorf("API URL '%s' must include scheme and host", apiURLArg)
			}

			deploymentsClient := api.NewDeploymentsClient(&api.DeploymentsClientOptions{
				Scheme: apiURL.Scheme,
				Host:   apiURL.Host,
			})

			apiKey := secret.Secret(c.String("api-key"))

			var deployment *types.Deployment
			if deploymentID != "" {
				logger.InfoContext(ctx, "Getting deployment status", slog.String("deployment_id", deploymentID))
				result, err := deploymentsClient.GetDeployment(ctx, apiKey, projectSlug, deploymentID)
				if err != nil {
					return fmt.Errorf("failed to get deployment: %w", err)
				}
				deployment = &types.Deployment{
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
			} else {
				logger.InfoContext(ctx, "Getting latest deployment status")
				result, err := deploymentsClient.GetLatestDeployment(ctx, apiKey, projectSlug)
				if err != nil {
					return fmt.Errorf("failed to get latest deployment: %w", err)
				}
				if result.Deployment == nil {
					if jsonOutput {
						fmt.Println("{}")
					} else {
						fmt.Println("No deployments found for this project")
					}
					return nil
				}
				deployment = result.Deployment
			}

			if jsonOutput {
				return printDeploymentStatusJSON(deployment)
			} else {
				printDeploymentStatus(deployment)
			}
			return nil
		},
	}
}

func printDeploymentStatus(deployment *types.Deployment) {
	fmt.Printf("Deployment Status\n")
	fmt.Printf("=================\n\n")

	fmt.Printf("ID:           %s\n", deployment.ID)
	fmt.Printf("Status:       %s\n", deployment.Status)

	if deployment.ExternalID != nil {
		fmt.Printf("External ID:  %s\n", *deployment.ExternalID)
	}

	if deployment.GithubRepo != nil {
		fmt.Printf("Repository:   %s\n", *deployment.GithubRepo)
	}

	if deployment.GithubPr != nil {
		fmt.Printf("Pull Request: %s\n", *deployment.GithubPr)
	}

	if deployment.GithubSha != nil {
		fmt.Printf("Commit SHA:   %s\n", *deployment.GithubSha)
	}

	if deployment.ExternalURL != nil {
		fmt.Printf("External URL: %s\n", *deployment.ExternalURL)
	}

	fmt.Printf("\nAssets:\n")
	fmt.Printf("  OpenAPI Tools: %d\n", deployment.Openapiv3ToolCount)
	fmt.Printf("  Functions:     %d\n", deployment.FunctionsToolCount)

	if len(deployment.Openapiv3Assets) > 0 {
		fmt.Printf("\nOpenAPI Assets:\n")
		for _, asset := range deployment.Openapiv3Assets {
			fmt.Printf("  - %s (%s)\n", asset.Name, asset.Slug)
		}
	}

	if len(deployment.FunctionsAssets) > 0 {
		fmt.Printf("\nFunctions Assets:\n")
		for _, asset := range deployment.FunctionsAssets {
			fmt.Printf("  - %s (%s) - %s\n", asset.Name, asset.Slug, asset.Runtime)
		}
	}

}

func printDeploymentStatusJSON(deployment *types.Deployment) error {
	jsonData, err := json.MarshalIndent(deployment, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal deployment to JSON: %w", err)
	}
	fmt.Println(string(jsonData))
	return nil
}
