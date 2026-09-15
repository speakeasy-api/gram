package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/speakeasy-api/agenthooks"
)

// ProviderPi is the provider slug stamped onto events decoded from the Pi
// coding agent (pi.dev). agenthooks ships no Pi codec: Pi has no hook config
// dialect at all — its hook surface is a TypeScript extension API — so the
// relay decodes Pi's frames itself and drives the same handlers every other
// provider goes through. The canonical ingest adapter slug is derived from
// this value, so the server records Pi traffic under "pi".
const ProviderPi agenthooks.Provider = "pi"

// Pi extension event names the shim forwards. They are Pi's own event names
// (pi.on("tool_call", ...)), which keeps the raw event name on the ingest
// envelope meaningful for anyone reading Pi's documentation.
const (
	piHookInitialize      = "initialize"
	piHookSessionStart    = "session_start"
	piHookSessionShutdown = "session_shutdown"
	piHookInput           = "input"
	piHookToolCall        = "tool_call"
	piHookToolResult      = "tool_result"
	piHookMessageEnd      = "message_end"
)

// maxPiFrameBytes bounds one NDJSON frame. Pi tool results carry model-visible
// output the shim already truncates, so a frame beyond this is malformed
// rather than large, and a bounded scanner keeps a runaway extension from
// growing the relay's heap without limit.
const maxPiFrameBytes = 8 << 20

// piFrame is one request frame the Pi extension shim writes to the relay's
// stdin, newline-delimited. The shim flattens each Pi event into Input so the
// payload shape is a contract between the generated shim and this decoder
// rather than a projection of Pi's evolving handler signatures.
type piFrame struct {
	// Seq correlates the reply with the awaiting handler in the shim. It is
	// echoed verbatim.
	Seq int64 `json:"seq"`

	// Hook is the Pi event name (session_start, tool_call, ...).
	Hook string `json:"hook"`

	// Session carries the session identity Pi exposes through the extension
	// context, repeated on every frame because Pi events carry no session
	// fields of their own.
	Session piFrameSession `json:"session"`

	// Input is the flattened event payload. It rides onto the event as the
	// verbatim provider payload.
	Input json.RawMessage `json:"input"`
}

// piFrameSession is the session identity the shim reads off Pi's extension
// context (ctx.sessionManager.getSessionId(), ctx.cwd, ctx.model).
type piFrameSession struct {
	// ID is Pi's session id, or "" for an ephemeral session.
	ID string `json:"id"`

	// TurnID identifies the agent turn the event belongs to, so tool calls
	// and the assistant response of one turn correlate.
	TurnID string `json:"turn_id"`

	// CWD is the directory Pi runs in.
	CWD string `json:"cwd"`

	// Model is the active model id, when Pi has one loaded.
	Model string `json:"model"`
}

// piReply is the relay's response frame. Absent fields mean "no decision":
// the shim then returns nothing to Pi and the action proceeds.
type piReply struct {
	// Seq echoes the request frame's sequence number.
	Seq int64 `json:"seq"`

	// Block asks the shim to stop the gated action — a blocked tool call, or
	// a user prompt Pi must not hand to the model.
	Block bool `json:"block,omitempty"`

	// Reason is the user-facing text the shim surfaces with the block.
	Reason string `json:"reason,omitempty"`
}

// piPromptData is the input frame for Pi's input event.
type piPromptData struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

// piToolData is the input frame for Pi's tool_call and tool_result events.
type piToolData struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
	Output     json.RawMessage `json:"output"`
	IsError    bool            `json:"is_error"`
	Error      string          `json:"error"`
	DurationMS *float64        `json:"duration_ms"`
}

// piMessageData is the input frame for Pi's message_end event, restricted by
// the shim to finalized assistant messages.
type piMessageData struct {
	Text       string   `json:"text"`
	StopReason string   `json:"stop_reason"`
	Usage      *piUsage `json:"usage"`
}

// piUsage mirrors pi-ai's Usage: per-response token counts plus the cost
// breakdown, of which only the total is reported.
type piUsage struct {
	Input      *int     `json:"input"`
	Output     *int     `json:"output"`
	CacheRead  *int     `json:"cache_read"`
	CacheWrite *int     `json:"cache_write"`
	Cost       *float64 `json:"cost"`
}

