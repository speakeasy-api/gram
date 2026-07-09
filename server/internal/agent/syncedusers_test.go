package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// withRBACGrants puts the caller on an enterprise account (so RBAC is enforced)
// and attaches the given grants, mirroring the access service's test helper.
func withRBACGrants(t *testing.T, ctx context.Context, grants ...authz.Grant) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	for i := range grants {
		if grants[i].Effect == "" {
			grants[i].Effect = authz.PolicyEffectAllow
		}
	}
	return authz.GrantsToContext(ctx, grants)
}

// withOrgAdmin grants the caller org-admin over their active org. Without it,
// ListSyncedUsers is denied on an enterprise account.
func withOrgAdmin(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return withRBACGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})
}

// TestGetPlugins_RecordsSync proves the plugin poll records a device-agent sync
// row keyed by the normalized email, and that a second poll inside the
// once-per-minute guard does not advance last_seen_at.
func TestGetPlugins_RecordsSync(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)

	rows, err := agentrepo.New(ti.conn).ListDeviceAgentSyncs(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, conv.NormalizeEmail(mockidp.MockUserEmail), rows[0].Email)
	firstSeen := rows[0].LastSeenAt.Time

	// A second poll within the guard window leaves last_seen_at untouched.
	_, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)

	rows, err = agentrepo.New(ti.conn).ListDeviceAgentSyncs(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "same email must not create a second row")
	require.Equal(t, firstSeen, rows[0].LastSeenAt.Time,
		"second poll inside the 1-minute guard must not advance last_seen_at")
}

// TestListSyncedUsers_AllowsOrgAdmin returns synced users most-recently-active
// first for an org admin.
func TestListSyncedUsers_AllowsOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	// Two distinct users poll; the second poll is later, so it sorts first.
	_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: "early@acme.corp"})
	require.NoError(t, err)
	_, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: "later@acme.corp"})
	require.NoError(t, err)

	adminCtx := withOrgAdmin(t, ctx)
	res, err := ti.service.ListSyncedUsers(adminCtx, &gen.ListSyncedUsersPayload{})
	require.NoError(t, err)
	require.Len(t, res.Users, 2)
	require.Equal(t, "later@acme.corp", res.Users[0].Email, "ordered by last_seen_at DESC")
	require.Equal(t, "early@acme.corp", res.Users[1].Email)
	require.NotEmpty(t, res.Users[0].LastSeenAt)
	require.NotEmpty(t, res.Users[0].FirstSeenAt)
}

// TestListSyncedUsers_ForbiddenWithoutOrgAdmin denies a non-admin caller.
func TestListSyncedUsers_ForbiddenWithoutOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = withRBACGrants(t, ctx) // enterprise account, no admin grant

	_, err := ti.service.ListSyncedUsers(ctx, &gen.ListSyncedUsersPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
