package authz

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

func TestAgentCreationSharedPolicyCases(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/agent_creation_cases.json")
	require.NoError(t, err)
	var cases []struct {
		Name       string `json:"name"`
		Caller     string `json:"caller"`
		Owner      string `json:"owner"`
		GrantOwner string `json:"grantOwner"`
		ResourceID string `json:"resourceId"`
		Allowed    bool   `json:"allowed"`
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			selector := AgentOwnerSelector(ScopeAgentWrite, tc.GrantOwner)
			selector[SelectorKeyResourceID] = tc.ResourceID
			access := NewAgentAccess(tc.Caller, []Grant{NewGrantWithSelector(ScopeAgentWrite, selector)})
			require.Equal(t, tc.Allowed, access.CanCreate(tc.Owner))
		})
	}
}

func TestAgentAccessOwnerBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		caller  string
		owner   string
		grants  []Grant
		allowed bool
	}{
		{name: "concrete owner", caller: "caller", owner: "owner", grants: []Grant{NewGrantWithSelector(ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, "owner"))}, allowed: true},
		{name: "transfer", caller: "caller", owner: "new-owner", grants: []Grant{NewGrantWithSelector(ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, "owner"))}},
		{name: "self", caller: "caller", owner: "caller", grants: []Grant{NewGrantWithSelector(ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, AgentOwnerSelf))}, allowed: true},
		{name: "unresolved self", owner: "owner", grants: []Grant{NewGrantWithSelector(ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, AgentOwnerSelf))}},
		{name: "missing owner", caller: "caller", grants: []Grant{NewGrant(ScopeAgentWrite, WildcardResource)}},
		{name: "wildcard owner", caller: "caller", owner: WildcardResource, grants: []Grant{NewGrant(ScopeAgentWrite, WildcardResource)}},
		{name: "revoked grant", caller: "caller", owner: "owner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			access := NewAgentAccess(tc.caller, tc.grants)
			require.Equal(t, tc.allowed, access.Allows(ScopeAgentWrite, "agent", tc.owner))
			require.Equal(t, tc.allowed, access.CanCreate(tc.owner))
			engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
			ctx := enterpriseSessionCtx(t)
			if tc.allowed {
				require.NoError(t, access.Require(ctx, engine, ScopeAgentWrite, "agent", tc.owner))
				require.NoError(t, access.RequireCreate(ctx, engine, tc.owner))
			} else {
				require.Error(t, access.Require(ctx, engine, ScopeAgentWrite, "agent", tc.owner))
				require.Error(t, access.RequireCreate(ctx, engine, tc.owner))
			}
		})
	}
}

func TestAgentAccessConcreteAgentCannotCreate(t *testing.T) {
	t.Parallel()
	access := NewAgentAccess("caller", []Grant{NewGrant(ScopeAgentWrite, "existing-agent")})
	require.True(t, access.Allows(ScopeAgentRead, "existing-agent", "owner"))
	require.False(t, access.Allows(ScopeAgentAuthorize, "existing-agent", "owner"))
	require.False(t, access.Allows(ScopeAgentTransfer, "existing-agent", "owner"))
	require.False(t, access.CanCreate("owner"))
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	var denied *oops.ShareableError
	require.ErrorAs(t, access.RequireCreate(enterpriseSessionCtx(t), engine, "owner"), &denied)
	require.Equal(t, oops.CodeForbidden, denied.Code)
}

func TestAgentAccessSnapshotDoesNotMutateSource(t *testing.T) {
	t.Parallel()
	grant := NewGrantWithSelector(ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, AgentOwnerSelf))
	grants := []Grant{grant}
	access := NewAgentAccess("caller", grants)
	require.Equal(t, AgentOwnerSelf, grant.Selector[SelectorKeyOwner])
	grants[0].Scope = ScopeAgentRead
	grant.Selector[SelectorKeyOwner] = "other"
	require.True(t, access.Allows(ScopeAgentWrite, "agent", "caller"))
	require.False(t, access.Allows(ScopeAgentWrite, "agent", "other"))
}
