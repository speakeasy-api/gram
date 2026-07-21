// Package shadowmcpscan is the single home for the shadow-MCP scanner. It flags
// MCP-routed tool calls that did not go to a Gram-hosted MCP server, and
// converts each into the shared scanners.Finding domain type.
//
// The scanner decides from provenance first. Every MCP-routed hook event
// records the server it resolved (`gram.mcp.match` / `gram.mcp.server_url`),
// and one batch-wide ProvenanceLookup replays those values back per tool call.
// A call whose provenance resolves to a Gram-hosted host is clean; one that
// resolves to anything else — a third-party URL or a local stdio command — is
// flagged. This is the same question the realtime hook guard answers, so the
// two paths agree on a call by construction.
//
// When provenance does not resolve — the hook log has not landed yet, the
// sender never resolved a server, or the sender's recorded tool-call id is not
// what its trace id derives from — the scanner falls back to the legacy
// signature check: the x-gram-toolset-id constant Gram injects into tool
// schemas and callers echo back. The fallback preserves today's behaviour for
// senders provenance cannot yet cover, and is meant to be deleted once the
// measured unresolved rate justifies it.
//
// The batch shape is intrinsic: the provenance lookup is one ClickHouse query
// amortized across the whole batch rather than issued per message.
package shadowmcpscan

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// Source labels findings produced by this scanner.
const Source = shadowmcp.SourceShadowMCP

// Rule is the canonical rule id emitted for every shadow_mcp finding. The
// detection mechanism (non-Gram URL, stdio server, missing toolset id, ...) is
// implementation detail kept in logs; the rule id describes the risk itself.
const Rule = "shadow_mcp"

// provenanceLookback bounds how far back the provenance query scans relative
// to the oldest message in the batch. The bound exists to keep the query on
// the table's time-ordered primary key instead of degrading into a 90-day
// scan; the slack absorbs hook events whose recorded time precedes the chat
// message (clock skew, and replayed events redelivered from a device's
// offline spool). A spool older than this window resolves to no provenance
// and falls through to the signature check, which is the safe direction.
const provenanceLookback = 7 * 24 * time.Hour

// Resolution outcomes reported to CoverageRecorder.
const (
	ResolutionHosted     = "hosted"
	ResolutionShadow     = "shadow"
	ResolutionUnresolved = "unresolved"
)

// Validator enforces that a Gram-hosted tool call carries a valid
// x-gram-toolset-id resolving to a toolset in the caller's organization.
// *shadowmcp.Client satisfies it. The bool return is true when the call is
// denied (fails validation). Used only for calls provenance could not resolve.
type Validator interface {
	ValidateToolsetCall(ctx context.Context, toolInput any, toolName string, orgID string) (string, bool)
}

// HostedChecker reports whether a resolved MCP server URL belongs to Gram for
// the calling organization. *shadowmcp.Client satisfies it.
type HostedChecker interface {
	IsGramHostedMCPURLForOrg(ctx context.Context, rawURL, orgID string) bool
}

// ProvenanceLookup replays the server identity the hook recorded for a set of
// tool calls. *telemetryrepo.Queries satisfies it.
type ProvenanceLookup interface {
	LookupMCPProvenanceByToolCallID(ctx context.Context, projectID uuid.UUID, toolCallIDs []string, since time.Time) (map[string]telemetryrepo.MCPProvenance, error)
}

// CoverageRecorder observes how each scanned MCP call was decided, so the
// unresolved rate driving the signature fallback's removal is measurable per
// sender rather than a single opaque number.
type CoverageRecorder interface {
	RecordShadowMCPResolution(ctx context.Context, orgID string, hookSource string, resolution string)
}

// ToolCall is one recorded tool invocation to scan. ID is the recorded
// tool-call id used to key the provenance lookup; Arguments is the raw JSON
// arguments string exactly as recorded. CreatedAt is the recording time of the
// message the call belongs to, used to bound the provenance query. Sender is
// the agent that recorded the message, used to attribute the resolution metric
// when no provenance row was found to carry a hook source of its own.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
	CreatedAt time.Time
	Sender    string
}

// Scanner flags MCP tool calls that did not reach a Gram-hosted server. It is
// safe for concurrent use so long as its dependencies are.
type Scanner struct {
	logger     *slog.Logger
	validator  Validator
	hosted     HostedChecker
	provenance ProvenanceLookup
	coverage   CoverageRecorder
}

