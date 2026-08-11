package customdomains_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type deleteTestProvisionerFactory struct {
	provisioner k8s.CustomDomainProvisioner
}

func (f *deleteTestProvisionerFactory) Provisioner(_ k8s.ProvisionerKind) k8s.CustomDomainProvisioner {
	return f.provisioner
}

func TestDeleteDomain_NoCustomDomain_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	err := ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{SessionToken: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestDeleteDomain_ZeroEndpoints_NoCascadeAudits(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "delete-zero.example.com",
		IngressName:     pgTextValid("ingress-zero"),
		CertSecretName:  pgTextValid("cert-zero"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	beforeEndpointDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	beforeDomainDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{SessionToken: nil}))

	afterEndpointDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	afterDomainDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsDelete)
	require.NoError(t, err)

	require.Equal(t, beforeEndpointDeletes, afterEndpointDeletes, "no mcp-endpoint delete events expected when domain has no endpoints")
	require.Equal(t, beforeDomainDeletes+1, afterDomainDeletes)
	require.Equal(t, 1, ti.temporal.reconcileCalls)
	require.Equal(t, domain.ID, ti.temporal.lastReconcileID)
	require.Zero(t, ti.temporal.deletionCalls)
}

func TestDeleteDomain_CascadesSoftDeleteToMcpEndpointsAcrossProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	domainRow, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "cascade.example.com",
		IngressName:     pgTextValid("ingress-cascade"),
		CertSecretName:  pgTextValid("cert-cascade"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	// Two endpoints in the caller's project + two endpoints in a separate
	// project under the same org. The cascade must sweep all four.
	seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, domainRow.ID, "primary-a")
	seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, domainRow.ID, "primary-b")

	otherProjectID := seedProject(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	seedMcpEndpoint(t, ctx, ti.conn, otherProjectID, domainRow.ID, "other-a")
	seedMcpEndpoint(t, ctx, ti.conn, otherProjectID, domainRow.ID, "other-b")

	// Decoy endpoint not registered under this domain — must NOT be touched.
	decoyMcpServer := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	decoyEndpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    decoyMcpServer,
		Slug:           authCtx.OrganizationSlug + "-decoy",
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	beforeEndpointDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	beforeDomainDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{SessionToken: nil}))

	endpointsRepo := mcpendpointsrepo.New(ti.conn)

	// Decoy endpoint stays in the active set; the org-scoped read query treats
	// soft-deleted rows as not found, so a successful lookup is enough.
	_, err = endpointsRepo.GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{
		ID:        decoyEndpoint.ID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err, "decoy endpoint must remain active")

	afterEndpointDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	afterDomainDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsDelete)
	require.NoError(t, err)

	require.Equal(t, beforeEndpointDeletes+4, afterEndpointDeletes, "one mcp-endpoint:delete per cascaded row")
	require.Equal(t, beforeDomainDeletes+1, afterDomainDeletes)
	require.Equal(t, 1, ti.temporal.reconcileCalls)
	require.Equal(t, domainRow.ID, ti.temporal.lastReconcileID)
	require.Zero(t, ti.temporal.deletionCalls)

	// The active set must no longer surface any endpoint that pointed at the
	// deleted domain in either project; the cascade hides them by setting
	// deleted_at, which all active-set queries filter on.
	for _, projectID := range []uuid.UUID{*authCtx.ProjectID, otherProjectID} {
		active, err := endpointsRepo.ListMCPEndpointsByProject(ctx, projectID)
		require.NoError(t, err)
		for _, endpoint := range active {
			require.False(t, endpoint.CustomDomainID.Valid && endpoint.CustomDomainID.UUID == domainRow.ID,
				"endpoint %s in project %s still references the deleted domain", endpoint.ID, projectID)
		}
	}
}

func TestDeleteDomain_AwaitsReconcileAfterCommit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "delete-await.example.com",
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	ti.temporal.reconcileErr = errors.New("reconcile failed")
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	err = ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{SessionToken: nil})
	require.Error(t, err)
	require.Equal(t, 1, ti.temporal.reconcileCalls)
	require.Equal(t, domain.ID, ti.temporal.lastReconcileID)
	require.Zero(t, ti.temporal.deletionCalls)

	_, err = ti.repo.GetCustomDomainByID(ctx, domain.ID)
	require.Error(t, err, "soft delete must commit before reconciliation starts")
}

