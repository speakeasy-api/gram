package authz

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Selector is a set of key-value constraints attached to a grant or check.
// Grants use explicit resource_kind and resource_id keys, e.g.
// {"resource_kind":"project","resource_id":"proj_123"}.
// Wildcard uses {"resource_kind":"*","resource_id":"*"}.
// For a grant selector to match a check selector, every key in the grant must
// either equal the corresponding check value or be the wildcard "*".
type Selector map[string]string

const (
	SelectorKeyResourceKind   = "resource_kind"
	SelectorKeyResourceID     = "resource_id"
	SelectorKeyTool           = "tool"
	SelectorKeyDisposition    = "disposition"
	SelectorKeyProjectID      = "project_id"
	SelectorKeyServerURL      = "server_url"
	SelectorKeyServerIdentity = "server_identity"
	// SelectorKeyClientType and SelectorKeyConnectionOwner narrow mcp:auth
	// grants to an auth client type and to the owner of a personal connection.
	SelectorKeyClientType      = "client_type"
	SelectorKeyConnectionOwner = "connection_owner"
	// SelectorKeyUpstreamClient narrows mcp:auth grants on personal clients to
	// one upstream OAuth client, the remote_session_clients id behind it. With
	// a connection owner it names one logical connection across every auth
	// client sharing that upstream client.
	SelectorKeyUpstreamClient = "upstream_client"
)

// SelectorValueRepresentedUser is the connection_owner value naming the user a
// request acts for. The engine binds it; checks never carry it.
const SelectorValueRepresentedUser = "$represented_user"

// Auth client types an mcp:auth check can name. Only remote_session clients
// are personal: their checks carry the connection owner.
const (
	AuthClientTypeRemoteSession    = "remote_session"
	AuthClientTypeServiceAccount   = "service_account"
	AuthClientTypeStaticCredential = "static_credential" //nolint:gosec // client type name, not a credential
	AuthClientTypePassThrough      = "pass_through"
	AuthClientTypeEMA              = "ema"
)

var validAuthClientTypes = map[string]bool{
	AuthClientTypeRemoteSession:    true,
	AuthClientTypeServiceAccount:   true,
	AuthClientTypeStaticCredential: true,
	AuthClientTypePassThrough:      true,
	AuthClientTypeEMA:              true,
}

// Matches reports whether this (grant) selector satisfies the given check
// selector. A nil/empty grant selector matches any check (defensive fallback).
// For each key present in BOTH the grant and check selectors, the values must
// be equal or the grant value must be "*". Keys present in the grant but absent
// from the check are skipped — the check is not constraining that dimension.
// This allows disposition-scoped grants (e.g. {"disposition":"read_only"}) to
// match connection-level checks that don't yet specify a disposition.
func (s Selector) Matches(check Selector) bool {
	return s.matches(check, "")
}

func (s Selector) matches(check Selector, representedUser string) bool {
	for k, grantVal := range s {
		checkVal, ok := check[k]
		if !ok {
			// Check doesn't constrain this dimension — skip.
			continue
		}
		if !selectorValueMatches(k, grantVal, checkVal, representedUser) {
			return false
		}
	}
	return true
}

// StrictMatches is like Matches but requires every key in the grant selector
// to be present in the check selector. Exclusion scopes use strict matching so
// a tool-scoped exclusion does not match a dimensionless server-level check.
func (s Selector) StrictMatches(check Selector) bool {
	return s.strictMatches(check, "")
}

func (s Selector) strictMatches(check Selector, representedUser string) bool {
	for k, grantVal := range s {
		checkVal, ok := check[k]
		if !ok {
			return false
		}
		if !selectorValueMatches(k, grantVal, checkVal, representedUser) {
			return false
		}
	}
	return true
}

// selectorValueMatches compares one grant value with one check value. Without a
// binding, $represented_user matches only itself, as containment needs.
func selectorValueMatches(key, grantVal, checkVal, representedUser string) bool {
	if grantVal == "*" || grantVal == checkVal {
		return true
	}
	return key == SelectorKeyConnectionOwner && grantVal == SelectorValueRepresentedUser &&
		representedUser != "" && checkVal == representedUser
}

