package organizations_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestService_VerifyOnboardingHooksSetupRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	readOnlyCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	result, err := ti.service.VerifyOnboardingHooksSetup(readOnlyCtx, &gen.VerifyOnboardingHooksSetupPayload{})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeForbidden)

	adminCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))
	result, err = ti.service.VerifyOnboardingHooksSetup(adminCtx, &gen.VerifyOnboardingHooksSetupPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Events)
}
