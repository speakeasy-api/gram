package organizations_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestService_GetGateway_DiscoveryModeDefaultsToProgressive(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.False(t, org.GatewayDiscoveryMode.Valid, "an organization that never set a mode stores none")

	res, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})
	require.NoError(t, err)
	require.Equal(t, "progressive", res.DiscoveryMode)
	require.False(t, res.CodeModeEnabled)
}

func TestService_GetGateway_ReportsCodeModeCapability(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	ti.codeModeAllowed.Store(true)

	res, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})
	require.NoError(t, err)
	require.True(t, res.CodeModeEnabled)
}

func TestService_SetGatewayDiscoveryMode_StoresAndAuditsMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	createGatewayIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationGatewayDiscoveryModeUpdated)
	require.NoError(t, err)

	res, err := ti.service.SetGatewayDiscoveryMode(ctx, &gen.SetGatewayDiscoveryModePayload{DiscoveryMode: "direct"})
	require.NoError(t, err)
	require.Equal(t, "direct", res.DiscoveryMode)
	require.True(t, res.Available)

	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "direct", org.GatewayDiscoveryMode.String)

	got, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})
	require.NoError(t, err)
	require.Equal(t, "direct", got.DiscoveryMode)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationGatewayDiscoveryModeUpdated)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationGatewayDiscoveryModeUpdated)
	require.NoError(t, err)
	require.Equal(t, "organization", record.SubjectType)
	require.Equal(t, authCtx.ActiveOrganizationID, record.SubjectID)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "progressive", beforeSnapshot["discovery_mode"])
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "direct", afterSnapshot["discovery_mode"])
}

func TestService_SetGatewayDiscoveryMode_UnchangedModeWritesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationGatewayDiscoveryModeUpdated)
	require.NoError(t, err)

	res, err := ti.service.SetGatewayDiscoveryMode(ctx, &gen.SetGatewayDiscoveryModePayload{DiscoveryMode: "progressive"})
	require.NoError(t, err)
	require.Equal(t, "progressive", res.DiscoveryMode)

	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.False(t, org.GatewayDiscoveryMode.Valid)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationGatewayDiscoveryModeUpdated)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestService_SetGatewayDiscoveryMode_ReportedWhileGatewayIsOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	res, err := ti.service.SetGatewayDiscoveryMode(ctx, &gen.SetGatewayDiscoveryModePayload{DiscoveryMode: "direct"})
	require.NoError(t, err)
	require.False(t, res.Available)
	require.NotNil(t, res.UnavailableReason)
	require.Equal(t, "turned_off", *res.UnavailableReason)
	require.Equal(t, "direct", res.DiscoveryMode, "admins choose the mode before turning the gateway on")
}

func TestService_SetGatewayDiscoveryMode_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.SetGatewayDiscoveryMode(ctx, &gen.SetGatewayDiscoveryModePayload{DiscoveryMode: "direct"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	res, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})
	require.NoError(t, err, "org:read still reads the mode")
	require.Equal(t, "progressive", res.DiscoveryMode)
}

func TestService_SetGatewayDiscoveryMode_OrgAdminGrantSetsMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	res, err := ti.service.SetGatewayDiscoveryMode(ctx, &gen.SetGatewayDiscoveryModePayload{DiscoveryMode: "direct"})
	require.NoError(t, err)
	require.Equal(t, "direct", res.DiscoveryMode)
}

func TestService_SetGatewayDiscoveryMode_CodeModeRequiresEntitlement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.SetGatewayDiscoveryMode(ctx, &gen.SetGatewayDiscoveryModePayload{DiscoveryMode: "code_mode"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.False(t, org.GatewayDiscoveryMode.Valid, "a refused Code Mode leaves the default unchanged")

	ti.codeModeAllowed.Store(true)
	res, err := ti.service.SetGatewayDiscoveryMode(ctx, &gen.SetGatewayDiscoveryModePayload{DiscoveryMode: "code_mode"})
	require.NoError(t, err)
	require.Equal(t, "code_mode", res.DiscoveryMode)
	require.True(t, res.CodeModeEnabled)
}

func TestService_SetGatewayDiscoveryMode_CodeModeKeptAfterEntitlementEnds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	ti.codeModeAllowed.Store(true)
	_, err := ti.service.SetGatewayDiscoveryMode(ctx, &gen.SetGatewayDiscoveryModePayload{DiscoveryMode: "code_mode"})
	require.NoError(t, err)
	ti.codeModeAllowed.Store(false)

	res, err := ti.service.SetGatewayDiscoveryMode(ctx, &gen.SetGatewayDiscoveryModePayload{DiscoveryMode: "code_mode"})
	require.NoError(t, err, "re-saving the current Code Mode default is not a new selection")
	require.Equal(t, "code_mode", res.DiscoveryMode)
	require.False(t, res.CodeModeEnabled)

	res, err = ti.service.SetGatewayDiscoveryMode(ctx, &gen.SetGatewayDiscoveryModePayload{DiscoveryMode: "progressive"})
	require.NoError(t, err)
	require.Equal(t, "progressive", res.DiscoveryMode)
}