// piSessionData is the input frame for Pi's session_start and
// session_shutdown events.
type piSessionData struct {
	Reason string `json:"reason"`
}

// RunPiServe serves the Pi extension shim over NDJSON stdio, translating each
// Pi event into the canonical Gram hook contract and answering with the
// server's verdict. It returns the process exit code.
//
// Frames are processed strictly in order: Pi awaits each handler, and a tool
// call must not execute before its verdict is known. A frame the relay cannot
// decode is answered with an empty reply so a shim/binary skew degrades to
// reporting nothing rather than wedging the agent — the credential ratchet and
// the org's fail-open posture govern the decisions the relay does make.
func RunPiServe(ctx context.Context, cfg Config, stdin io.Reader, stdout io.Writer) int {
	r := NewRelay(cfg)
	inventory := newPiInventoryReporter()

	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 0, 64<<10), maxPiFrameBytes)
	enc := json.NewEncoder(stdout)

	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var frame piFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			r.debugf("pi: bad shim frame: %v", err)
			continue
		}
		reply := piReply{Seq: frame.Seq, Block: false, Reason: ""}
		if frame.Hook != piHookInitialize {
			inventory.report(ctx, r, frame)
			block, reason := r.dispatchPiFrame(ctx, frame)
			reply.Block = block
			reply.Reason = reason
		}
		if err := enc.Encode(reply); err != nil {
			r.debugf("pi: write reply: %v", err)
			return 1
		}
	}
	if err := sc.Err(); err != nil {
		r.debugf("pi: read shim stream: %v", err)
		return 1
	}
	return 0
}

// dispatchPiFrame routes one decoded frame through the relay handler for its
// kind and reports the gating outcome. Unknown hooks are ignored: the shim
// subscribes a fixed set, and relaying an event the decoder cannot classify
// would store telemetry with no canonical type.
func (r *Relay) dispatchPiFrame(ctx context.Context, frame piFrame) (bool, string) {
	switch frame.Hook {
	case piHookSessionStart:
		var data piSessionData
		_ = json.Unmarshal(frame.Input, &data)
		_, _ = r.onSessionStart(ctx, &agenthooks.SessionStartEvent{
			Event:  piEvent(frame, agenthooks.KindSessionStart),
			Source: data.Reason,
		})
		return false, ""

	case piHookSessionShutdown:
		var data piSessionData
		_ = json.Unmarshal(frame.Input, &data)
		_ = r.onObserve(ctx, &agenthooks.SessionEndEvent{
			Event:  piEvent(frame, agenthooks.KindSessionEnd),
			Reason: data.Reason,
		})
		return false, ""

	case piHookInput:
		var data piPromptData
		if err := json.Unmarshal(frame.Input, &data); err != nil {
			r.debugf("pi: decode %s: %v", frame.Hook, err)
			return false, ""
		}
		dec, _ := r.onPrompt(ctx, &agenthooks.PromptEvent{
			Event:  piEvent(frame, agenthooks.KindPromptSubmitted),
			Prompt: data.Text,
		})
		return piDecision(dec, "Speakeasy blocked this prompt.")

	case piHookToolCall:
		var data piToolData
		if err := json.Unmarshal(frame.Input, &data); err != nil {
			r.debugf("pi: decode %s: %v", frame.Hook, err)
			return false, ""
		}
		event := piEvent(frame, agenthooks.KindToolPre)
		dec, _ := r.onToolPre(ctx, &agenthooks.ToolPreEvent{
			Event: event,
			Tool:  piToolCall(event.Session, &data),
		})
		return piDecision(dec, "Speakeasy blocked this tool call.")

	case piHookToolResult:
		var data piToolData
		if err := json.Unmarshal(frame.Input, &data); err != nil {
			r.debugf("pi: decode %s: %v", frame.Hook, err)
			return false, ""
		}
		kind := agenthooks.KindToolPost
		if data.IsError {
			kind = agenthooks.KindToolError
		}
		event := piEvent(frame, kind)
		_, _ = r.onToolPost(ctx, &agenthooks.ToolPostEvent{
			Event:      event,
			Tool:       piToolCall(event.Session, &data),
			Output:     normalizeOutput(data.Output),
			Failed:     data.IsError,
			Error:      data.Error,
			DurationMS: data.DurationMS,
		})
		return false, ""

	case piHookMessageEnd:
		var data piMessageData
		if err := json.Unmarshal(frame.Input, &data); err != nil {
			r.debugf("pi: decode %s: %v", frame.Hook, err)
			return false, ""
		}
		_, _ = r.onStop(ctx, &agenthooks.StopEvent{
			Event:               piEvent(frame, agenthooks.KindStop),
			PreviouslyContinued: false,
			LoopCount:           0,
			FinalMessage:        data.Text,
			Usage:               piUsageOf(data.Usage, data.StopReason),
		})
		return false, ""

	default:
		r.debugf("pi: unhandled hook %q", frame.Hook)
		return false, ""
	}
}

