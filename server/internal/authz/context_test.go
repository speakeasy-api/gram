package authz

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func rbacAlwaysEnabled(context.Context, string) (bool, error)             { return true, nil }
func challengeLoggingAlwaysEnabled(context.Context, string) (bool, error) { return true, nil }

func TestPrepareContext_loadsUserGrants(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedConnectedUser(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID, "test@example.com", "Test User", "user_workos_test", "membership_test")
	seedGrant(t, ctx, conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeProjectRead, WildcardResource)

	ctx, err = engine.PrepareContext(ctx)
	require.NoError(t, err)

	_, ok = GrantsFromContext(ctx)
	require.True(t, ok)
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_123"}))
}

func TestPrepareContext_skipsNonSessionAuth(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	authCtx.APIKeyID = "api-key-123"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	ctx, err = engine.PrepareContext(ctx)
	require.NoError(t, err)

	_, ok = GrantsFromContext(ctx)
	require.False(t, ok)
}

func TestPrepareContext_loadsAssistantPrincipalGrants(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedConnectedUser(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID, "owner@example.com", "Owner", "user_workos_owner", "membership_owner")
	seedGrant(t, ctx, conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeProjectRead, WildcardResource)

	ctx, err = engine.PrepareContext(ctx)
	require.NoError(t, err)

	_, ok = GrantsFromContext(ctx)
	require.True(t, ok)
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_assistant"}))
}

func TestShouldEnforce_assistantPrincipalOnEnterpriseOrgEnforces(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

func TestShouldEnforce_assistantPrincipalOnNonEnterpriseSkips(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	authCtx.AccountType = "pro"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce)
}

func TestPrepareContext_skipsNonEnterpriseOrgs(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "pro"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedGrant(t, ctx, conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeProjectRead, WildcardResource)

	ctx, err = engine.PrepareContext(ctx)
	require.NoError(t, err)

	_, ok = GrantsFromContext(ctx)
	require.False(t, ok)
}
