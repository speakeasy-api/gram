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
		Usage: "Check the status of a project",
		Description: `
Check the status of a project.

If no deployment ID is provided, shows the status of the latest deployment.`,
		Flags: []cli.Flag{
			flags.APIEndpoint(),
			flags.APIKey(),
			flags.Project(),
			flags.Org(),
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

			result.ListToolsets(ctx)
			if result.Failed() {
				return result.Err
			}

			if jsonOutput {
				return printProjectStatusJSON(result)
			} else {
				printProjectStatus(result)
			}

			return nil
		},
	}
}

func printProjectStatus(workflow *workflow.Workflow) {
	deployment := workflow.Deployment
	if deployment == nil {
		fmt.Println("No deployments found for this project")
		return
	}

	nameAndSlug := func(name string, slug types.Slug) string {
		if name == string(slug) {
			return name
		} else {
			return fmt.Sprintf("%s (%s)", name, slug)
		}
	}

	fmt.Printf("Project Status\n")
	fmt.Printf("=================\n\n")

	fmt.Printf("Deployment\n")
	fmt.Printf("  ID:      %s\n", deployment.ID)
	fmt.Printf("  Status:  %s\n", deployment.Status)

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
			fmt.Printf("  - %s\n", nameAndSlug(asset.Name, asset.Slug))
		}
	}

	if len(deployment.FunctionsAssets) > 0 {
		fmt.Printf("\nFunctions Assets:\n")
		for _, asset := range deployment.FunctionsAssets {
			fmt.Printf("  - %s [%s]\n", nameAndSlug(asset.Name, asset.Slug), asset.Runtime)
		}
	}

	fmt.Printf("\nToolsets:\n")
	if len(workflow.Toolsets) > 0 {
		for _, toolset := range workflow.Toolsets {
			fmt.Printf("  - %s [%d tools]\n", nameAndSlug(toolset.Name, toolset.Slug), len(toolset.ToolUrns))
		}
	} else {
		fmt.Printf("  None\n")
	}
}

func printProjectStatusJSON(workflow *workflow.Workflow) error {
	fmt.Printf("Deployment\n")
	fmt.Printf("=================\n\n")

	jsonData, err := json.MarshalIndent(workflow.Deployment, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal deployment to JSON: %w", err)
	}
	fmt.Println(string(jsonData))

	fmt.Printf("\nToolsets\n")
	fmt.Printf("========\n\n")

	jsonData, err = json.MarshalIndent(workflow.Toolsets, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal toolsets to JSON: %w", err)
	}
	fmt.Println(string(jsonData))
	return nil
}
