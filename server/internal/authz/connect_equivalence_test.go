package authz

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// mcp:read and mcp:write used to satisfy mcp:connect. They no longer do, and
// an explicit-connect backfill gives every principal holding mcp:read or
// mcp:write a matching mcp:connect grant (same principal and selector) before
// this ships. These tests pin that the two together change no decision: each
// case records what the engine decided while read/write implied connect, and
// the engine must decide the same on the backfilled grants.

// backfillConnect mirrors the explicit-connect backfill: every mcp:read or
// mcp:write grant without an mcp:connect grant for the same principal and
// selector gains one.
func backfillConnect(grants []Grant) []Grant {
	out := slices.Clone(grants)
	for _, grant := range grants {
		if grant.Scope != ScopeMCPRead && grant.Scope != ScopeMCPWrite {
			continue
		}
		connect := grant
		connect.Scope = ScopeMCPConnect
		if slices.ContainsFunc(out, func(g Grant) bool {
			return g.Scope == ScopeMCPConnect && g.PrincipalUrn == connect.PrincipalUrn && maps.Equal(g.Selector, connect.Selector)
		}) {
			continue
		}
		out = append(out, connect)
	}
	return out
}

type connectEquivalenceCase struct {
	name   string
	grants []Grant
	check  Check
	// legacy is the decision made while mcp:read and mcp:write implied
	// mcp:connect.
	legacy bool
}

func connectEquivalenceCases() []connectEquivalenceCase {
	server := MCPCheck(ScopeMCPConnect, "server-1", "project-1")
	toolCall := MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "tool-a", Disposition: "", ProjectID: "project-1"})
	projectRead := NewGrantWithSelector(ScopeMCPRead, Selector{
		SelectorKeyResourceKind: ResourceKindMCP,
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyProjectID:    "project-1",
	})
	otherProjectRead := NewGrantWithSelector(ScopeMCPRead, Selector{
		SelectorKeyResourceKind: ResourceKindMCP,
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyProjectID:    "project-2",
	})

	cases := []connectEquivalenceCase{
		{name: "role read on every server", grants: []Grant{heldBy(precedenceRole, NewGrant(ScopeMCPRead, WildcardResource))}, legacy: true},
		{name: "role write on every server", grants: []Grant{heldBy(precedenceRole, NewGrant(ScopeMCPWrite, WildcardResource))}, legacy: true},
		{name: "role read and connect on every server", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPRead, WildcardResource)),
			heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
		}, legacy: true},
		{name: "role write and connect on every server", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPWrite, WildcardResource)),
			heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
		}, legacy: true},
		{name: "user read on the server", grants: []Grant{heldBy(precedenceUser, NewGrant(ScopeMCPRead, "server-1"))}, legacy: true},
		{name: "everyone read on every server", grants: []Grant{heldBy(precedenceEveryone, NewGrant(ScopeMCPRead, WildcardResource))}, legacy: true},
		{name: "agent write on the server", grants: []Grant{heldBy(precedenceAgent, NewGrant(ScopeMCPWrite, "server-1"))}, legacy: true},
		{name: "role read on the server's project", grants: []Grant{heldBy(precedenceRole, projectRead)}, legacy: true},
		{name: "role read on another project", grants: []Grant{heldBy(precedenceRole, otherProjectRead)}, legacy: false},
		{name: "role read on another server", grants: []Grant{heldBy(precedenceRole, NewGrant(ScopeMCPRead, "server-2"))}, legacy: false},
		{name: "role read with role connect block", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPRead, WildcardResource)),
			heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
		}, legacy: false},
		{name: "user write with own connect block", grants: []Grant{
			heldBy(precedenceUser, NewGrant(ScopeMCPWrite, "server-1")),
			heldBy(precedenceUser, NewGrant(ScopeMCPBlockedConnect, "server-1")),
		}, legacy: false},
		{name: "role read with a read block does not block connect", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPRead, WildcardResource)),
			heldBy(precedenceRole, NewGrant(ScopeMCPBlockedRead, "server-1")),
		}, legacy: true},
		{name: "role write with a write block does not block connect", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPWrite, WildcardResource)),
			heldBy(precedenceRole, NewGrant(ScopeMCPBlockedWrite, "server-1")),
		}, legacy: true},
		{name: "direct read on the server outranks role connect block", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
			heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
			heldBy(precedenceUser, NewGrant(ScopeMCPRead, "server-1")),
		}, legacy: true},
		{name: "direct write on the server outranks everyone connect block", grants: []Grant{
			heldBy(precedenceEveryone, NewGrant(ScopeMCPBlockedConnect, "server-1")),
			heldBy(precedenceUser, NewGrant(ScopeMCPWrite, "server-1")),
		}, legacy: true},
		{name: "direct read and connect on the server outranks role connect block", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
			heldBy(precedenceUser, NewGrant(ScopeMCPRead, "server-1")),
			heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-1")),
		}, legacy: true},
		{name: "direct read on every server does not outrank role connect block", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
			heldBy(precedenceUser, NewGrant(ScopeMCPRead, WildcardResource)),
		}, legacy: false},
		{name: "direct read on another server does not outrank role connect block", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPRead, WildcardResource)),
			heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
			heldBy(precedenceUser, NewGrant(ScopeMCPRead, "server-2")),
		}, legacy: false},
		{name: "agent read on the server does not outrank role connect block", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
			heldBy(precedenceAgent, NewGrant(ScopeMCPRead, "server-1")),
		}, legacy: false},
		{name: "role read does not outrank everyone connect block", grants: []Grant{
			heldBy(precedenceEveryone, NewGrant(ScopeMCPBlockedConnect, "server-1")),
			heldBy(precedenceRole, NewGrant(ScopeMCPRead, "server-1")),
		}, legacy: false},
		{name: "direct read does not outrank own connect block", grants: []Grant{
			heldBy(precedenceUser, NewGrant(ScopeMCPRead, "server-1")),
			heldBy(precedenceUser, NewGrant(ScopeMCPBlockedConnect, "server-1")),
		}, legacy: false},
		{name: "tool connect block withdraws only that tool from a read holder", grants: []Grant{
			heldBy(precedenceRole, NewGrant(ScopeMCPRead, WildcardResource)),
			heldBy(precedenceRole, mcpToolGrant(ScopeMCPBlockedConnect, "server-1", "tool-a")),
		}, legacy: true},
	}

	// Each case is decided both at the server level and for a tool call on
	// it. Only the tool-scoped block distinguishes the two.
	out := make([]connectEquivalenceCase, 0, 2*len(cases))
	for _, tc := range cases {
		atServer := tc
		atServer.name = tc.name + " / server"
		atServer.check = server
		out = append(out, atServer)

		atTool := tc
		atTool.name = tc.name + " / tool call"
		atTool.check = toolCall
		if tc.name == "tool connect block withdraws only that tool from a read holder" {
			atTool.legacy = false
		}
		out = append(out, atTool)
	}
	return out
}

