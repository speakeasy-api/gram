package toolfilter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
)

// GatewayOptions contains explicit connection choices. An absent mode follows
// the owner's current default on every request.
type GatewayOptions struct {
	Frozen *FrozenToolset `json:"frozen,omitempty"`
	// DiscoveryMode overrides the gateway default for this connection.
	DiscoveryMode *metamcp.DiscoveryMode `json:"discovery_mode,omitempty"`
}

// SessionPolicy separates connection options from restrictive tool selection.
// A nil policy is a legacy unrestricted session. A policy with only gateway
// options is also unrestricted, but remains bound to its gateway.
type SessionPolicy struct {
	// Resource binds both options and restrictions to one endpoint identity.
	Resource string

	// Selection is nil for unrestricted tools; an empty allow list denies all.
	Selection *SessionSelection

	// Gateway holds explicit options for a stored gateway connection.
	Gateway *GatewayOptions
}

type gatewayPolicyDocument struct {
	Version   int               `json:"version"`
	Resource  string            `json:"resource"`
	Gateway   *GatewayOptions   `json:"gateway"`
	Selection *SessionSelection `json:"selection,omitempty"`
}

// PolicyForSelection preserves a legacy restrictive selection without adding
// gateway options or changing its stored representation.
func PolicyForSelection(selection *SessionSelection) *SessionPolicy {
	if selection == nil {
		return nil
	}
	return &SessionPolicy{Resource: selection.Resource, Selection: selection, Gateway: nil}
}

// ParseSessionPolicy reads legacy selections and versioned gateway documents.
// Malformed, unknown, or unsupported documents fail closed.
func ParseSessionPolicy(raw []byte) (*SessionPolicy, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > selectionMaxRawBytes {
		return nil, fmt.Errorf("session policy exceeds %d bytes", selectionMaxRawBytes)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("decode session policy: %w", err)
	}
	if _, versioned := fields["version"]; !versioned {
		selection, err := ParseSessionSelection(raw)
		if err != nil {
			return nil, err
		}
		return PolicyForSelection(selection), nil
	}
	var document gatewayPolicyDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode gateway policy: %w", err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("gateway policy carries trailing data")
	}
	if document.Version != 1 && document.Version != 2 {
		return nil, fmt.Errorf("unsupported gateway policy version")
	}
	if document.Version == 2 && (document.Gateway == nil || document.Gateway.Frozen == nil) {
		return nil, fmt.Errorf("policy version 2 requires a frozen toolset")
	}
	if document.Version == 1 && document.Gateway != nil && document.Gateway.Frozen != nil {
		return nil, fmt.Errorf("frozen toolsets require policy version 2")
	}
	policy := &SessionPolicy{Resource: document.Resource, Selection: document.Selection, Gateway: document.Gateway}
	if err := policy.validateGateway(); err != nil {
		return nil, err
	}
	return policy, nil
}

func (p *SessionPolicy) validateGateway() error {
	kind, id, ok := strings.Cut(p.Resource, ":")
	parsed, err := uuid.Parse(id)
	if !ok || kind != "meta_mcp_server" || err != nil || parsed == uuid.Nil || parsed.String() != id {
		return fmt.Errorf("gateway policy carries an invalid resource binding")
	}
	if p.Gateway == nil || (p.Gateway.DiscoveryMode == nil && p.Gateway.Frozen == nil) || (p.Gateway.DiscoveryMode != nil && !p.Gateway.DiscoveryMode.Valid()) {
		return fmt.Errorf("gateway policy requires a supported discovery mode")
	}
	if p.Gateway.Frozen != nil {
		if err := p.Gateway.Frozen.Validate(); err != nil {
			return err
		}
	}
	// Gateway tool restrictions use definition snapshots, not a legacy
	// member-local name selection that could match another member's tool.
	if p.Selection != nil {
		return fmt.Errorf("gateway policy does not support legacy tool selections")
	}
	return nil
}

// MarshalJSON preserves the legacy selection format and versions gateway options.
func (p *SessionPolicy) MarshalJSON() ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}
	if p.Gateway == nil {
		if p.Selection == nil || p.Resource != p.Selection.Resource {
			return nil, fmt.Errorf("session policy has no valid selection")
		}
		selection := *p.Selection
		if err := selection.compile(); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(&selection)
		if err != nil {
			return nil, fmt.Errorf("marshal selection: %w", err)
		}
		return raw, nil
	}
	if err := p.validateGateway(); err != nil {
		return nil, err
	}
	version := 1
	if p.Gateway.Frozen != nil {
		version = 2
	}
	raw, err := json.Marshal(gatewayPolicyDocument{Version: version, Resource: p.Resource, Gateway: p.Gateway, Selection: p.Selection})
	if err != nil {
		return nil, fmt.Errorf("marshal gateway policy: %w", err)
	}
	if len(raw) > selectionMaxRawBytes {
		return nil, fmt.Errorf("session policy exceeds 2 MiB")
	}
	return raw, nil
}

// UnmarshalJSON validates cached authorization grants as strictly as stored rows.
func (p *SessionPolicy) UnmarshalJSON(raw []byte) error {
	policy, err := ParseSessionPolicy(raw)
	if err != nil {
		return err
	}
	if policy == nil {
		return fmt.Errorf("missing session policy")
	}
	*p = *policy
	return nil
}
