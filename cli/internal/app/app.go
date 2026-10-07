package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
		Name:    commandName,
		Usage:   "A command line interface for the Speakeasy AI Control Plane. Get started at https://ai.speakeasy.com",
		Version: fmt.Sprintf("%s (%s)", Version, shortSha),
		Commands: []*cli.Command{
			newAuthCommand(),
			newPushCommand(),
			newUploadCommand(),
			newStatusCommand(),
			newWhoAmICommand(),
			newStageCommand(),
			newFunctionsCommand(),
			newInstallCommand(),
			newUpdateCommand(),
			newRedeployCommand(),
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
				EnvVars: flags.EnvVars("LOG_LEVEL"),
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
				EnvVars: flags.EnvVars("LOG_PRETTY"),
			},
			&cli.StringFlag{
				Name:    "profile",
				Usage:   "Profile name to use",
				EnvVars: flags.EnvVars("PROFILE"),
			},
			&cli.StringFlag{
				Name:    "profile-path",
				Usage:   fmt.Sprintf("Path to profile JSON file (default: %s)", defaultProfilePath),
				EnvVars: flags.EnvVars("PROFILE_PATH"),
				Hidden:  true,
			},
			&cli.BoolFlag{
				Name:   controlPlaneMarkerFlag,
				Usage:  "Print a fixed marker that identifies this CLI and exit",
				Hidden: true,
			},
		},
		Action: func(c *cli.Context) error {
			if c.Bool(controlPlaneMarkerFlag) {
				_, err := fmt.Fprintln(c.App.Writer, controlPlaneMarker)
				if err != nil {
					return fmt.Errorf("write control plane marker: %w", err)
				}
				return nil
			}
			if c.Args().Present() {
				return cli.ShowCommandHelp(c, c.Args().First())
			}
			return cli.ShowAppHelp(c)
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

const (
	// commandName is the name the CLI is installed and documented under.
	commandName = "speakeasy"

	// legacyCommandName is the name the CLI shipped under before it became
	// speakeasy. The gram Homebrew formulas still install the binary under
	// this name so existing scripts keep working for one release cycle.
	legacyCommandName = "gram"

	// controlPlaneMarkerFlag is a hidden flag that prints controlPlaneMarker.
	// Tools such as the Gram Functions SDK run it to tell this CLI apart from
	// the Speakeasy SDK generator CLI, which installs a binary with the same
	// name and rejects the flag.
	controlPlaneMarkerFlag = "control-plane-cli"

	// controlPlaneMarker is the fixed output of --control-plane-cli. Callers
	// match it exactly, so it must not change.
	controlPlaneMarker = "speakeasy-ai-control-plane-cli"

	// legacyCommandNotice is printed to stderr when the CLI runs as gram.
	legacyCommandNotice = "Warning: the gram command is deprecated and will be removed in a future release. " +
		"Install the speakeasy command with 'brew install speakeasy-api/tap/cli' or 'npm i -g @speakeasy-api/cli', " +
		"then run 'speakeasy' instead of 'gram'."
)

// invokedAs returns the command name the CLI was run as, derived from argv[0]
// without its directory or Windows .exe suffix.
func invokedAs(arg0 string) string {
	name := filepath.Base(strings.ReplaceAll(arg0, `\`, "/"))
	return strings.TrimSuffix(strings.ToLower(name), ".exe")
}

// writeLegacyCommandNotice writes the deprecation notice to w when the CLI was
// run as the legacy gram command.
func writeLegacyCommandNotice(w io.Writer, arg0 string) {
	if invokedAs(arg0) == legacyCommandName {
		_, _ = fmt.Fprintln(w, legacyCommandNotice)
	}
}

func Execute(ctx context.Context, osArgs []string) {
	if len(osArgs) > 0 {
		writeLegacyCommandNotice(os.Stderr, osArgs[0])
	}
	if err := flags.UnsetEmptyEnv(os.LookupEnv, os.Unsetenv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if notice := flags.LegacyEnvNotice(os.LookupEnv); notice != "" {
		_, _ = fmt.Fprintln(os.Stderr, notice)
	}

	if err := newApp().RunContext(ctx, osArgs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
