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

type AuthOptions struct {
	Profile      *profile.Profile
	ProfilePath  string
	APIURL       string
	DashboardURL string
	ProjectSlug  string
}

type AuthResult struct {
	Profile *profile.Profile
}

func DoAuth(ctx context.Context, opts AuthOptions) (*AuthResult, error) {
	logger := logging.PullLogger(ctx)

	if opts.ProfilePath == "" {
		var err error
		opts.ProfilePath, err = profile.DefaultProfilePath()
		if err != nil {
			return nil, fmt.Errorf("failed to get profile path: %w", err)
		}
	}

	apiURLStr := opts.APIURL
	if apiURLStr == "" {
		apiURLStr = workflow.DefaultBaseURL
	}

	apiURL, err := url.Parse(apiURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid API URL: %w", err)
	}

	dashboardURL := opts.DashboardURL
	if dashboardURL == "" {
		dashboardURL = apiURL.String()
	}

	profileName := profileNameFromURL(apiURL.String())
	keysClient := api.NewKeysClientFromURL(apiURL)

	prof := opts.Profile
	if prof == nil {
		prof, _ = profile.LoadByName(opts.ProfilePath, profileName)
	}

	if canRefreshProfile(prof) {
		err := refreshProfile(ctx, logger, prof, profileName, apiURL.String(), keysClient, opts.ProfilePath)
		if err == nil {
			logger.InfoContext(ctx, fmt.Sprintf(
				"Authentication successful for org '%s' (project: '%s')",
				prof.Org.Name,
				prof.DefaultProjectSlug,
			))

			savedProf, err := profile.LoadByName(opts.ProfilePath, profileName)
			if err != nil {
				return nil, fmt.Errorf("failed to load profile: %w", err)
			}
			return &AuthResult{Profile: savedProf}, nil
		}
	}

	projectSlug := opts.ProjectSlug
	if err := authenticateNewProfile(
		ctx,
		logger,
		profileName,
		apiURL.String(),
		dashboardURL,
		keysClient,
		opts.ProfilePath,
	); err != nil {
		return nil, err
	}

	savedProf, err := profile.LoadByName(opts.ProfilePath, profileName)
	if err != nil {
		return nil, fmt.Errorf("failed to load profile: %w", err)
	}

	if projectSlug != "" && projectSlug != savedProf.DefaultProjectSlug {
		if err := profile.UpdateProjectSlug(opts.ProfilePath, projectSlug); err != nil {
			logger.WarnContext(ctx, "failed to update project slug", slog.String("error", err.Error()))
		}
		savedProf, _ = profile.LoadByName(opts.ProfilePath, profileName)
	}

	return &AuthResult{Profile: savedProf}, nil
}

func newAuthCommand() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "Authenticate with Gram",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "api-url",
				Usage:   "URL of the Gram API server",
				EnvVars: []string{"GRAM_API_URL"},
			},
			&cli.StringFlag{
				Name:    "dashboard-url",
				Usage:   "URL of the Gram dashboard for authentication",
				EnvVars: []string{"GRAM_DASHBOARD_URL"},
			},
		},
		Subcommands: []*cli.Command{
			newAuthSwitchCommand(),
			newAuthClearCommand(),
		},
		Action: doAuth,
	}
}

func newAuthSwitchCommand() *cli.Command {
	return &cli.Command{
		Name:  "switch",
		Usage: "Switch the default project for the current profile",
		Description: `
Switch the default project for the current profile.

The project slug must be one of the projects available in your current profile.
Use 'gram status' to see your current project.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "project",
				Usage:    "The project slug to switch to",
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {
			projectSlug := c.String("project")

			profilePath, err := profile.DefaultProfilePath()
			if err != nil {
				return fmt.Errorf("failed to get profile path: %w", err)
			}

			if err := profile.UpdateProjectSlug(profilePath, projectSlug); err != nil {
				return fmt.Errorf("failed to switch project: %w", err)
			}

			fmt.Printf("Successfully switched to project: %s\n", projectSlug)
			return nil
		},
	}
}

func newAuthClearCommand() *cli.Command {
	return &cli.Command{
		Name:  "clear",
		Usage: "Clear all authentication profiles",
		Description: `
Clear all authentication profiles from the profile configuration file.

This will remove all stored API keys and profile information.
You will need to run 'gram auth' again to authenticate.`,
		Action: func(c *cli.Context) error {
			profilePath, err := profile.DefaultProfilePath()
			if err != nil {
				return fmt.Errorf("failed to get profile path: %w", err)
			}

			if err := profile.Clear(profilePath); err != nil {
				return fmt.Errorf("failed to clear profiles: %w", err)
			}

			fmt.Println("Successfully cleared all profiles")
			return nil
		},
	}
}

func profileNameFromURL(apiURL string) string {
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Host == "" {
		return "default"
	}
	return strings.ReplaceAll(parsed.Host, ".", "-")
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
) (*auth.CallbackResult, error) {
	listener, err := auth.NewListener()
	if err != nil {
		return nil, fmt.Errorf("failed to create callback listener: %w", err)
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
		return nil, fmt.Errorf("failed to dispatch auth request: %w", err)
	}

	result, err := listener.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
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
	dashboardURL string,
	keysClient *api.KeysClient,
	profilePath string,
) error {
	callbackResult, err := mintKey(ctx, logger, dashboardURL)
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

	profilePath, err := getProfilePath(c)
	if err != nil {
		return fmt.Errorf("failed to get profile path: %w", err)
	}

	var dashboardURL string
	if c.IsSet("dashboard-url") {
		dashboardURL = c.String("dashboard-url")
	}

	result, err := DoAuth(ctx, AuthOptions{
		Profile:      profile.FromContext(ctx),
		ProfilePath:  profilePath,
		APIURL:       c.String("api-url"),
		DashboardURL: dashboardURL,
		ProjectSlug:  c.String("project"),
	})
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	logger.InfoContext(ctx, fmt.Sprintf(
		"Authentication successful for org '%s' (project: '%s')",
		result.Profile.Org.Name,
		result.Profile.DefaultProjectSlug,
	))

	return nil
}
