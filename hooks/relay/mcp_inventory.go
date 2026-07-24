package relay

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// newMCPInventoryCommand builds the detached collector invocation. Deployment
// identity is forwarded via the same flags main.go accepts so the child
// re-resolves the full config (including the config-file org key) itself,
// keeping every credential out of argv. Session id and cwd ride as extra flags
// SplitInlineFlags leaves untouched.
var newMCPInventoryCommand = func(cfg Config, cwd, sessionID string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}
	args := []string{"mcp-inventory"}
	if cfg.ConfigPath != "" {
		args = append(args, "--config="+cfg.ConfigPath)
	}
	if cfg.ServerURL != "" {
		args = append(args, "--server-url="+cfg.ServerURL)
	}
	if cfg.ProjectSlug != "" {
		args = append(args, "--project="+cfg.ProjectSlug)
	}
	if cfg.OrgID != "" {
		args = append(args, "--org="+cfg.OrgID)
	}
	if cwd != "" {
		args = append(args, "--cwd="+cwd)
	}
	if sessionID != "" {
		args = append(args, "--session-id="+sessionID)
	}
	return exec.Command(exe, args...), nil
}

// spawnMCPInventory launches the detached collector. A package var so tests can
// intercept it. Detached from the hook's process group (like the drain worker)
// so a provider that signals the hook on timeout cannot kill the collection
// mid-run. agenthooks.Main os.Exits as soon as the handler returns, so only
// process creation happens on the hook's path.
var spawnMCPInventory = func(cfg Config, cwd, sessionID string) error {
	cmd, err := newMCPInventoryCommand(cfg, cwd, sessionID)
	if err != nil {
		return err
	}
	cmd.SysProcAttr = drainSysProcAttr()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start mcp-inventory: %w", err)
	}
	return cmd.Process.Release()
}

// RunMCPInventoryCmd is the entrypoint for the detached `mcp-inventory`
// subcommand. It resolves the deployment config exactly as the hook path does,
// then collects and relays the inventory.
func RunMCPInventoryCmd(ctx context.Context, args []string) int {
	flagCfg, rest := SplitInlineFlags(Config{ServerURL: "", ProjectSlug: "", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}, args)
	cfg := LoadConfig(flagCfg)
	fs := flag.NewFlagSet("mcp-inventory", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cwd := fs.String("cwd", "", "")
	sessionID := fs.String("session-id", "", "")
	if fs.Parse(rest) != nil {
		return 1
	}
	return RunMCPInventory(ctx, cfg, *cwd, *sessionID)
}

// RunMCPInventory warms the shared agenthooks MCP inventory cache and relays
// the same list to the server as its own fire-and-forget session.updated event.
// The single `claude mcp list` run serves both purposes: it primes the cache
// that per-tool-call URL attribution reads (so the first MCP tool call never
// stalls) and produces the full inventory for admin visibility. Always
// non-blocking from the hook's perspective because it runs in the detached
// child. Returns 0 even when nothing is sent: an absent CLI, an empty list, or a
// missing credential are all normal and must not surface as a failure.
func RunMCPInventory(ctx context.Context, cfg Config, cwd, sessionID string) int {
	// The CLI fallback only ever adds plugin- and claude.ai-connector servers,
	// which are user-global rather than project-scoped (project servers come
	// from config files via the cwd-aware fast path), so the collector needs no
	// particular working directory.
	listed := agenthooks.WarmClaudeMCPList()
	if len(listed) == 0 {
		return 0
	}
	entries := make([]mcpInventoryEntry, 0, len(listed))
	for _, e := range listed {
		entries = append(entries, mcpInventoryEntry{Name: e.Name, URL: e.URL, Command: e.Command})
	}
	NewRelay(cfg).sendMCPInventory(ctx, sessionID, cwd, entries)
	return 0
}

// sendMCPInventory relays a collected inventory snapshot. It mirrors deliver's
// pre-send guards (broken config, insecure URL, no credential) so a detached
// collector never leaks events an in-line hook would have skipped.
func (r *Relay) sendMCPInventory(ctx context.Context, sessionID, cwd string, entries []mcpInventoryEntry) {
	if r.cfg.ConfigError != "" || insecureServerURL(r.cfg.ServerURL) {
		return
	}
	c, ok := resolveAuth(r.cfg)
	if !ok {
		return
	}
	now := time.Now().UTC()
	payload := components.IngestRequestBody{
		SchemaVersion: schemaVersion,
		Source: components.HookIngestSource{
			Adapter:        adapterSlug(agenthooks.ProviderClaudeCode),
			AdapterVersion: nil,
			RawEventName:   optStr("SessionStart"),
			Hostname:       optStr(hostname()),
			UserEmail:      nil,
		},
		Session: &components.HookIngestSession{
			ID:     optStr(sessionID),
			TurnID: nil,
			Cwd:    optStr(cwd),
			Model:  nil,
		},
		Event: components.HookIngestEvent{
			Type:       components.TypeSessionUpdated,
			OccurredAt: &now,
		},
		Data: &components.HookIngestData{
			Mcp:            nil,
			McpAttribution: nil,
			McpInventory:   nil,
			Message:        nil,
			Notification:   nil,
			Prompt:         nil,
			Skill:          nil,
			ToolCall:       nil,
			Usage:          nil,
		},
		Raw: nil,
	}
	attachMCPInventory(&payload, entries)
	if isEmptyData(payload.Data) {
		return
	}
	idemKey := newIdempotencyToken()
	res := r.send(ctx, c, payload, idemKey)
	r.debugf("mcp-inventory relayed servers=%d status=%d", len(payload.Data.McpInventory), res.statusCode)
	r.finishExchange(idemKey, payload, res, nil)
}

type mcpInventoryEntry struct {
	Name    string
	URL     string
	Command string
}

func attachMCPInventory(payload *components.IngestRequestBody, entries []mcpInventoryEntry) {
	if len(entries) == 0 {
		return
	}
	if payload.Data == nil {
		payload.Data = &components.HookIngestData{
			Mcp:            nil,
			McpAttribution: nil,
			McpInventory:   nil,
			Message:        nil,
			Notification:   nil,
			Prompt:         nil,
			Skill:          nil,
			ToolCall:       nil,
			Usage:          nil,
		}
	}
	payload.Data.McpInventory = make([]components.HookMCPData, 0, len(entries))
	for _, entry := range entries {
		redactedURL := ""
		if entry.URL != "" {
			var ok bool
			redactedURL, ok = redactMCPInventoryURL(entry.URL)
			if !ok {
				continue
			}
		}
		payload.Data.McpInventory = append(payload.Data.McpInventory, components.HookMCPData{
			ServerName:     optStr(entry.Name),
			ServerIdentity: optStr(entry.Name),
			URL:            optStr(redactedURL),
			Command:        optStr(redactCommand(entry.Command)),
			ResultJSON:     nil,
		})
	}
}

// redactMCPInventoryURL omits malformed absolute HTTP URLs from the snapshot.
// The hook still continues; only the unsafe inventory entry is skipped. The
// generic tool-call redactor preserves unparseable strings for observability,
// but a bulk-collected snapshot must not transmit a raw URL whose credentials
// could not be inspected.
func redactMCPInventoryURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return redactURL(raw), true
}
