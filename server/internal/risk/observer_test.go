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
	ti.analyzeNewMsgSignaler.calls = nil

	msgA := uuid.New()
	msgB := uuid.New()
	ti.service.OnMessagesStored(ctx, *authCtx.ProjectID, []uuid.UUID{msgA, msgB})

	// Drain workflow is not signaled from the observer hot path.
	require.Empty(t, ti.signaler.calls)

	// One SignalNewMessage per (enabled policy, message ID). Disabled policy
	// is filtered out before fan-out.
	require.Len(t, ti.analyzeNewMsgSignaler.calls, 2)
	seen := map[uuid.UUID]int{}
	for _, c := range ti.analyzeNewMsgSignaler.calls {
		require.Equal(t, *authCtx.ProjectID, c.Params.ProjectID)
		seen[c.MessageID]++
	}
	require.Equal(t, 1, seen[msgA])
	require.Equal(t, 1, seen[msgB])
}

func TestOnMessagesStored_NoPolicies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// No policies created — should not signal anything.
	ti.service.OnMessagesStored(ctx, uuid.New(), []uuid.UUID{uuid.New()})

	require.Empty(t, ti.signaler.calls)
	require.Empty(t, ti.analyzeNewMsgSignaler.calls)
}

func TestOnMessagesStored_NoMessages(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// Empty messageIDs slice short-circuits before policy lookup.
	ti.service.OnMessagesStored(ctx, uuid.New(), nil)

	require.Empty(t, ti.signaler.calls)
	require.Empty(t, ti.analyzeNewMsgSignaler.calls)
}
