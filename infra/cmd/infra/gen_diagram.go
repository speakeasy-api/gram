package infra

import (
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/infra/gen"
	"github.com/speakeasy-api/gram/infra/internal/attr"
	"github.com/speakeasy-api/gram/infra/internal/diagram"
)

func newGenDiagramCommand() *cli.Command {
	return &cli.Command{
		Name:  "gen-diagram",
		Usage: "Generate the Pub/Sub topology + usage mermaid diagram",
		Flags: []cli.Flag{
			&cli.PathFlag{
				Name:  "out",
				Usage: "Path to write the generated markdown document",
				Value: "./docs/pubsub-topology.md",
			},
			&cli.PathFlag{
				Name:  "repo-root",
				Usage: "Repository root that ast-grep scans for call sites",
				Value: ".",
			},
		},
		Action: func(c *cli.Context) error {
			logger := PullLogger(c.Context)

			out := strings.TrimSpace(c.Path("out"))
			if out == "" {
				return fmt.Errorf("--out must not be empty")
			}
			if len(gen.Descriptors) == 0 {
				return fmt.Errorf("embedded descriptor set is empty: cannot generate pubsub diagram")
			}

			doc, err := diagram.Generate(c.Context, gen.Descriptors, c.Path("repo-root"))
			if err != nil {
				return fmt.Errorf("generate pubsub diagram: %w", err)
			}

			if err := os.WriteFile(out, []byte(doc), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", out, err)
			}

			logger.InfoContext(c.Context, "generated pubsub topology diagram", attr.SlogFilePath(out))
			return nil
		},
	}
}
