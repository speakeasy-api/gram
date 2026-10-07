package infra

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/infra/gen"
	"github.com/speakeasy-api/gram/infra/internal/gcp"
	"github.com/urfave/cli/v2"
)

func newGenStorageCommand() *cli.Command {
	return &cli.Command{
		Name:  "gen-storage",
		Usage: "Generate explicit Parquet schemas, typed Go encoders and schema manifests",
		Flags: []cli.Flag{
			&cli.PathFlag{Name: "out", Value: "./infra/pkg/storagebindings"},
			&cli.StringFlag{Name: "package", Value: "storagebindings"},
			&cli.StringFlag{Name: "import-path", Value: "github.com/speakeasy-api/gram/infra/pkg/storagebindings"},
			&cli.PathFlag{Name: "descriptors", Usage: "Use a descriptor set instead of the embedded topology (fixtures)"},
			&cli.PathFlag{Name: "proto-root", Value: "./infra/proto"},
			&cli.BoolFlag{Name: "check", Usage: "Fail if committed storage artifacts have drifted"},
		},
		Action: func(c *cli.Context) error {
			raw := gen.Descriptors
			if source := c.Path("descriptors"); source != "" {
				var err error
				raw, err = os.ReadFile(source)
				if err != nil {
					return fmt.Errorf("read storage descriptors: %w", err)
				}
			}
			topics, subs, err := gcp.DiscoverPubSub(raw)
			if err != nil {
				return err
			}
			schemas, err := gcp.DiscoverSchemas(c.Context, raw, c.Path("proto-root"))
			if err != nil {
				return err
			}
			if err := gcp.ValidateStorageSchemas(topics, subs, schemas); err != nil {
				return err
			}
			code, manifest, err := gcp.RenderStorage(raw, c.String("package"), c.String("import-path"))
			if err != nil {
				return err
			}
			for _, artifact := range []struct {
				name string
				data []byte
			}{{"storage_gen.go", code}, {"storage_manifest.json", manifest}} {
				out := filepath.Join(c.Path("out"), artifact.name)
				if c.Bool("check") {
					existing, err := os.ReadFile(out)
					if err != nil {
						return fmt.Errorf("read generated artifact: %w", err)
					}
					if !bytes.Equal(existing, artifact.data) {
						return fmt.Errorf("%s is out of date; run mise run gen:infra", out)
					}
					continue
				}
				if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
					return fmt.Errorf("create storage output directory: %w", err)
				}
				tmp, err := os.CreateTemp(filepath.Dir(out), ".storage-*")
				if err != nil {
					return fmt.Errorf("create storage artifact: %w", err)
				}
				defer os.Remove(tmp.Name())
				_, writeErr := tmp.Write(artifact.data)
				closeErr := tmp.Close()
				if writeErr != nil {
					return fmt.Errorf("write storage artifact: %w", writeErr)
				}
				if closeErr != nil {
					return fmt.Errorf("close storage artifact: %w", closeErr)
				}
				if err := os.Rename(tmp.Name(), out); err != nil {
					return fmt.Errorf("replace storage artifact: %w", err)
				}
			}
			return nil
		},
	}
}
