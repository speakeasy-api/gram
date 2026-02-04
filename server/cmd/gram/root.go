package gram

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
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
		Commands: []*cli.Command{newStartCommand(), newWorkerCommand(), newVersionCommand()},
		Before: func(c *cli.Context) error {
			c.Context = o11y.PushAppInfo(c.Context, &o11y.AppInfo{
				Name:    "gram",
				Command: "",
				GitSHA:  GitSHA,
			})

			logger := slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
				RawLevel:    c.String("log-level"),
				Pretty:      c.Bool("log-pretty"),
				DataDogAttr: os.Getenv("DD_SERVICE") != "",
			})).With(
				attr.SlogDataDogGitCommitSHA(GitSHA),
				attr.SlogDataDogGitRepoURL("github.com/speakeasy-api/gram"),
			)

			// Sets `GOMEMLIMIT` to 90% of cgroup's memory limit.
			_, err := memlimit.SetGoMemLimitWithOpts(memlimit.WithLogger(nil))
			if err != nil {
				logger.ErrorContext(c.Context, "automemlimit", attr.SlogError(err))
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
