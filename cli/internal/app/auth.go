package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

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

func profileNameFromURL(apiURL string) string {
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Host == "" {
		return "default"
	}
	return strings.ReplaceAll(parsed.Host, ".", "-")
}

func determineProfileName(prof *profile.Profile, apiURL string) string {
	if prof != nil {
		return prof.Name
	}
	return profileNameFromURL(apiURL)
}

func getProfilePath(c *cli.Context) (string, error) {
	if p := c.String("profile-path"); p != "" {
		return p, nil
	}

	path, err := profile.DefaultProfilePath()
	if err != nil {
		return "", fmt.Errorf("failed to get default profile path: %w", err)
	}
	return path, nil
}

func canRefreshProfile(prof *profile.Profile) bool {
	return prof != nil && prof.Secret != ""
}

// mintKey starts a local HTTP listener while the user authenticates in the web
// app. The user's new API key is returned to the listener as a query param.
func mintKey(
	ctx context.Context,
	logger *slog.Logger,
	apiURL string,
) (auth.CallbackResult, error) {
	listener, err := auth.NewListener()
	if err != nil {
		return auth.CallbackResult{}, fmt.Errorf("failed to create callback listener: %w", err)
	}

	defer func() {
		bg := context.Background()
		if err := listener.Stop(bg); err != nil {
			msg := "failed to stop listener"
			logger.WarnContext(bg, msg, slog.String("error", err.Error()))
		}
	}()

	callbackURL := listener.URL()
	listener.Start()

	dispatcher := auth.NewDispatcher(logger)
	if err := dispatcher.Dispatch(ctx, apiURL, callbackURL); err != nil {
		return auth.CallbackResult{}, fmt.Errorf("failed to dispatch auth request: %w", err)
	}

	result, err := listener.Wait(ctx)
	if err != nil {
		return auth.CallbackResult{}, fmt.Errorf("authentication failed: %w", err)
	}

	return result, nil
}

func saveProfile(
	ctx context.Context,
	logger *slog.Logger,
	apiKey string,
	apiURL string,
	result *keys.ValidateKeyResult,
	profilePath string,
	profileName string,
	projectSlug string,
) error {
	err := profile.UpdateOrCreate(
		apiKey,
		apiURL,
		result.Organization,
		result.Projects,
		profilePath,
		profileName,
		projectSlug,
	)
	if err != nil {
		return fmt.Errorf("failed to save profile: %w", err)
	}

	savedProfile, err := profile.LoadByName(profilePath, profileName)
	if err == nil {
		for _, warning := range profile.LintProfile(savedProfile) {
			logger.WarnContext(ctx, warning)
		}
	}

	return nil
}

func refreshProfile(
	ctx context.Context,
	logger *slog.Logger,
	prof *profile.Profile,
	profileName string,
	apiURL string,
	keysClient *api.KeysClient,
	profilePath string,
) error {
	result, err := keysClient.Verify(ctx, secret.Secret(prof.Secret))
	if err != nil {
		msg := "existing API key is invalid, starting authentication flow"
		logger.InfoContext(ctx, msg, slog.String("error", err.Error()))

		return fmt.Errorf("failed to refresh profile: %w", err)
	}

	return saveProfile(ctx, logger, prof.Secret, apiURL, result, profilePath, profileName, prof.DefaultProjectSlug)
}

func authenticateNewProfile(
	ctx context.Context,
	logger *slog.Logger,
	profileName string,
	apiURL string,
	keysClient *api.KeysClient,
	profilePath string,
) error {
	callbackResult, err := mintKey(ctx, logger, apiURL)
	if err != nil {
		return err
	}

	result, err := keysClient.Verify(ctx, secret.Secret(callbackResult.APIKey))
	if err != nil {
		return fmt.Errorf("failed to authenticate profile: %w", err)
	}

	return saveProfile(ctx, logger, callbackResult.APIKey, apiURL, result, profilePath, profileName, callbackResult.Project)
}

func doAuth(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)
	prof := profile.FromContext(ctx)

	apiURL, err := workflow.ResolveURL(c, prof)
	if err != nil {
		return fmt.Errorf("invalid API URL: %w", err)
	}

	profileName := c.String("profile")
	if profileName == "" {
		profileName = determineProfileName(prof, apiURL.String())
	}

	keysClient := api.NewKeysClientFromURL(apiURL)

	profilePath, err := getProfilePath(c)
	if err != nil {
		return fmt.Errorf("failed to get profile path: %w", err)
	}

	if canRefreshProfile(prof) {
		err := refreshProfile(ctx, logger, prof, profileName, apiURL.String(), keysClient, profilePath)
		if err == nil {
			msg := fmt.Sprintf(
				"Authentication successful for org '%s' (project: '%s')",
				prof.Org.Name,
				prof.DefaultProjectSlug,
			)
			logger.InfoContext(ctx, msg)
			return nil
		}
		// If refresh failed, fall through to authenticate new profile
	}

	return authenticateNewProfile(
		ctx,
		logger,
		profileName,
		apiURL.String(),
		keysClient,
		profilePath,
	)
}
