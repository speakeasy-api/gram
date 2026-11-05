package relay

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"github.com/speakeasy-api/agenthooks"
)

// openclawLLMInputHook is OpenClaw's native name for the hook that fires
// before the model call of a run. Its event carries the run's user prompt.
const openclawLLMInputHook = "llm_input"

// The Codex app-server harness decorates the prompt it sends the model, and so
// the prompt in its llm_input, as up to three sections joined by a blank line
// (prependCodexOpenClawPromptContext in OpenClaw's codex extension):
//
//	OpenClaw runtime context for this turn:   workspace files, free text
//	OpenClaw delivery metadata:               a fixed routing hint
//	Current user request:                     what the user typed
//
// A prompt with no context and no delivery hint is sent undecorated.
const (
	openclawContextHeader  = "OpenClaw runtime context for this turn:\n"
	openclawDeliveryHeader = "OpenClaw delivery metadata:\n"
	openclawRequestHeader  = "Current user request:\n"
)

// openclawUserRequest returns the text the user typed from an llm_input
// prompt, without the workspace context, often several kilobytes, that the
// harness puts before it.
//
// The delivery section holds only OpenClaw's fixed hints, so the first request
// header after it is the boundary, and a request that quotes the header stays
// whole. Runtime context is free text that can hold anything, so without a
// delivery section the last header is the boundary: a request containing a
// header line loses the text before that line, but workspace files never pass
// as the prompt. An undecorated prompt is the user's text as typed.
func openclawUserRequest(prompt string) string {
	separator := "\n\n" + openclawRequestHeader
	if strings.HasPrefix(prompt, openclawDeliveryHeader) {
		return textAfter(prompt, separator, strings.Index(prompt, separator))
	}
	if !strings.HasPrefix(prompt, openclawContextHeader) {
		return prompt
	}
	if delivery := strings.LastIndex(prompt, "\n\n"+openclawDeliveryHeader); delivery >= 0 {
		if i := strings.Index(prompt[delivery:], separator); i >= 0 {
			return textAfter(prompt, separator, delivery+i)
		}
	}
	return textAfter(prompt, separator, strings.LastIndex(prompt, separator))
}

// textAfter returns s after the separator found at index i, or all of s when
// the separator was not found.
func textAfter(s, separator string, i int) string {
	if i < 0 {
		return s
	}
	return s[i+len(separator):]
}

// openclawPromptRunsCap bounds how many OpenClaw runs the relay remembers as
// having reported their user prompt. The serve daemon lives as long as the
// Gateway, so the record must not grow with every run. 1024 is far more runs
// than one Gateway has in flight at once. A run evicted while it is still
// running can report its prompt again; the server keeps one user message per
// OpenClaw run id.
const openclawPromptRunsCap = 1024

// openclawPromptRuns records the OpenClaw runs whose user prompt the relay
// already reported, so that each run reports it once. The serve loop handles
// gate frames (before_agent_run) inline and observe frames (llm_input) on a
// worker goroutine, so every access holds mu.
type openclawPromptRuns struct {
	mu sync.Mutex

	// seen holds the recorded run ids.
	seen map[string]struct{}

	// ring holds the recorded run ids in insertion order. A new run id
	// replaces the oldest one when the ring is full.
	ring [openclawPromptRunsCap]string

	// next is the ring slot that the next recorded run id takes.
	next int
}

func newOpenclawPromptRuns() *openclawPromptRuns {
	return &openclawPromptRuns{
		mu:   sync.Mutex{},
		seen: make(map[string]struct{}, openclawPromptRunsCap),
		ring: [openclawPromptRunsCap]string{},
		next: 0,
	}
}

// claim records runID and reports whether this call recorded it first.
func (p *openclawPromptRuns) claim(runID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.seen[runID]; ok {
		return false
	}
	if evicted := p.ring[p.next]; evicted != "" {
		delete(p.seen, evicted)
	}
	p.ring[p.next] = runID
	p.next = (p.next + 1) % openclawPromptRunsCap
	p.seen[runID] = struct{}{}
	return true
}

// openclawRunID returns the OpenClaw run that an event belongs to: ctx.runId,
// which the decoder maps to the turn id, else the event's own runId.
func openclawRunID(e *agenthooks.Event) string {
	if runID := strings.TrimSpace(e.Session.TurnID); runID != "" {
		return runID
	}
	var runID string
	if raw := e.RawField("event.runId"); len(raw) > 0 && json.Unmarshal(raw, &runID) == nil {
		return strings.TrimSpace(runID)
	}
	return ""
}

// noteOpenclawPrompt records that a before_agent_run gate reported the user
// prompt of its run, so that the llm_input of the same run does not report it
// again.
func (r *Relay) noteOpenclawPrompt(e *agenthooks.PromptEvent) {
	if e.Provider != agenthooks.ProviderOpenClaw || strings.TrimSpace(e.Prompt) == "" {
		return
	}
	if runID := openclawRunID(&e.Event); runID != "" {
		r.openclawPrompts.claim(runID)
	}
}

