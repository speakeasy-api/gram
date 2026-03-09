package app

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/urfave/cli/v2"
)

func newRedeployCommand() *cli.Command {
	return &cli.Command{
		Name:  "redeploy",
		Usage: "Redeploy an existing deployment",
		Description: `
Redeploy an existing deployment by cloning it with the same assets.

If no deployment ID is provided, redeploys the latest deployment.`,
		Flags: []cli.Flag{
			flags.APIEndpoint(),
			flags.APIKey(),
			flags.Project(),
			flags.Org(),
			&cli.StringFlag{
				Name:  "id",
				Usage: "The deployment ID to redeploy (if not provided, redeploys the latest deployment)",
			},
			&cli.BoolFlag{
				Name:  "skip-poll",
				Usage: "Skip polling for deployment completion and return immediately",
				Value: false,
			},
			flags.JSON(),
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer cancel()

			logger := logging.PullLogger(ctx)
			prof := profile.FromContext(ctx)
			deploymentID := c.String("id")
			skipPoll := c.Bool("skip-poll")
			jsonOutput := c.Bool("json")

			workflowParams, err := workflow.ResolveParams(c, prof)
			if err != nil {
				return fmt.Errorf("failed to resolve workflow params: %w", err)
			}

			wf := workflow.New(ctx, logger, workflowParams)

			// Load the target deployment
			if deploymentID != "" {
				wf.LoadDeploymentByID(ctx, deploymentID)
			} else {
				wf.LoadLatestDeployment(ctx)
			}
			if wf.Failed() {
				return fmt.Errorf("failed to load deployment: %w", wf.Err)
			}

			originalID := wf.Deployment.ID
			logger.InfoContext(ctx, "Redeploying deployment", slog.String("deployment_id", originalID))

			// Trigger the redeploy
			wf.RedeployDeployment(ctx)
			if wf.Failed() {
				return fmt.Errorf("failed to redeploy: %w", wf.Err)
			}

			newID := wf.Deployment.ID
			logger.InfoContext(ctx,
				"New deployment created",
				slog.String("deployment_id", newID),
				slog.String("cloned_from", originalID),
			)

			// Poll for completion
			if !skipPoll {
				wf.Poll(ctx)
				if wf.Failed() {
					return fmt.Errorf("deployment polling failed: %w", wf.Err)
				}
			}

			// Output result
			if jsonOutput {
				return printDeploymentStatusJSON(wf.Deployment)
			}

			logsURL := fmt.Sprintf("%s://%s/%s/%s/deployments/%s",
				workflowParams.APIURL.Scheme,
				workflowParams.APIURL.Host,
				workflowParams.OrgSlug,
				workflowParams.ProjectSlug,
				newID,
			)

			switch wf.Deployment.Status {
			case "completed":
				logger.InfoContext(ctx, "Deployment succeeded",
					slog.String("deployment_id", newID),
					slog.String("logs_url", logsURL),
				)
				fmt.Printf("\nView deployment: %s\n", logsURL)
			case "failed":
				logger.ErrorContext(ctx, "Deployment failed",
					slog.String("deployment_id", newID),
					slog.String("logs_url", logsURL),
				)
				fmt.Printf("\nView deployment logs: %s\n", logsURL)
				return fmt.Errorf("deployment failed")
			default:
				logger.InfoContext(ctx, "Deployment is still in progress",
					slog.String("deployment_id", newID),
					slog.String("status", wf.Deployment.Status),
				)
				fmt.Printf("\nView deployment: %s\n", logsURL)
			}

			return nil
		},
	}
}
