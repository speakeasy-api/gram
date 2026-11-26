package platformmcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestPluginNamesVisibleOnlyToOrganizationAdmins(t *testing.T) {
	t.Parallel()

	principal := Principal{UserID: "user_1", OrganizationID: "org_1"}
	reader := &PostgresReader{authz: authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())}

	admin := pluginNamesCallerContext(t, principal, authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID))
	require.True(t, reader.pluginNamesVisible(admin, principal))

	member := pluginNamesCallerContext(t, principal, authz.NewGrant(authz.ScopeOrgRead, principal.OrganizationID))
	require.False(t, reader.pluginNamesVisible(member, principal))
}

func pluginNamesCallerContext(t *testing.T, principal Principal, grants ...authz.Grant) context.Context {
	t.Helper()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID})
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	return authz.GrantsToContext(ctx, grants)
}
