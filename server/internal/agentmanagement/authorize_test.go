package agentmanagement

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type fakeAuthorizationEngine struct {
	allowed map[string]bool
	checks  []authz.Check
}

func (f *fakeAuthorizationEngine) Require(_ context.Context, checks ...authz.Check) error {
	f.checks = append(f.checks, checks...)
	for _, check := range checks {
		if !f.allowed[checkKey(check)] {
			return oops.C(oops.CodeForbidden)
		}
	}
	return nil
}

func (f *fakeAuthorizationEngine) Evaluate(_ context.Context, checks ...authz.Check) (bool, error) {
	for _, check := range checks {
		if !f.allowed[checkKey(check)] {
			return false, nil
		}
	}
	return true, nil
}

func checkKey(check authz.Check) string { return string(check.Scope) + ":" + check.ResourceID }

func allow(engine *fakeAuthorizationEngine, scope authz.Scope, agentID uuid.UUID) {
	engine.allowed[checkKey(authz.Check{Scope: scope, ResourceID: agentID.String()})] = true
}

func validatedHumanContext(t *testing.T, organizationID, userID string) context.Context {
	t.Helper()
	sessionID := "session-" + userID
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: organizationID,
		UserID:               userID,
		SessionID:            &sessionID,
	})
	return contextvalues.WithValidatedGramSession(ctx, mustAuthContext(t, ctx), false)
}

func mustAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	return authCtx
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

func TestOwnerPredicatesAreIntrinsicAndIndependentScopesAreExact(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	seedOrganizationUser(t, conn, "org-a", "other")
	agent := createAgent(t, conn, "org-a", "owner", "Managed agent")
	engine := &fakeAuthorizationEngine{allowed: map[string]bool{}}
	authorizer := NewAuthorizer(engine)

	ownerCtx := validatedHumanContext(t, "org-a", "owner")
	for _, predicate := range []OwnerPredicate{OwnedAgentRead, OwnedAgentSetup, OwnedAgentAuthorize, OwnedAgentTransfer} {
		_, got, err := authorizer.RequireAgent(ownerCtx, conn, agent.ID, predicate)
		require.NoError(t, err)
		require.Equal(t, agent.ID, got.ID)
	}
	require.Empty(t, engine.checks, "owner predicates must not materialize or evaluate scope grants")

	nonownerCtx := validatedHumanContext(t, "org-a", "other")
	allow(engine, authz.ScopeAgentAuthorize, agent.ID)
	_, _, err := authorizer.RequireAgent(nonownerCtx, conn, agent.ID, OwnedAgentAuthorize)
	require.NoError(t, err)
	for _, predicate := range []OwnerPredicate{OwnedAgentRead, OwnedAgentSetup, OwnedAgentTransfer} {
		_, _, err := authorizer.RequireAgent(nonownerCtx, conn, agent.ID, predicate)
		requireOopsCode(t, err, oops.CodeForbidden)
	}
}

