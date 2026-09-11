package runtimepolicy

import (
	"encoding/json"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestNewDelegatedPolicyV1CanonicalizesAndClosesImplications(t *testing.T) {
	t.Parallel()

	policy, err := NewDelegatedPolicyV1([]authz.Grant{
		authz.NewGrant(authz.ScopeProjectRead, "project-one"),
		{Scope: authz.ScopeMCPWrite, Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   "server-one",
			authz.SelectorKeyTool:         "tool-one",
		}},
	})
	require.NoError(t, err)

	require.Equal(t, []authz.Scope{authz.ScopeMCPWrite, authz.ScopeProjectRead}, delegatedPolicyScopes(policy.Requested))
	require.Equal(t, []authz.Scope{authz.ScopeMCPConnect, authz.ScopeMCPRead, authz.ScopeMCPWrite, authz.ScopeProjectRead}, delegatedPolicyScopes(policy.Effective))

	runtime := policy.RuntimeGrants()
	require.Len(t, runtime, 4)
	require.True(t, authz.GrantsSatisfy(runtime, authz.MCPToolCallCheck("server-one", authz.MCPToolCallDimensions{Tool: "tool-one"})))
	require.True(t, authz.GrantsSatisfy(runtime, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: "project-one"}))

	encoded, err := EncodeDelegatedPolicy(DelegatedPolicyVersion1, policy)
	require.NoError(t, err)
	decoded, err := DecodeDelegatedPolicy(DelegatedPolicyVersion1, encoded)
	require.NoError(t, err)
	require.Equal(t, policy.Requested, decoded.Requested)
	require.Equal(t, policy.Effective, decoded.Effective)
}

func TestDecodeDelegatedPolicyUsesEffectivePolicy(t *testing.T) {
	t.Parallel()

	selector := authz.NewSelector(authz.ScopeProjectRead, "project-one")
	raw := mustPolicyJSON(t, DelegatedPolicy{
		Requested: []DelegatedPolicyGrant{{Scope: authz.ScopeProjectRead, Selector: selector}},
		Effective: []DelegatedPolicyGrant{{Scope: authz.ScopeProjectRead, Selector: selector}},
	})
	policy, err := DecodeDelegatedPolicy(DelegatedPolicyVersion1, raw)
	require.NoError(t, err)

	policy.Requested[0].Selector[authz.SelectorKeyResourceID] = "mutated-request"
	runtime := policy.RuntimeGrants()
	require.True(t, authz.GrantsSatisfy(runtime, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: "project-one"}))
	require.False(t, authz.GrantsSatisfy(runtime, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: "mutated-request"}))

	runtime[0].Selector[authz.SelectorKeyResourceID] = "mutated-runtime"
	require.True(t, authz.GrantsSatisfy(policy.RuntimeGrants(), authz.Check{Scope: authz.ScopeProjectRead, ResourceID: "project-one"}))
}

func TestDecodeDelegatedPolicySkipsUnknownAndRetiredEntries(t *testing.T) {
	t.Parallel()

	entries := []DelegatedPolicyGrant{
		{Scope: scopeMCPApprovalReadTombstone, Selector: authz.Selector{authz.SelectorKeyResourceKind: "mcp_approval", authz.SelectorKeyResourceID: "approval-one"}},
		{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, "project-one")},
		{Scope: authz.Scope("unknown:scope"), Selector: authz.Selector{authz.SelectorKeyResourceKind: "unknown", authz.SelectorKeyResourceID: "unknown-one"}},
	}
	raw := mustPolicyJSON(t, DelegatedPolicy{Requested: entries, Effective: entries})

	policy, err := DecodeDelegatedPolicy(DelegatedPolicyVersion1, raw)
	require.NoError(t, err)
	require.Len(t, policy.Requested, 3)
	require.Equal(t, []authz.Scope{authz.ScopeProjectRead}, grantScopes(policy.RuntimeGrants()))
	require.True(t, authz.GrantsSatisfy(policy.RuntimeGrants(), authz.Check{Scope: authz.ScopeProjectRead, ResourceID: "project-one"}))
}

