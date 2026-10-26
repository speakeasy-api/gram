package authz

import (
	"errors"
	"fmt"
	"slices"
)

// ErrInvalidAPIKeyGrant reports a grant a legacy API key may not hold.
var ErrInvalidAPIKeyGrant = errors.New("invalid api key grant")

// apiKeyPolicyScopes are the scopes a legacy API key's own policy may hold.
// They are the ones the mandatory authorizer evaluates; every other request a
// legacy key makes keeps its transport scopes until API keys move to RBAC.
var apiKeyPolicyScopes = []Scope{ScopeMCPConnect, ScopeMCPBlockedConnect, ScopeMCPAuth, ScopeMCPBlockedAuth}

// ValidateAPIKeyPolicyGrant rejects a grant a legacy API key may not hold. An
// mcp:auth allow must name its client, so a key is never pre-authorized for
// clients that do not exist yet.
func ValidateAPIKeyPolicyGrant(scope Scope, selector Selector) error {
	if !slices.Contains(apiKeyPolicyScopes, scope) {
		return fmt.Errorf("%w: scope %q is not available to API keys", ErrInvalidAPIKeyGrant, scope)
	}
	if err := ValidateSelector(scope, selector); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAPIKeyGrant, err)
	}
	if scope == ScopeMCPAuth && selector.ResourceID() == WildcardResource {
		return fmt.Errorf("%w: an API key's mcp:auth grant must name its client", ErrInvalidAPIKeyGrant)
	}
	for key := range selector {
		if key != SelectorKeyResourceKind && key != SelectorKeyResourceID && key != SelectorKeyProjectID && key != SelectorKeyClientType && key != SelectorKeyConnectionOwner {
			return fmt.Errorf("%w: selector key %q does not apply to API keys", ErrInvalidAPIKeyGrant, key)
		}
	}
	if _, owner := selector[SelectorKeyConnectionOwner]; owner || selector[SelectorKeyClientType] == AuthClientTypeRemoteSession {
		return fmt.Errorf("%w: an API key represents no user and cannot use personal connections", ErrInvalidAPIKeyGrant)
	}
	return nil
}