// piEvent builds the unified envelope for one Pi frame. Raw is the shim's
// flattened payload: Pi's handler arguments are live JavaScript objects with
// no wire form of their own, so the shim's projection is the most verbatim
// payload that exists for a Pi event.
func piEvent(frame piFrame, kind agenthooks.EventKind) agenthooks.Event {
	roots := []string(nil)
	if frame.Session.CWD != "" {
		roots = []string{frame.Session.CWD}
	}
	return agenthooks.Event{
		Provider:   ProviderPi,
		Variant:    agenthooks.VariantCLI,
		NativeName: frame.Hook,
		Kind:       kind,
		Time:       time.Now().UTC(),
		Session: agenthooks.SessionInfo{
			ID:             frame.Session.ID,
			TurnID:         frame.Session.TurnID,
			CWD:            frame.Session.CWD,
			WorkspaceRoots: roots,
			TranscriptPath: "",
			Model:          frame.Session.Model,
			PermissionMode: "",
			UserEmail:      "",
		},
		Agent:               nil,
		DetectionConfidence: agenthooks.DetectionConfig,
		Backfilled:          false,
		Raw:                 frame.Input,
		Ext:                 nil,
	}
}

// piToolCall normalizes a Pi tool invocation, resolving MCP identity and
// transport for tools an MCP extension bridged into Pi.
func piToolCall(session agenthooks.SessionInfo, data *piToolData) agenthooks.ToolCall {
	input := normalizePiToolInput(data.Input)
	tc := agenthooks.ToolCall{
		ID:          data.ToolCallID,
		Synthesized: false,
		Name:        data.ToolName,
		Canonical:   agenthooks.CanonicalToolFor(data.ToolName),
		MCP:         agenthooks.ParseMCPName(data.ToolName),
		Input:       input,
		RawInput:    data.Input,
	}
	if tc.ID == "" {
		tc.ID = agenthooks.SynthesizeToolID(session.ID, session.TurnID, data.ToolName, input)
		tc.Synthesized = true
	}
	resolvePiMCP(&tc, loadPiMCPServers(session.CWD))
	return tc
}

// normalizePiToolInput guarantees the tool input is a JSON object the way the
// unified ToolCall contract promises: Pi hands extensions a parameters object,
// but a custom tool registered by another extension can declare a scalar
// schema, and a non-object input must stay visible rather than be dropped.
func normalizePiToolInput(in json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(in))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("{}")
	}
	if trimmed[0] == '{' && json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	wrapped, err := json.Marshal(map[string]json.RawMessage{"value": json.RawMessage(trimmed)})
	if err != nil {
		return json.RawMessage("{}")
	}
	return wrapped
}

// piUsageOf lifts the assistant message's token and cost totals onto the stop
// event. Pi reports usage per assistant message, which is the same per-turn
// granularity the canonical usage block carries.
func piUsageOf(usage *piUsage, stopReason string) *agenthooks.Usage {
	if usage == nil {
		return nil
	}
	return &agenthooks.Usage{
		InputTokens:      usage.Input,
		OutputTokens:     usage.Output,
		CacheReadTokens:  usage.CacheRead,
		CacheWriteTokens: usage.CacheWrite,
		Cost:             usage.Cost,
		LoopCount:        nil,
		Status:           stopReason,
	}
}

