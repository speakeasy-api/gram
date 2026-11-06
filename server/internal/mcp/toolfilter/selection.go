package toolfilter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/google/uuid"

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

// AllowType discriminates the entries of a selection's allow list. The
// typed-object pattern follows OAuth Rich Authorization Details (RFC 9396):
// new grant kinds become new types without reshaping the document.
type AllowType string

const (
	AllowTypeTool       AllowType = "tool"
	AllowTypeAnnotation AllowType = "annotation"
)

// AnnotationMode is how an annotation allow-entry is enforced. Snapshot
// freezes the annotation's matching tool names at approval time into the
// entry's own Tools array; live re-matches the server's declared hints at
// serve time, so new or renamed tools carrying the annotation join the
// session. The mode is the consenting subject's own choice, stamped at
// approval; proxied live grants are additionally constrained to tools
// captured for the session by the listing witness.
type AnnotationMode string

const (
	AnnotationModeSnapshot AnnotationMode = "snapshot"
	AnnotationModeLive     AnnotationMode = "live"
)

// AllowEntry is one inclusion in a selection's allow list. The union of all
// entries is the session's tool scope; there are no deny semantics.
type AllowEntry struct {
	// Type is tool or annotation.
	Type AllowType `json:"type"`

	// Name is the tool name (tool entries) or KnownAnnotations value
	// (annotation entries).
	Name string `json:"name"`

	// Mode is required on annotation entries and forbidden on tool entries.
	// It is never defaulted: a snapshot grant reclassified as live would
	// widen the session, so ambiguity is a parse error.
	Mode *AnnotationMode `json:"mode,omitempty"`

	// Tools is the server-derived approval-time expansion of a
	// snapshot-mode annotation entry: the names of the displayed tools that
	// carried the annotation when the user approved. Required (possibly
	// empty) exactly on snapshot entries and forbidden elsewhere; the
	// pointer keeps an explicitly empty expansion distinct from an absent
	// field.
	Tools *[]string `json:"tools,omitempty"`
}

// SessionSelection is the consent-screen tool policy persisted on
// the legacy user_sessions.tool_selection document. A nil selection is unrestricted;
// versioned policies may carry connection options without a selection. Any
// SessionSelection document is restrictive: a tool is in scope
// iff some allow entry includes it — by exact name for tool entries and
// snapshot expansions, or by serve-time hint matching for live annotation
// entries. An empty allow list authorizes zero tools.
type SessionSelection struct {
	// Resource pins the selection to the endpoint whose tool inventory the
	// user consented on: "mcp_server:<uuid>", "toolset:<uuid>" or, for a
	// gateway, "meta_mcp_server:<uuid>". Session
	// tokens are portable across endpoints sharing an issuer, so a selection
	// served against a different resource is rejected, never reinterpreted.
	Resource string `json:"resource"`

	// GrantID identifies this consent grant stably across refresh-token
	// rotations; the proxied live-inventory cache keys on it.
	GrantID uuid.UUID `json:"grant_id"`

	// Allow is the inclusion union. Required: a document missing it is
	// corrupt and rejected wholesale, never read as all-tools.
	Allow []AllowEntry `json:"allow"`

	// nameSet and liveAnnotations are the enforcement projection compiled at
	// parse/build time; unexported so the wire document stays the canonical
	// entries alone.
	nameSet         map[string]struct{}
	liveAnnotations []string
}

// MaxTools bounds a captured inventory and every selection made from it.
const MaxTools = 10000

// Serialized policies include overlapping annotation snapshots.
const selectionMaxRawBytes = 16 << 20

// Leave 1 MiB for JSONB's added separator whitespace when policies are read back.
// The tool and entry limits bound that expansion to fewer than 100,000 bytes.
const selectionMaxWriteBytes = selectionMaxRawBytes - (1 << 20)

const selectionMaxNameBytes = 200

// NewSessionSelection assembles, canonicalizes, and validates a selection:
// annotation entries first in vocabulary order, tool entries after in
// lexical order, snapshot expansions sorted. Entries are deep-copied so
// caller-owned slices are never mutated or aliased. The returned selection
// is compiled — a persisted document is always a valid one.
func NewSessionSelection(resource string, grantID uuid.UUID, entries []AllowEntry) (*SessionSelection, error) {
	size, err := selectionJSONSize(resource, grantID, entries)
	if err != nil {
		return nil, err
	}
	if size > selectionMaxWriteBytes {
		return nil, fmt.Errorf("tool selection document exceeds %d bytes", selectionMaxWriteBytes)
	}
	vocabRank := func(name string) int {
		return slices.Index(KnownAnnotations, name)
	}
	sorted := make([]AllowEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Mode != nil {
			mode := *entry.Mode
			entry.Mode = &mode
		}
		if entry.Tools != nil {
			tools := slices.Clone(*entry.Tools)
			slices.Sort(tools)
			entry.Tools = &tools
		}
		sorted = append(sorted, entry)
	}
	slices.SortStableFunc(sorted, func(a, b AllowEntry) int {
		if a.Type != b.Type {
			if a.Type == AllowTypeAnnotation {
				return -1
			}
			return 1
		}
		if a.Type == AllowTypeAnnotation {
			return vocabRank(a.Name) - vocabRank(b.Name)
		}
		return strings.Compare(a.Name, b.Name)
	})
	selection := &SessionSelection{
		Resource:        resource,
		GrantID:         grantID,
		Allow:           sorted,
		nameSet:         nil,
		liveAnnotations: nil,
	}
	if err := selection.compile(); err != nil {
		return nil, err
	}
	return selection, nil
}

