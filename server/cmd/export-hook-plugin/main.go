// Command export-hook-plugin renders the dogfood hook plugins (plugin-claude,
// plugin-cursor) from the same generators that publish customer plugins. The
// rendered trees are not checked in — this exists for local development, e.g.
// running the Claude plugin against a dev server:
//
//	out=$(mktemp -d)
//	go run ./cmd/export-hook-plugin -out "$out"
//	claude --plugin-dir "$out/plugin-claude"
//
// The mise task `hooks:test` wraps exactly that flow.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// localClaudeManifest makes the rendered plugin-claude tree loadable via
// `claude --plugin-dir`. Published plugins get an org-derived manifest from the
// publish path instead; this one only names the local-dev copy.
const localClaudeManifest = `{
  "name": "gram-hooks-local",
  "description": "Forward Claude Code hooks to Gram for analytics, monitoring, and compliance",
  "version": "0.0.1",
  "author": {
    "name": "Gram",
    "url": "https://getgram.ai"
  }
}
`

func main() {
	out := flag.String("out", "", "directory to render the plugin-claude and plugin-cursor trees into (required)")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "export-hook-plugin: -out is required")
		os.Exit(1)
	}

	files, err := plugins.DogfoodPluginFiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "render dogfood plugins: %v\n", err)
		os.Exit(1)
	}
	files["plugin-claude/.claude-plugin/plugin.json"] = []byte(localClaudeManifest)
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
