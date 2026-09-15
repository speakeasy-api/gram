package identityproviders_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestVerifySetupStepReturnsUnavailableWithoutAuditOrWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	createConnection(t, ctx, ti, "https://acme.okta.com")
	before, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	beforeAudits, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{
		StepKey:      "connect",
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeUnavailable)

	after, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	afterAudits, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeAudits, afterAudits)
}

func TestVerifySetupStepRequiresOrganizationAdminBeforeUnavailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{
		StepKey:      "connect",
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
