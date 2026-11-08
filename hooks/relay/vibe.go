package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/agenthooks"
)

// ProviderVibe is the provider slug stamped onto events decoded from Mistral
// Vibe (the vibe CLI). agenthooks ships no Vibe codec: Vibe drives hooks as
// one subprocess per event reading a single JSON object on stdin, which the
// external module's `agenthooks run --provider=...` dispatch cannot express
// (it rejects providers it does not know), so the relay decodes Vibe's wire
// protocol itself and drives the same handlers every other provider goes
// through. The canonical ingest adapter slug is derived from this value, so
// the server records Vibe traffic under "vibe".
const ProviderVibe agenthooks.Provider = "vibe"

// Vibe hook event names, the hook_event_name discriminant of the stdin
// payload. Vibe has exactly three hook types; there is no session or prompt
// hook, so session telemetry rides on the tool/agent events.
const (
	vibeHookPreTool   = "pre_tool"
	vibeHookPostTool  = "post_tool"
	vibeHookPostAgent = "post_agent"
)

// Vibe tool_status values on post_tool. A non-zero exit code is still
// "success"; only a thrown tool error is "failure".
const (
	vibeToolStatusSuccess = "success"
	vibeToolStatusFailure = "failure"
)

// Vibe structured-response decision values.
const (
	vibeDecisionAllow = "allow"
	vibeDecisionDeny  = "deny"
)

// maxVibePayloadBytes bounds the single stdin JSON object. Tool outputs carry
// model-visible text Vibe already truncates, so an object beyond this is
// malformed rather than large.
const maxVibePayloadBytes = 8 << 20

// vibeHookInput is the JSON object Vibe writes to the hook process's stdin.
// Fields absent for an event type decode as their zero value; Vibe tolerates
// unknown fields on its side, and this decoder ignores the ones it does not
// read.
type vibeHookInput struct {
	// HookEventName discriminates the payload: pre_tool, post_tool, post_agent.
	HookEventName string `json:"hook_event_name"`

	// Session identity, present on every event.
	SessionID       string  `json:"session_id"`
	ParentSessionID *string `json:"parent_session_id"` // non-nil inside a subagent
	TranscriptPath  string  `json:"transcript_path"`
	CWD             string  `json:"cwd"`

	// AgentName is the agent configuration name on post_agent (null when
	// unknown).
	AgentName *string `json:"agent_name"`

	// Tool call fields on pre_tool and post_tool. ToolName is always the
	// qualified name: file_system.bash, mcp_<server>.<tool> (with
	// non-[A-Za-z0-9_$] characters mapped to _ by Vibe), connector_<...>.
	ToolName   string          `json:"tool_name"`
	ToolCallID string          `json:"tool_call_id"`
	ToolInput  json.RawMessage `json:"tool_input"`

	// Completion fields on post_tool. ToolOutput is the structured result
	// (null on failure); ToolOutputText is the text the LLM will see, already
	// mutable by earlier hooks in the chain; ToolError is populated on
	// failure.
	ToolStatus     string          `json:"tool_status"`
	ToolOutput     json.RawMessage `json:"tool_output"`
	ToolOutputText string          `json:"tool_output_text"`
	ToolError      json.RawMessage `json:"tool_error"`
	DurationMS     *float64        `json:"duration_ms"`
}

// vibeResponse is the structured stdout reply (always paired with exit 0).
// Vibe treats empty stdout as pass-through and any other non-conforming
// stdout as a failure, so the relay always emits this exact schema.
type vibeResponse struct {
	// Decision is "allow" or "deny"; Vibe defaults it to "allow" when omitted,
	// but the relay states it explicitly.
	Decision string `json:"decision,omitempty"`
	// Reason is required when Decision is "deny": the tool error on pre_tool,
	// the tool_output_text replacement on post_tool, the retry user message
	// on post_agent.
	Reason string `json:"reason,omitempty"`
	// SystemMessage is an optional UI note.
	SystemMessage string `json:"system_message,omitempty"`
	// HookSpecificOutput carries the per-hook-type mutations: tool_input is a
	// full replacement of the model's arguments (pre_tool only);
	// additional_context is appended to tool_output_text (post_tool only).
	HookSpecificOutput *vibeHookSpecificOutput `json:"hook_specific_output,omitempty"`
}