// ResourceKindForScope derives the selector resource kind from a scope.
func ResourceKindForScope(scope Scope) string {
	if scope == ScopeMCPAuth || scope == ScopeMCPBlockedAuth {
		return ResourceKindMCPAuthClient
	}
	switch scope.Parts().Resource {
	case "project", "plugin", "log":
		return ResourceKindProject
	case "remote-mcp":
		return ResourceKindMCP
	case "mcp":
		return ResourceKindMCP
	case "org":
		return ResourceKindOrg
	case "environment":
		return ResourceKindEnvironment
	case "skill":
		return ResourceKindSkill
	case "assistant":
		return ResourceKindAssistant
	case "risk_policy":
		return ResourceKindRiskPolicy
	case "chat":
		return ResourceKindChat
	case "agent":
		return ResourceKindAgent
	case "workload":
		return ResourceKindWorkload
	default:
		return ResourceKindWildcard
	}
}

// Disposition values matching MCP tool annotation hint names (snake_case, no _hint suffix).
const (
	DispositionReadOnly    = "read_only"
	DispositionDestructive = "destructive"
	DispositionIdempotent  = "idempotent"
	DispositionOpenWorld   = "open_world"
)

// validDispositions is the set of allowed disposition values.
var validDispositions = map[string]bool{
	DispositionReadOnly:    true,
	DispositionDestructive: true,
	DispositionIdempotent:  true,
	DispositionOpenWorld:   true,
}

// allowedSelectorKeys defines which extra keys (beyond resource_kind and
// resource_id) are valid for each scope family. Scope families not listed here
// allow no extra keys, except where scopeAllowsSelectorKey admits one for a
// single scope.
var allowedSelectorKeys = map[string]map[string]bool{
	ResourceKindMCP: {
		SelectorKeyTool:        true,
		SelectorKeyDisposition: true,
		SelectorKeyProjectID:   true,
	},
	ResourceKindEnvironment: {
		SelectorKeyProjectID: true,
	},
	ResourceKindAssistant: {
		SelectorKeyProjectID: true,
	},
	ResourceKindRiskPolicy: {
		SelectorKeyServerURL:      true,
		SelectorKeyServerIdentity: true,
	},
	ResourceKindMCPAuthClient: {
		SelectorKeyClientType:      true,
		SelectorKeyProjectID:       true,
		SelectorKeyConnectionOwner: true,
		SelectorKeyUpstreamClient:  true,
	},
}

// scopeAllowsSelectorKey allows project_id on org:hooks_ingest only: it is the
// one org scope checked per project, and elsewhere the key would restrict nothing.
func scopeAllowsSelectorKey(scope Scope, key string) bool {
	return scope == ScopeOrgHooksIngest && key == SelectorKeyProjectID
}

