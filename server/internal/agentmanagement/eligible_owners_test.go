package agentmanagement

import (
	"fmt"
	"slices"
	"strings"
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

func TestListEligibleOwnersValidatesDirectCalls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		payload gen.ListEligibleOwnersPayload
	}{
		{name: "negative limit", payload: gen.ListEligibleOwnersPayload{Limit: -1}},
		{name: "excessive limit", payload: gen.ListEligibleOwnersPayload{Limit: 101}},
		{name: "overlong cursor", payload: gen.ListEligibleOwnersPayload{Cursor: new(strings.Repeat("a", 1025))}},
		{name: "overlong search", payload: gen.ListEligibleOwnersPayload{Search: new(strings.Repeat("a", 201))}},
		{name: "overlong whitespace search", payload: gen.ListEligibleOwnersPayload{Search: new(strings.Repeat(" ", 201))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			seedOrganization(t, db, "org-validation")
			seedAgentCreator(t, db, "org-validation", "caller")
			ctx := validatedHumanContext(t, "org-validation", "caller")
			result, err := newTestService(db, &fakeAuthorizationEngine{loadedGrantsOnly: true}).ListEligibleOwners(ctx, &tc.payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
			require.Nil(t, result)
		})
	}
}

func TestEligibleOwnerTargetsMatchesCreationContainment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		grants []authz.Grant
		all    bool
		owners []string
	}{
		{name: "root wildcard", grants: []authz.Grant{authz.NewGrant(authz.ScopeRoot, "*")}, all: true, owners: []string{}},
		{name: "root concrete cannot create", grants: []authz.Grant{authz.NewGrant(authz.ScopeRoot, "employee")}, owners: []string{}},
		{name: "concrete and self union", grants: []authz.Grant{authz.NewGrantWithSelector(authz.ScopeAgentWrite, authz.AgentOwnerSelector(authz.ScopeAgentWrite, "employee")), authz.NewGrantWithSelector(authz.ScopeAgentWrite, authz.AgentOwnerSelector(authz.ScopeAgentWrite, authz.AgentOwnerSelf))}, owners: []string{"employee", "caller"}},
		{name: "self is not literal user ID", grants: []authz.Grant{authz.NewGrantWithSelector(authz.ScopeAgentWrite, authz.AgentOwnerSelector(authz.ScopeAgentWrite, authz.AgentOwnerSelf))}, owners: []string{"caller"}},
		{name: "unrestricted write implies create", grants: []authz.Grant{authz.NewGrant(authz.ScopeAgentWrite, "*")}, all: true, owners: []string{}},
		{name: "single agent cannot create", grants: []authz.Grant{authz.NewGrant(authz.ScopeAgentWrite, "existing-agent")}, owners: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			all, owners := eligibleOwnerTargets(tc.grants, "caller")
			require.Equal(t, tc.all, all)
			require.ElementsMatch(t, tc.owners, owners)
			for _, ownerID := range []string{"caller", "employee", "unrelated", "self"} {
				grants := authz.BindSelectorValue(tc.grants, authz.SelectorKeyOwner, authz.AgentOwnerSelf, "caller")
				allowed := authz.GrantsContainSelector(grants, authz.ScopeAgentWrite, authz.AgentOwnerSelector(authz.ScopeAgentWrite, ownerID))
				require.Equal(t, allowed, all || slices.Contains(owners, ownerID), ownerID)
			}
		})
	}
}

