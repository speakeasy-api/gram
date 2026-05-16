// dev-observability-plugin renders an observability plugin directory using
// the same generator (server/internal/plugins.GenerateObservabilityPluginPackage)
// that the publish flow uses, so the local hooks test harness exercises the
// real templated hook.sh instead of a hand-maintained stub that drifts.
//
// Intended caller: .mise-tasks/hooks/test.sh.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

func main() {
	var (
		out         = flag.String("out", "", "Output directory (will be created; existing contents preserved)")
		platform    = flag.String("platform", "claude", "Plugin platform: claude, cursor, or codex")
		apiKey      = flag.String("api-key", "", "Plaintext hooks-scoped Gram API key to bake into hook.sh")
		projectSlug = flag.String("project-slug", "", "Project slug to bake into hook.sh as Gram-Project header")
		serverURL   = flag.String("server-url", "", "Gram server URL the hook script will POST to")
		orgName     = flag.String("org-name", "Gram Local", "Org display name for plugin.json")
		orgEmail    = flag.String("org-email", "", "Org email for plugin.json (optional)")
	)
	flag.Parse()

	if *out == "" || *apiKey == "" || *projectSlug == "" || *serverURL == "" {
		fmt.Fprintln(os.Stderr, "usage: dev-observability-plugin --out DIR --api-key KEY --project-slug SLUG --server-url URL [--platform claude|cursor|codex] [--org-name NAME]")
		os.Exit(2)
	}

	files, err := plugins.GenerateObservabilityPluginPackage(plugins.GenerateConfig{
		OrgName:     *orgName,
		OrgEmail:    *orgEmail,
		ServerURL:   *serverURL,
		HooksAPIKey: *apiKey,
		ProjectSlug: *projectSlug,
	}, *platform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate plugin: %v\n", err)
		os.Exit(1)
	}

	for relPath, content := range files {
		dst := filepath.Join(*out, relPath)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", filepath.Dir(dst), err)
			os.Exit(1)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(relPath, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(dst, content, mode); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", dst, err)
			os.Exit(1)
		}
	}

	fmt.Fprintf(os.Stderr, "Rendered %d files to %s\n", len(files), *out)
}
