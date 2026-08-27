package gram

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

func newRenderPlatformMCPCommand() *cli.Command {
	return &cli.Command{
		Name:  "render-platform-mcp",
		Usage: "Render the public Speakeasy marketplace tree (speakeasy-api/marketplace) into a directory",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "server-url",
				Value: "https://app.getgram.ai",
				Usage: "Gram deployment the rendered package points at",
			},
			&cli.StringFlag{
				Name:  "version",
				Value: "",
				Usage: "Monotonic publish counter mixed into every plugin.json; empty pins the deterministic default",
			},
			&cli.StringFlag{
				Name:     "out",
				Required: true,
				Usage:    "Directory to render into. Existing generated content is replaced",
			},
		},
		Action: func(c *cli.Context) error {
			files, err := plugins.PublicPlatformMCPFiles(c.String("server-url"), c.String("version"))
			if err != nil {
				return fmt.Errorf("render public Platform MCP files: %w", err)
			}

			out := c.String("out")
			// The publish job copies this directory wholesale into the public
			// repository, so anything already sitting here would be published
			// alongside the render. Refuse rather than delete: the caller named
			// the path, and clearing it for them could destroy unrelated files.
			entries, err := os.ReadDir(out)
			switch {
			case err == nil && len(entries) > 0:
				return fmt.Errorf("output directory %s is not empty; render into a fresh directory", out)
			case err != nil && !os.IsNotExist(err):
				return fmt.Errorf("read output directory: %w", err)
			}
			if err := os.MkdirAll(out, 0o750); err != nil {
				return fmt.Errorf("create output directory: %w", err)
			}

			paths := make([]string, 0, len(files))
			for p := range files {
				paths = append(paths, p)
			}
			sort.Strings(paths)

			for _, p := range paths {
				// Generated paths are constructed from package constants, never
				// from user input, but a traversal here would write outside the
				// output directory so it is rejected rather than trusted.
				if strings.Contains(p, "..") || filepath.IsAbs(p) {
					return fmt.Errorf("unsafe generated path %q", p)
				}
				dest := filepath.Join(out, filepath.FromSlash(p))
				if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
					return fmt.Errorf("create directory for %s: %w", p, err)
				}
				if err := os.WriteFile(dest, files[p], 0o600); err != nil {
					return fmt.Errorf("write %s: %w", p, err)
				}
				_, _ = fmt.Fprintln(os.Stdout, p)
			}
			return nil
		},
	}
}
