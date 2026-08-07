package toolfilter

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// Annotation names a raw MCP tool hint the consent screen can filter on.
// These are declared behavior hints supplied by tool authors, not verified
// safety properties.
const (
	AnnotationReadOnly    = "read_only"
	AnnotationDestructive = "destructive"
	AnnotationIdempotent  = "idempotent"
	AnnotationOpenWorld   = "open_world"
)

// KnownAnnotations is the fixed vocabulary consent forms may submit, in
// display order.
var KnownAnnotations = []string{
	AnnotationReadOnly,
	AnnotationDestructive,
	AnnotationIdempotent,
	AnnotationOpenWorld,
}

// SessionSelection is the consent-screen tool policy persisted on
// user_sessions.tool_selection. A nil selection (NULL column) means every
// tool — the only all-tools representation. Any non-nil selection is
// restrictive: a tool passes iff one of its RAW hints is explicitly true for
// a selected annotation, OR its name is listed. Both axes empty means zero
// tools.
type SessionSelection struct {
	// Annotations are live grants against the fixed vocabulary above: a tool
	// that later acquires a selected hint joins the session.
	Annotations []string `json:"annotations,omitempty"`
	// Tools are snapshot grants by tool name. Consent-time group picks are
	// pre-expanded into names so later group-membership changes cannot widen
	// a live session.
	Tools []string `json:"tools,omitempty"`
	// Resource pins the selection to the endpoint whose tool inventory the
	// user consented on: "mcp_server:<uuid>" or "toolset:<uuid>". Session
	// tokens are portable across endpoints sharing an issuer, so a selection
	// served against a different resource is rejected, never reinterpreted.
	Resource string `json:"resource"`
}

// ParseSessionSelection decodes a stored tool_selection document. A stored
// policy with no resource is malformed — callers fail closed on error.
// Unknown stored annotation values are dropped (not an error) and reported
// in dropped so callers can log them: during a mixed-version deploy that
// fails narrower rather than wider.
func ParseSessionSelection(raw []byte) (sel *SessionSelection, dropped []string, err error) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	var decoded SessionSelection
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, fmt.Errorf("decode tool selection: %w", err)
	}
	if decoded.Resource == "" {
		return nil, nil, fmt.Errorf("tool selection missing resource binding")
	}
	known := decoded.Annotations[:0]
	for _, a := range decoded.Annotations {
		if slices.Contains(KnownAnnotations, a) {
			known = append(known, a)
		} else {
			dropped = append(dropped, a)
		}
	}
	decoded.Annotations = known
	return &decoded, dropped, nil
}

// MatchesAnnotations reports whether any selected annotation's raw hint is
// explicitly true on the tool. Nil and false hints are identical (fail
// closed). Deliberately NOT the priority-collapsed disposition used by RBAC:
// a tool carrying readOnlyHint and idempotentHint matches either selection.
func (s *SessionSelection) MatchesAnnotations(annotations *types.ToolAnnotations) bool {
	if s == nil || annotations == nil {
		return false
	}
	hintTrue := func(hint *bool) bool { return hint != nil && *hint }
	for _, a := range s.Annotations {
		switch a {
		case AnnotationReadOnly:
			if hintTrue(annotations.ReadOnlyHint) {
				return true
			}
		case AnnotationDestructive:
			if hintTrue(annotations.DestructiveHint) {
				return true
			}
		case AnnotationIdempotent:
			if hintTrue(annotations.IdempotentHint) {
				return true
			}
		case AnnotationOpenWorld:
			if hintTrue(annotations.OpenWorldHint) {
				return true
			}
		}
	}
	return false
}

// FilterToolsBySelection intersects tools with a session selection. A nil
// selection returns the slice untouched. Proxy/external-MCP placeholders are
// dropped from every restrictive selection: their callable names exist only
// after upstream unfolding and their hints cannot be verified call-side, so
// they fail closed.
func FilterToolsBySelection(tools []*types.Tool, sel *SessionSelection) []*types.Tool {
	if sel == nil {
		return tools
	}

	names := make(map[string]struct{}, len(sel.Tools))
	for _, name := range sel.Tools {
		names[name] = struct{}{}
	}

	filtered := make([]*types.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool == nil || conv.IsProxyTool(tool) {
			continue
		}
		base, err := conv.ToBaseTool(tool)
		if err != nil {
			continue
		}
		if _, ok := names[base.Name]; ok {
			filtered = append(filtered, tool)
			continue
		}
		if sel.MatchesAnnotations(base.Annotations) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}