// piDecision reads the gating outcome off a relay decision, falling back to
// fallbackReason when the server denied without a message so the user is
// never shown an unexplained block.
func piDecision(dec agenthooks.Decision, fallbackReason string) (bool, string) {
	if dec == nil || !dec.Blocks() {
		return false, ""
	}
	for _, candidate := range []string{dec.SystemMessage(), dec.Reason(), fallbackReason} {
		if strings.TrimSpace(candidate) != "" {
			return true, candidate
		}
	}
	return true, fallbackReason
}

// piInventoryReporter reports the MCP inventory Pi's workspace exposes, at
// most once per distinct snapshot per serve process. Pi ships no MCP client,
// so every server visible to it comes from a third-party MCP extension's
// config file; reporting the snapshot at session start puts those servers in
// Shadow MCP inventory before any tool is called, the same way the Claude and
// OpenCode paths do.
type piInventoryReporter struct {
	reported map[string]bool
}

func newPiInventoryReporter() *piInventoryReporter {
	return &piInventoryReporter{reported: map[string]bool{}}
}

// report sends a snapshot for session-scoped frames and for MCP tool calls,
// mirroring agenthooks' reporting points. It is a no-op once a snapshot with
// the same contents has been reported for the session.
func (p *piInventoryReporter) report(ctx context.Context, r *Relay, frame piFrame) {
	switch frame.Hook {
	case piHookSessionStart, piHookToolCall:
	default:
		return
	}
	if frame.Session.ID == "" {
		return
	}
	servers := loadPiMCPServers(frame.Session.CWD)
	if len(servers) == 0 {
		return
	}
	key := frame.Session.ID + "\x00" + piInventoryDigest(servers)
	if p.reported[key] {
		return
	}
	// The snapshot is marked reported before delivery: a failed report is
	// retried on the next session or config change, and retrying it on every
	// tool call of a session whose control plane is down would gate each call
	// on a second request.
	p.reported[key] = true
	event := piEvent(frame, agenthooks.KindMCPInventory)
	event.NativeName = "mcp_inventory"
	if err := r.onMCPInventory(ctx, &agenthooks.MCPInventoryEvent{
		Event: event,
		// Pi's own configuration knows nothing about MCP, so a config-file
		// read is the complete picture of what an MCP extension can reach.
		Servers:  servers,
		Complete: true,
	}); err != nil {
		r.debugf("pi: report MCP inventory: %v", err)
	}
}