// NewScanner returns a Scanner. logger, validator, hosted, and provenance must
// all be non-nil; coverage may be nil to disable resolution metrics.
func NewScanner(logger *slog.Logger, validator Validator, hosted HostedChecker, provenance ProvenanceLookup, coverage CoverageRecorder) *Scanner {
	return &Scanner{
		logger:     logger,
		validator:  validator,
		hosted:     hosted,
		provenance: provenance,
		coverage:   coverage,
	}
}

// Scan returns a Finding for each MCP tool call that did not reach a
// Gram-hosted server, one findings slice per input message (positionally
// aligned with messages).
func (s *Scanner) Scan(ctx context.Context, orgID string, projectID uuid.UUID, messages [][]ToolCall) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(messages))

	provenance := s.lookupProvenance(ctx, projectID, messages)

	for i, calls := range messages {
		var findings []scanners.Finding
		for _, call := range calls {
			if finding := s.scanCall(ctx, orgID, call, provenance[call.ID]); finding != nil {
				findings = append(findings, *finding)
			}
		}
		out[i] = findings
	}
	return out
}

// lookupProvenance issues the single batch-wide provenance query. A failure is
// logged and yields an empty map: every call then falls through to the
// signature check rather than being decided on absent evidence.
func (s *Scanner) lookupProvenance(ctx context.Context, projectID uuid.UUID, messages [][]ToolCall) map[string]telemetryrepo.MCPProvenance {
	var ids []string
	var oldest time.Time
	for _, calls := range messages {
		for _, call := range calls {
			if call.ID == "" || call.Name == "" || !toolref.IsMCPToolName(call.Name) {
				continue
			}
			ids = append(ids, call.ID)
			if !call.CreatedAt.IsZero() && (oldest.IsZero() || call.CreatedAt.Before(oldest)) {
				oldest = call.CreatedAt
			}
		}
	}
	if len(ids) == 0 {
		return map[string]telemetryrepo.MCPProvenance{}
	}

	var since time.Time
	if !oldest.IsZero() {
		since = oldest.Add(-provenanceLookback)
	}

	found, err := s.provenance.LookupMCPProvenanceByToolCallID(ctx, projectID, ids, since)
	if err != nil {
		s.logger.WarnContext(ctx, "shadow_mcp scan: provenance lookup failed; falling back to signature validation",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
		)
		return map[string]telemetryrepo.MCPProvenance{}
	}
	return found
}

// scanCall decides a single tool call, returning nil when the call is clean or
// is not an MCP call at all.
func (s *Scanner) scanCall(ctx context.Context, orgID string, call ToolCall, prov telemetryrepo.MCPProvenance) *scanners.Finding {
	if call.Name == "" || !toolref.IsMCPToolName(call.Name) {
		return nil
	}

	serverPrefix := toolref.MCPServerOf(call.Name)
	match, resolved := resolvedServerIdentity(prov, serverPrefix)

	if resolved {
		if s.isHostedIdentity(ctx, match, orgID) {
			s.recordResolution(ctx, orgID, senderOf(prov, call), ResolutionHosted)
			return nil
		}
		// A resolved non-Gram URL, or a stdio server that does not front a
		// Gram URL. Both are shadow MCP by the same rule the realtime guard
		// applies.
		s.recordResolution(ctx, orgID, senderOf(prov, call), ResolutionShadow)
		finding := s.finding(call, match)
		return &finding
	}

	s.recordResolution(ctx, orgID, senderOf(prov, call), ResolutionUnresolved)

	// Provenance could not identify the server. Fall back to the echoed
	// x-gram-toolset-id signature, which is self-contained in the recorded
	// arguments and so is unaffected by whatever kept the hook log from
	// joining.
	toolInput := parseToolInput(call.Arguments)
	bareName := toolref.MCPFunctionOf(call.Name)
	if _, denied := s.validator.ValidateToolsetCall(ctx, toolInput, bareName, orgID); !denied {
		return nil
	}

	fallbackMatch := serverPrefix
	if fallbackMatch == "" {
		fallbackMatch = call.Name
	}
	finding := s.finding(call, fallbackMatch)
	return &finding
}

