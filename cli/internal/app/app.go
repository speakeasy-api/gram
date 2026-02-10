package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/o11y"
	"github.com/speakeasy-api/gram/cli/internal/profile"
)

func newApp() *cli.App {
	shortSha := GitSHA
	if len(GitSHA) > 7 {
		shortSha = GitSHA[:7]
	}

	defaultProfilePath, _ := profile.DefaultProfilePath()

	return &cli.App{
		Name:    "gram",
		Usage:   "A command line interface for the Gram platform. Get started at https://docs.getgram.ai/",
		Version: fmt.Sprintf("%s (%s)", Version, shortSha),
		Commands: []*cli.Command{
			newAuthCommand(),
			newPushCommand(),
			newUploadCommand(),
			newStatusCommand(),
			newWhoAmICommand(),
			newStageCommand(),
			newInstallCommand(),
			newUpdateCommand(),
			newSkillsCommand(),
		},
		Flags: []cli.Flag{
			flags.APIKey(),
			flags.APIEndpoint(),
			flags.Project(),
			flags.Org(),
			&cli.StringFlag{
				Name:    "log-level",
				Value:   "info",
				Usage:   "Set the base log level",
				EnvVars: []string{"GRAM_LOG_LEVEL"},
				Action: func(c *cli.Context, val string) error {
					if _, ok := o11y.Levels[val]; !ok {
						return fmt.Errorf("invalid log level: %s", val)
					}
					return nil
				},
			},
			&cli.BoolFlag{
				Name:    "log-pretty",
				Value:   true,
				Usage:   "Toggle pretty logging",
				EnvVars: []string{"GRAM_LOG_PRETTY"},
			},
			&cli.StringFlag{
				Name:    "profile",
				Usage:   "Profile name to use",
				EnvVars: []string{"GRAM_PROFILE"},
			},
			&cli.StringFlag{
				Name:    "profile-path",
				Usage:   fmt.Sprintf("Path to profile JSON file (default: %s)", defaultProfilePath),
				EnvVars: []string{"GRAM_PROFILE_PATH"},
				Hidden:  true,
			},
		},
		Before: func(c *cli.Context) error {
			logger := slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
				RawLevel:    c.String("log-level"),
				Pretty:      c.Bool("log-pretty"),
				DataDogAttr: true,
			}))

			ctx := logging.PushLogger(c.Context, logger)

			profilePath := c.String("profile-path")
			profileName := c.String("profile")
			userSpecifiedPath := c.IsSet("profile-path")
			if profilePath == "" {
				profilePath = defaultProfilePath
			}
			prof, err := profile.LoadByName(profilePath, profileName)
			if err != nil {
				logger.WarnContext(
					ctx,
					"failed to load profile, continuing without it",
					slog.String("profile path", profilePath),
					slog.String("error", err.Error()),
				)
			} else if userSpecifiedPath && prof == nil {
				logger.WarnContext(
					ctx,
					"profile file not found at specified path",
					slog.String("profile path", profilePath),
				)
			}
			ctx = profile.WithProfile(ctx, prof)

			c.Context = ctx
			return nil
		},
	}
}

func Execute(ctx context.Context, osArgs []string) {
	if err := newApp().RunContext(ctx, osArgs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