func TestListEligibleOwnersPaginatesAfterEligibility(t *testing.T) {
	t.Parallel()
	for _, target := range []string{authz.AgentOwnerSelf, "z-owner", "*"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			seedOrganization(t, db, "org-pages")
			seedOrganizationUser(t, db, "org-pages", "z-caller")
			seedOrganizationUser(t, db, "org-pages", "z-owner")
			// Unrelated active users sort before both permitted concrete owners.
			for i := range 125 {
				seedOrganizationUser(t, db, "org-pages", fmt.Sprintf("unrelated-%03d", i))
			}
			ctx := validatedHumanContext(t, "org-pages", "z-caller")
			seedGrant(t, ctx, db, "org-pages", urn.NewPrincipal(urn.PrincipalTypeUser, "z-caller"), authz.ScopeAgentWrite, "*", target)
			service := newTestService(db, &fakeAuthorizationEngine{loadedGrantsOnly: true})
			var cursor *string
			ids := make([]string, 0)
			pages := 0
			for {
				page, err := service.ListEligibleOwners(ctx, &gen.ListEligibleOwnersPayload{Cursor: cursor, Limit: 50})
				require.NoError(t, err)
				require.LessOrEqual(t, len(page.Owners), 50)
				for _, owner := range page.Owners {
					if len(ids) > 0 {
						require.Greater(t, owner.UserID, ids[len(ids)-1])
					}
					ids = append(ids, owner.UserID)
				}
				pages++
				if page.NextCursor == nil {
					break
				}
				// A cursor never exposes the lookahead row or an unauthorized ID.
				require.NotEmpty(t, page.Owners)
				require.Equal(t, page.Owners[len(page.Owners)-1].UserID, *page.NextCursor)
				cursor = page.NextCursor
				require.Less(t, pages, 4)
			}
			switch target {
			case authz.AgentOwnerSelf:
				require.Equal(t, []string{"z-caller"}, ids)
				require.Equal(t, 1, pages)
			case "z-owner":
				require.Equal(t, []string{"z-owner"}, ids)
				require.Equal(t, 1, pages)
			default:
				require.Len(t, ids, 127)
				require.Equal(t, 3, pages)
			}
		})
	}
}

func TestListEligibleOwnersSearchAndPageBoundary(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-search")
	for _, id := range []string{"caller", "match-a", "match-b", "match-c", "unrelated"} {
		seedOrganizationUser(t, db, "org-search", id)
	}
	ctx := validatedHumanContext(t, "org-search", "caller")
	seedGrant(t, ctx, db, "org-search", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeAgentWrite, "*")
	service := newTestService(db, &fakeAuthorizationEngine{loadedGrantsOnly: true})
	page, err := service.ListEligibleOwners(ctx, &gen.ListEligibleOwnersPayload{Search: new("MATCH"), Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Owners, 2)
	require.Equal(t, "match-b", *page.NextCursor)
	next, err := service.ListEligibleOwners(ctx, &gen.ListEligibleOwnersPayload{Search: new("MATCH"), Limit: 2, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Owners, 1)
	require.Equal(t, "match-c", next.Owners[0].UserID)
	require.Nil(t, next.NextCursor)
	exact, err := service.ListEligibleOwners(ctx, &gen.ListEligibleOwnersPayload{Search: new("MATCH"), Limit: 3})
	require.NoError(t, err)
	require.Len(t, exact.Owners, 3)
	require.Nil(t, exact.NextCursor)
	literal, err := service.ListEligibleOwners(ctx, &gen.ListEligibleOwnersPayload{Search: new("%")})
	require.NoError(t, err)
	require.Empty(t, literal.Owners)
	require.Nil(t, literal.NextCursor)
}

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
			seedGrant(t, ctx, db, "org-owners", principal, authz.ScopeAgentWrite, "*", tc.target)
			service := newTestService(db, &fakeAuthorizationEngine{loadedGrantsOnly: true})
			// Prepared grants must not widen the live owner roster.
			ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeAgentWrite, "*")})
			owners, err := service.ListEligibleOwners(ctx, &gen.ListEligibleOwnersPayload{})
			require.NoError(t, err)
			ids := make([]string, 0, len(owners.Owners))
			for _, owner := range owners.Owners {
				ids = append(ids, owner.UserID)
				require.Equal(t, owner.UserID, owner.DisplayName)
			}
			require.ElementsMatch(t, tc.want, ids)
			require.Nil(t, owners.NextCursor)
		})
	}
}

func TestListEligibleOwnersRejectsCallersWithoutLiveCreation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-owners")
	seedOrganizationUser(t, db, "org-owners", "caller")
	ctx := validatedHumanContext(t, "org-owners", "caller")
	seedGrant(t, ctx, db, "org-owners", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeAgentRead, "*")
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeAgentWrite, "*")})
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