// ValidateSelector checks that a selector is well-formed for the given scope.
// Rules:
//   - resource_kind and resource_id must both be present
//   - resource_kind must match the scope family (or be "*" for root)
//   - extra keys must be in the allowed set for the scope family, or one
//     scopeAllowsSelectorKey admits for the scope itself
//   - unknown keys are rejected
func ValidateSelector(scope Scope, sel Selector) error {
	kind, hasKind := sel[SelectorKeyResourceKind]
	_, hasID := sel[SelectorKeyResourceID]
	if !hasKind || !hasID {
		return fmt.Errorf("selector must include both resource_kind and resource_id")
	}

	expectedKind := ResourceKindForScope(scope)
	if scope == ScopeRoot {
		if kind != ResourceKindWildcard {
			return fmt.Errorf("root scope requires resource_kind=%s, got %q", ResourceKindWildcard, kind)
		}
		// root allows no extra keys
		for k := range sel {
			if k != SelectorKeyResourceKind && k != SelectorKeyResourceID {
				return fmt.Errorf("root scope does not allow extra selector key %q", k)
			}
		}
		return nil
	}

	if kind != expectedKind {
		return fmt.Errorf("scope %q requires resource_kind=%q, got %q", scope, expectedKind, kind)
	}

	allowed := allowedSelectorKeys[expectedKind]
	for k, v := range sel {
		if v == SelectorValueRepresentedUser && k != SelectorKeyConnectionOwner {
			return fmt.Errorf("selector value %q is only allowed for %q", SelectorValueRepresentedUser, SelectorKeyConnectionOwner)
		}
		if k == SelectorKeyResourceKind || k == SelectorKeyResourceID {
			continue
		}
		if !allowed[k] && !scopeAllowsSelectorKey(scope, k) {
			return fmt.Errorf("selector key %q is not allowed for scope %q", k, scope)
		}
		if k == SelectorKeyDisposition && !validDispositions[v] {
			return fmt.Errorf("invalid disposition value %q", v)
		}
		if k == SelectorKeyClientType && v != WildcardResource && !validAuthClientTypes[v] {
			return fmt.Errorf("invalid client_type value %q", v)
		}
		if k == SelectorKeyConnectionOwner || k == SelectorKeyUpstreamClient {
			if clientType, ok := sel[SelectorKeyClientType]; ok && clientType != WildcardResource && clientType != AuthClientTypeRemoteSession {
				return fmt.Errorf("selector key %q requires client_type %q", k, AuthClientTypeRemoteSession)
			}
		}
		if expectedKind == ResourceKindMCPAuthClient && v == "" {
			return fmt.Errorf("selector key %q must not be empty", k)
		}
		if k == SelectorKeyServerURL {
			parsed, err := url.Parse(v)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				return fmt.Errorf("invalid server_url value %q: must include URI scheme and host", v)
			}
		}
	}

	return nil
}

// NewSelector creates a selector with resource_kind derived from scope.
func NewSelector(scope Scope, resourceID string) Selector {
	return Selector{
		SelectorKeyResourceKind: ResourceKindForScope(scope),
		SelectorKeyResourceID:   resourceID,
	}
}

// NewGrant creates a Grant with selector derived from scope and resource ID.
func NewGrant(scope Scope, resourceID string) Grant {
	return Grant{
		PrincipalUrn: "",
		Scope:        scope,
		Selector:     NewSelector(scope, resourceID),
		Inferred:     false,
	}
}

// NewGrantWithSelector creates a Grant with an explicit selector.
func NewGrantWithSelector(scope Scope, selector Selector) Grant {
	return Grant{
		PrincipalUrn: "",
		Scope:        scope,
		Selector:     selector,
		Inferred:     false,
	}
}

// ResourceID extracts the resource_id value from the selector.
// Returns "*" if no resource_id key is present.
func (s Selector) ResourceID() string {
	if id, ok := s[SelectorKeyResourceID]; ok {
		return id
	}
	return WildcardResource
}

// IsRestricted reports whether the selector constrains at least one resource
// dimension. A short wildcard selector is treated as unrestricted for backward
// compatibility with pre-resource-kind rows that only carried resource_id.
func (s Selector) IsRestricted() bool {
	if len(s) == 0 {
		return false
	}
	if len(s) == 1 {
		id, ok := s[SelectorKeyResourceID]
		return !ok || id != WildcardResource
	}
	if len(s) != 2 {
		return true
	}

	kind, hasKind := s[SelectorKeyResourceKind]
	id, hasID := s[SelectorKeyResourceID]
	if !hasKind || !hasID {
		return true
	}
	return kind != WildcardResource || id != WildcardResource
}

// SelectorFromRow parses the selectors JSONB column into a Selector.
func SelectorFromRow(selectors []byte) (Selector, error) {
	var sel Selector
	if err := json.Unmarshal(selectors, &sel); err != nil {
		return nil, fmt.Errorf("unmarshal selector: %w", err)
	}
	return sel, nil
}

// MarshalJSON implements json.Marshaler. A nil selector marshals as the
// explicit wildcard {"resource_kind":"*","resource_id":"*"}.
func (s Selector) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte(`{"resource_kind":"*","resource_id":"*"}`), nil
	}
	b, err := json.Marshal(map[string]string(s))
	if err != nil {
		return nil, fmt.Errorf("marshal selector: %w", err)
	}
	return b, nil
}
