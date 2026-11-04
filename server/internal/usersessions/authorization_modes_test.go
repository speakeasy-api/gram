package usersessions_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestAuthorizationModesAgree runs the same project, MCP server and
// organization decisions through the engine, a shadow comparison and the
// registry evaluator: every mode must allow and deny the same callers.
func TestAuthorizationModesAgree(t *testing.T) {
	t.Parallel()

	for _, mode := range []authz.MigrationMode{authz.MigrationModeOld, authz.MigrationModeShadow, authz.MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestServiceInMode(t, mode)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID := authCtx.ProjectID.String()
			organizationID := authCtx.ActiveOrganizationID
			listProject := func(t *testing.T, grants ...authz.Grant) error {
				t.Helper()
				if _, err := ti.service.ListUserSessionIssuers(withExactAuthzGrants(t, ctx, ti.conn, grants...), &issuersgen.ListUserSessionIssuersPayload{}); err != nil {
					return fmt.Errorf("list project issuers: %w", err)
				}
				return nil
			}
			resourceID := createIssuerListToolset(t, ctx, ti, *authCtx.ProjectID, "modes-"+mode.String()).ID.String()
			listForServer := func(t *testing.T, grants ...authz.Grant) error {
				t.Helper()
				if _, err := ti.service.ListUserSessionIssuers(withExactAuthzGrants(t, ctx, ti.conn, grants...), &issuersgen.ListUserSessionIssuersPayload{McpResourceID: &resourceID}); err != nil {
					return fmt.Errorf("list issuers for server: %w", err)
				}
				return nil
			}
			listOrganization := func(t *testing.T, grants ...authz.Grant) error {
				t.Helper()
				if _, err := ti.service.ListIssuers(withExactAuthzGrants(t, ctx, ti.conn, grants...), &orggen.ListIssuersPayload{}); err != nil {
					return fmt.Errorf("list organization issuers: %w", err)
				}
				return nil
			}

			require.NoError(t, listProject(t, authz.NewGrant(authz.ScopeProjectRead, projectID)))
			require.NoError(t, listProject(t, authz.NewGrant(authz.ScopeMCPWrite, projectID)), "project-wide MCP write is the other alternative")
			require.NoError(t, listProject(t,
				authz.NewGrant(authz.ScopeProjectRead, projectID),
				authz.NewGrant(authz.ScopeProjectBlockedRead, projectID),
				authz.NewGrant(authz.ScopeMCPWrite, projectID),
			), "a block on one alternative does not stop the other")
			requireForbidden(t, listProject(t, authz.NewGrant(authz.ScopeProjectRead, "other-project")))
			requireForbidden(t, listProject(t, authz.NewGrant(authz.ScopeOrgRead, organizationID)))

			require.NoError(t, listForServer(t, authz.NewGrant(authz.ScopeMCPWrite, resourceID)), "editing the named server is enough")
			require.NoError(t, listForServer(t, authz.NewGrant(authz.ScopeProjectRead, projectID)))
			requireForbidden(t, listForServer(t, authz.NewGrant(authz.ScopeMCPRead, resourceID)), "reading the server is not editing it")
			requireForbidden(t, listForServer(t, authz.NewGrant(authz.ScopeMCPWrite, uuid.NewString())))

			require.NoError(t, listOrganization(t, authz.NewGrant(authz.ScopeOrgRead, organizationID)))
			require.NoError(t, listOrganization(t, authz.NewGrant(authz.ScopeOrgAdmin, organizationID)), "admin implies read")
			requireForbidden(t, listOrganization(t, authz.NewGrant(authz.ScopeProjectWrite, projectID)))
		})
	}
}

func requireForbidden(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr, msgAndArgs...)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code, msgAndArgs...)
}