func (s *Scanner) finding(call ToolCall, match string) scanners.Finding {
	description := "Detected an unverified MCP tool call."
	if call.Name != "" {
		description = fmt.Sprintf("Detected an unverified MCP tool call to %q.", call.Name)
	}
	return scanners.Finding{
		Source:      Source,
		RuleID:      scanners.GuardRuleID(Rule),
		Description: description,
		Match:       match,
		StartPos:    0,
		EndPos:      0,
		Tags:        []string{},
		Confidence:  1.0,

		DeadLetterReason:    "",
		McpLookupToolCallID: call.ID,
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	}
}

func (s *Scanner) recordResolution(ctx context.Context, orgID, hookSource, resolution string) {
	if s.coverage == nil {
		return
	}
	s.coverage.RecordShadowMCPResolution(ctx, orgID, hookSource, resolution)
}

// senderOf names the agent to attribute a resolution to. The provenance row's
// own hook source is authoritative when there is one, but the unresolved case
// — the population the resolution metric exists to measure — has no row at
// all, so it falls back to the sender recorded on the chat message. Without
// that fallback every unresolved call would be attributed to "unknown",
// producing exactly the single opaque number the split is meant to avoid.
//
// Lowercased because the two sources disagree on case ("Codex" on the message,
// "codex" on the telemetry row) and a metric dimension must not split one
// sender across two series.
func senderOf(prov telemetryrepo.MCPProvenance, call ToolCall) string {
	if prov.HookSource != "" {
		return strings.ToLower(prov.HookSource)
	}
	return strings.ToLower(strings.TrimSpace(call.Sender))
}

// resolvedServerIdentity picks the server identifier to decide on and reports
// whether it actually identifies a server.
//
// ServerURL is preferred: it is set only when the sender knew a real HTTP/SSE
// URL. The match attribute is a union of URL, stdio command, and — when the
// sender's inventory snapshot did not resolve the server — the bare
// `mcp__<server>__` tool-name prefix. That last case carries no server
// identity, so it is reported unresolved rather than being flagged on the
// strength of a value derived from the tool name the scanner already has.
func resolvedServerIdentity(prov telemetryrepo.MCPProvenance, serverPrefix string) (string, bool) {
	// Trim both: senders relay these straight off a client payload without
	// normalizing (the Cursor hook does not), and a trailing newline would
	// otherwise defeat URL parsing in the hosted check.
	if serverURL := strings.TrimSpace(prov.ServerURL); serverURL != "" {
		return serverURL, true
	}
	match := strings.TrimSpace(prov.Match)
	if match == "" {
		return "", false
	}
	if serverPrefix != "" && match == serverPrefix {
		return "", false
	}
	return match, true
}

// isHostedIdentity reports whether a resolved server identity points at Gram.
//
// The identity is tested directly, then — when it is a stdio launch command —
// each absolute http(s) argument within it is tested too. Gram's own install
// snippet for OAuth-backed servers is a stdio entry that fronts a Gram URL
// (`npx mcp-remote@<version> https://app.getgram.ai/mcp/<slug>`), so treating
// every stdio server as shadow would flag calls to Gram's own servers whenever
// a customer installed them the way Gram told them to.
//
// The hosted check is never gated on the identity parsing as a URL: it already
// returns false for values with no host, and accepts forms (scheme-relative,
// non-http schemes) that a stricter pre-filter would wrongly reject before the
// host was ever compared.
func (s *Scanner) isHostedIdentity(ctx context.Context, identity, orgID string) bool {
	if s.hosted.IsGramHostedMCPURLForOrg(ctx, identity, orgID) {
		return true
	}
	for field := range strings.FieldsSeq(identity) {
		if !strings.HasPrefix(field, "http://") && !strings.HasPrefix(field, "https://") {
			continue
		}
		if s.hosted.IsGramHostedMCPURLForOrg(ctx, field, orgID) {
			return true
		}
	}
	return false
}

// parseToolInput parses a recorded tool call's raw arguments into a value the
// validator can inspect. Empty or malformed input yields nil — the validator
// treats a nil input as a missing toolset id and denies the call, which is the
// desired outcome for an unverifiable MCP call.
func parseToolInput(raw string) any {
	if raw == "" {
		return nil
	}
	var toolInput any
	if err := json.Unmarshal([]byte(raw), &toolInput); err != nil {
		return nil
	}
	return toolInput
}
