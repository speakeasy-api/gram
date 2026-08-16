package chat_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type creditUsageProvisioner struct {
	openrouter.Provisioner
	creditsUsed    float64
	creditLimit    int
	calls          int
	organizationID string
	keyType        openrouter.KeyType
}

func (p *creditUsageProvisioner) GetCreditsUsed(_ context.Context, organizationID string, keyType openrouter.KeyType) (float64, int, error) {
	p.calls++
	p.organizationID = organizationID
	p.keyType = keyType
	return p.creditsUsed, p.creditLimit, nil
}

func TestCreditUsageAllowsOrgRead(t *testing.T) {
	t.Parallel()

	provisioner := &creditUsageProvisioner{creditsUsed: 12.5, creditLimit: 50}
	ti := newTestChatServiceWithProvisioner(t, provisioner)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))

	result, err := ti.service.CreditUsage(ctx, &gen.CreditUsagePayload{SessionToken: nil})
	require.NoError(t, err)
	require.InDelta(t, 12.5, result.CreditsUsed, 0.001)
	require.Equal(t, 50, result.MonthlyCredits)
	require.Equal(t, 1, provisioner.calls)
	require.Equal(t, authCtx.ActiveOrganizationID, provisioner.organizationID)
	require.Equal(t, openrouter.KeyTypeChat, provisioner.keyType)
}

func TestCreditUsageDeniesWithoutOrgRead(t *testing.T) {
	t.Parallel()

	provisioner := &creditUsageProvisioner{creditsUsed: 12.5, creditLimit: 50}
	ti := newTestChatServiceWithProvisioner(t, provisioner)
	ctx := authztest.WithExactGrants(t, initSessionCtx(t, ti))

	result, err := ti.service.CreditUsage(ctx, &gen.CreditUsagePayload{SessionToken: nil})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Zero(t, provisioner.calls)
}
