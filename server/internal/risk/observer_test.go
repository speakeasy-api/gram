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

	// Reset signaler calls from create (drain workflow path).
	ti.signaler.calls = nil
	ti.analyzeSignaler.calls = nil

	msgIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	ti.service.OnMessagesStored(ctx, *authCtx.ProjectID, msgIDs)

	// One per-message signal per enabled policy, drain workflow untouched.
	require.Empty(t, ti.signaler.calls)
	require.Len(t, ti.analyzeSignaler.calls, len(msgIDs))
	for _, c := range ti.analyzeSignaler.calls {
		require.Equal(t, *authCtx.ProjectID, c.Params.ProjectID)
	}
}

func TestOnMessagesStored_NoPolicies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	ti.service.OnMessagesStored(ctx, uuid.New(), []uuid.UUID{uuid.New()})

	require.Empty(t, ti.signaler.calls)
	require.Empty(t, ti.analyzeSignaler.calls)
}
