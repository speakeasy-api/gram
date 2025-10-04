package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/app/upload"
	"github.com/speakeasy-api/gram/cli/internal/o11y"
)

func newApp() *cli.App {
	shortSha := GitSHA
	if len(GitSHA) > 7 {
		shortSha = GitSHA[:7]
	}

	return &cli.App{
		Name:    "gram",
		Usage:   "A command line interface for the Gram platform. Get started at https://docs.getgram.ai/",
		Version: fmt.Sprintf("%s (%s)", Version, shortSha),
		Commands: []*cli.Command{
			newPushCommand(),
			upload.NewCommand(),
			newStatusCommand(),
		},
		Flags: []cli.Flag{
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
		},
		Before: func(c *cli.Context) error {
			logger := slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
				RawLevel:    c.String("log-level"),
				Pretty:      c.Bool("log-pretty"),
				DataDogAttr: true,
			}))

			ctx := logging.PushLogger(c.Context, logger)
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