type vibeHookSpecificOutput struct {
	ToolInput         json.RawMessage `json:"tool_input,omitempty"`
	AdditionalContext string          `json:"additional_context,omitempty"`
}

// vibeInvocation is the parsed vibe-flavored argv: consumer flags before the
// "agenthooks" sentinel are dropped, exactly like agenthooks' parseArgs.
type vibeInvocation struct {
	timeout time.Duration
	// strict mirrors the hook's strict = true in hooks.toml. Vibe enforces
	// strict itself on a non-zero exit, but the relay also accepts it as a
	// flag (the generated hook command carries --strict) so a failure can be
	// answered with the structured deny/clear response directly.
	strict bool
}

// IsVibeInvocation reports whether args drive the relay's Vibe adapter: an
// "agenthooks" sentinel anywhere in argv followed by --provider=vibe. main
// routes these to RunVibeCommand because the external agenthooks module
// rejects providers it does not know at parse time.
func IsVibeInvocation(args []string) bool {
	_, rest, found := splitVibeArgs(args)
	if !found {
		return false
	}
	return slices.Contains(rest, "--provider=vibe")
}

// splitVibeArgs returns the argv after the "agenthooks" sentinel (consumer
// flags like --config precede it, exactly like agenthooks' parseArgs), with
// the mode word split off separately.
func splitVibeArgs(args []string) (mode string, rest []string, found bool) {
	mode = "run"
	for i, a := range args {
		if a != "agenthooks" {
			continue
		}
		rest = args[i+1:]
		if len(rest) > 0 && (rest[0] == "run" || rest[0] == "notify" || rest[0] == "serve") {
			mode = rest[0]
			rest = rest[1:]
		}
		return mode, rest, true
	}
	return "", nil, false
}

// parseVibeArgs parses the agenthooks-side argv of a vibe invocation. Vibe
// spawns one process per hook event, so only the run mode exists.
func parseVibeArgs(args []string) (vibeInvocation, error) {
	inv := vibeInvocation{}
	mode, rest, found := splitVibeArgs(args)
	if !found {
		return inv, fmt.Errorf("speakeasy-hooks: vibe: missing agenthooks sentinel in argv")
	}
	if mode != "run" {
		return inv, fmt.Errorf("speakeasy-hooks: vibe: only 'agenthooks run' is supported, got %q", mode)
	}
	provider := ""
	for _, a := range rest {
		switch {
		case strings.HasPrefix(a, "--provider="):
			provider = strings.TrimPrefix(a, "--provider=")
		case strings.HasPrefix(a, "--timeout="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--timeout="))
			if err != nil {
				return inv, fmt.Errorf("speakeasy-hooks: bad --timeout: %w", err)
			}
			inv.timeout = d
		case a == "--strict" || a == "--strict=true":
			inv.strict = true
		case strings.HasPrefix(a, "--"):
			// Unknown flags are tolerated for forward compatibility with
			// newer generated hook commands driving older binaries.
		}
	}
	if provider != "" && agenthooks.Provider(provider) != ProviderVibe {
		return inv, fmt.Errorf("speakeasy-hooks: vibe runner got --provider=%q", provider)
	}
	if v := Env("HOOKS_STRICT"); v == "1" || strings.EqualFold(v, "true") {
		inv.strict = true
	}
	return inv, nil
}

// RunVibeCommand runs one Vibe hook invocation: it reads the single JSON
// object on stdin, decodes it into an agenthooks wire event, runs it through
// the relay pipeline, and writes the structured Vibe response to stdout. The
// exit code is 0 whenever a decision was reached (Vibe's protocol encodes the
// decision in stdout, not the exit code); 64 is an argv error.
func RunVibeCommand(ctx context.Context, cfg Config, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	inv, err := parseVibeArgs(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 64
	}
	if inv.timeout > 0 {
		// Match agenthooks: the flag budgets the whole invocation at 90%, so
		// the relay's gate budget lands the verdict before Vibe's own
		// timeout kills the process tree.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, inv.timeout*9/10)
		defer cancel()
	}
	payload, err := io.ReadAll(io.LimitReader(stdin, maxVibePayloadBytes))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "speakeasy-hooks: vibe: reading stdin: %v\n", err)
		return 1
	}
	r := NewRelay(cfg)
	resp, diag := r.serveVibe(ctx, payload, inv.strict)
	if diag != "" {
		_, _ = fmt.Fprintln(stderr, diag)
	}
	if resp == nil {
		// Pass through: exit 0 with empty stdout lets the action proceed.
		return 0
	}
	b, err := json.Marshal(resp)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "speakeasy-hooks: vibe: encoding response: %v\n", err)
		return 1
	}
	_, _ = stdout.Write(append(b, '\n'))
	return 0
}

