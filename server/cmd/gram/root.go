package gram

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/KimMachineGun/automemlimit/memlimit"
	charmlog "github.com/charmbracelet/log"
	"github.com/urfave/cli/v2"
	"go.uber.org/automaxprocs/maxprocs"

	"github.com/speakeasy-api/gram/internal/control"
	"github.com/speakeasy-api/gram/internal/log"
)

func newApp() *cli.App {
	var shutdownFuncs []func(context.Context) error

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
					if _, ok := log.LogLevels[val]; !ok {
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
			pretty := c.Bool("log-pretty")

			var logger *slog.Logger
			if pretty {
				logger = slog.New(charmlog.NewWithOptions(os.Stderr, charmlog.Options{
					ReportCaller: true,
					Level:        log.LogLevels[c.String("log-level")].Charm,
				}))
			} else {
				logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
					AddSource: true,
					Level:     log.LogLevels[c.String("log-level")].Slog,
				}))
			}

			// Sets GOMAXPROCS to match the Linux container CPU quota.
			maxprocs.Set(maxprocs.Logger(func(s string, i ...interface{}) {
				logger.InfoContext(c.Context, fmt.Sprintf(s, i...))
			}))
			// Sets `GOMEMLIMIT` to 90% of cgroup's memory limit.
			memlimit.SetGoMemLimitWithOpts(memlimit.WithLogger(logger))

			c.Context = log.With(c.Context, logger)

			controlServer := control.Server{
				Address: ":8081",
				Logger:  logger.With(slog.String("service", "control")),
			}

			shutdown, err := controlServer.Start(c.Context)
			if err != nil {
				return err
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			return nil
		},
		After: func(c *cli.Context) error {
			for _, shutdown := range shutdownFuncs {
				if err := shutdown(c.Context); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func Execute(ctx context.Context, osArgs []string) {
	if err := newApp().RunContext(ctx, osArgs); err != nil {
		os.Exit(1)
	}
}
