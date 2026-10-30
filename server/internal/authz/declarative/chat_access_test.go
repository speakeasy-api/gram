package declarative

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authz/declarative/relschema"
)

// Synthetic fixture for the agent-history evidence gate. Agent G is owned by
// U1 until it is transferred to U2. M manages G. R reviews sessions.
const (
	ehU1 = "user_owner_1"
	ehU2 = "user_owner_2"
	ehM  = "user_manager"
	ehR  = "user_reviewer"
	ehG  = "agent_g"
)

func ehUser(id string) relschema.ObjectRef { return relschema.ObjectRef{Type: typeUser, ID: id} }

func ehOwnedBy(owner string) *AgentOwnerFact {
	return &AgentOwnerFact{AgentID: ehG, OrganizationID: fxOrg, OwnerUserID: owner, Eligible: true}
}

var (
	// An agent-only session: no member and no external identity.
	ehAgentOnly = ChatRef{ID: "chat_agent_only", ProjectID: fxP1, OrganizationID: fxOrg, AuthorUserID: "", ExternalUserID: "", AgentID: ehG}
	// U1's own session, which G was also stamped onto.
	ehHumanAndAgent = ChatRef{ID: "chat_human", ProjectID: fxP1, OrganizationID: fxOrg, AuthorUserID: ehU1, ExternalUserID: "", AgentID: ehG}
	// An external user's session, which G was also stamped onto.
	ehExternalAndAgent = ChatRef{ID: "chat_external", ProjectID: fxP1, OrganizationID: fxOrg, AuthorUserID: "", ExternalUserID: "someone@example.com", AgentID: ehG}
	// A session with no attribution at all.
	ehUnattributed = ChatRef{ID: "chat_none", ProjectID: fxP1, OrganizationID: fxOrg, AuthorUserID: "", ExternalUserID: "", AgentID: ""}
)

func ehDecide(t *testing.T, subject string, chat ChatRef, agent *AgentOwnerFact, policies ...[]authz.Grant) ChatDecision {
	t.Helper()
	if len(policies) == 0 {
		policies = [][]authz.Grant{nil}
	}
	decision, err := decideChatRead(authz.PolicySet{Enforced: true, OrganizationID: fxOrg, Policies: policies}, ehUser(subject), chat, agent)
	require.NoError(t, err)
	return decision
}

func ehRow(principal string, scope authz.Scope, kind string) authz.Grant {
	return authz.Grant{PrincipalUrn: principal, Scope: scope, Selector: authz.Selector{"resource_kind": kind, "resource_id": "*"}, Inferred: false}
}

