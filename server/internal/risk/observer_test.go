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
		Name:    new("Enabled Policy"),
		Enabled: &enabled,
	})
	require.NoError(t, err)

	disabled := false
	_, err = ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Disabled Policy"),
		Enabled: &disabled,
	})
	require.NoError(t, err)

	// Reset signaler calls from create.
	ti.signaler.calls = nil

	ti.service.OnMessagesStored(ctx, *authCtx.ProjectID)

	// The project should have been signaled once.
	require.Len(t, ti.signaler.calls, 1)
	require.Equal(t, *authCtx.ProjectID, ti.signaler.calls[0])
}

func TestOnMessagesStored_NoPolicies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// Coordinator is always signaled; it discovers no policies during FetchUnanalyzed.
	projectID := uuid.New()
	ti.service.OnMessagesStored(ctx, projectID)

	require.Len(t, ti.signaler.calls, 1)
	require.Equal(t, projectID, ti.signaler.calls[0])
}
