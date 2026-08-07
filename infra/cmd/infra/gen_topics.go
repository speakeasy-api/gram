package infra

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/infra/gen"
	"github.com/speakeasy-api/gram/infra/internal/attr"
	"github.com/speakeasy-api/gram/infra/internal/gcp"
)

func newGenTopicsCommand() *cli.Command {
	return &cli.Command{
		Name:  "gen-topics",
		Usage: "Generate the statically typed Pub/Sub topic registry",
		Flags: []cli.Flag{
			&cli.PathFlag{
				Name:  "out",
				Usage: "Path to write the generated topic registry",
				Value: "./infra/pkg/topics/topics_gen.go",
			},
			&cli.BoolFlag{
				Name:  "check",
				Usage: "Fail if the generated registry is out of date instead of rewriting it",
			},
		},
		Action: func(c *cli.Context) error {
			logger := PullLogger(c.Context)

			out := strings.TrimSpace(c.Path("out"))
			if out == "" {
				return fmt.Errorf("--out must not be empty")
			}
			if len(gen.Descriptors) == 0 {
				return fmt.Errorf("embedded descriptor set is empty: cannot generate topic registry")
			}

			if c.Bool("check") {
				if err := gcp.CheckTopics(gen.Descriptors, out); err != nil {
					return fmt.Errorf("check topic registry: %w", err)
				}
				logger.InfoContext(c.Context, "topic registry is up to date", attr.SlogFilePath(out))
				return nil
			}

			if err := gcp.WriteTopics(gen.Descriptors, out); err != nil {
				return fmt.Errorf("generate topic registry: %w", err)
			}

			logger.InfoContext(c.Context, "topic registry written", attr.SlogFilePath(out))

			return nil
		},
	}
}