// onModelRequest reports the user prompt of an OpenClaw run from llm_input
// when no before_agent_run gate reported it. OpenClaw's Codex app-server
// harness fires llm_input but never before_agent_run, so without this report
// the turn has no user message. The built-in harnesses fire before_agent_run
// first, which records the run, so the relay ignores their llm_input.
//
// The report is observe-only. OpenClaw does not await llm_input, and it fires
// no gate on the Codex harness, so prompt policies cannot block those turns.
// The relay discards the server's verdict.
func (r *Relay) onModelRequest(ctx context.Context, e *agenthooks.ModelEvent) error {
	if e.Provider != agenthooks.ProviderOpenClaw || e.NativeName != openclawLLMInputHook {
		return nil
	}
	// Without a run id the relay cannot tell whether before_agent_run already
	// reported this turn, and the server correlates OpenClaw prompts by run.
	runID := openclawRunID(&e.Event)
	if runID == "" {
		return nil
	}
	var prompt string
	if raw := e.RawField("event.prompt"); len(raw) == 0 || json.Unmarshal(raw, &prompt) != nil {
		return nil
	}
	prompt = openclawUserRequest(prompt)
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	if !r.openclawPrompts.claim(runID) {
		return nil
	}

	pe := &agenthooks.PromptEvent{Event: e.Event, Prompt: prompt}
	// Model requests can include history, system instructions and tool schemas.
	// Only the extracted user prompt belongs in this synthetic event.
	pe.Raw = nil
	pe.Kind = agenthooks.KindPromptSubmitted
	pe.Session.TurnID = runID
	r.deliver(ctx, pe)
	return nil
}

// openclawMCPServersField is the top-level frame field in which the OpenClaw
// shim lists the MCP servers configured under mcp.servers on tool frames.
const openclawMCPServersField = "mcpServers"

// openclawToolNameSeparator joins the server and tool parts of an OpenClaw MCP
// tool name: <server>__<tool>.
const openclawToolNameSeparator = "__"

// openclawToolNameMaxServerChars is the length OpenClaw cuts a sanitized
// server name to before it prefixes the server's tool names with it.
const openclawToolNameMaxServerChars = 30

// openclawToolNameUnsafe matches the characters OpenClaw replaces with "-"
// when it builds a server's tool name prefix.
var openclawToolNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// openclawMCPServer is one entry of the shim's mcpServers list. The shim sends
// no headers, env or command, only these two fields.
type openclawMCPServer struct {
	// Name is the mcp.servers config key. It is the value OpenClaw reports as
	// details.mcpServer on the server's tool results.
	Name string `json:"name"`

	// URL is the server's URL as the shim sent it, without userinfo, query or
	// fragment.
	URL string `json:"url"`
}

// openclawConfiguredMCPServers returns the named servers with a valid http or
// https URL from the frame's mcpServers list, each URL redacted. A missing or
// malformed list yields none, and a malformed entry is skipped.
func openclawConfiguredMCPServers(e *agenthooks.Event) []openclawMCPServer {
	raw := e.RawField(openclawMCPServersField)
	if len(raw) == 0 {
		return nil
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	servers := make([]openclawMCPServer, 0, len(entries))
	for _, entry := range entries {
		var server openclawMCPServer
		if json.Unmarshal(entry, &server) != nil {
			continue
		}
		server.Name = strings.TrimSpace(server.Name)
		if server.Name == "" {
			continue
		}
		redacted, ok := redactMCPInventoryURL(server.URL)
		if !ok {
			continue
		}
		server.URL = redacted
		servers = append(servers, server)
	}
	return servers
}

// openclawToolNamePrefix returns the prefix OpenClaw gives the tool names of
// the server configured as name: the name with every character outside
// [A-Za-z0-9_-] replaced by "-", cut to 30 characters. OpenClaw appends -2,
// -3 and so on when two servers sanitize to the same prefix; those suffixed
// prefixes are not reproduced, so such tools stay unresolved.
func openclawToolNamePrefix(name string) string {
	prefix := openclawToolNameUnsafe.ReplaceAllString(strings.TrimSpace(name), "-")
	if len(prefix) > openclawToolNameMaxServerChars {
		prefix = prefix[:openclawToolNameMaxServerChars]
	}
	return prefix
}

// openclawMCPServerForToolName returns the configured server whose tool name
// prefix the tool name carries, and the tool part after the prefix. Both the
// config key and its sanitized form count as a prefix, and the longest match
// wins, so a server named "a__b" beats one named "a" for "a__b__tool".
func openclawMCPServerForToolName(servers []openclawMCPServer, toolName string) (openclawMCPServer, string, bool) {
	var best openclawMCPServer
	bestLen := 0
	for _, server := range servers {
		for _, prefix := range []string{server.Name, openclawToolNamePrefix(server.Name)} {
			candidate := prefix + openclawToolNameSeparator
			if len(candidate) <= bestLen || len(toolName) <= len(candidate) || !strings.HasPrefix(toolName, candidate) {
				continue
			}
			best = server
			bestLen = len(candidate)
		}
	}
	if bestLen == 0 {
		return openclawMCPServer{Name: "", URL: ""}, "", false
	}
	return best, toolName[bestLen:], true
}

// openclawResolveMCPCall resolves the MCP server an OpenClaw tool call
// reached. A tool result names its server exactly in details.mcpServer, which
// is looked up among the configured servers for its URL. A pre-call event has
// no result, so its tool name is matched against the configured servers' tool
// name prefixes. Only configured names match: a tool name that merely
// contains "__" is never taken for an MCP call. A result naming a server the
// frame does not list keeps that name and no URL. Nil means the call is not
// attributed to an MCP server.
func openclawResolveMCPCall(base *agenthooks.Event, toolName string, results ...json.RawMessage) *agenthooks.MCPCall {
	servers := openclawConfiguredMCPServers(base)
	if base.Kind == agenthooks.KindToolPre {
		server, tool, ok := openclawMCPServerForToolName(servers, toolName)
		if !ok {
			return nil
		}
		return &agenthooks.MCPCall{
			Server:     server.Name,
			Tool:       tool,
			URL:        server.URL,
			Command:    "",
			FromConfig: true,
		}
	}

	call := openClawMCPCall(results...)
	if call == nil {
		return nil
	}
	for _, server := range servers {
		if server.Name == call.Server {
			call.URL = server.URL
			call.FromConfig = true
			break
		}
	}
	return call
}
