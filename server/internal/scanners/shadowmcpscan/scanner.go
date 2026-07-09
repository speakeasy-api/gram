// Package shadowmcpscan is the single home for the shadow-MCP scanner. It flags
// MCP-routed tool calls that fail shadow-MCP validation — calls that do not
// carry a valid x-gram-toolset-id resolving to a toolset in the caller's
// organization — and converts each into the shared scanners.Finding domain
// type.
//
// The scanner is a two-phase batch operation. Phase one validates every MCP
// call across the batch (via a Validator, satisfied by *shadowmcp.Client) and
// records the denied ones. Phase two issues a single MatchLookup for all denied
// tool-call IDs to enrich each finding's Match with the stored MCP match string,
// falling back to the tool name's server prefix when the lookup is unavailable
// or misses. The batch shape is intrinsic: the enrichment lookup is amortized
// across the whole batch rather than issued per message.
package shadowmcpscan

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// Source labels findings produced by this scanner.
const Source = shadowmcp.SourceShadowMCP

// Rule is the canonical rule id emitted for every shadow_mcp finding. The
// detection mechanism (missing toolset id, unknown toolset, ...) is
// implementation detail kept in logs; the rule id describes the risk itself.
const Rule = "shadow_mcp"

// Validator enforces that a Gram-hosted tool call carries a valid
// x-gram-toolset-id resolving to a toolset in the caller's organization.
// *shadowmcp.Client satisfies it. The bool return is true when the call is
// denied (fails validation).
type Validator interface {
	ValidateToolsetCall(ctx context.Context, toolInput any, toolName string, orgID string) (string, bool)
}

// MatchLookup resolves stored tool-call IDs to MCP match strings used to
// enrich a denied finding's Match.
type MatchLookup interface {
	LookupMCPMatchesByToolCallID(ctx context.Context, projectID uuid.UUID, toolCallIDs []string) (map[string]string, error)
}

// ToolCall is one recorded tool invocation to scan. ID is the recorded
// tool-call id used to key the enrichment lookup; Arguments is the raw JSON
// arguments string exactly as recorded.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Scanner flags MCP tool calls denied by shadow-MCP validation. It is safe for
// concurrent use so long as its Validator and MatchLookup are.
type Scanner struct {
	logger      *slog.Logger
	validator   Validator
	matchLookup MatchLookup
}

// NewScanner returns a Scanner. logger, validator, and matchLookup must all be
// non-nil.
func NewScanner(logger *slog.Logger, validator Validator, matchLookup MatchLookup) *Scanner {
	return &Scanner{logger: logger, validator: validator, matchLookup: matchLookup}
}

// Scan validates every MCP tool call across the batch and returns a Finding for
// each denied call, one findings slice per input message (positionally aligned
// with messages). Denied findings are then enriched in a single MatchLookup
// keyed by tool-call id; enrichment failures are logged and leave the
// server-prefix fallback Match in place.
func (s *Scanner) Scan(ctx context.Context, orgID string, projectID uuid.UUID, messages [][]ToolCall) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(messages))

	var deniedCallIDs []string
	for i, calls := range messages {
		findings, ids := s.scanMessage(ctx, orgID, calls)
		out[i] = findings
		deniedCallIDs = append(deniedCallIDs, ids...)
	}

	if len(deniedCallIDs) == 0 {
		return out
	}

	matches, err := s.matchLookup.LookupMCPMatchesByToolCallID(ctx, projectID, deniedCallIDs)
	if err != nil {
		s.logger.WarnContext(ctx, "shadow_mcp scan: mcp match lookup failed; using server-prefix fallback", attr.SlogError(err))
		return out
	}
	for i := range out {
		for j := range out[i] {
			f := &out[i][j]
			if f.Source != Source {
				continue
			}
			if v, ok := matches[f.McpLookupToolCallID]; ok && v != "" {
				f.Match = v
			}
		}
	}
	return out
}

// scanMessage validates a single message's tool calls, returning a Finding per
// denied MCP call plus the recorded ids of those calls (for batch enrichment).
func (s *Scanner) scanMessage(ctx context.Context, orgID string, calls []ToolCall) ([]scanners.Finding, []string) {
	var findings []scanners.Finding
	var deniedCallIDs []string
	for _, call := range calls {
		toolName := call.Name
		if toolName == "" || !toolref.IsMCPToolName(toolName) {
			continue
		}

		toolInput := parseToolInput(call.Arguments)
		bareName := toolref.MCPFunctionOf(toolName)
		_, denied := s.validator.ValidateToolsetCall(ctx, toolInput, bareName, orgID)
		if !denied {
			continue
		}

		match := toolref.MCPServerOf(toolName)
		if match == "" {
			match = toolName
		}
		ruleID, description := describe(toolName)
		findings = append(findings, scanners.Finding{
			Source:      Source,
			RuleID:      ruleID,
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
		})
		if call.ID != "" {
			deniedCallIDs = append(deniedCallIDs, call.ID)
		}
	}
	return findings, deniedCallIDs
}

// describe returns the canonical (rule_id, description) for a shadow_mcp
// finding. The description names the tool but never leaks validator internals
// (e.g. the x-gram-toolset-id property).
func describe(toolName string) (string, string) {
	if toolName == "" {
		return scanners.GuardRuleID(Rule), "Detected an unverified MCP tool call."
	}
	return scanners.GuardRuleID(Rule), fmt.Sprintf("Detected an unverified MCP tool call to %q.", toolName)
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