func TestFormerOwnerLosesIntrinsicPredicates(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "former-owner")
	seedOrganizationUser(t, conn, "org-a", "current-owner")
	agent := createAgent(t, conn, "org-a", "former-owner", "Transferred agent")
	_, err := conn.Exec(t.Context(), `UPDATE agents SET owner_user_id = 'current-owner' WHERE id = $1`, agent.ID) //nolint:glint // notestingrawsql: simulates AIM-184 ownership transfer without introducing that excluded API
	require.NoError(t, err)

	authorizer := NewAuthorizer(&fakeAuthorizationEngine{allowed: map[string]bool{}})
	_, _, err = authorizer.RequireAgent(validatedHumanContext(t, "org-a", "former-owner"), conn, agent.ID, OwnedAgentRead)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestOwnerPredicateRequiresUnblockedCurrentOwnership(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	agent := createAgent(t, conn, "org-a", "owner", "Latched agent")
	_, err := conn.Exec(t.Context(), `UPDATE agents SET owner_reassignment_required_at = clock_timestamp(), owner_reassignment_reason = 'owner_inactive' WHERE id = $1`, agent.ID) //nolint:glint // notestingrawsql: simulates the AIM-184 owner-loss latch without introducing that excluded API
	require.NoError(t, err)

	authorizer := NewAuthorizer(&fakeAuthorizationEngine{allowed: map[string]bool{}})
	_, _, err = authorizer.RequireAgent(validatedHumanContext(t, "org-a", "owner"), conn, agent.ID, OwnedAgentRead)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestRequireHumanRejectsAlternateAndUntrustedCallers(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "human")
	authorizer := NewAuthorizer(&fakeAuthorizationEngine{allowed: map[string]bool{}})

	valid := validatedHumanContext(t, "org-a", "human")
	_, err := authorizer.RequireHuman(valid, conn)
	require.NoError(t, err)

	tests := map[string]context.Context{
		"anonymous": t.Context(),
		"unvalidated attribution": contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
			ActiveOrganizationID: "org-a", UserID: "human", SessionID: func() *string { value := "untrusted"; return &value }(),
		}),
		"api key": func() context.Context {
			ctx := validatedHumanContext(t, "org-a", "human")
			authCtx := *mustAuthContext(t, ctx)
			authCtx.APIKeyID = "key-id"
			return contextvalues.WithValidatedGramSession(contextvalues.SetAuthContext(t.Context(), &authCtx), &authCtx, false)
		}(),
		"assistant":            contextvalues.SetAssistantPrincipal(valid, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()}),
		"oauth":                contextvalues.SetOAuthClientID(valid, "client-id"),
		"scope override":       contextvalues.SetRBACScopeOverride(valid, "root"),
		"legacy impersonation": contextvalues.WithValidatedGramSession(t.Context(), mustAuthContext(t, valid), true),
	}

	for name, ctx := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := authorizer.RequireHuman(ctx, conn)
			require.Error(t, err)
		})
	}

	supportAuth := *mustAuthContext(t, valid)
	supportAuth.IsAdmin = true
	supportAuth.SupportOrganizationID = "org-a"
	support := contextvalues.WithValidatedGramSession(contextvalues.SetAuthContext(t.Context(), &supportAuth), &supportAuth, false)
	support = contextvalues.WithValidatedSupportSession(support, mustAuthContext(t, support))
	_, err = authorizer.RequireHuman(support, conn)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSelectedAgentDenialsDoNotDiscloseExistenceOrTenant(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganization(t, conn, "org-b")
	seedOrganizationUser(t, conn, "org-a", "caller")
	seedOrganizationUser(t, conn, "org-b", "owner-b")
	crossTenant := createAgent(t, conn, "org-b", "owner-b", "Other tenant agent")
	authorizer := NewAuthorizer(&fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	for _, id := range []uuid.UUID{crossTenant.ID, uuid.New()} {
		_, _, err := authorizer.RequireAgent(ctx, conn, id, OwnedAgentRead)
		requireOopsCode(t, err, oops.CodeForbidden)
	}
}

func TestCreateForAnotherOwnerUsesProspectiveAgentSelector(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	seedOrganizationUser(t, conn, "org-a", "owner")
	engine := &fakeAuthorizationEngine{allowed: map[string]bool{}}
	authorizer := NewAuthorizer(engine)
	ctx := validatedHumanContext(t, "org-a", "caller")
	prospectiveID := uuid.New()

	_, err := authorizer.RequireCreate(ctx, conn, prospectiveID, "owner")
	requireOopsCode(t, err, oops.CodeForbidden)
	allow(engine, authz.ScopeAgentWrite, prospectiveID)
	_, err = authorizer.RequireCreate(ctx, conn, prospectiveID, "owner")
	require.NoError(t, err)
	require.Equal(t, authz.ScopeAgentWrite, engine.checks[len(engine.checks)-1].Scope)
	require.Equal(t, prospectiveID.String(), engine.checks[len(engine.checks)-1].ResourceID)

	_, err = authorizer.RequireCreate(ctx, conn, uuid.New(), "caller")
	require.NoError(t, err, "self-owned creation is intrinsic")
	_, err = authorizer.RequireCreate(ctx, conn, uuid.New(), "missing-owner")
	requireOopsCode(t, err, oops.CodeForbidden)
}