func TestAgentHistoryAcceptance(t *testing.T) {
	t.Parallel()

	t.Run("1 the new owner gains access and the former owner loses it", func(t *testing.T) {
		t.Parallel()
		before, after := ehOwnedBy(ehU1), ehOwnedBy(ehU2)
		require.True(t, ehDecide(t, ehU1, ehAgentOnly, before).Allowed)
		require.False(t, ehDecide(t, ehU2, ehAgentOnly, before).Allowed)

		gained := ehDecide(t, ehU2, ehAgentOnly, after)
		require.True(t, gained.Allowed)
		require.Equal(t, []string{"user:" + ehU2 + " is agent:" + ehG + "#owner", "via chat:chat_agent_only#agent"}, gained.Policies[0].Path)
		require.False(t, ehDecide(t, ehU1, ehAgentOnly, after).Allowed, "the former owner loses ownership-based access")

		reviewer := []authz.Grant{ehRow("user:"+ehU1, authz.ScopeChatRead, authz.ResourceKindChat)}
		require.True(t, ehDecide(t, ehU1, ehAgentOnly, after, reviewer).Allowed, "unless separately authorized")
	})

	t.Run("2 agent management alone grants no transcript access", func(t *testing.T) {
		t.Parallel()
		management := []authz.Grant{
			ehRow("user:"+ehM, authz.ScopeAgentRead, authz.ResourceKindAgent),
			ehRow("user:"+ehM, authz.ScopeAgentWrite, authz.ResourceKindAgent),
			ehRow("user:"+ehM, authz.ScopeAgentAuthorize, authz.ResourceKindAgent),
			ehRow("user:"+ehM, authz.ScopeAgentTransfer, authz.ResourceKindAgent),
			ehRow("user:"+ehM, authz.ScopeOrgAdmin, authz.ResourceKindOrg),
		}
		for _, chat := range []ChatRef{ehAgentOnly, ehHumanAndAgent, ehUnattributed} {
			d := ehDecide(t, ehM, chat, ehOwnedBy(ehU1), management)
			require.False(t, d.Allowed, chat.ID)
			require.Equal(t, "no relationship grants chat.read", d.Policies[0].Reason)
		}
	})

	t.Run("3 human attribution keeps a session its human's", func(t *testing.T) {
		t.Parallel()
		owner := ehOwnedBy(ehU2)
		require.False(t, ehDecide(t, ehU2, ehHumanAndAgent, owner).Allowed, "owning the stamped agent does not open a member's session")
		require.False(t, ehDecide(t, ehU2, ehExternalAndAgent, owner).Allowed, "nor an external user's")
		require.True(t, ehDecide(t, ehU1, ehHumanAndAgent, owner).Allowed, "the author keeps access")
	})

	t.Run("4 missing ownership facts and cross-tenant references never allow", func(t *testing.T) {
		t.Parallel()
		missing := ehDecide(t, ehU1, ehAgentOnly, nil)
		require.False(t, missing.Allowed)
		require.Equal(t, relschema.Undetermined.String(), missing.Policies[0].Outcome, "an agent row that was not loaded is undetermined")

		otherAgent := &AgentOwnerFact{AgentID: "agent_other", OrganizationID: fxOrg, OwnerUserID: ehU1, Eligible: true}
		require.False(t, ehDecide(t, ehU1, ehAgentOnly, otherAgent).Allowed, "a fact about a different agent is not this agent's")

		foreign := &AgentOwnerFact{AgentID: ehG, OrganizationID: fxOtherOrg, OwnerUserID: ehU1, Eligible: true}
		require.False(t, ehDecide(t, ehU1, ehAgentOnly, foreign).Allowed, "an agent in another organization")

		latched := &AgentOwnerFact{AgentID: ehG, OrganizationID: fxOrg, OwnerUserID: ehU1, Eligible: false}
		require.False(t, ehDecide(t, ehU1, ehAgentOnly, latched).Allowed, "a latched or deleted agent's owner answers for nothing")

		foreignChat := ChatRef{ID: "chat_foreign", ProjectID: fxPB, OrganizationID: fxOtherOrg, AuthorUserID: ehU1, ExternalUserID: "", AgentID: ""}
		reviewer := []authz.Grant{ehRow("user:"+ehU1, authz.ScopeChatRead, authz.ResourceKindChat)}
		d := ehDecide(t, ehU1, foreignChat, nil, reviewer)
		require.False(t, d.Allowed)
		require.Equal(t, "object belongs to another organization", d.Policies[0].Reason)

		require.False(t, ehDecide(t, "", ehAgentOnly, ehOwnedBy(ehU1)).Allowed, "a caller who is not a person is nobody's owner")
	})

	t.Run("6 every admitted policy must allow", func(t *testing.T) {
		t.Parallel()
		reviewer := []authz.Grant{ehRow("user:"+ehR, authz.ScopeChatRead, authz.ResourceKindChat)}
		narrowed := []authz.Grant{ehRow("user:"+ehR, authz.ScopeMCPConnect, authz.ResourceKindMCP)}
		require.True(t, ehDecide(t, ehR, ehUnattributed, nil, reviewer).Allowed)
		require.False(t, ehDecide(t, ehR, ehUnattributed, nil, reviewer, narrowed).Allowed, "a second policy without chat:read caps the reviewer")

		// Ownership is a relationship of the person, not of a grant, so a
		// narrowed second policy does not cap it. Whether it should is a
		// product decision this test records rather than settles.
		require.True(t, ehDecide(t, ehU1, ehAgentOnly, ehOwnedBy(ehU1), nil, narrowed).Allowed)
	})

	t.Run("list plan follows the schema", func(t *testing.T) {
		t.Parallel()
		set := func(grants ...authz.Grant) authz.PolicySet {
			return authz.PolicySet{Enforced: true, OrganizationID: fxOrg, Policies: [][]authz.Grant{grants}}
		}
		plan := func(s authz.PolicySet) bool {
			d, err := decideChatRead(s, relschema.ObjectRef{}, ChatRef{ID: "any", ProjectID: fxP1, OrganizationID: fxOrg, AuthorUserID: "", ExternalUserID: "", AgentID: ""}, nil)
			require.NoError(t, err)
			return d.Allowed
		}
		require.False(t, plan(set()))
		require.False(t, plan(set(ehRow("user:"+ehM, authz.ScopeAgentWrite, authz.ResourceKindAgent))))
		require.True(t, plan(set(ehRow("user:"+ehR, authz.ScopeChatRead, authz.ResourceKindChat))))
		require.True(t, plan(set(ehRow("user:"+ehR, authz.ScopeChatWrite, authz.ResourceKindChat))))
	})
}

