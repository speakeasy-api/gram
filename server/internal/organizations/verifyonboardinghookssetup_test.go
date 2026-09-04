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

func TestService_VerifyOnboardingHooksSetupAllowsConfirmTrafficAssignee(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey:  "confirm-traffic",
		Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID},
	})
	require.NoError(t, err)

	readOnlyCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	result, err := ti.service.VerifyOnboardingHooksSetup(readOnlyCtx, &gen.VerifyOnboardingHooksSetupPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Events)

	otherEmail := "other@example.test"
	_, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey:  "confirm-traffic",
		Assignee: &gen.SetupTaskAssigneeInput{Email: &otherEmail},
	})
	require.NoError(t, err)

	result, err = ti.service.VerifyOnboardingHooksSetup(readOnlyCtx, &gen.VerifyOnboardingHooksSetupPayload{})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeForbidden)
}
