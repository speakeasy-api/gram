package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestOnMessagesStored_SignalsEnabledPolicies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	enabled := true
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    strPtr("Enabled Policy"),
		Enabled: &enabled,
	})
	require.NoError(t, err)

	disabled := false
	_, err = ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    strPtr("Disabled Policy"),
		Enabled: &disabled,
	})
	require.NoError(t, err)

	// Reset signaler calls from create.
	ti.signaler.calls = nil

	ti.service.OnMessagesStored(ctx, *authCtx.ProjectID)

	// Only the enabled policy should have been signaled.
	require.Len(t, ti.signaler.calls, 1)
	require.Equal(t, *authCtx.ProjectID, ti.signaler.calls[0].ProjectID)
}

func TestOnMessagesStored_NoPolicies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// No policies created — should not signal anything.
	ti.service.OnMessagesStored(ctx, uuid.New())

	require.Empty(t, ti.signaler.calls)
}
