package customdomains_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
)

func TestUpdateDomain_Activated_TriggersUpdateWorkflow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	created, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "update-active.example.com",
		IngressName:     pgTextValid("ingress-active"),
		CertSecretName:  pgTextValid("cert-active"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	// Mark the domain activated so the edit flow re-applies to k8s.
	_, err = ti.repo.UpdateCustomDomain(ctx, cdrepo.UpdateCustomDomainParams{
		Verified:        true,
		Activated:       true,
		IngressName:     pgTextValid("ingress-active"),
		CertSecretName:  pgTextValid("cert-active"),
		ProvisionerKind: "ingress",
		ID:              created.ID,
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	res, err := ti.service.UpdateDomain(ctx, &gen.UpdateDomainPayload{
		SessionToken: nil,
		IPAllowlist:  []string{"1.2.3.4", "10.0.0.0/8"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"1.2.3.4", "10.0.0.0/8"}, res.IPAllowlist)

	require.Equal(t, 1, ti.temporal.updateCalls, "activated domain must re-apply allowlist to k8s")

	row, err := ti.repo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, []string{"1.2.3.4", "10.0.0.0/8"}, row.IpAllowlist)
}

func TestUpdateDomain_NotActivated_NoWorkflow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "update-pending.example.com",
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	res, err := ti.service.UpdateDomain(ctx, &gen.UpdateDomainPayload{
		SessionToken: nil,
		IPAllowlist:  []string{"1.2.3.4"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"1.2.3.4"}, res.IPAllowlist)

	// Not yet provisioned — the persisted allowlist is picked up by the
	// registration workflow's Setup, so no separate update workflow runs.
	require.Equal(t, 0, ti.temporal.updateCalls)
}
