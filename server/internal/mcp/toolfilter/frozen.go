package toolfilter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// GatewayInventoryProvider reads the current authorized inventory. It must
// return an error when any admitted member cannot supply a complete catalog.
type GatewayInventoryProvider interface {
	GatewayInventory(context.Context, uuid.UUID, uuid.UUID, urn.SessionSubject) (*FrozenToolset, error)
}

// FrozenTool binds a full MCP definition to its member and routing identity.
// The fingerprint describes declarations, not the upstream implementation.
type FrozenTool struct {
	Name            string          `json:"name"`
	MemberID        uuid.UUID       `json:"member_id"`
	RoutingIdentity string          `json:"routing_identity"`
	Definition      json.RawMessage `json:"definition"`
	Fingerprint     string          `json:"fingerprint"`
}

// FrozenToolset is an explicit allow list. An empty list permits no tools.
type FrozenToolset struct {
	Tools []FrozenTool `json:"tools"`
}

func NewFrozenTool(name string, memberID uuid.UUID, routing string, definition json.RawMessage) (FrozenTool, error) {
	var decoded map[string]any
	decoder := json.NewDecoder(bytes.NewReader(definition))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return FrozenTool{}, fmt.Errorf("decode tool definition: %w", err)
	}
	if decoded == nil || name == "" || memberID == uuid.Nil || routing == "" {
		return FrozenTool{}, fmt.Errorf("incomplete frozen tool")
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return FrozenTool{}, fmt.Errorf("canonicalize tool definition: %w", err)
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(name+"\x00"+memberID.String()+"\x00"+routing+"\x00"+string(canonical))))
	return FrozenTool{Name: name, MemberID: memberID, RoutingIdentity: routing, Definition: canonical, Fingerprint: fingerprint}, nil
}

func (f *FrozenToolset) Validate() error {
	if f == nil || f.Tools == nil || len(f.Tools) > 10000 {
		return fmt.Errorf("invalid frozen toolset")
	}
	seen := make(map[string]bool, len(f.Tools))
	for _, tool := range f.Tools {
		if seen[tool.Name] {
			return fmt.Errorf("duplicate frozen tool")
		}
		seen[tool.Name] = true
		expected, err := NewFrozenTool(tool.Name, tool.MemberID, tool.RoutingIdentity, tool.Definition)
		if err != nil {
			return err
		}
		if expected.Fingerprint != tool.Fingerprint {
			return fmt.Errorf("invalid frozen tool fingerprint")
		}
	}
	return nil
}

func (f *FrozenToolset) Fingerprint() (string, error) {
	if err := f.Validate(); err != nil {
		return "", err
	}
	tools := slices.Clone(f.Tools)
	slices.SortFunc(tools, func(a, b FrozenTool) int { return strings.Compare(a.Name, b.Name) })
	raw, err := json.Marshal(tools)
	if err != nil {
		return "", fmt.Errorf("encode frozen toolset: %w", err)
	}
	if len(raw) > selectionMaxRawBytes {
		return "", fmt.Errorf("frozen inventory exceeds 2 MiB")
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

// Select rejects a stale review before applying the user's exact selection.
func (f *FrozenToolset) Select(fingerprint string, names []string) (*FrozenToolset, error) {
	current, err := f.Fingerprint()
	if err != nil {
		return nil, err
	}
	if fingerprint != current {
		return nil, fmt.Errorf("tool definitions changed during review; review the current inventory again")
	}
	byName := make(map[string]FrozenTool, len(f.Tools))
	for _, tool := range f.Tools {
		byName[tool.Name] = tool
	}
	selected := &FrozenToolset{Tools: make([]FrozenTool, 0, len(names))}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		tool, ok := byName[name]
		if !ok || seen[name] {
			return nil, fmt.Errorf("unknown or duplicate reviewed tool")
		}
		seen[name] = true
		selected.Tools = append(selected.Tools, tool)
	}
	return selected, nil
}

func (f *FrozenToolset) Allows(current FrozenTool) bool {
	for _, approved := range f.Tools {
		if approved.Name == current.Name {
			return approved.MemberID == current.MemberID && approved.Fingerprint == current.Fingerprint
		}
	}
	return false
}

// SelectionForTool projects an approved gateway tool onto its member dispatcher.
func SelectionForTool(resource, name string) (*SessionSelection, error) {
	return NewSessionSelection(resource, uuid.NewSHA1(uuid.NameSpaceOID, []byte(resource+"\x00"+name)), []AllowEntry{{Type: AllowTypeTool, Name: name, Mode: nil, Tools: nil}})
}
