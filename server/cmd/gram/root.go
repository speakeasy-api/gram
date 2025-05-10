package gram

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/urfave/cli/v2"
	"go.uber.org/automaxprocs/maxprocs"

	"github.com/speakeasy-api/gram/internal/o11y"
)

var (
	GitSHA = "dev"
)

func newApp() *cli.App {
	return &cli.App{
		Name:  "gram",
		Usage: "CLI for the Gram API service",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "log-level",
				Value:   "info",
				Usage:   "Set the base log level",
				EnvVars: []string{"GRAM_LOG_LEVEL"},
				Action: func(c *cli.Context, val string) error {
					if _, ok := o11y.LogLevels[val]; !ok {
						return fmt.Errorf("invalid log level: %s", val)
					}
					return nil
				},
			},
			&cli.BoolFlag{
				Name:    "log-pretty",
				Value:   false,
				Usage:   "Enable pretty logging",
				EnvVars: []string{"GRAM_LOG_PRETTY"},
			},
		},
		Commands: []*cli.Command{newStartCommand(), newVersionCommand()},
		Before: func(c *cli.Context) error {
			c.Context = o11y.PushAppInfo(c.Context, &o11y.AppInfo{
				Name:   "gram",
				GitSHA: GitSHA,
			})

			shortGitSHA := GitSHA
			if len(GitSHA) > 8 {
				shortGitSHA = GitSHA[:8]
			}

			logger := slog.New(o11y.NewLogHandler(c.String("log-level"), c.Bool("log-pretty")))
			logger = logger.With(
				slog.String("app", "gram"),
				slog.String("app_name", "gram"),
				slog.String("app_git_sha", shortGitSHA),
			)

			// Sets GOMAXPROCS to match the Linux container CPU quota.
			_, err := maxprocs.Set(maxprocs.Logger(nil))
			if err != nil {
				logger.ErrorContext(c.Context, "automaxprocs", slog.String("error", err.Error()))
			}

			// Sets `GOMEMLIMIT` to 90% of cgroup's memory limit.
			_, err = memlimit.SetGoMemLimitWithOpts(memlimit.WithLogger(nil))
			if err != nil {
				logger.ErrorContext(c.Context, "automemlimit", slog.String("error", err.Error()))
			}

			c.Context = PushLogger(c.Context, logger)

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