func TestDecodeDelegatedPolicyRejectsInvalidProfiles(t *testing.T) {
	t.Parallel()

	project := DelegatedPolicyGrant{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, "project-one")}
	mcp := DelegatedPolicyGrant{Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, "server-one")}

	tests := map[string]struct {
		version DelegatedPolicyVersion
		raw     []byte
	}{
		"unsupported version":         {version: 2, raw: mustPolicyJSON(t, DelegatedPolicy{Requested: []DelegatedPolicyGrant{}, Effective: []DelegatedPolicyGrant{}})},
		"unknown envelope field":      {version: 1, raw: []byte(`{"requested":[],"effective":[],"effect":"allow"}`)},
		"trailing value":              {version: 1, raw: []byte(`{"requested":[],"effective":[]} {}`)},
		"missing requested":           {version: 1, raw: []byte(`{"effective":[]}`)},
		"null effective":              {version: 1, raw: []byte(`{"requested":[],"effective":null}`)},
		"unknown grant field":         {version: 1, raw: []byte(`{"requested":[{"scope":"project:read","selector":{"resource_kind":"project","resource_id":"project-one"},"effect":"allow"}],"effective":[]}`)},
		"case variant envelope field": {version: 1, raw: []byte(`{"Requested":[],"effective":[]}`)},
		"case variant grant field":    {version: 1, raw: []byte(`{"requested":[{"Scope":"project:read","selector":{"resource_kind":"project","resource_id":"project-one"}}],"effective":[]}`)},
		"duplicate envelope field":    {version: 1, raw: []byte(`{"requested":[],"requested":[],"effective":[]}`)},
		"duplicate grant field":       {version: 1, raw: []byte(`{"requested":[{"scope":"project:read","scope":"mcp:read","selector":{"resource_kind":"project","resource_id":"project-one"}}],"effective":[]}`)},
		"duplicate selector field":    {version: 1, raw: []byte(`{"requested":[{"scope":"project:read","selector":{"resource_kind":"project","resource_id":"project-one","resource_id":"project-two"}}],"effective":[]}`)},
		"unordered requested":         {version: 1, raw: mustPolicyJSON(t, DelegatedPolicy{Requested: []DelegatedPolicyGrant{project, mcp}, Effective: []DelegatedPolicyGrant{mcp, project}})},
		"duplicate requested":         {version: 1, raw: mustPolicyJSON(t, DelegatedPolicy{Requested: []DelegatedPolicyGrant{project, project}, Effective: []DelegatedPolicyGrant{project}})},
		"duplicate effective":         {version: 1, raw: mustPolicyJSON(t, DelegatedPolicy{Requested: []DelegatedPolicyGrant{project}, Effective: []DelegatedPolicyGrant{project, project}})},
		"noncanonical closure":        {version: 1, raw: mustPolicyJSON(t, DelegatedPolicy{Requested: []DelegatedPolicyGrant{{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, "project-one")}}, Effective: []DelegatedPolicyGrant{{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, "project-one")}}})},
		"unsafe active scope":         {version: 1, raw: mustPolicyJSON(t, DelegatedPolicy{Requested: []DelegatedPolicyGrant{{Scope: authz.ScopeAgentWrite, Selector: authz.NewSelector(authz.ScopeAgentWrite, "agent-one")}}, Effective: []DelegatedPolicyGrant{{Scope: authz.ScopeAgentWrite, Selector: authz.NewSelector(authz.ScopeAgentWrite, "agent-one")}}})},
		"malformed known selector":    {version: 1, raw: mustPolicyJSON(t, DelegatedPolicy{Requested: []DelegatedPolicyGrant{{Scope: authz.ScopeProjectRead, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: "project-one"}}}, Effective: []DelegatedPolicyGrant{{Scope: authz.ScopeProjectRead, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: "project-one"}}}})},
		"malformed unknown selector":  {version: 1, raw: mustPolicyJSON(t, DelegatedPolicy{Requested: []DelegatedPolicyGrant{{Scope: authz.Scope("unknown:scope"), Selector: authz.Selector{authz.SelectorKeyResourceKind: "unknown"}}}, Effective: []DelegatedPolicyGrant{{Scope: authz.Scope("unknown:scope"), Selector: authz.Selector{authz.SelectorKeyResourceKind: "unknown"}}}})},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeDelegatedPolicy(test.version, test.raw)
			require.ErrorIs(t, err, ErrInvalidDelegatedPolicy)
		})
	}
}

func TestNewDelegatedPolicyRejectsInvalidIssuerInput(t *testing.T) {
	t.Parallel()

	_, err := NewDelegatedPolicyV1([]authz.Grant{authz.NewGrant(authz.ScopeAgentWrite, "agent-one")})
	require.ErrorIs(t, err, ErrInvalidDelegatedPolicy)

	grant := authz.NewGrant(authz.ScopeProjectRead, "project-one")
	_, err = NewDelegatedPolicyV1([]authz.Grant{grant, grant})
	require.ErrorIs(t, err, ErrInvalidDelegatedPolicy)
}

func delegatedPolicyScopes(grants []DelegatedPolicyGrant) []authz.Scope {
	scopes := make([]authz.Scope, len(grants))
	for i, grant := range grants {
		scopes[i] = grant.Scope
	}
	return scopes
}

func grantScopes(grants []authz.Grant) []authz.Scope {
	scopes := make([]authz.Scope, len(grants))
	for i, grant := range grants {
		scopes[i] = grant.Scope
	}
	return scopes
}

func mustPolicyJSON(t *testing.T, policy DelegatedPolicy) []byte {
	t.Helper()
	raw, err := json.Marshal(struct {
		Requested []DelegatedPolicyGrant `json:"requested"`
		Effective []DelegatedPolicyGrant `json:"effective"`
	}{Requested: policy.Requested, Effective: policy.Effective})
	require.NoError(t, err)
	return raw
}
