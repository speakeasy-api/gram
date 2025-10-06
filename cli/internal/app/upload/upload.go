package upload

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/urfave/cli/v2"
)

func NewCommand() *cli.Command {
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
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(
				c.Context,
				os.Interrupt,
				syscall.SIGTERM,
			)
			defer cancel()

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
				ProjectSlug: c.String("project"),
			}

			result := deploy.NewWorkflow(ctx, params).
				UploadAssets(ctx, []deploy.Source{parseSource(c)}).
				LoadActiveDeployment(ctx)
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
