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
		Commands: []*cli.Command{newStartCommand()},
		Before: func(c *cli.Context) error {
			c.Context = o11y.PushAppInfo(c.Context, &o11y.AppInfo{
				Name:   "gram",
				GitSHA: GitSHA,
			})

			logger := slog.New(o11y.NewLogHandler(c.String("log-level"), c.Bool("log-pretty")))

			// Sets GOMAXPROCS to match the Linux container CPU quota.
			maxprocs.Set(maxprocs.Logger(func(s string, i ...interface{}) {
				logger.InfoContext(c.Context, fmt.Sprintf(s, i...))
			}))
			// Sets `GOMEMLIMIT` to 90% of cgroup's memory limit.
			memlimit.SetGoMemLimitWithOpts(memlimit.WithLogger(logger))

			c.Context = PushLogger(c.Context, logger.With(slog.String("app", "gram")))

			return nil
		},
	}
}

func Execute(ctx context.Context, osArgs []string) {
	if err := newApp().RunContext(ctx, osArgs); err != nil {
		os.Exit(1)
	}
}
