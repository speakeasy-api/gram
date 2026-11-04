package authz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func roleGrant(scope Scope, selector Selector) Grant {
	grant := NewGrantWithSelector(scope, selector)
	grant.PrincipalUrn = "role:organization:role_fixture"
	return grant
}

// TestClientLoadedPoliciesMatchTheEngine decides loaded grants in every
// migration mode and checks that each mode reaches the engine's answer, that
// every policy must allow, and that shadow mode records a match.
func TestClientLoadedPoliciesMatchTheEngine(t *testing.T) {
	t.Parallel()

	assistantRead := Project("org_1", "proj_1").Assistant("asst_1").Read()
	assistantWrite := Project("org_1", "proj_1").Assistant("asst_1").Write()
	projectRead := Project(fixtureOrgID, "proj_1").Read()

	projectWide := roleGrant(ScopeAssistantWrite, Selector{
		SelectorKeyResourceKind: ResourceKindAssistant, SelectorKeyResourceID: "*", SelectorKeyProjectID: "proj_1",
	})
	otherProject := roleGrant(ScopeAssistantRead, Selector{
		SelectorKeyResourceKind: ResourceKindAssistant, SelectorKeyResourceID: "*", SelectorKeyProjectID: "proj_2",
	})
	blocked := roleGrant(ScopeAssistantBlockedWrite, Selector{
		SelectorKeyResourceKind: ResourceKindAssistant, SelectorKeyResourceID: "asst_1",
	})
	projectReader := roleGrant(ScopeProjectRead, Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_1"})

	cases := []struct {
		name     string
		policies [][]Grant
		ops      []Operation
		allowed  bool
		noUser   bool
	}{
		{name: "project-wide write reads an assistant", policies: [][]Grant{{projectWide}}, ops: []Operation{assistantRead}, allowed: true},
		{name: "a grant in another project does not match", policies: [][]Grant{{otherProject}}, ops: []Operation{assistantRead}, allowed: false},
		{name: "a write block denies the write", policies: [][]Grant{{projectWide, blocked}}, ops: []Operation{assistantWrite}, allowed: false},
		{name: "a write block leaves the read", policies: [][]Grant{{projectWide, blocked}}, ops: []Operation{assistantRead}, allowed: true},
		{name: "every operation must be allowed", policies: [][]Grant{{projectReader}}, ops: []Operation{projectRead, assistantRead}, allowed: false},
		{name: "every policy must allow", policies: [][]Grant{{projectWide}, {projectReader}}, ops: []Operation{assistantRead}, allowed: false},
		{name: "both policies allow", policies: [][]Grant{{projectWide, projectReader}, {projectReader, projectWide}}, ops: []Operation{assistantRead, projectRead}, allowed: true},
		{name: "no policies deny", policies: nil, ops: []Operation{projectRead}, allowed: false},
		{name: "grant-only operations need no loaded user", policies: [][]Grant{{projectWide, projectReader}}, ops: []Operation{assistantRead, projectRead}, allowed: true, noUser: true},
	}

	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		for _, tc := range cases {
			t.Run(mode.String()+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				recorder := &capturedComparisons{}
				client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)
				loaded := LoadedPolicies{OrganizationID: fixtureOrgID, User: fixtureUserID, Policies: tc.policies}
				if tc.noUser {
					loaded.User = ""
				}
				ctx := context.Background()

				allowed, err := client.EvaluateLoaded(ctx, loaded, tc.ops...)
				require.NoError(t, err)
				require.Equal(t, tc.allowed, allowed)

				err = client.RequireLoaded(ctx, loaded, tc.ops...)
				if tc.allowed {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					require.NoError(t, ignoreForbidden(err))
				}

				if mode == MigrationModeShadow {
					require.Equal(t, []ShadowOutcome{ShadowMatch, ShadowMatch}, recorder.outcomes())
					for _, comparison := range recorder.all() {
						require.Equal(t, fixtureOrgID, comparison.OrganizationID, "an unadmitted loaded decision records its loaded organization")
					}
				} else {
					require.Empty(t, recorder.outcomes())
				}
			})
		}
	}
}

func TestClientLoadedPoliciesRejectsInvalidCalls(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t)
	loaded := LoadedPolicies{OrganizationID: fixtureOrgID, User: fixtureUserID, Policies: nil}
	ctx := context.Background()

	_, err := client.EvaluateLoaded(ctx, loaded)
	require.Error(t, err)
	require.Error(t, client.RequireLoaded(ctx, loaded, Operation{}))
	require.Error(t, client.RequireLoaded(ctx, loaded, Project(fixtureOrgID, "proj_1").PublishPlugin()))
}

// TestClientLoadedPoliciesRefuseCredentialTransports keeps the engine's
// refusal of principal credentials, including principal API keys, whose own
// policy a
// decision over someone else's loaded grants would bypass.
func TestClientLoadedPoliciesRefuseCredentialTransports(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t)
	grants := [][]Grant{{roleGrant(ScopeProjectRead, Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "*"})}}
	loaded := LoadedPolicies{OrganizationID: fixtureOrgID, User: fixtureUserID, Policies: grants}

	authCtx := &contextvalues.AuthContext{ActiveOrganizationID: fixtureOrgID, UserID: fixtureUserID}
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, "agent_fixture")
	ctx := contextvalues.WithPrincipalCredentialAuthorization(context.Background(), authCtx, actor, contextvalues.PrincipalCredential{})
	require.Error(t, client.RequireLoaded(ctx, loaded, Project(fixtureOrgID, "proj_1").Read()))
	_, err := client.EvaluateLoaded(ctx, loaded, Project(fixtureOrgID, "proj_1").Read())
	require.Error(t, err)
}
