package customdomains_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

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

	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "delete-zero.example.com",
		IngressName:    pgTextValid("ingress-zero"),
		CertSecretName: pgTextValid("cert-zero"),
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
}

func TestDeleteDomain_CascadesSoftDeleteToMcpEndpointsAcrossProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	domainRow, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "cascade.example.com",
		IngressName:    pgTextValid("ingress-cascade"),
		CertSecretName: pgTextValid("cert-cascade"),
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