// serveVibe decodes one Vibe stdin payload, dispatches it through the relay,
// and returns the structured response to write (nil means pass through).
// diag carries a stderr diagnostic for failure paths; stderr is never parsed
// by Vibe for control.
func (r *Relay) serveVibe(ctx context.Context, payload []byte, strict bool) (*vibeResponse, string) {
	// Sniff the discriminant first so a payload too malformed to decode whole
	// still fails on the right hook type under strict.
	var head struct {
		HookEventName string `json:"hook_event_name"`
	}
	_ = json.Unmarshal(payload, &head)

	var in vibeHookInput
	if err := json.Unmarshal(payload, &in); err != nil {
		r.debugf("vibe: bad hook payload: %v", err)
		// The head sniff above leaves HookEventName empty on any syntax error
		// (including a payload truncated by the stdin LimitReader), so recover
		// the event type from the raw bytes: under strict, a payload too
		// malformed to decode must still fail closed on the right hook type.
		return vibeFailureResponse(sniffVibeHookEvent(payload, head.HookEventName), strict, err), fmt.Sprintf("speakeasy-hooks: vibe: bad hook payload: %v", err)
	}

	var (
		resp *vibeResponse
		err  error
	)
	switch in.HookEventName {
	case vibeHookPreTool:
		resp, err = r.vibePreTool(ctx, &in, payload)
	case vibeHookPostTool:
		resp, err = r.vibePostTool(ctx, &in, payload)
	case vibeHookPostAgent:
		resp, err = r.vibePostAgent(ctx, &in, payload)
	default:
		// A hook_event_name the relay cannot classify is not relayed (the
		// server would store telemetry with no canonical type) and must not
		// gate: pass through.
		r.debugf("vibe: unhandled hook_event_name %q", in.HookEventName)
		return nil, ""
	}
	if err != nil {
		r.debugf("vibe: %s handler error: %v", in.HookEventName, err)
		return vibeFailureResponse(in.HookEventName, strict, err), fmt.Sprintf("speakeasy-hooks: vibe: %s: %v", in.HookEventName, err)
	}
	return resp, ""
}

// vibePreTool runs the gating path for a pre_tool event and encodes the
// decision per Vibe's response schema: deny carries the reason Vibe surfaces
// as the tool error, and an updated input rides as the full tool_input
// replacement.
func (r *Relay) vibePreTool(ctx context.Context, in *vibeHookInput, raw json.RawMessage) (*vibeResponse, error) {
	pre := &agenthooks.ToolPreEvent{
		Event: vibeEvent(in, agenthooks.KindToolPre, raw),
		Tool:  vibeToolCall(in),
	}
	dec, err := r.onToolPre(ctx, pre)
	if err != nil {
		return nil, err
	}
	return vibeEncodePreTool(dec), nil
}

