package mcpservers_test

import (
	"context"
	"testing"
	"time"

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
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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

func TestUpdateMcpServer_ClearsHostedIssuer(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := usersessionsIssuerForTest(t, ctx, f, authCtx.ActiveOrganizationID)
	require.NoError(t, err)

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.UserSessionIssuerID = &issuer }))
	require.NoError(t, err)

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

	reattached, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.UserSessionIssuerID = &issuer }))
	require.NoError(t, err)
	require.Equal(t, issuer, *reattached.UserSessionIssuerID)
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

func createOrganizationIssuer(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) string {
	t.Helper()

	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: organizationID, Valid: true},
		Slug:                         "org-clear-" + uuid.NewString()[:8],
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	return issuer.ID.String()
}

// linkRemoteClient configures an upstream OAuth client on a user session issuer and resyncs, so servers carrying the
// issuer get a derived remote issuer.
func linkRemoteClient(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, projectID uuid.UUID, issuerID string) uuid.UUID {
	t.Helper()

	q := remoterepo.New(ti.conn)
	slug := "clear-remote-" + uuid.NewString()[:8]
	remoteIssuer, err := q.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{
		ProjectID: conv.ToNullUUID(projectID), OrganizationID: conv.ToPGText(organizationID), Slug: slug, Issuer: "https://" + slug + ".example.com",
		AuthorizationEndpoint: conv.ToPGText("https://" + slug + ".example.com/authorize"), TokenEndpoint: conv.ToPGText("https://" + slug + ".example.com/token"),
		ScopesSupported: []string{"openid"}, GrantTypesSupported: []string{"authorization_code", "refresh_token"}, ResponseTypesSupported: []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"}, CodeChallengeMethodsSupported: []string{"S256"},
	})
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(ctx, remoterepo.CreateRemoteSessionClientParams{
		ProjectID: conv.ToNullUUID(projectID), OrganizationID: conv.ToPGText(organizationID), RemoteSessionIssuerID: remoteIssuer.ID,
		ClientID: slug + "-client", ClientIDIssuedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remoterepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID, UserSessionIssuerID: uuid.MustParse(issuerID),
	}))
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, organizationID, projectID, []uuid.UUID{uuid.MustParse(issuerID)}))
	return remoteIssuer.ID
}

func storedServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, id string) mcpserversrepo.McpServer {
	t.Helper()

	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(id), ProjectID: projectID})
	require.NoError(t, err)
	return server
}

// Clearing one server of a shared, OAuth-configured issuer clears only its own derived state.
func TestUpdateMcpServer_ClearSharedOrganizationIssuerKeepsOtherServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	issuer := createOrganizationIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)
	servers := make([]string, 0, 2)
	backends := make([]string, 0, 2)
	for _, name := range []string{"shared one", "shared two"} {
		remoteID := seedRemoteMcpServer(t, ctx, ti.conn, projectID).String()
		created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{Name: name, RemoteMcpServerID: &remoteID, UserSessionIssuerID: &issuer, Visibility: types.McpServerVisibility("disabled")})
		require.NoError(t, err)
		servers = append(servers, created.ID)
		backends = append(backends, remoteID)
	}
	remoteIssuerID := linkRemoteClient(t, ctx, ti, authCtx.ActiveOrganizationID, projectID, issuer)
	for _, id := range servers {
		require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, storedServer(t, ctx, ti, projectID, id).RemoteSessionIssuerID, "the derived remote issuer starts populated")
	}

	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: servers[0], RemoteMcpServerID: &backends[0], ClearUserSessionIssuer: new(true), Visibility: types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err, "an organization-owned issuer can be detached")
	requireNoIssuer(t, ctx, ti, projectID, servers[0])
	other := storedServer(t, ctx, ti, projectID, servers[1])
	require.Equal(t, issuer, other.UserSessionIssuerID.UUID.String())
	require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, other.RemoteSessionIssuerID, "the other server keeps its derived state")
}

func TestUpdateMcpServer_MintBoundary(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, projectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{Name: "mint boundary", RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("disabled")})
	require.NoError(t, err)
	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{ID: created.ID, RemoteMcpServerID: &remoteID, ClearUserSessionIssuer: new(true), Visibility: types.McpServerVisibility("disabled")})
	require.NoError(t, err)

	saved, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("disabled")})
	require.NoError(t, err)
	require.Nil(t, saved.UserSessionIssuerID, "an issuerless server saved on the same backend does not mint")

	replacement := seedRemoteMcpServer(t, ctx, ti.conn, projectID).String()
	switched, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{ID: created.ID, RemoteMcpServerID: &replacement, Visibility: types.McpServerVisibility("disabled")})
	require.NoError(t, err)
	require.NotNil(t, switched.UserSessionIssuerID, "replacing the remote backend mints, even after a clear")

	staffCtx := withStaffEmail(t, ctx)
	unproxiedID := seedUnproxiedMcpServer(t, ctx, ti.conn, projectID).String()
	unproxied, err := ti.service.CreateMcpServer(staffCtx, &gen.CreateMcpServerPayload{Name: "unproxied to remote", UnproxiedMcpServerID: &unproxiedID, Visibility: types.McpServerVisibility("disabled")})
	require.NoError(t, err)
	require.Nil(t, unproxied.UserSessionIssuerID)
	onto := seedRemoteMcpServer(t, ctx, ti.conn, projectID).String()
	moved, err := ti.service.UpdateMcpServer(staffCtx, &gen.UpdateMcpServerPayload{ID: unproxied.ID, RemoteMcpServerID: &onto, Visibility: types.McpServerVisibility("disabled")})
	require.NoError(t, err)
	require.NotNil(t, moved.UserSessionIssuerID, "switching an unproxied server onto a remote backend mints")
}

func TestUpdateMcpServer_ClearsHostedOrganizationIssuer(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := createOrganizationIssuer(t, ctx, f.ti, authCtx.ActiveOrganizationID)

	_, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.UserSessionIssuerID = &issuer }))
	require.NoError(t, err)
	updated, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.ClearUserSessionIssuer = new(true) }))
	require.NoError(t, err, "an organization-owned issuer can be detached from a hosted server")
	require.Nil(t, updated.UserSessionIssuerID)
}

func TestUpdateMcpServer_HostedClearRefusedWhileEMABound(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := usersessionsIssuerForTest(t, ctx, f, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.UserSessionIssuerID = &issuer }))
	require.NoError(t, err)

	q := remoterepo.New(f.ti.conn)
	remoteIssuer, err := q.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{ProjectID: conv.ToNullUUID(f.projectID), Slug: "hosted-clear-ema", Issuer: "https://authorization.example.com", ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{}})
	require.NoError(t, err)
	key := remoterepo.GetEMABindingParams{ProjectID: f.projectID, OrganizationID: authCtx.ActiveOrganizationID, UserSessionIssuerID: uuid.MustParse(issuer), RemoteSessionIssuerID: remoteIssuer.ID, Resource: "https://resource.example.com/"}
	require.NoError(t, q.EnsureEMABinding(ctx, remoterepo.EnsureEMABindingParams(key)))

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.ClearUserSessionIssuer = new(true) }))
	requireOopsCode(t, err, oops.CodeConflict)
	stored := storedServer(t, ctx, f.ti, f.projectID, f.toolset.ID.String())
	require.Equal(t, issuer, stored.UserSessionIssuerID.UUID.String(), "a refused clear keeps the issuer")
}
