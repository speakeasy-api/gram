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

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/urfave/cli/v2"
)

type PushOptions struct {
	Profile        *profile.Profile
	ConfigFile     string
	ProjectSlug    string
	OrgSlug        string
	IdempotencyKey string
	Method         string
	NonBlocking    bool
	APIKey         string
	APIURL         string
	// AutoAttach is the union of the --auto-attach flag and the
	// `auto_attach` field in gram.deploy.json. Each entry is a toolset
	// slug. On the server, every function source in this push is set-
	// unioned into the named toolsets' auto_sync_sources column.
	AutoAttach []string
}

type PushResult struct {
	DeploymentID string
	Status       string
	LogsURL      string
}

func DoPush(ctx context.Context, opts PushOptions) (*PushResult, error) {
	logger := logging.PullLogger(ctx)
	prof := opts.Profile

	if opts.Method == "" {
		opts.Method = "merge"
	}
	if opts.Method != "replace" && opts.Method != "merge" {
		return nil, fmt.Errorf("invalid method: %s (allowed values: replace, merge)", opts.Method)
	}
	if opts.ConfigFile == "" {
		return nil, fmt.Errorf("config file is required")
	}

	apiKey := secret.Secret(opts.APIKey)
	if apiKey == "" && prof != nil {
		apiKey = secret.Secret(prof.Secret)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("API key required: provide via APIKey option or authenticate first")
	}

	apiURLStr := opts.APIURL
	if apiURLStr == "" && prof != nil {
		apiURLStr = prof.APIUrl
	}
	if apiURLStr == "" {
		apiURLStr = workflow.DefaultBaseURL
	}

	apiURL, err := url.Parse(apiURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid API URL: %w", err)
	}

	orgSlug := opts.OrgSlug
	if orgSlug == "" && prof != nil && prof.Org != nil {
		orgSlug = prof.Org.Slug
	}
	if orgSlug == "" {
		return nil, fmt.Errorf("organization required: provide via OrgSlug option or authenticate first")
	}

	projectSlug := opts.ProjectSlug
	if projectSlug == "" && prof != nil {
		projectSlug = prof.DefaultProjectSlug
	}
	if projectSlug == "" {
		return nil, fmt.Errorf("project required: provide via ProjectSlug option or authenticate first")
	}

	configFilename, err := filepath.Abs(opts.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve deployment file path: %w", err)
	}

	configFile, err := os.Open(filepath.Clean(configFilename))
	if err != nil {
		return nil, fmt.Errorf("failed to open deployment file: %w", err)
	}
	defer func() {
		if err := configFile.Close(); err != nil {
			logger.WarnContext(ctx, "failed to close config file", slog.String("error", err.Error()))
		}
	}()

	config, err := deploy.NewConfig(configFile, configFilename)
	if err != nil {
		return nil, fmt.Errorf("failed to parse deployment config: %w", err)
	}

	// Set-union the flag's slugs with the config file's, deduped.
	autoAttach := mergeAutoAttach(config.AutoAttach, opts.AutoAttach)

	workflowParams := workflow.Params{
		APIKey:      apiKey,
		APIURL:      apiURL,
		OrgSlug:     orgSlug,
		ProjectSlug: projectSlug,
	}

	logger.InfoContext(
		ctx,
		"Deploying to project",
		slog.String("project", projectSlug),
		slog.String("config", opts.ConfigFile),
	)

	result := workflow.New(ctx, logger, workflowParams).
		UploadAssets(ctx, config.Sources)

	deployTicker := time.NewTicker(time.Second)
	done := make(chan struct{})
	startTime := time.Now()

	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-deployTicker.C:
				elapsed := time.Since(startTime).Truncate(time.Second)
				message := processingMessage(elapsed)

				if message != "" {
					logger.InfoContext(ctx, message)
				}
			}
		}
	}()

	if opts.Method == "replace" {
		result = result.CreateDeployment(ctx, opts.IdempotencyKey, autoAttach)
	} else {
		result = result.EvolveDeployment(ctx, autoAttach)
	}

	deployTicker.Stop()
	done <- struct{}{}
	<-done

	if !opts.NonBlocking {
		result.Poll(ctx)
	}

	if result.Failed() {
		if result.Deployment != nil {
			return &PushResult{
				DeploymentID: result.Deployment.ID,
				Status:       result.Deployment.Status,
				LogsURL:      fmt.Sprintf("%s://%s/%s/%s/deployments/%s", apiURL.Scheme, apiURL.Host, orgSlug, projectSlug, result.Deployment.ID),
			}, fmt.Errorf("deployment failed: %w", result.Err)
		}
		return nil, fmt.Errorf("failed to push deploy: %w", result.Err)
	}

	logsURL := fmt.Sprintf("%s://%s/%s/%s/deployments/%s", apiURL.Scheme, apiURL.Host, orgSlug, projectSlug, result.Deployment.ID)

	return &PushResult{
		DeploymentID: result.Deployment.ID,
		Status:       result.Deployment.Status,
		LogsURL:      logsURL,
	}, nil
}

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
			flags.APIEndpoint(),
			flags.APIKey(),
			flags.Project(),
			flags.Org(),
			&cli.PathFlag{
				Name:     "config",
				Usage:    "Path to the deployment file",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "method",
				Usage: "When set to 'replace', the deployment replaces any existing deployment artifacts in Gram projects. When set to 'merge', the deployment merges with any existing deployment artifacts in Gram project.",
				Action: func(ctx *cli.Context, s string) error {
					if s != "replace" && s != "merge" {
						return fmt.Errorf("invalid method: %s (allowed values: replace, merge)", s)
					}
					return nil
				},
				Value: "merge",
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
			&cli.StringSliceFlag{
				Name:  "auto-attach",
				Usage: "Toolset slug(s) to subscribe to this push's function sources. Repeatable; values union with gram.deploy.json's auto_attach. New tool URNs from subsequent pushes will flow to these toolsets automatically.",
			},
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer cancel()

			logger := logging.PullLogger(ctx)

			result, err := DoPush(ctx, PushOptions{
				Profile:        profile.FromContext(ctx),
				ConfigFile:     c.String("config"),
				ProjectSlug:    c.String("project"),
				OrgSlug:        c.String("org"),
				IdempotencyKey: c.String("idempotency-key"),
				Method:         c.String("method"),
				NonBlocking:    c.Bool("skip-poll"),
				APIKey:         c.String("api-key"),
				APIURL:         c.String("api-url"),
				AutoAttach:     c.StringSlice("auto-attach"),
			})

			if err != nil {
				if result != nil && result.DeploymentID != "" {
					statusCommand := fmt.Sprintf("gram status --id %s", result.DeploymentID)
					logger.WarnContext(
						ctx,
						"Deployment issue",
						slog.String("command", statusCommand),
						slog.String("error", err.Error()),
					)
					return nil
				}
				return err
			}

			slogID := slog.String("deployment_id", result.DeploymentID)
			logsURL := result.LogsURL

			switch result.Status {
			case "completed":
				logger.InfoContext(ctx, "Deployment succeeded", slogID, slog.String("logs_url", logsURL))
				fmt.Printf("\nView deployment: %s\n", logsURL)
				return nil
			case "failed":
				logger.ErrorContext(ctx, "Deployment failed", slogID, slog.String("logs_url", logsURL))
				fmt.Printf("\nView deployment logs: %s\n", logsURL)
				return fmt.Errorf("deployment failed")
			default:
				logger.InfoContext(
					ctx,
					"Deployment is still in progress",
					slogID,
					slog.String("status", result.Status),
				)
				fmt.Printf("\nView deployment: %s\n", logsURL)
			}

			return nil
		},
	}
}

// mergeAutoAttach returns the deduplicated union of the deployment config's
// `auto_attach` field and any `--auto-attach` flag invocations. Order
// follows config-first, flag-second so deterministic output is easy to
// reason about in logs and tests.
func mergeAutoAttach(fromConfig, fromFlag []string) []string {
	if len(fromConfig) == 0 && len(fromFlag) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(fromConfig)+len(fromFlag))
	merged := make([]string, 0, len(fromConfig)+len(fromFlag))
	for _, slug := range append(append([]string{}, fromConfig...), fromFlag...) {
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		merged = append(merged, slug)
	}
	return merged
}

func processingMessage(elapsed time.Duration) string {
	switch {
	case elapsed > 10*time.Second:
		// only output if multiple of 5
		if int(elapsed.Seconds())%5 == 0 {
			return fmt.Sprintf("still processing (%s)...", elapsed)
		}
		return ""
	default:
		return fmt.Sprintf("processing (%s)...", elapsed)
	}
}