func TestConnectBackfillPreservesLegacyDecisions(t *testing.T) {
	t.Parallel()

	for _, tc := range connectEquivalenceCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			allowed, err := GrantsAuthorize(backfillConnect(tc.grants), tc.check)
			require.NoError(t, err)
			require.Equal(t, tc.legacy, allowed)
		})
	}
}

// Grant sets that already hold mcp:connect next to every mcp:read and
// mcp:write grant need no backfill: the change alone leaves them as they were.
func TestExplicitConnectHoldersAreUnchanged(t *testing.T) {
	t.Parallel()

	tested := 0
	for _, tc := range connectEquivalenceCases() {
		if len(backfillConnect(tc.grants)) != len(tc.grants) {
			continue
		}
		tested++
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			allowed, err := GrantsAuthorize(tc.grants, tc.check)
			require.NoError(t, err)
			require.Equal(t, tc.legacy, allowed)
		})
	}
	require.NotZero(t, tested)
}

func TestMCPReadAndWriteDoNotSatisfyConnect(t *testing.T) {
	t.Parallel()

	for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite} {
		for _, principal := range []string{precedenceUser, precedenceRole, precedenceEveryone, precedenceAgent} {
			grants := []Grant{heldBy(principal, NewGrant(scope, WildcardResource)), heldBy(principal, NewGrant(scope, "server-1"))}

			allowed, err := GrantsAuthorize(grants, MCPCheck(ScopeMCPConnect, "server-1", "project-1"))
			require.NoError(t, err)
			require.False(t, allowed, "%s held by %s must not connect", scope, principal)

			allowed, err = GrantsAuthorize(grants, MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "tool-a", Disposition: "", ProjectID: "project-1"}))
			require.NoError(t, err)
			require.False(t, allowed, "%s held by %s must not call tools", scope, principal)
		}
	}

	require.NotContains(t, ScopeImplicationClosure(ScopeMCPRead), ScopeMCPConnect)
	require.NotContains(t, ScopeImplicationClosure(ScopeMCPWrite), ScopeMCPConnect)
	require.NotContains(t, CalculateSubScopes(ScopeMCPRead), string(ScopeMCPConnect))
	require.NotContains(t, CalculateSubScopes(ScopeMCPWrite), string(ScopeMCPConnect))
}
