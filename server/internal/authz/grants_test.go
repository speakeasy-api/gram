package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrantsAuthorizeRejectsEmptyResourceID(t *testing.T) {
	t.Parallel()

	allowed, err := GrantsAuthorize([]Grant{NewGrant(ScopeRoot, WildcardResource)}, Check{
		Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: "", Dimensions: nil,
	})
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.False(t, allowed)
}

func TestGrantsAuthorizeRejectsWildcardResourceID(t *testing.T) {
	t.Parallel()

	allowed, err := GrantsAuthorize([]Grant{NewGrant(ScopeRoot, WildcardResource)}, Check{
		Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: WildcardResource, Dimensions: nil,
	})
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.False(t, allowed)
}