// vibePostTool delivers a post_tool completion. The shared pipeline treats
// tool.post as observe-only because most providers cannot act on it, but
// Vibe can: a post_tool deny replaces the model-visible tool_output_text with
// the reason, and a non-blocking message rides as additional_context appended
// to the output. Only an explicit server deny blocks — the credential ratchet
// never rewrites tool output the way it gates tool calls.
func (r *Relay) vibePostTool(ctx context.Context, in *vibeHookInput, raw json.RawMessage) (*vibeResponse, error) {
	kind := agenthooks.KindToolPost
	failed := strings.EqualFold(strings.TrimSpace(in.ToolStatus), vibeToolStatusFailure)
	if failed {
		kind = agenthooks.KindToolError
	}
	post := &agenthooks.ToolPostEvent{
		Event:      vibeEvent(in, kind, raw),
		Tool:       vibeToolCall(in),
		Output:     vibeToolOutput(in),
		Failed:     failed,
		Error:      vibeToolError(in),
		DurationMS: in.DurationMS,
	}
	res, state := r.deliver(ctx, post)
	resp := &vibeResponse{Decision: vibeDecisionAllow}
	if state == stateReady && res.accepted() && res.decision.denied() {
		resp.Decision = vibeDecisionDeny
		resp.Reason = vibeFirstNonEmpty(res.decision.Message, res.decision.Reason, "Speakeasy blocked this tool output.")
		if msg := strings.TrimSpace(res.decision.Message); msg != "" {
			resp.SystemMessage = msg
		}
		return resp, nil
	}
	if state == stateReady && res.accepted() {
		if msg := strings.TrimSpace(res.decision.Message); msg != "" {
			resp.HookSpecificOutput = &vibeHookSpecificOutput{AdditionalContext: msg}
		}
	}
	return resp, nil
}

// vibePostAgent delivers a post_agent (turn finished) event as agent.stop.
// Vibe's post_agent deny injects the reason as a retry user message (Vibe
// caps retries at 3 per turn across all post_agent hooks), so an explicit
// server deny is encoded as deny; everything else passes through.
func (r *Relay) vibePostAgent(ctx context.Context, in *vibeHookInput, raw json.RawMessage) (*vibeResponse, error) {
	stop := &agenthooks.StopEvent{
		Event: vibeEvent(in, agenthooks.KindStop, raw),
	}
	res, state := r.deliver(ctx, stop)
	resp := &vibeResponse{Decision: vibeDecisionAllow}
	if state == stateReady && res.accepted() && res.decision.denied() {
		resp.Decision = vibeDecisionDeny
		resp.Reason = vibeFirstNonEmpty(res.decision.Message, res.decision.Reason, "Speakeasy requested another turn.")
	}
	if msg := strings.TrimSpace(res.decision.Message); state == stateReady && res.accepted() && resp.Decision == vibeDecisionAllow && msg != "" {
		resp.SystemMessage = msg
	}
	return resp, nil
}

// vibeFailureResponse renders the structured reply for an internal failure
// (undecodable payload, handler error). Vibe fail-opens a failed hook unless
// the hook is strict; under strict the relay emits the structured response
// that achieves the strict outcome directly rather than relying on Vibe's
// non-zero-exit path: pre_tool denies the call with the failure reason, and
// post_tool clears the output (a deny whose reason replaces tool_output_text;
// an empty reason clears it). strict is forbidden on post_agent, so those
// always fail open. Without strict the response is nil — exit 0 with empty
// stdout, Vibe's pass-through.
// sniffVibeHookEvent recovers the hook_event_name from raw payload bytes when
// the whole object no longer parses, so strict failure handling still knows
// which hook type failed. Unknown bytes report "".
func sniffVibeHookEvent(payload []byte, fallback string) string {
	if fallback != "" {
		return fallback
	}
	for _, ev := range []string{vibeHookPreTool, vibeHookPostTool, vibeHookPostAgent} {
		if bytes.Contains(payload, []byte(strconv.Quote(ev))) {
			return ev
		}
	}
	return ""
}

