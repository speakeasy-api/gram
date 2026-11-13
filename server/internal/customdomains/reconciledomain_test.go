package customdomains_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestReconcileDomain_StartsReconciliationForVerifiedOrganizationDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "reconcile.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	_, err = ti.repo.UpdateCustomDomain(ctx, cdrepo.UpdateCustomDomainParams{
		Verified:        true,
		Activated:       true,
		IngressName:     pgtype.Text{String: "reconcile-example-com", Valid: true},
		CertSecretName:  pgtype.Text{String: "reconcile-example-com-tls", Valid: true},
		ProvisionerKind: domain.ProvisionerKind,
		ID:              domain.ID,
	})
	require.NoError(t, err)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})
	// The mutation acknowledges the signal without waiting for the operational
	// workflow result; polling reports its eventual success or failure.
	ti.temporal.reconcileErr = errors.New("reconciliation failed after starting")

	result, err := ti.service.ReconcileDomain(ctx, &gen.ReconcileDomainPayload{})
	require.NoError(t, err)
	require.Equal(t, domain.ID.String(), result.ID)
	require.Equal(t, "applying", result.ReconciliationStatus)
	require.True(t, result.IsReconciling)
	require.Equal(t, 1, ti.temporal.reconcileCalls)
	require.Equal(t, domain.ID, ti.temporal.lastReconcileID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionCustomDomainsReconcile)
	require.NoError(t, err)
	require.Equal(t, domain.ID.String(), record.SubjectID)
}

func TestReconcileDomain_RejectsUnverifiedDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "pending.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	_, err = ti.service.ReconcileDomain(ctx, &gen.ReconcileDomainPayload{})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
	require.Zero(t, ti.temporal.reconcileCalls)
}
