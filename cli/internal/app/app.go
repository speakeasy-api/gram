package app

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
)

func newApp() *cli.App {
	return &cli.App{
		Name:    "gram",
		Usage:   "A command line interface for the Gram platform. Get started at https://docs.getgram.ai/",
		Version: Version,
		Commands: []*cli.Command{
			newPushCommand(),
		},
	}
}

func Execute(ctx context.Context, osArgs []string) {
	if err := newApp().RunContext(ctx, osArgs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
