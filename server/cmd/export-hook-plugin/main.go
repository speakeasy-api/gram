// Command export-hook-plugin regenerates the checked-in dogfood hook plugins
// under hooks/plugin-claude and hooks/plugin-cursor from the same generators
// that publish customer plugins. Run it after changing the plugin script
// renderers in internal/plugins:
//
//	go run ./cmd/export-hook-plugin
//
// TestDogfoodPluginsMatchCheckedIn fails when the checked-in copies drift
// from the renders.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

func main() {
	out := flag.String("out", "../hooks", "directory holding the checked-in plugin-claude and plugin-cursor trees")
	flag.Parse()

	files, err := plugins.DogfoodPluginFiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "render dogfood plugins: %v\n", err)
		os.Exit(1)
	}
	// The hooks/ subtrees are fully rendered; prune them first so a file the
	// generators no longer emit does not linger as a stale checked-in copy.
	// The hand-maintained plugin manifests live outside hooks/ and survive.
	for _, plugin := range []string{"plugin-claude", "plugin-cursor"} {
		if err := os.RemoveAll(filepath.Join(*out, plugin, "hooks")); err != nil {
			fmt.Fprintf(os.Stderr, "prune %s/hooks: %v\n", plugin, err)
			os.Exit(1)
		}
	}
	for name, content := range files {
		dst := filepath.Join(*out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir for %s: %v\n", dst, err)
			os.Exit(1)
		}
		mode := os.FileMode(0o644)
		if filepath.Ext(dst) == ".sh" {
			mode = 0o755
		}
		if err := os.WriteFile(dst, content, mode); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", dst, err)
			os.Exit(1)
		}
		fmt.Println(dst)
	}
}