func TestDeleteDomain_CheckpointsDerivedNamesWhenApplyNeverPersisted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "delete-checkpoint.example.com",
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	provisioner := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	reconciler := activities.NewCustomDomainIngress(
		logger,
		ti.conn,
		&deleteTestProvisionerFactory{provisioner: provisioner},
	)
	ti.temporal.reconcile = func(ctx context.Context, customDomainID uuid.UUID) error {
		return reconciler.ReconcileCustomDomain(ctx, activities.ReconcileCustomDomainArgs{CustomDomainID: customDomainID})
	}
	ti.temporal.reconcileStartErr = errors.New("signal failed")
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	// Apply never persisted names; the tombstone must checkpoint derived ones
	// so the pending-cleanup retry path can rediscover it.
	require.Error(t, ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{SessionToken: nil}))
	pending, err := ti.repo.GetCustomDomainRouteConfig(ctx, domain.ID)
	require.NoError(t, err)
	require.True(t, pending.Deleted)
	require.Equal(t, "delete-checkpoint-example-com", pending.IngressName.String)
	require.Equal(t, "delete-checkpoint-example-com-tls", pending.CertSecretName.String)

	ti.temporal.reconcileStartErr = nil
	require.NoError(t, ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{SessionToken: nil}))
	require.Equal(t, 2, ti.temporal.reconcileCalls)
	require.Equal(t, domain.ID, ti.temporal.lastReconcileID)

	calls := provisioner.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "Delete", calls[0].Method)
	require.Equal(t, "delete-checkpoint-example-com", calls[0].ResourceName)
	require.Equal(t, "delete-checkpoint-example-com-tls", calls[0].SecretName)

	cleaned, err := ti.repo.GetCustomDomainRouteConfig(ctx, domain.ID)
	require.NoError(t, err)
	require.False(t, cleaned.IngressName.Valid)
	require.False(t, cleaned.CertSecretName.Valid)
}

func TestEnsureCustomDomainResourceNames_NeverRepopulatesTombstone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "tombstone-guard.example.com",
		IngressName:     pgTextValid("tombstone-ingress"),
		CertSecretName:  pgTextValid("tombstone-secret"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	require.NoError(t, ti.repo.DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID))
	require.NoError(t, ti.repo.ClearDeletedCustomDomainResourceNames(ctx, domain.ID))

	// A stale deletion request must not resurrect a cleaned tombstone's
	// identity: the derived names may now belong to a successor domain
	// reusing the hostname.
	rows, err := ti.repo.EnsureCustomDomainResourceNames(ctx, cdrepo.EnsureCustomDomainResourceNamesParams{
		IngressName:    pgTextValid("tombstone-guard-example-com"),
		CertSecretName: pgTextValid("tombstone-guard-example-com-tls"),
		ID:             domain.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.Zero(t, rows)
	route, err := ti.repo.GetCustomDomainRouteConfig(ctx, domain.ID)
	require.NoError(t, err)
	require.False(t, route.IngressName.Valid)
	require.False(t, route.CertSecretName.Valid)
}

func TestDeleteDomain_RetryCompletesPendingCleanup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "delete-retry.example.com",
		IngressName:     pgTextValid("retry-ingress"),
		CertSecretName:  pgTextValid("retry-secret"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	provisioner := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	reconciler := activities.NewCustomDomainIngress(
		logger,
		ti.conn,
		&deleteTestProvisionerFactory{provisioner: provisioner},
	)
	ti.temporal.reconcile = func(ctx context.Context, customDomainID uuid.UUID) error {
		return reconciler.ReconcileCustomDomain(ctx, activities.ReconcileCustomDomainArgs{CustomDomainID: customDomainID})
	}
	ti.temporal.reconcileStartErr = errors.New("signal failed")
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	require.Error(t, ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{SessionToken: nil}))
	pending, err := ti.repo.GetCustomDomainRouteConfig(ctx, domain.ID)
	require.NoError(t, err)
	require.True(t, pending.IngressName.Valid)

	ti.temporal.reconcileStartErr = nil
	require.NoError(t, ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{SessionToken: nil}))
	require.Equal(t, 2, ti.temporal.reconcileCalls)
	require.Equal(t, domain.ID, ti.temporal.lastReconcileID)

	calls := provisioner.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "Delete", calls[0].Method)
	require.Equal(t, "retry-ingress", calls[0].ResourceName)
	require.Equal(t, "retry-secret", calls[0].SecretName)

	cleaned, err := ti.repo.GetCustomDomainRouteConfig(ctx, domain.ID)
	require.NoError(t, err)
	require.False(t, cleaned.IngressName.Valid)
	require.False(t, cleaned.CertSecretName.Valid)

	err = ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{SessionToken: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
	require.Equal(t, 2, ti.temporal.reconcileCalls)
}
