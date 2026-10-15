package agentmanagement

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListEligibleOwnersUsesOnlyLiveCreationGrants(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		target string
		want   []string
	}{
		{name: "self only", target: authz.AgentOwnerSelf, want: []string{"caller"}},
		{name: "specified owner", target: "employee", want: []string{"employee"}},
		{name: "everyone excludes inactive and foreign", target: "*", want: []string{"caller", "employee", "other", "self"}},
		{name: "foreign target is not disclosed", target: "foreign", want: []string{}},
		{name: "inactive target is not disclosed", target: "inactive", want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			seedOrganization(t, db, "org-owners")
			seedOrganization(t, db, "org-foreign")
			for _, id := range []string{"caller", "employee", "other", "self", "inactive", "deleted"} {
				seedOrganizationUser(t, db, "org-owners", id)
			}
			seedOrganizationUser(t, db, "org-foreign", "foreign")
			require.NoError(t, orgrepo.New(db).DeleteOrganizationUserRelationship(t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{
				OrganizationID: "org-owners", UserID: conv.ToPGText("inactive"),
			}))
			require.NoError(t, testrepo.New(db).ForceSoftDeleteUser(t.Context(), "deleted"))
			ctx := validatedHumanContext(t, "org-owners", "caller")
			principal := urn.NewPrincipal(urn.PrincipalTypeUser, "caller")
			seedGrant(t, ctx, db, "org-owners", principal, authz.ScopeAgentCreate, tc.target)
			service := newTestService(db, &fakeAuthorizationEngine{loadedGrantsOnly: true})
			// Prepared grants must not widen the live owner roster.
			ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeAgentCreate, "*")})
			owners, err := service.ListEligibleOwners(ctx, &gen.ListEligibleOwnersPayload{})
			require.NoError(t, err)
			ids := make([]string, 0, len(owners))
			for _, owner := range owners {
				ids = append(ids, owner.UserID)
				require.Equal(t, owner.UserID, owner.DisplayName)
			}
			require.ElementsMatch(t, tc.want, ids)
		})
	}
}

func TestListEligibleOwnersRejectsCallersWithoutLiveCreation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-owners")
	seedOrganizationUser(t, db, "org-owners", "caller")
	ctx := validatedHumanContext(t, "org-owners", "caller")
	seedGrant(t, ctx, db, "org-owners", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeAgentWrite, "*")
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeAgentCreate, "*")})
	owners, err := newTestService(db, &fakeAuthorizationEngine{loadedGrantsOnly: true}).ListEligibleOwners(ctx, &gen.ListEligibleOwnersPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Nil(t, owners)
}

func TestListEligibleOwnersRejectsNonOrdinaryAndInactiveCallers(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"anonymous", "unvalidated", "api key", "principal credential", "assistant", "oauth", "scope override", "support", "inactive membership"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			seedOrganization(t, db, "org-owners")
			seedAgentCreator(t, db, "org-owners", "caller")
			ctx := validatedHumanContext(t, "org-owners", "caller")
			code := oops.CodeForbidden
			switch name {
			case "anonymous":
				ctx = t.Context()
				code = oops.CodeUnauthorized
			case "unvalidated":
				ctx = contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
					ActiveOrganizationID: "org-owners", UserID: "caller", SessionID: new("untrusted-session"),
				})
				code = oops.CodeUnauthorized
			case "api key":
				authCtx := *mustAuthContext(t, ctx)
				authCtx.APIKeyID = "key-id"
				ctx = contextvalues.WithValidatedGramSession(contextvalues.SetAuthContext(t.Context(), &authCtx), &authCtx, false)
			case "assistant":
				ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
			case "principal credential":
				ctx = contextvalues.WithPrincipalCredentialAuthorization(ctx, mustAuthContext(t, ctx), urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()), contextvalues.PrincipalCredential{})
			case "oauth":
				ctx = contextvalues.SetOAuthClientID(ctx, "client-id")
			case "scope override":
				ctx = contextvalues.SetRBACScopeOverride(ctx, "root")
			case "support":
				authCtx := *mustAuthContext(t, ctx)
				authCtx.IsAdmin = true
				authCtx.SupportOrganizationID = "org-owners"
				ctx = contextvalues.WithValidatedSupportSession(ctx, &authCtx)
			case "inactive membership":
				require.NoError(t, orgrepo.New(db).DeleteOrganizationUserRelationship(t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{
					OrganizationID: "org-owners", UserID: conv.ToPGText("caller"),
				}))
			}
			owners, err := newTestService(db, &fakeAuthorizationEngine{loadedGrantsOnly: true}).ListEligibleOwners(ctx, &gen.ListEligibleOwnersPayload{})
			requireOopsCode(t, err, code)
			require.Nil(t, owners)
		})
	}
}