// A scope registered without a decision stops startup: it must be read by a
// grant or listed, with a reason, in outOfSchemaScopes.
func TestNewScopeNeedsADecision(t *testing.T) {
	t.Parallel()
	_, err := compileSchema(append(authz.RegisteredScopes(), authz.Scope("widget:read")))
	require.ErrorContains(t, err, `scope "widget:read" is neither mapped nor declared out of schema`)

	_, err = compileSchema(authz.RegisteredScopes())
	require.NoError(t, err)
}

// A malformed row in a mapped scope fails every check in its policy, including
// checks on resources the row does not name, with an error naming the row.
// Rows in out-of-schema scopes are never decoded.
func TestMalformedRowIsolation(t *testing.T) {
	t.Parallel()
	good := authz.Grant{PrincipalUrn: fxUser, Scope: authz.ScopeMCPConnect, Selector: authz.Selector{"resource_kind": "mcp", "resource_id": fxS1}, Inferred: false}
	malformedAllow := authz.Grant{PrincipalUrn: fxRole, Scope: authz.ScopeMCPConnect, Selector: authz.Selector{"resource_kind": "mcp", "resource_id": fxS2, "region": "eu"}, Inferred: false}
	malformedBlock := authz.Grant{PrincipalUrn: fxRole, Scope: authz.ScopeMCPBlockedConnect, Selector: authz.Selector{"resource_kind": "toolset", "resource_id": fxS2}, Inferred: false}
	outOfSchema := authz.Grant{PrincipalUrn: fxRole, Scope: authz.ScopeEnvironmentRead, Selector: authz.Selector{"nonsense": "x"}, Inferred: false}

	ok := authz.PolicySet{Enforced: true, OrganizationID: fxOrg, Policies: [][]authz.Grant{{good, outOfSchema}}}
	d, err := decideMCPServer(ok, MCPServerConnect, refS1)
	require.NoError(t, err, "an out-of-schema row is skipped, however malformed")
	require.True(t, d.Allowed)

	for name, bad := range map[string]authz.Grant{"allow": malformedAllow, "block": malformedBlock} {
		set := authz.PolicySet{Enforced: true, OrganizationID: fxOrg, Policies: [][]authz.Grant{{good, bad}}}
		_, err := decideMCPServer(set, MCPServerConnect, refS1)
		require.Error(t, err, "%s: a check on S1 fails although the row names S2", name)
		require.ErrorContains(t, err, bad.PrincipalUrn+" "+string(bad.Scope), name)

		_, err = decideChatRead(set, ehUser(ehU1), ehHumanAndAgent, ehOwnedBy(ehU1))
		require.Error(t, err, "%s: and so does a session read, which no MCP row could affect", name)
	}

	// The audit that recovery starts from: every row the schema cannot read.
	problems := AuditGrants(fxOrg, []authz.Grant{good, malformedAllow, malformedBlock, outOfSchema})
	require.Len(t, problems, 2)
	require.Contains(t, problems[0].Error(), "unsupported selector key \"region\"")
	require.Contains(t, problems[1].Error(), "resource_kind \"toolset\" does not match")
}
