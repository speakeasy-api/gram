package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	backfill "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func seedPreparedEMA(t *testing.T, ctx context.Context, ti *testInstance, resource string) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := backfill.New(ti.conn)
	usi, issuer, client := uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, q.SeedUserSessionIssuerFixture(ctx, backfill.SeedUserSessionIssuerFixtureParams{ID: usi, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, Slug: "ema-usi-" + usi.String()}))
	require.NoError(t, q.SeedRemoteSessionIssuerFixture(ctx, backfill.SeedRemoteSessionIssuerFixtureParams{ID: issuer, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, Slug: "ema-rsi-" + issuer.String(), Issuer: "https://issuer.example.com/" + issuer.String()}))
	require.NoError(t, q.SeedRemoteSessionClientFixture(ctx, backfill.SeedRemoteSessionClientFixtureParams{ID: client, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, OrganizationID: pgtype.Text{String: auth.ActiveOrganizationID, Valid: true}, RemoteSessionIssuerID: issuer, ClientID: "client-" + client.String(), CredentialOwner: "self", TokenEndpointAuthMethod: pgtype.Text{String: "client_secret_basic", Valid: true}}))
	binding, err := q.SeedEMABindingFixture(ctx, backfill.SeedEMABindingFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: usi, RemoteSessionIssuerID: issuer, RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true}, Resource: resource, RequestedScopes: []string{"read"}})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, backfill.SyncEMABinding(ctx, tx, auth.ActiveOrganizationID, binding))
	require.NoError(t, tx.Commit(ctx))
	return usi, issuer, client
}

func TestEMA_PreparedBeforeNormalServerCreateAndRetarget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := backfill.New(ti.conn)
	resource := "https://mcp.example.com/ema/" + uuid.NewString()
	usi, issuer, client := seedPreparedEMA(t, ctx, ti, resource)
	id := backfill.EMAClientID(auth.ActiveOrganizationID, issuer, client, resource)
	source := uuid.New()
	require.NoError(t, q.SeedRemoteMCPServerFixture(ctx, backfill.SeedRemoteMCPServerFixtureParams{ID: source, ProjectID: *auth.ProjectID, Url: resource}))
	seedAuxiliaryHeader(t, ctx, ti, source, "X-Enterprise-Key", "source-key")
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{Name: "EMA prepared first", RemoteMcpServerID: conv.PtrEmpty(source.String()), UserSessionIssuerID: conv.PtrEmpty(usi.String()), Visibility: types.McpServerVisibility("private")})
	require.NoError(t, err)
	links, err := q.ListBackfillLinkedServersFixture(ctx, id)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{uuid.MustParse(created.ID)}, links)

	nextResource := resource + "/next"
	nextSource := uuid.New()
	require.NoError(t, q.SeedRemoteMCPServerFixture(ctx, backfill.SeedRemoteMCPServerFixtureParams{ID: nextSource, ProjectID: *auth.ProjectID, Url: nextResource}))
	_, err = q.SeedEMABindingFixture(ctx, backfill.SeedEMABindingFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: usi, RemoteSessionIssuerID: issuer, RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true}, Resource: nextResource, RequestedScopes: []string{"read"}})
	require.NoError(t, err)
	_, err = ti.service.UpdateMcpServer(ctx, newBackendUpdatePayload(created.ID, "retargeted EMA", conv.PtrEmpty(nextSource.String()), nil))
	require.NoError(t, err)
	nextID := backfill.EMAClientID(auth.ActiveOrganizationID, issuer, client, nextResource)
	links, err = q.ListBackfillLinkedServersFixture(ctx, nextID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{uuid.MustParse(created.ID)}, links, "retarget needed another preparation")
	old, err := q.GetAuthClientFixture(ctx, id)
	require.NoError(t, err)
	require.True(t, old.Deleted)
	headers, err := mcpserversrepo.New(ti.conn).CountEMAClientHeadersFixture(ctx, mcpserversrepo.CountEMAClientHeadersFixtureParams{AuthClientID: id, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	require.Zero(t, headers)
}

func TestEMA_NormalGatewayOptInAddsExistingServerReach(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := backfill.New(ti.conn)
	resource := "https://mcp.example.com/gateway/" + uuid.NewString()
	_, issuer, client := seedPreparedEMA(t, ctx, ti, resource)
	// Move the prepared binding to the organization's gateway issuer, leaving
	// the member with a different direct issuer and no previous gateway choice.
	gatewayIssuer := uuid.New()
	require.NoError(t, q.SeedOrganizationUserSessionIssuerFixture(ctx, backfill.SeedOrganizationUserSessionIssuerFixtureParams{ID: gatewayIssuer, OrganizationID: pgtype.Text{String: auth.ActiveOrganizationID, Valid: true}, Slug: "gateway-" + gatewayIssuer.String()}))
	err := mcpserversrepo.New(ti.conn).SetEMABindingGatewayIssuerFixture(ctx, mcpserversrepo.SetEMABindingGatewayIssuerFixtureParams{UserSessionIssuerID: gatewayIssuer, RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true}, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	source := uuid.New()
	require.NoError(t, q.SeedRemoteMCPServerFixture(ctx, backfill.SeedRemoteMCPServerFixtureParams{ID: source, ProjectID: *auth.ProjectID, Url: resource}))
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{Name: "gateway member", RemoteMcpServerID: conv.PtrEmpty(source.String()), Visibility: types.McpServerVisibility("private")})
	require.NoError(t, err)
	id := backfill.EMAClientID(auth.ActiveOrganizationID, issuer, client, resource)
	links, err := q.ListBackfillLinkedServersFixture(ctx, id)
	require.NoError(t, err)
	require.Empty(t, links)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	features := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), ti.conn, redisClient)
	require.NoError(t, features.SetFeatureEnabled(ctx, auth.ActiveOrganizationID, productfeatures.FeatureOrganizationGatewayDisabled, false))
	links, err = q.ListBackfillLinkedServersFixture(ctx, id)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{uuid.MustParse(created.ID)}, links, "gateway opt-in required preparation or migration")
	require.NoError(t, features.SetFeatureEnabled(ctx, auth.ActiveOrganizationID, productfeatures.FeatureOrganizationGatewayDisabled, true))
	links, err = q.ListBackfillLinkedServersFixture(ctx, id)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{uuid.MustParse(created.ID)}, links, "availability toggle changed configured reach")
}