// selectionJSONSize measures the wire document using only bounded individual
// strings. It stops at the write budget before cloning or encoding snapshots.
func selectionJSONSize(resource string, grantID uuid.UUID, entries []AllowEntry) (int, error) {
	if !validResource(resource) {
		return 0, fmt.Errorf("tool selection carries an invalid resource binding")
	}
	if len(entries) > MaxTools+len(KnownAnnotations) {
		return 0, fmt.Errorf("tool selection exceeds %d allow entries", MaxTools+len(KnownAnnotations))
	}
	header, err := json.Marshal(&SessionSelection{Resource: resource, GrantID: grantID, Allow: []AllowEntry{}, nameSet: nil, liveAnnotations: nil})
	if err != nil {
		return 0, fmt.Errorf("measure tool selection: %w", err)
	}
	size := len(header)
	for i, entry := range entries {
		if entry.Type != AllowTypeTool && entry.Type != AllowTypeAnnotation {
			return 0, fmt.Errorf("tool selection carries unknown allow entry type")
		}
		if len(entry.Name) > selectionMaxNameBytes {
			return 0, fmt.Errorf("tool selection carries an invalid tool name")
		}
		if entry.Mode != nil && *entry.Mode != AnnotationModeSnapshot && *entry.Mode != AnnotationModeLive {
			return 0, fmt.Errorf("tool selection carries unknown annotation mode")
		}
		metadata := entry
		metadata.Tools = nil
		raw, err := json.Marshal(metadata)
		if err != nil {
			return 0, fmt.Errorf("measure tool selection entry: %w", err)
		}
		size += len(raw)
		if i > 0 {
			size++ // Comma between entries.
		}
		if entry.Tools != nil {
			if len(*entry.Tools) > MaxTools {
				return 0, fmt.Errorf("snapshot expansion exceeds %d names", MaxTools)
			}
			if *entry.Tools == nil {
				size += len(`,"tools":null`)
			} else {
				size += len(`,"tools":[]`)
			}
			for j, name := range *entry.Tools {
				if len(name) > selectionMaxNameBytes {
					return 0, fmt.Errorf("tool selection carries an invalid snapshot tool name")
				}
				encoded, err := json.Marshal(name)
				if err != nil {
					return 0, fmt.Errorf("measure snapshot tool name: %w", err)
				}
				size += len(encoded)
				if j > 0 {
					size++ // Comma between names.
				}
				if size > selectionMaxWriteBytes {
					return size, nil
				}
			}
		}
		if size > selectionMaxWriteBytes {
			return size, nil
		}
	}
	return size, nil
}

