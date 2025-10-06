package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/speakeasy-api/gram/cli/internal/deploy"
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

			projectSlug := c.String("project")
			deploymentID := c.String("id")
			jsonOutput := c.Bool("json")

			apiURL, err := url.Parse(c.String("api-url"))
			if err != nil {
				return fmt.Errorf(
					"failed to parse API URL '%s': %w",
					c.String("api-url"),
					err,
				)
			}

			params := deploy.WorkflowParams{
				APIKey:      secret.Secret(c.String("api-key")),
				APIURL:      apiURL,
				ProjectSlug: projectSlug,
			}
			result := deploy.NewWorkflow(ctx, params)

			if deploymentID != "" {
				result.LoadDeploymentByID(ctx, deploymentID)
			} else {
				result.LoadLatestDeployment(ctx)
			}
			if result.Failed() {
				return fmt.Errorf("failed to get status: %w", result.Err)
			}

			if jsonOutput {
				return printDeploymentStatusJSON(result.Deployment)
			} else {
				printDeploymentStatus(result.Deployment)
			}

			return nil
		},
	}
}

func printDeploymentStatus(deployment *types.Deployment) {
	if deployment == nil {
		fmt.Println("No deployments found for this project")
		return
	}

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

	fmt.Printf("\nTools:\n")
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
	if deployment == nil {
		fmt.Println("{}")
		return nil
	}

	jsonData, err := json.MarshalIndent(deployment, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal deployment to JSON: %w", err)
	}
	fmt.Println(string(jsonData))
	return nil
}
