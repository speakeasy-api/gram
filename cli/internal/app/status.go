package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
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
			flags.APIEndpoint(),
			flags.APIKey(),
			flags.Project(),
			&cli.StringFlag{
				Name:  "id",
				Usage: "The deployment ID to check status for (if not provided, shows latest deployment)",
			},
			flags.JSON(),
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer cancel()

			logger := logging.PullLogger(ctx)
			prof := profile.FromContext(ctx)
			deploymentID := c.String("id")
			jsonOutput := c.Bool("json")

			workflowParams, err := workflow.ResolveParams(c, prof)
			if err != nil {
				return fmt.Errorf("failed to resolve workflow params: %w", err)
			}

			result := workflow.New(ctx, logger, workflowParams)

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
