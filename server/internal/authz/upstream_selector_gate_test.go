package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpstreamSelectorWritesFailClosed(t *testing.T) {
	t.Parallel()

	named := Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: WildcardResource, SelectorKeyClientType: AuthClientTypeRemoteSession, SelectorKeyUpstreamClient: "upstream_u"}
	plain := Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: WildcardResource, SelectorKeyClientType: AuthClientTypeRemoteSession}
	allow := func(context.Context, string) (bool, error) { return true, nil }

	for name, tc := range map[string]struct {
		gate    UpstreamSelectorWriteGate
		org     string
		allowed bool
	}{
		"no provider":       {gate: nil, org: "org", allowed: false},
		"flag off":          {gate: func(context.Context, string) (bool, error) { return false, nil }, org: "org", allowed: false},
		"evaluation error":  {gate: func(context.Context, string) (bool, error) { return true, errors.New("flags unavailable") }, org: "org", allowed: false},
		"unknown org":       {gate: allow, org: "", allowed: false},
		"flag on":           {gate: allow, org: "org", allowed: true},
		"flag on other org": {gate: func(_ context.Context, org string) (bool, error) { return org == "other", nil }, org: "org", allowed: false},
	} {
		ctx := WithUpstreamSelectorWriteGate(t.Context(), tc.gate)
		require.Equal(t, tc.allowed, UpstreamSelectorWritesAllowed(ctx, tc.org), name)
		err := RequireUpstreamSelectorWrites(ctx, tc.org, plain, named)
		if tc.allowed {
			require.NoError(t, err, name)
		} else {
			require.ErrorIs(t, err, ErrUpstreamSelectorWritesDisabled, name)
			var shareable *oops.ShareableError
			require.ErrorAs(t, err, &shareable, name)
			require.Equal(t, oops.CodeForbidden, shareable.Code, name)
		}
		require.NoError(t, RequireUpstreamSelectorWrites(ctx, tc.org, plain), "%s: a selector without upstream_client never consults the gate", name)
	}
}

func TestUpstreamSelectorWritesCheckEveryGrantAndStoredSelector(t *testing.T) {
	t.Parallel()

	ctx := WithUpstreamSelectorWriteGate(t.Context(), nil)
	named := Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: WildcardResource, SelectorKeyUpstreamClient: "upstream_u"}
	grants := []*RoleGrant{nil, {Scope: string(ScopeMCPAuth), Selectors: []Selector{NewSelector(ScopeMCPAuth, "client")}}, {Scope: string(ScopeMCPBlockedAuth), Selectors: []Selector{named}}}
	require.ErrorIs(t, RequireUpstreamSelectorWritesForGrants(ctx, "org", grants), ErrUpstreamSelectorWritesDisabled)
	require.NoError(t, RequireUpstreamSelectorWritesForGrants(ctx, "org", grants[:2]))

	raw, err := named.MarshalJSON()
	require.NoError(t, err)
	require.ErrorIs(t, RequireUpstreamSelectorWritesRaw(ctx, "org", raw), ErrUpstreamSelectorWritesDisabled)
}
