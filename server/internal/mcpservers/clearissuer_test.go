package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostedmcptest"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func requireNoIssuer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, serverID string) {
	t.Helper()

	stored, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(serverID), ProjectID: projectID})
	require.NoError(t, err)
	require.False(t, stored.UserSessionIssuerID.Valid)
	require.False(t, stored.RemoteSessionIssuerID.Valid, "the derived remote issuer goes with it")
	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionMcpServerUpdate, serverID)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.NotEmpty(t, before["UserSessionIssuerID"], "the audit records the detached issuer")
	require.Empty(t, after["UserSessionIssuerID"])
}

func TestUpdateMcpServer_ClearsRemoteIssuerAndReattaches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{Name: "clear remote", RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("disabled")})
	require.NoError(t, err)
	require.NotNil(t, created.UserSessionIssuerID)
	previous := *created.UserSessionIssuerID

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, ClearUserSessionIssuer: new(true), Visibility: types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, updated.UserSessionIssuerID)
	requireNoIssuer(t, ctx, ti, *authCtx.ProjectID, created.ID)

	// A later save does not mint a replacement.
	renamed, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, Name: new("clear remote renamed"), RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, renamed.UserSessionIssuerID)

	reattached, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, UserSessionIssuerID: &previous, Visibility: types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, reattached.UserSessionIssuerID)
	require.Equal(t, previous, *reattached.UserSessionIssuerID)
}

func TestUpdateMcpServer_ClearsTunneledIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	tunneledID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{Name: "clear tunneled", TunneledMcpServerID: &tunneledID, Visibility: types.McpServerVisibility("disabled")})
	require.NoError(t, err)
	require.NotNil(t, created.UserSessionIssuerID)

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, TunneledMcpServerID: &tunneledID, ClearUserSessionIssuer: new(true), Visibility: types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, updated.UserSessionIssuerID)
	requireNoIssuer(t, ctx, ti, *authCtx.ProjectID, created.ID)
}

func TestUpdateMcpServer_ClearIssuerRefusals(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{Name: "clear refusals", RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("disabled")})
	require.NoError(t, err)
	issuerID := *created.UserSessionIssuerID
	clearPayload := func(id string) *gen.UpdateMcpServerPayload {
		return &gen.UpdateMcpServerPayload{ID: id, RemoteMcpServerID: &remoteID, ClearUserSessionIssuer: new(true), Visibility: types.McpServerVisibility("disabled")}
	}

	both := clearPayload(created.ID)
	both.UserSessionIssuerID = &issuerID
	_, err = ti.service.UpdateMcpServer(ctx, both)
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.UpdateMcpServer(ctx, clearPayload(uuid.NewString()))
	requireOopsCode(t, err, oops.CodeNotFound)

	readOnly := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, created.ID))
	_, err = ti.service.UpdateMcpServer(readOnly, clearPayload(created.ID))
	requireOopsCode(t, err, oops.CodeForbidden)

	q := remoterepo.New(ti.conn)
	remoteIssuer, err := q.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{ProjectID: conv.ToNullUUID(*authCtx.ProjectID), Slug: "clear-ema", Issuer: "https://authorization.example.com", ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{}})
	require.NoError(t, err)
	key := remoterepo.GetEMABindingParams{ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID, UserSessionIssuerID: uuid.MustParse(issuerID), RemoteSessionIssuerID: remoteIssuer.ID, Resource: "https://resource.example.com/"}
	require.NoError(t, q.EnsureEMABinding(ctx, remoterepo.EnsureEMABindingParams(key)))
	_, err = ti.service.UpdateMcpServer(ctx, clearPayload(created.ID))
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "identity-chaining bindings")

	stored, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, issuerID, stored.UserSessionIssuerID.UUID.String(), "a refused clear keeps the issuer")
}

func TestUpdateMcpServer_ClearsHostedIssuerAndMirrorsIt(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := usersessionsIssuerForTest(t, ctx, f, authCtx.ActiveOrganizationID)
	require.NoError(t, err)

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.UserSessionIssuerID = &issuer }))
	require.NoError(t, err)
	hostedmcptest.RequireMirrored(t, ctx, f.ti.conn, f.projectID, f.toolset.ID)

	both := f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) {
		p.UserSessionIssuerID = &issuer
		p.ClearUserSessionIssuer = new(true)
	})
	_, err = f.ti.service.UpdateMcpServer(ctx, both)
	requireOopsCode(t, err, oops.CodeBadRequest)

	updated, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.ClearUserSessionIssuer = new(true) }))
	require.NoError(t, err)
	require.Nil(t, updated.UserSessionIssuerID)
	requireNoIssuer(t, ctx, f.ti, f.projectID, f.toolset.ID.String())
	toolset, err := toolsetsrepo.New(f.ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.False(t, toolset.UserSessionIssuerID.Valid, "the toolset column mirrors the cleared issuer")
	hostedmcptest.RequireMirrored(t, ctx, f.ti.conn, f.projectID, f.toolset.ID)

	reattached, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.UserSessionIssuerID = &issuer }))
	require.NoError(t, err)
	require.Equal(t, issuer, *reattached.UserSessionIssuerID)
	hostedmcptest.RequireMirrored(t, ctx, f.ti.conn, f.projectID, f.toolset.ID)
}

func usersessionsIssuerForTest(t *testing.T, ctx context.Context, f *hostedFixture, organizationID string) (string, error) {
	t.Helper()

	issuer, err := usersessionsrepo.New(f.ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          f.projectID,
		OrganizationID:     pgtype.Text{String: organizationID, Valid: true},
		Slug:               "hosted-clear-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 24 * 60 * 60 * 1_000_000, Valid: true},
	})
	if err != nil {
		return "", err //nolint:wrapcheck // test helper
	}
	return issuer.ID.String(), nil
}
