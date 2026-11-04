package remotesessions_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestAuthorizationModesAgree runs the same project and organization
// decisions through the engine, a shadow comparison and the registry
// evaluator: every mode must allow and deny the same callers.
func TestAuthorizationModesAgree(t *testing.T) {
	t.Parallel()

	for _, mode := range []authz.MigrationMode{authz.MigrationModeOld, authz.MigrationModeShadow, authz.MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestServiceWithConfig(t, testServiceConfig{authzMode: &mode})
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID := authCtx.ProjectID.String()
			listProject := func(t *testing.T, grants ...authz.Grant) error {
				t.Helper()
				if _, err := ti.service.ListRemoteSessionClients(withExactAccessGrants(t, ctx, ti.conn, grants...), &clientsgen.ListRemoteSessionClientsPayload{}); err != nil {
					return fmt.Errorf("list project clients: %w", err)
				}
				return nil
			}
			listOrganization := func(t *testing.T, grants ...authz.Grant) error {
				t.Helper()
				if _, err := ti.service.ListClients(withExactAccessGrants(t, ctx, ti.conn, grants...), &orgclientsgen.ListClientsPayload{IssuerID: uuid.NewString()}); err != nil {
					return fmt.Errorf("list organization clients: %w", err)
				}
				return nil
			}

			require.NoError(t, listProject(t, authz.NewGrant(authz.ScopeProjectRead, projectID)))
			require.NoError(t, listProject(t, authz.NewGrant(authz.ScopeProjectWrite, projectID)), "write implies read")
			requireForbidden(t, listProject(t, authz.NewGrant(authz.ScopeProjectRead, "other-project")))
			requireForbidden(t, listProject(t,
				authz.NewGrant(authz.ScopeProjectRead, projectID),
				authz.NewGrant(authz.ScopeProjectBlockedRead, projectID),
			))
			requireForbidden(t, listProject(t, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)), "organization admin does not reach project read")

			require.NoError(t, listOrganization(t, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)))
			require.NoError(t, listOrganization(t, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)), "admin implies read")
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
