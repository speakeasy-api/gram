package app

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/urfave/cli/v2"
)

func newUploadCommand() *cli.Command {
	return &cli.Command{
		Name:  "upload",
		Usage: "Upload an asset to Gram",
		Description: `
Example:
  gram upload --type openapiv3 \
    --location https://raw.githubusercontent.com/my/spec.yaml \
    --name "My API" \
    --slug my-api`[1:],
		Flags: []cli.Flag{
			flags.APIEndpoint(),
			flags.APIKey(),
			flags.Project(),
			&cli.StringFlag{
				Name:     "type",
				Usage:    fmt.Sprintf("The type of asset to upload: %+v", deploy.AllowedTypes),
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
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(
				c.Context,
				os.Interrupt,
				syscall.SIGTERM,
			)
			defer cancel()

			logger := logging.PullLogger(ctx)
			prof := ProfileFromContext(ctx)

			workflowParams, err := workflow.ResolveParams(c, prof)
			if err != nil {
				return fmt.Errorf("failed to resolve workflow params: %w", err)
			}

			result := workflow.New(ctx, logger, workflowParams).
				UploadAssets(ctx, []deploy.Source{parseSource(c)}).
				LoadLatestDeployment(ctx)
			if result.Deployment == nil {
				result.CreateDeployment(ctx, "")
			} else {
				result.EvolveDeployment(ctx)
			}

			if result.Failed() {
				return fmt.Errorf("failed to upload: %w", result.Err)
			}

			result.Logger.InfoContext(
				ctx,
				"upload success",
				slog.String("deployment_id", result.Deployment.ID),
			)
			return nil
		},
	}
}

func parseSource(c *cli.Context) deploy.Source {
	return deploy.Source{
		Type:     deploy.SourceType(c.String("type")),
		Location: c.String("location"),
		Name:     c.String("name"),
		Slug:     c.String("slug"),
	}
}