func piInventoryDigest(servers []agenthooks.MCPServer) string {
	parts := make([]string, 0, len(servers))
	for _, s := range servers {
		parts = append(parts, s.Name+"\x00"+s.URL+"\x00"+s.Command)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x01")
}

// loadPiMCPServers reads the MCP servers reachable from a Pi workspace.
//
// Pi has no first-party MCP client, so there is no canonical config location:
// the established MCP extensions for Pi read a Claude-Desktop-shaped file at
// <cwd>/.pi/mcp.json (project) and ~/.pi/agent/mcp.json (global), which is
// what this reads. Project entries win over global entries of the same name,
// matching the precedence Pi applies to its own project-local configuration.
func loadPiMCPServers(cwd string) []agenthooks.MCPServer {
	paths := make([]string, 0, 2)
	if cwd != "" {
		paths = append(paths, filepath.Join(cwd, piConfigDirName, "mcp.json"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, piConfigDirName, "agent", "mcp.json"))
	}

	seen := map[string]bool{}
	out := make([]agenthooks.MCPServer, 0, 4)
	for _, path := range paths {
		for _, server := range readPiMCPConfig(path) {
			if server.Name == "" || seen[server.Name] {
				continue
			}
			seen[server.Name] = true
			out = append(out, server)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// piConfigDirName is Pi's config directory name. Pi exports CONFIG_DIR_NAME to
// extensions because rebranded distributions can rename it; a relay reading
// the file from outside the Pi process only knows the default.
const piConfigDirName = ".pi"

// piMCPConfig is the MCP config file shape the Pi MCP extensions read. Both
// key spellings are accepted: "mcpServers" is the Claude-Desktop-compatible
// name they document, and "mcp" appears in configs carried over from agents
// that use that spelling.
type piMCPConfig struct {
	MCPServers map[string]piMCPServer `json:"mcpServers"`
	MCP        map[string]piMCPServer `json:"mcp"`
}

// piMCPServer is one configured MCP server. Remote servers carry a url, stdio
// servers a command plus args.
type piMCPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	URL     string   `json:"url"`
	Enabled *bool    `json:"enabled"`
}

func readPiMCPConfig(path string) []agenthooks.MCPServer {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc piMCPConfig
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	out := make([]agenthooks.MCPServer, 0, len(doc.MCPServers)+len(doc.MCP))
	for _, servers := range []map[string]piMCPServer{doc.MCPServers, doc.MCP} {
		for name, server := range servers {
			if server.Enabled != nil && !*server.Enabled {
				continue
			}
			command := strings.TrimSpace(server.Command)
			if command != "" && len(server.Args) > 0 {
				command = strings.Join(append([]string{command}, server.Args...), " ")
			}
			out = append(out, agenthooks.MCPServer{
				Name:    strings.TrimSpace(name),
				URL:     strings.TrimSpace(server.URL),
				Command: command,
			})
		}
	}
	return out
}

// resolvePiMCP attaches MCP identity and transport to a Pi tool call.
//
// MCP extensions register bridged tools as ordinary Pi tools, so the name is
// the only signal. Names in one of the reserved MCP dialects (mcp__s__t,
// mcp_s_t) are decoded by the shared parser and only need their transport
// filled in; the rest are matched against the configured server names with
// both separators Pi extensions use. Longest match wins and a tie resolves to
// nothing: misattributing a call to the wrong server is worse than reporting
// an unattributed tool call.
func resolvePiMCP(tc *agenthooks.ToolCall, servers []agenthooks.MCPServer) {
	if len(servers) == 0 {
		return
	}
	if tc.MCP != nil && tc.MCP.Server != "" {
		for i := range servers {
			if servers[i].Name != tc.MCP.Server {
				continue
			}
			tc.MCP.URL = servers[i].URL
			tc.MCP.Command = servers[i].Command
			tc.MCP.FromConfig = true
			tc.Canonical = agenthooks.ToolMCP
			return
		}
		return
	}

	matched, server, tool := matchPiMCPName(servers, tc.Name)
	if matched == nil {
		return
	}
	tc.MCP = &agenthooks.MCPCall{
		Server:     server,
		Tool:       tool,
		URL:        matched.URL,
		Command:    matched.Command,
		FromConfig: true,
	}
	tc.Canonical = agenthooks.ToolMCP
}

// matchPiMCPName finds the configured server whose name, followed by a
// separator, prefixes the tool name and returns it with the resolved server
// and tool names. The longest configured name wins, so a server named
// "notes_admin" claims notes_admin_delete ahead of a server named "notes";
// two different names cannot both prefix one tool name at the same length, so
// the longest match is always unique.
func matchPiMCPName(servers []agenthooks.MCPServer, name string) (*agenthooks.MCPServer, string, string) {
	rest := name
	for _, prefix := range []string{"mcp__", "mcp_"} {
		if trimmed, ok := strings.CutPrefix(rest, prefix); ok {
			rest = trimmed
			break
		}
	}

	var (
		best     *agenthooks.MCPServer
		bestName string
		bestTool string
	)
	for i := range servers {
		configured := servers[i].Name
		if configured == "" || len(configured) <= len(bestName) {
			continue
		}
		for _, sep := range []string{"__", "_"} {
			tool, ok := strings.CutPrefix(rest, configured+sep)
			if !ok || tool == "" {
				continue
			}
			best, bestName, bestTool = &servers[i], configured, tool
			break
		}
	}
	if best == nil {
		return nil, "", ""
	}
	return best, bestName, bestTool
}

// piServeUsage documents the serve subcommand's contract for a misinvocation.
const piServeUsage = "usage: speakeasy-hooks pi serve --config=<path>"

// RunPiCommand runs the pi subcommand. Only the serve mode exists: Pi drives
// hooks through a long-lived extension, never a per-event process.
func RunPiCommand(ctx context.Context, cfg Config, args []string) int {
	mode := ""
	if len(args) > 0 {
		mode = args[0]
	}
	if mode != "serve" {
		fmt.Fprintf(os.Stderr, "speakeasy-hooks pi: unknown mode %q\n%s\n", mode, piServeUsage)
		return 64
	}
	return RunPiServe(ctx, cfg, os.Stdin, os.Stdout)
}
