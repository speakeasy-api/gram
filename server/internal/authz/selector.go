package authz

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
)

// Selector is a set of key-value constraints attached to a grant or check.
// Grants use explicit resource_kind and resource_id keys, e.g.
// {"resource_kind":"project","resource_id":"proj_123"}.
// Wildcard uses {"resource_kind":"*","resource_id":"*"}.
// For a grant selector to match a check selector, every key in the grant must
// either equal the corresponding check value or be the wildcard "*".
type Selector map[string]string

// The SelectorKey*, AuthClientType* and Disposition* names are generated from
// the registry into selectors_gen.go.

// SelectorValueRepresentedUser is the connection_owner value naming the user a
// request acts for. The engine binds it; checks never carry it.
const SelectorValueRepresentedUser = "$represented_user"

// IsSharedAuthClientType reports whether clientType serves every caller with
// one credential that belongs to nobody, as opposed to a person's own
// connection or identity-provider sign-in.
func IsSharedAuthClientType(clientType string) bool {
	switch clientType {
	case AuthClientTypeServiceAccount, AuthClientTypeStaticCredential, AuthClientTypePassThrough:
		return true
	}
	return false
}

// IsAgentKeyAuthClientType reports whether an agent key can be set up to use
// clientType: a shared credential that needs nothing from the caller's
// request, since the agent gateway does not carry the caller's headers to its
// servers.
func IsAgentKeyAuthClientType(clientType string) bool {
	return IsSharedAuthClientType(clientType) && clientType != AuthClientTypePassThrough
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

// StrictMatchesFor is StrictMatches with $represented_user bound to
// representedUser on either side. An empty representedUser binds nothing, so
// the marker matches only itself.
func (s Selector) StrictMatchesFor(check Selector, representedUser string) bool {
	return s.strictMatches(check, representedUser)
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
// binding, $represented_user matches only itself, as containment needs. With
// one, the marker and the bound user are the same owner on either side, so a
// symbolic candidate is contained by a grant pinned to the represented user.
func selectorValueMatches(key, grantVal, checkVal, representedUser string) bool {
	if grantVal == "*" || grantVal == checkVal {
		return true
	}
	if key != SelectorKeyConnectionOwner || representedUser == "" {
		return false
	}
	return (grantVal == SelectorValueRepresentedUser && checkVal == representedUser) ||
		(checkVal == SelectorValueRepresentedUser && grantVal == representedUser)
}

// ResourceKindForScope returns the selector resource kind of a scope: the kind
// the registry declares it on, or, for a scope the model does not know, the kind its
// prefix has always meant.
func ResourceKindForScope(scope Scope) string {
	if kind, ok := scopeResourceKinds[scope]; ok {
		return kind
	}
	return legacyResourceKindForScope(scope)
}

// legacyResourceKindForScope keeps today's answer for scopes outside the
// model, such as retired ones still stored in old policies.
func legacyResourceKindForScope(scope Scope) string {
	prefix := scope.Parts().Resource
	switch prefix {
	case "plugin", "log":
		return ResourceKindProject
	case "remote-mcp":
		return ResourceKindMCP
	}
	if declaredResourceKinds[prefix] && prefix != ResourceKindMCPAuthClient {
		return prefix
	}
	return ResourceKindWildcard
}

// scopeAllowsSelectorKey reports whether a single permission declares key
// beyond its type's dimensions. Today that is project_id on org:hooks_ingest:
// the one org scope checked per project (R6, parked).
func scopeAllowsSelectorKey(scope Scope, key string) bool {
	return scopeSelectorKeys[scope][key]
}

// storedLimitScopes are the scopes that could once be saved with a tool or
// disposition limit and no longer can. Limits belong to connect, where a tool
// is being called; viewing or managing a server names no tool.
var storedLimitScopes = []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPBlockedRead, ScopeMCPBlockedWrite}

// StoredSelectorInEffect returns the selector a stored grant takes effect
// with, and false when the grant has no effect at all. Both follow from how
// checks are matched: no check for viewing or managing a server names a tool,
// so a limited allow already matches as if it had no limit, and a limited
// block never matches. Loading grants this way changes no decision; it lets
// everything after the load treat limits as a connect-only idea.
func StoredSelectorInEffect(scope Scope, sel Selector) (Selector, bool) {
	if !slices.Contains(storedLimitScopes, scope) {
		return sel, true
	}
	_, hasTool := sel[SelectorKeyTool]
	_, hasDisposition := sel[SelectorKeyDisposition]
	if !hasTool && !hasDisposition {
		return sel, true
	}
	if IsBlocklistScope(scope) {
		return nil, false
	}
	unlimited := maps.Clone(sel)
	delete(unlimited, SelectorKeyTool)
	delete(unlimited, SelectorKeyDisposition)
	return unlimited, true
}

// ValidateStoredSelector is ValidateSelector for a selector read back from
// storage. It also accepts a tool or disposition limit on the scopes in
// storedLimitScopes, because rows saved before limits moved to connect still
// carry one and must keep the effect they have today. Use ValidateSelector for
// anything a caller is asking to save.
func ValidateStoredSelector(scope Scope, sel Selector) error {
	if !slices.Contains(storedLimitScopes, scope) {
		return ValidateSelector(scope, sel)
	}
	if disposition, ok := sel[SelectorKeyDisposition]; ok && !validDispositions[disposition] {
		return fmt.Errorf("invalid disposition value %q", disposition)
	}
	unlimited := maps.Clone(sel)
	delete(unlimited, SelectorKeyTool)
	delete(unlimited, SelectorKeyDisposition)
	return ValidateSelector(scope, unlimited)
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

	if scope == ScopeInferenceExecute {
		if sel[SelectorKeyResourceID] != WildcardResource {
			return fmt.Errorf("scope %q requires resource_id=%q", scope, WildcardResource)
		}
		if projectID, ok := sel[SelectorKeyProjectID]; ok && projectID == "" {
			return fmt.Errorf("selector key %q must not be empty", SelectorKeyProjectID)
		}
	}

	allowed := allowedSelectorKeys[expectedKind]
	for k, v := range sel {
		if v == SelectorValueRepresentedUser && k != SelectorKeyConnectionOwner {
			return fmt.Errorf("selector value %q is only allowed for %q", SelectorValueRepresentedUser, SelectorKeyConnectionOwner)
		}
		// Checked before resource keys are skipped: an empty auth client ID
		// could never authorize a check.
		if expectedKind == ResourceKindMCPAuthClient && v == "" {
			return fmt.Errorf("selector key %q must not be empty", k)
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

// CoversEveryResource reports whether the selector names every resource of
// its kind: a wildcard resource id and no narrowing dimension such as a
// project, tool or server URL. Unlike IsRestricted, a wildcard confined to one
// resource kind still counts, since that is how an unrestricted grant of a
// scope is stored.
func (s Selector) CoversEveryResource() bool {
	for selectorKey, value := range s {
		switch selectorKey {
		case SelectorKeyResourceKind:
		case SelectorKeyResourceID:
			if value != WildcardResource {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Key is a canonical text form of the selector: two selectors that name the
// same resource the same way have the same key, whatever order their
// dimensions were set in.
func (s Selector) Key() string {
	var b strings.Builder
	for i, key := range slices.Sorted(maps.Keys(s)) {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(s[key])
	}
	return b.String()
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
