package customdomains_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSetRootMcpEndpoint_SetReplaceReapplyAndClear(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "root-selection.example.com",
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	firstID := seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, domain.ID, "first")
	secondID := seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, domain.ID, "second")

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})
	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)

	result, err := ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
		McpEndpointID:  new(firstID.String()),
	})
	require.NoError(t, err)
	require.Equal(t, firstID.String(), requireValue(t, result.RootMcpEndpointID))
	requireRootSelection(t, ctx, ti, firstID, true)
	requireRootSelection(t, ctx, ti, secondID, false)
	requireLatestRootAuditTransition(t, ctx, ti, nil, new(firstID.String()))
	domainView, err := ti.service.GetDomain(ctx, &gen.GetDomainPayload{})
	require.NoError(t, err)
	require.Equal(t, firstID.String(), requireValue(t, domainView.RootMcpEndpointID))
	endpoints, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{})
	require.NoError(t, err)
	require.Len(t, endpoints.McpEndpoints, 2)
	require.Equal(t, 1, countRootEndpoints(endpoints.McpEndpoints))

	result, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
		McpEndpointID:  new(secondID.String()),
	})
	require.NoError(t, err)
	require.Equal(t, secondID.String(), requireValue(t, result.RootMcpEndpointID))
	requireRootSelection(t, ctx, ti, firstID, false)
	requireRootSelection(t, ctx, ti, secondID, true)
	requireLatestRootAuditTransition(t, ctx, ti, new(firstID.String()), new(secondID.String()))

	// Idempotent re-apply still reconciles desired state.
	_, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
		McpEndpointID:  new(secondID.String()),
	})
	require.NoError(t, err)
	requireLatestRootAuditTransition(t, ctx, ti, new(secondID.String()), new(secondID.String()))

	result, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
	})
	require.NoError(t, err)
	require.Nil(t, result.RootMcpEndpointID)
	requireRootSelection(t, ctx, ti, secondID, false)
	requireLatestRootAuditTransition(t, ctx, ti, new(secondID.String()), nil)
	require.Equal(t, 4, ti.temporal.reconcileCalls)
	require.Equal(t, domain.ID, ti.temporal.lastReconcileID)

	afterAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount+4, afterAuditCount)
}

func TestSetRootMcpEndpoint_ReconcileFailureRetainsDesiredStateForRetry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "root-retry.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	endpointID := seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, domain.ID, "retry")
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	ti.temporal.reconcileErr = errors.New("apply failed")
	_, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
		McpEndpointID:  new(endpointID.String()),
	})
	require.Error(t, err)
	requireRootSelection(t, ctx, ti, endpointID, true)

	ti.temporal.reconcileErr = nil
	result, err := ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
		McpEndpointID:  new(endpointID.String()),
	})
	require.NoError(t, err)
	require.Equal(t, endpointID.String(), requireValue(t, result.RootMcpEndpointID))
	require.Equal(t, 2, ti.temporal.reconcileCalls)
}

func TestSetRootMcpEndpoint_RejectsDeletedEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "root-validation.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	endpointID := seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, domain.ID, "deleted")
	_, err = mcpendpointsrepo.New(ti.conn).DeleteMCPEndpoint(ctx, mcpendpointsrepo.DeleteMCPEndpointParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	_, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
		McpEndpointID:  new(endpointID.String()),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.Zero(t, ti.temporal.reconcileCalls)
}

func TestSetRootMcpEndpoint_RejectsForeignOrganizationEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "root-foreign-org.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	foreignProjectID := seedProject(t, ctx, ti.conn, "foreign-organization")
	endpointID := seedMcpEndpoint(t, ctx, ti.conn, foreignProjectID, domain.ID, "foreign")
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	_, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
		McpEndpointID:  new(endpointID.String()),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Zero(t, ti.temporal.reconcileCalls)
}

func TestSetRootMcpEndpoint_RejectsWrongDomainEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	ownedDomain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "root-owned-domain.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	otherDomain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: "other-organization",
		Domain:         "root-other-domain.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	endpointID := seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, otherDomain.ID, "wrong-domain")
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	_, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: ownedDomain.ID.String(),
		McpEndpointID:  new(endpointID.String()),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Zero(t, ti.temporal.reconcileCalls)
}