// vibeFailureResponse renders the structured reply for an internal failure
// (bad payload or handler error): nil means pass through (fail open). Under
// strict, an unrecognised payload ("") fails closed as a pre_tool-style deny:
// for pre_tool that is the tool error, and for a truncated post_tool Vibe's
// own failure path under strict also denies, so either way the call does not
// proceed unanswered.
func vibeFailureResponse(hook string, strict bool, err error) *vibeResponse {
	switch {
	case strict && (hook == vibeHookPreTool || hook == ""):
		reason := fmt.Sprintf("Speakeasy hooks failed to process this %s event: %v", hook, err)
		return &vibeResponse{Decision: vibeDecisionDeny, Reason: reason, SystemMessage: reason}
	case strict && hook == vibeHookPostTool:
		return &vibeResponse{Decision: vibeDecisionDeny, Reason: ""}
	default:
		return nil
	}
}

// vibeEncodePreTool maps a ToolPreDecision onto the Vibe response schema.
func vibeEncodePreTool(dec agenthooks.ToolPreDecision) *vibeResponse {
	resp := &vibeResponse{Decision: vibeDecisionAllow}
	if dec.Blocks() {
		resp.Decision = vibeDecisionDeny
		resp.Reason = vibeFirstNonEmpty(dec.SystemMessage(), dec.Reason(), "Speakeasy blocked this tool call.")
	}
	if msg := dec.SystemMessage(); msg != "" {
		resp.SystemMessage = msg
	}
	if updated, ok := dec.UpdatedInput(); ok {
		if b, err := json.Marshal(updated); err == nil {
			if resp.HookSpecificOutput == nil {
				resp.HookSpecificOutput = &vibeHookSpecificOutput{}
			}
			resp.HookSpecificOutput.ToolInput = b
		}
	}
	return resp
}

// vibeEvent builds the unified envelope for one Vibe payload. Raw is the
// verbatim stdin object.
func vibeEvent(in *vibeHookInput, kind agenthooks.EventKind, raw json.RawMessage) agenthooks.Event {
	roots := []string(nil)
	if in.CWD != "" {
		roots = []string{in.CWD}
	}
	ev := agenthooks.Event{
		Provider:   ProviderVibe,
		Variant:    agenthooks.VariantCLI,
		NativeName: in.HookEventName,
		Kind:       kind,
		Time:       time.Now().UTC(),
		Session: agenthooks.SessionInfo{
			ID:             in.SessionID,
			CWD:            in.CWD,
			WorkspaceRoots: roots,
			TranscriptPath: in.TranscriptPath,
		},
		DetectionConfidence: agenthooks.DetectionConfig,
		Raw:                 raw,
	}
	// parent_session_id is set when the event comes from a subagent; surface
	// that as the unified Agent marker, with the agent config name as its type.
	if in.ParentSessionID != nil && *in.ParentSessionID != "" {
		agentName := ""
		if in.AgentName != nil {
			agentName = *in.AgentName
		}
		ev.Agent = &agenthooks.AgentInfo{ID: in.SessionID, Type: agentName}
		ev.Ext = map[string]any{"parent_session_id": *in.ParentSessionID}
	}
	return ev
}

// vibeBuiltinNamespaces are Vibe's reserved built-in tool namespaces. Their
// tools arrive qualified (file_system.bash) while every other provider sends
// the short name (bash), so the namespace is stripped to map the name into
// the shape other providers produce; the qualified form stays on Raw.
var vibeBuiltinNamespaces = map[string]bool{
	"file_system": true,
	"self":        true,
	"process":     true,
	"vibe":        true,
	"skill":       true,
	"subagent":    true,
	"agents":      true,
}

