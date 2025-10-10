package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/auth"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/speakeasy-api/gram/server/gen/keys"
)

func newAuthCommand() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "Authenticate with Gram",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "api-url",
				Usage:   "URL of the Gram web application",
				EnvVars: []string{"GRAM_API_URL"},
			},
		},
		Action: doAuth,
	}
}

func doAuth(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)
	prof := profile.FromContext(ctx)
	apiURL, err := workflow.ResolveURL(c, prof)
	if err != nil {
		return fmt.Errorf("invalid API URL: %w", err)
	}

	keysClient := api.NewKeysClientFromURL(apiURL)

	// Check if we have an existing valid API key for this URL
	var apiKey string
	var result *keys.ValidateKeyResult
	if prof != nil && prof.Secret != "" && prof.APIUrl == apiURL.String() {
		result, err = keysClient.Verify(ctx, secret.Secret(prof.Secret))
		if err == nil {
			// Existing key is valid, use it
			logger.InfoContext(ctx, "existing API key is valid, skipping authentication flow")
			apiKey = prof.Secret
		} else {
			logger.InfoContext(
				ctx,
				"existing API key is invalid, starting authentication flow",
				slog.String("error", err.Error()),
			)
		}
	}

	// If no valid existing key, go through the auth flow
	if apiKey == "" {
		listener, err := auth.NewListener()
		if err != nil {
			return fmt.Errorf("failed to create callback listener: %w", err)
		}

		defer func() {
			shutdownCtx := context.Background()
			if err := listener.Stop(shutdownCtx); err != nil {
				logger.WarnContext(
					shutdownCtx,
					"failed to stop listener",
					slog.String("error", err.Error()),
				)
			}
		}()

		callbackURL := listener.URL()
		listener.Start()

		dispatcher := auth.NewDispatcher(logger)
		if err := dispatcher.Dispatch(
			ctx,
			apiURL.String(),
			callbackURL,
		); err != nil {
			return fmt.Errorf("failed to dispatch auth request: %w", err)
		}

		apiKey, err = listener.Wait(ctx)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		result, err = keysClient.Verify(ctx, secret.Secret(apiKey))
		if err != nil {
			return fmt.Errorf("failed to verify API key: %w", err)
		}
	}

	profilePath := c.String("profile-path")
	if profilePath == "" {
		defaultPath, err := profile.DefaultProfilePath()
		if err != nil {
			return fmt.Errorf("failed to get profile path: %w", err)
		}
		profilePath = defaultPath
	}

	err = profile.UpdateOrCreate(
		apiKey,
		apiURL.String(),
		result.Organization,
		result.Projects,
		profilePath,
	)
	if err != nil {
		return fmt.Errorf("failed to save profile: %w", err)
	}

	return nil
}
