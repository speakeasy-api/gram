package infra

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/infra/gen"
	"github.com/speakeasy-api/gram/infra/internal/gcp"
)

func newGenCCCommand() *cli.Command {
	return &cli.Command{
		Name:  "gen-cc",
		Usage: "Generate the config-connector Helm values",
		Flags: []cli.Flag{
			&cli.PathFlag{
				Name:  "out",
				Usage: "Path to write the generated Helm values document",
				Value: "./infra/gen/kcc.yaml",
			},
			&cli.PathFlag{
				Name:  "proto-root",
				Usage: "Path to the proto source tree, used to inline schema definitions",
				Value: "./infra/proto",
			},
		},
		Action: func(c *cli.Context) error {
			logger := PullLogger(c.Context)

			out := strings.TrimSpace(c.Path("out"))
			if out == "" {
				return fmt.Errorf("--out must not be empty")
			}
			protoRoot := strings.TrimSpace(c.Path("proto-root"))
			if protoRoot == "" {
				return fmt.Errorf("--proto-root must not be empty")
			}
			if len(gen.Descriptors) == 0 {
				return fmt.Errorf("embedded descriptor set is empty: cannot generate pubsub topology")
			}

			cc := gcp.NewCCPubSub(
				logger,
				out,
				gen.Descriptors,
				protoRoot,
			)
			if err := cc.Generate(c.Context); err != nil {
				return fmt.Errorf("generate helm values: %w", err)
			}
			return nil
		},
	}
}