// vibeToolCall normalizes a Vibe tool invocation. Vibe already normalizes MCP
// tool names to mcp_<server>.<tool> (dot-separated, with non-[A-Za-z0-9_$]
// mapped to _), which the shared ParseMCPName would mis-split on the first
// underscore, so the dot dialect is decoded here.
func vibeToolCall(in *vibeHookInput) agenthooks.ToolCall {
	name := strings.TrimSpace(in.ToolName)
	session := agenthooks.SessionInfo{ID: in.SessionID, CWD: in.CWD}
	tc := agenthooks.ToolCall{
		ID:        strings.TrimSpace(in.ToolCallID),
		Name:      name,
		Canonical: agenthooks.CanonicalToolFor(name),
		MCP:       agenthooks.ParseMCPName(name),
		Input:     normalizePiToolInput(in.ToolInput),
		RawInput:  in.ToolInput,
	}
	if rest, ok := strings.CutPrefix(name, "mcp_"); ok {
		if server, tool, ok := strings.Cut(rest, "."); ok && server != "" && tool != "" {
			tc.MCP = &agenthooks.MCPCall{Server: server, Tool: tool}
			tc.Canonical = agenthooks.ToolMCP
		}
	}
	if tc.MCP == nil {
		if ns, short, ok := strings.Cut(name, "."); ok && vibeBuiltinNamespaces[ns] && short != "" {
			tc.Name = short
			tc.Canonical = agenthooks.CanonicalToolFor(short)
		}
	}
	if tc.ID == "" {
		tc.ID = agenthooks.SynthesizeToolID(session.ID, "", tc.Name, tc.Input)
		tc.Synthesized = true
	}
	return tc
}

// vibeToolOutput picks the model-visible output for a post_tool event: the
// structured result when present, else the text form.
func vibeToolOutput(in *vibeHookInput) json.RawMessage {
	if out := normalizeOutput(in.ToolOutput); out != nil {
		return out
	}
	if trimmed := strings.TrimSpace(in.ToolOutputText); trimmed != "" {
		if b, err := json.Marshal(trimmed); err == nil {
			return b
		}
	}
	return nil
}

// vibeToolError flattens the failure payload to the error string the unified
// ToolPostEvent carries.
func vibeToolError(in *vibeHookInput) string {
	trimmed := bytes.TrimSpace(in.ToolError)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(trimmed, &s) == nil {
		return s
	}
	return string(trimmed)
}

// vibeFirstNonEmpty returns the first candidate that is not blank, mirroring
// the fallback ordering the Pi adapter uses for deny reasons.
func vibeFirstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		if strings.TrimSpace(c) != "" {
			return c
		}
	}
	return ""
}

// vibeHooksToml renders the project-local Vibe hooks config that drives this
// binary, for `speakeasy-hooks install --provider=vibe`. Tool hooks are
// strict so a crashed hook denies the call / clears the output instead of
// silently allowing (the same fail-closed posture the other providers' configs
// render); post_agent cannot be strict in Vibe's schema.
func vibeHooksToml(cfg PluginConfig, dir string) string {
	command := shellQuoteArg(cfg.BinaryPath) +
		" --config=" + shellQuoteArg(filepath.Join(dir, configFileName)) +
		" agenthooks run --provider=vibe --strict"
	hook := func(name, typ string, strict bool) string {
		var b strings.Builder
		fmt.Fprintf(&b, "[[hooks]]\nname = %s\ntype = %s\ncommand = %s\ntimeout = 60.0\n", strconv.Quote(name), strconv.Quote(typ), strconv.Quote(command))
		if strict {
			b.WriteString("strict = true\n")
		}
		return b.String()
	}
	return "# Generated by speakeasy-hooks install --provider=vibe. Drives the\n" +
		"# speakeasy-hooks binary for every Vibe hook event. See hooks.toml in the\n" +
		"# Vibe documentation for the wire protocol.\n\n" +
		hook("speakeasy-pre-tool", vibeHookPreTool, true) + "\n" +
		hook("speakeasy-post-tool", vibeHookPostTool, true) + "\n" +
		hook("speakeasy-post-agent", vibeHookPostAgent, false)
}