// ParseSessionSelection decodes and validates a stored tool_selection
// document. Speakeasy is the document's only writer, so any malformation is
// corruption and rejects the whole document — callers fail closed into
// reauthorization, never into a wider session.
func ParseSessionSelection(raw []byte) (*SessionSelection, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > selectionMaxRawBytes {
		return nil, fmt.Errorf("tool selection document exceeds %d bytes", selectionMaxRawBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decoded SessionSelection
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode tool selection: %w", err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("tool selection document carries trailing data")
	}
	if err := decoded.compile(); err != nil {
		return nil, err
	}
	return &decoded, nil
}

// validResource reports whether the resource binding has the
// server-authored "<kind>:<uuid>" shape with a canonical, non-zero UUID.
func validResource(resource string) bool {
	kind, id, ok := strings.Cut(resource, ":")
	if !ok || (kind != "mcp_server" && kind != "toolset" && kind != gatewayResourceKind) {
		return false
	}
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

// compile validates the document and builds the enforcement projection.
func (s *SessionSelection) compile() error {
	if !validResource(s.Resource) {
		return fmt.Errorf("tool selection carries an invalid resource binding")
	}
	if s.GrantID == uuid.Nil {
		return fmt.Errorf("tool selection missing grant id")
	}
	if s.Allow == nil {
		return fmt.Errorf("tool selection missing allow list")
	}
	if len(s.Allow) > MaxTools+len(KnownAnnotations) {
		return fmt.Errorf("tool selection exceeds %d allow entries", MaxTools+len(KnownAnnotations))
	}

	names := map[string]struct{}{}
	var live []string
	seenTools := map[string]bool{}
	seenAnnotations := map[string]bool{}

	validName := func(name string) bool {
		return name != "" && len(name) <= selectionMaxNameBytes
	}

	for _, entry := range s.Allow {
		switch entry.Type {
		case AllowTypeTool:
			if !validName(entry.Name) {
				return fmt.Errorf("tool selection carries an invalid tool name")
			}
			if entry.Mode != nil || entry.Tools != nil {
				return fmt.Errorf("tool allow entries carry no mode or tools")
			}
			if seenTools[entry.Name] {
				return fmt.Errorf("tool selection repeats tool entry %q", entry.Name)
			}
			seenTools[entry.Name] = true
			names[entry.Name] = struct{}{}

		case AllowTypeAnnotation:
			if !slices.Contains(KnownAnnotations, entry.Name) {
				return fmt.Errorf("tool selection carries unknown annotation %q", entry.Name)
			}
			if seenAnnotations[entry.Name] {
				return fmt.Errorf("tool selection repeats annotation entry %q", entry.Name)
			}
			seenAnnotations[entry.Name] = true
			if entry.Mode == nil {
				return fmt.Errorf("annotation allow entries require a mode")
			}
			switch *entry.Mode {
			case AnnotationModeLive:
				if entry.Tools != nil {
					return fmt.Errorf("live annotation allow entries carry no tools")
				}
				live = append(live, entry.Name)
			case AnnotationModeSnapshot:
				if entry.Tools == nil {
					return fmt.Errorf("snapshot annotation allow entries require a tools array")
				}
				if len(*entry.Tools) > MaxTools {
					return fmt.Errorf("snapshot expansion exceeds %d names", MaxTools)
				}
				withinEntry := map[string]bool{}
				for _, name := range *entry.Tools {
					if !validName(name) {
						return fmt.Errorf("tool selection carries an invalid snapshot tool name")
					}
					if withinEntry[name] {
						return fmt.Errorf("snapshot expansion repeats tool %q", name)
					}
					withinEntry[name] = true
					names[name] = struct{}{}
				}
			default:
				return fmt.Errorf("tool selection carries unknown annotation mode %q", *entry.Mode)
			}

		default:
			return fmt.Errorf("tool selection carries unknown allow entry type %q", entry.Type)
		}

		if len(names) > MaxTools {
			return fmt.Errorf("tool selection exceeds %d distinct tools", MaxTools)
		}
	}

	s.nameSet = names
	s.liveAnnotations = live
	return nil
}

// AllowsName reports whether the name is in scope via a tool entry or a
// snapshot expansion.
func (s *SessionSelection) AllowsName(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.nameSet[name]
	return ok
}

// LiveAnnotations returns the annotation names granted live, in document
// order. Empty for selections with no live grants. The slice is a copy —
// compiled authorization state is never handed out mutable.
func (s *SessionSelection) LiveAnnotations() []string {
	if s == nil {
		return nil
	}
	return slices.Clone(s.liveAnnotations)
}

// AnnotationsMatch reports whether any of the given annotation values has
// its raw hint explicitly true on the tool. Nil and false hints are
// identical (fail closed). Deliberately NOT the priority-collapsed
// disposition used by RBAC: a tool carrying readOnlyHint and idempotentHint
// matches either value.
func AnnotationsMatch(annotations *types.ToolAnnotations, values []string) bool {
	if annotations == nil || len(values) == 0 {
		return false
	}
	hintTrue := func(hint *bool) bool { return hint != nil && *hint }
	for _, value := range values {
		switch value {
		case AnnotationReadOnly, "readOnlyHint":
			if hintTrue(annotations.ReadOnlyHint) {
				return true
			}
		case AnnotationDestructive, "destructiveHint":
			if hintTrue(annotations.DestructiveHint) {
				return true
			}
		case AnnotationIdempotent, "idempotentHint":
			if hintTrue(annotations.IdempotentHint) {
				return true
			}
		case AnnotationOpenWorld, "openWorldHint":
			if hintTrue(annotations.OpenWorldHint) {
				return true
			}
		}
	}
	return false
}

// FilterToolsBySelection intersects tools with a session selection: a tool
// passes iff an allow entry includes it by name or a live annotation grant
// matches its declared hints. A nil selection returns the slice untouched.
// Proxy/external-MCP placeholders are dropped from every restrictive
// selection: their callable names exist only after upstream unfolding, so
// they fail closed.
func FilterToolsBySelection(tools []*types.Tool, sel *SessionSelection) []*types.Tool {
	if sel == nil {
		return tools
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
		if sel.AllowsName(base.Name) {
			filtered = append(filtered, tool)
			continue
		}
		if AnnotationsMatch(base.Annotations, sel.LiveAnnotations()) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

const gatewayResourceKind = "meta_mcp_server"
