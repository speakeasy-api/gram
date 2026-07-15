// Command speakeasy-hooks is the single Speakeasy hooks binary. It receives
// coding-agent hook events, relays them to the Gram server, honors the server's
// blocking decisions, and can perform an interactive browser sign-in on its
// own so it doubles as the mid-session auth fallback.
//
// Invocation contract (baked into generated provider configs):
//
//	speakeasy-hooks agenthooks run --provider=claude-code   # hook event on stdin
//	speakeasy-hooks login [--force] [--config=<path>]        # interactive sign-in
//
// The server URL, project slug, and org id come from the GRAM_HOOKS_* env vars
// injected by the generated config, falling back to the production defaults.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/gram/hooks/relay"
)

// version is stamped by goreleaser at release time.
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Printf("speakeasy-hooks %s\n", version)
			os.Exit(0)
		case "login":
			// login accepts the same deployment flags as the hook path
			// (--config, --server-url, --project, --org) so the nudge can
			// point it at the plugin's identity instead of the prod defaults.
			flagCfg, rest := relay.SplitInlineFlags(relay.Config{ServerURL: "", ProjectSlug: "", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}, os.Args[2:])
			os.Exit(runLogin(relay.LoadConfig(flagCfg), rest))
		case "install":
			os.Exit(runInstall(os.Args[2:]))
		case "drain":
			// Replays the offline payload spool (see relay/drain.go). Takes
			// no arguments — spool entries carry their own deployment
			// identity. Invoked by hooks opportunistically after a
			// successful send, and by the device agent when its downtime
			// detector sees the control plane recover.
			os.Exit(relay.RunDrain(context.Background(), os.Stdout))
		}
	}

	// The install packaging points the hook command at the plugin's
	// speakeasy.json via --config. Read the deployment flags without mutating
	// argv: agenthooks tolerates unknown flags, and its --async re-exec
	// forwards argv to the detached worker, which must keep --config.
	flagCfg, _ := relay.SplitInlineFlags(relay.Config{ServerURL: "", ProjectSlug: "", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}, os.Args[1:])
	cfg := relay.LoadConfig(flagCfg)

	agenthooks.Main(relay.NewRunner(cfg))
}

// runInstall renders a provider plugin package that drives this binary. It
// backs local end-to-end testing; production distribution is wired separately.
func runInstall(args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	provider := fs.String("provider", "", "provider slug: claude-code, cursor, codex")
	dir := fs.String("dir", "", "output directory for the plugin package")
	serverURL := fs.String("server-url", relay.DefaultServerURL, "Gram server URL to bake into the plugin")
	project := fs.String("project", "default", "project slug")
	org := fs.String("org", "", "organization id hint")
	browserLogin := fs.Bool("browser-login", false, "enable per-user browser sign-in")
	nonblocking := fs.Bool("nonblocking", false, "record events without enforcing deny decisions")
	binary := fs.String("binary", "", "path to the speakeasy-hooks binary (defaults to this executable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *provider == "" || *dir == "" {
		fmt.Fprintln(os.Stderr, "speakeasy-hooks install: --provider and --dir are required")
		return 2
	}
	binaryPath := *binary
	if binaryPath == "" {
		if exe, err := os.Executable(); err == nil {
			binaryPath = exe
		} else {
			binaryPath = "speakeasy-hooks"
		}
	}
	if err := relay.WritePlugin(context.Background(), *provider, *dir, relay.PluginConfig{
		ServerURL:    *serverURL,
		ProjectSlug:  *project,
		OrgID:        *org,
		HooksAPIKey:  os.Getenv("GRAM_HOOKS_ORG_KEY"),
		BrowserLogin: *browserLogin,
		Nonblocking:  *nonblocking,
		BinaryPath:   binaryPath,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "speakeasy-hooks install: %v\n", err)
		return 1
	}
	return 0
}

func runLogin(cfg relay.Config, rest []string) int {
	force := false
	for _, a := range rest {
		if a == "--force" || a == "-f" {
			force = true
		}
	}
	if err := relay.NewRelay(cfg).Login(context.Background(), force); err != nil {
		fmt.Fprintf(os.Stderr, "speakeasy-hooks login: %v\n", err)
		return 1
	}
	return 0
}