func TestSetRootMcpEndpoint_RejectsDisabledParentServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "root-disabled-server.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	serverID := seedMcpServerWithVisibility(t, ctx, ti.conn, *authCtx.ProjectID, "disabled")
	endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpServerID:    serverID,
		Slug:           "disabled",
	})
	require.NoError(t, err)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	_, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
		McpEndpointID:  new(endpoint.ID.String()),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Zero(t, ti.temporal.reconcileCalls)
}

func TestSetRootMcpEndpoint_TranslatesUniqueConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "root-conflict.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	targetID := seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, domain.ID, "target")
	conflictingID := seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, domain.ID, "conflicting")
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	conflictingTx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, cdrepo.New(conflictingTx).SetRootMcpEndpoint(ctx, cdrepo.SetRootMcpEndpointParams{
		McpEndpointID:  conflictingID,
		CustomDomainID: domain.ID,
	}))

	var finished atomic.Bool
	result := make(chan error, 1)
	go func() {
		_, setErr := ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
			CustomDomainID: domain.ID.String(),
			McpEndpointID:  new(targetID.String()),
		})
		result <- setErr
		finished.Store(true)
	}()

	// Wait until the API transaction owns the target endpoint lock, then give
	// it time to reach the unique-index wait on the uncommitted competing root.
	require.Eventually(t, func() bool {
		lockCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
		defer cancel()
		lockErr := pgx.BeginFunc(lockCtx, ti.conn, func(tx pgx.Tx) error {
			_, queryErr := cdrepo.New(tx).LockRootMcpEndpointSelection(lockCtx, cdrepo.LockRootMcpEndpointSelectionParams{
				CustomDomainID: domain.ID,
				McpEndpointID:  uuid.NullUUID{UUID: targetID, Valid: true},
			})
			if queryErr != nil {
				return fmt.Errorf("lock root mcp endpoint selection: %w", queryErr)
			}
			return nil
		})
		return errors.Is(lockErr, context.DeadlineExceeded)
	}, 2*time.Second, 10*time.Millisecond)
	require.Never(t, finished.Load, 100*time.Millisecond, 10*time.Millisecond)

	require.NoError(t, conflictingTx.Commit(ctx))
	require.Eventually(t, finished.Load, 2*time.Second, 10*time.Millisecond)
	requireOopsCode(t, <-result, oops.CodeConflict)
	requireRootSelection(t, ctx, ti, conflictingID, true)
	requireRootSelection(t, ctx, ti, targetID, false)
	require.Zero(t, ti.temporal.reconcileCalls)
}

func TestSetRootMcpEndpoint_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "root-rbac.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestSetRootMcpEndpoint_MalformedDomainIDBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	_, err := ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: "not-a-uuid",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Zero(t, ti.temporal.reconcileCalls)
}

func TestSetRootMcpEndpoint_UnknownDomainNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	_, err := ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Zero(t, ti.temporal.reconcileCalls)
}

func TestSetRootMcpEndpoint_ForeignOrganizationDomainNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	foreignDomain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: "foreign-organization",
		Domain:         "root-foreign-domain.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	// Foreign-owned ids collapse to the same NotFound as unknown ids so
	// existence is not disclosed across organizations.
	_, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: foreignDomain.ID.String(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Zero(t, ti.temporal.reconcileCalls)
}

func requireRootSelection(t *testing.T, ctx context.Context, ti *serviceTestInstance, endpointID uuid.UUID, expected bool) {
	t.Helper()
	authCtx := testAuthContext(t, ctx)
	row, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, expected, row.IsDomainRoot.Valid && row.IsDomainRoot.Bool)
}

func requireValue(t *testing.T, value *string) string {
	t.Helper()
	require.NotNil(t, value)
	return *value
}

func countRootEndpoints(endpoints []*gen.CustomDomainMcpEndpoint) int {
	count := 0
	for _, endpoint := range endpoints {
		if endpoint.IsDomainRoot {
			count++
		}
	}
	return count
}

func requireLatestRootAuditTransition(t *testing.T, ctx context.Context, ti *serviceTestInstance, before, after *string) {
	t.Helper()

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	if before == nil {
		require.Nil(t, beforeSnapshot["RootMcpEndpointID"])
	} else {
		require.Equal(t, *before, beforeSnapshot["RootMcpEndpointID"])
	}
	if after == nil {
		require.Nil(t, afterSnapshot["RootMcpEndpointID"])
	} else {
		require.Equal(t, *after, afterSnapshot["RootMcpEndpointID"])
	}
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
