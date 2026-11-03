package protectedresource

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	backfill "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/stretchr/testify/require"
)

type metadataEMAFixture struct {
	env                                                  proberEnv
	source, server, client, issuer, typed, sessionIssuer uuid.UUID
	resource, issuerURL                                  string
}

func seedMetadataEMA(t *testing.T, ctx context.Context, env proberEnv, resource string) metadataEMAFixture {
	t.Helper()
	q := backfill.New(env.conn)
	usi, issuer, client, source, server := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	issuerURL := "https://issuer.example.test/" + issuer.String()
	require.NoError(t, q.SeedUserSessionIssuerFixture(ctx, backfill.SeedUserSessionIssuerFixtureParams{ID: usi, ProjectID: uuid.NullUUID{UUID: env.projectID, Valid: true}, Slug: "usi-" + usi.String()}))
	require.NoError(t, q.SeedRemoteSessionIssuerFixture(ctx, backfill.SeedRemoteSessionIssuerFixtureParams{ID: issuer, ProjectID: uuid.NullUUID{UUID: env.projectID, Valid: true}, Slug: "rsi-" + issuer.String(), Issuer: issuerURL}))
	require.NoError(t, q.SeedRemoteSessionClientFixture(ctx, backfill.SeedRemoteSessionClientFixtureParams{ID: client, ProjectID: uuid.NullUUID{UUID: env.projectID, Valid: true}, OrganizationID: pgtype.Text{String: env.organizationID, Valid: true}, RemoteSessionIssuerID: issuer, ClientID: "client-" + client.String(), CredentialOwner: "self", TokenEndpointAuthMethod: pgtype.Text{String: "client_secret_basic", Valid: true}}))
	require.NoError(t, q.SeedRemoteMCPServerFixture(ctx, backfill.SeedRemoteMCPServerFixtureParams{ID: source, ProjectID: env.projectID, Url: resource}))
	require.NoError(t, q.SeedMCPServerFixture(ctx, backfill.SeedMCPServerFixtureParams{ID: server, ProjectID: env.projectID, Slug: pgtype.Text{String: "server-" + server.String(), Valid: true}, RemoteMcpServerID: uuid.NullUUID{UUID: source, Valid: true}}))
	require.NoError(t, q.SetServerUserSessionIssuerFixture(ctx, backfill.SetServerUserSessionIssuerFixtureParams{ID: server, UserSessionIssuerID: uuid.NullUUID{UUID: usi, Valid: true}}))
	_, err := q.SeedRemoteHeaderFixture(ctx, backfill.SeedRemoteHeaderFixtureParams{RemoteMcpServerID: source, Name: "X-Enterprise-Key", IsSecret: true, Value: pgtype.Text{String: "test-secret", Valid: true}})
	require.NoError(t, err)
	_, err = q.SeedEMABindingFixture(ctx, backfill.SeedEMABindingFixtureParams{ProjectID: env.projectID, OrganizationID: env.organizationID, UserSessionIssuerID: usi, RemoteSessionIssuerID: issuer, RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true}, Resource: resource, RequestedScopes: []string{"read"}})
	require.NoError(t, err)
	return metadataEMAFixture{env: env, source: source, server: server, client: client, issuer: issuer, typed: backfill.EMAClientID(env.organizationID, issuer, client, resource), sessionIssuer: usi, resource: resource, issuerURL: issuerURL}
}

func (f metadataEMAFixture) doc(allowed bool) wellknown.OAuthProtectedResourceMetadata {
	issuer := f.issuerURL
	if !allowed {
		issuer = "https://other-issuer.example.test"
	}
	raw, _ := json.Marshal(map[string]any{"resource": f.resource, "authorization_servers": []string{issuer}})
	return wellknown.OAuthProtectedResourceMetadata{Resource: f.resource, MetadataURL: metadataLocationPattern.ReplaceAllString(f.resource, `${1}/.well-known/oauth-protected-resource${2}`), AuthorizationServers: []string{issuer}, Raw: raw}
}

func (f metadataEMAFixture) assertTyped(t *testing.T, ctx context.Context, live bool) {
	t.Helper()
	q := authclientsrepo.New(f.env.conn)
	client, clientErr := q.GetAuthClient(ctx, authclientsrepo.GetAuthClientParams{ID: f.typed, OrganizationID: f.env.organizationID})
	links, err := q.ListAuthClientServers(ctx, authclientsrepo.ListAuthClientServersParams{AuthClientIds: []uuid.UUID{f.typed}, OrganizationID: f.env.organizationID})
	require.NoError(t, err)
	headers, err := q.ListAuthClientHeaders(ctx, authclientsrepo.ListAuthClientHeadersParams{AuthClientIds: []uuid.UUID{f.typed}, OrganizationID: f.env.organizationID})
	require.NoError(t, err)
	if live {
		require.NoError(t, clientErr)
		require.Equal(t, f.typed, client.ID)
		require.Len(t, links, 1)
		require.Equal(t, f.server, links[0].ID)
		require.Len(t, headers, 1)
	} else {
		require.ErrorIs(t, clientErr, pgx.ErrNoRows)
		require.Empty(t, links)
		require.Empty(t, headers)
	}
}

func TestRecordEMA_MetadataEligibilityLifecycle(t *testing.T) {
	t.Parallel()
	ctx, env := newProberEnv(t)
	f := seedMetadataEMA(t, ctx, env, "https://resource.example.test/mcp")
	unrelated := seedMetadataEMA(t, ctx, env, "https://unrelated.example.test/mcp")
	require.NoError(t, Record(ctx, env.conn, env.projectID, env.organizationID, f.resource, f.doc(true)))
	f.assertTyped(t, ctx, true)
	unrelated.assertTyped(t, ctx, false)
	require.NoError(t, RecordIfChanged(ctx, env.conn, env.projectID, env.organizationID, f.resource, f.doc(false)))
	f.assertTyped(t, ctx, false)
	// A failed fetch makes persisted metadata unusable. Normal URL matching
	// resumes, and must recreate typed reach without a preparation replay.
	require.NoError(t, RecordFetchError(ctx, env.conn, env.projectID, env.organizationID, f.resource, &wellknown.ProtectedResourceDiscoveryError{ProbeURL: f.doc(true).MetadataURL}))
	f.assertTyped(t, ctx, true)
	require.NoError(t, Record(ctx, env.conn, env.projectID, env.organizationID, f.resource, f.doc(false)))
	f.assertTyped(t, ctx, false)
	mismatch := f.doc(true)
	mismatch.Resource = "https://different.example.test/mcp"
	require.NoError(t, RecordIfChanged(ctx, env.conn, env.projectID, env.organizationID, f.resource, mismatch))
	f.assertTyped(t, ctx, true)
	require.NoError(t, RecordIfChanged(ctx, env.conn, env.projectID, env.organizationID, f.resource, f.doc(true)))
	f.assertTyped(t, ctx, true)
	unrelated.assertTyped(t, ctx, false)
}

func TestRecordEMA_RollbackAndInvalidDataRecovery(t *testing.T) {
	t.Parallel()
	ctx, env := newProberEnv(t)
	f := seedMetadataEMA(t, ctx, env, "https://resource.example.test/mcp")
	tx := testenv.BeginTx(t, ctx, env.conn)
	require.NoError(t, Record(ctx, tx, env.projectID, env.organizationID, f.resource, f.doc(true)))
	require.NoError(t, tx.Rollback(ctx))
	f.assertTyped(t, ctx, false)
	_, err := remoterepo.New(env.conn).GetRemoteProtectedResource(ctx, remoterepo.GetRemoteProtectedResourceParams{ProjectID: env.projectID, ResourceIdentifier: f.resource})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	bad := f.doc(true)
	bad.Raw = []byte(`{"resource":"https://resource.example.test/mcp","unsafe":"\u0000"}`)
	require.NoError(t, Record(ctx, env.conn, env.projectID, env.organizationID, f.resource, bad))
	f.assertTyped(t, ctx, true)
	record, err := remoterepo.New(env.conn).GetRemoteProtectedResource(ctx, remoterepo.GetRemoteProtectedResourceParams{ProjectID: env.projectID, ResourceIdentifier: f.resource})
	require.NoError(t, err)
	require.Equal(t, "The metadata document contains values that cannot be stored.", record.MetadataLastError.String)
}

func TestRecordEMA_ReconcilesOldAndNewMetadataLocations(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"", "?tenant=one", "#section"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			ctx, env := newProberEnv(t)
			current := seedMetadataEMA(t, ctx, env, "https://resource.example.test/current")
			former := seedMetadataEMA(t, ctx, env, "https://resource.example.test/former"+suffix)
			// Legacy evidence may occupy another source's diagnostic location.
			// Query and fragment redaction must not hide that former source.
			q := backfill.New(env.conn)
			require.NoError(t, q.SeedValidProtectedResourceFixture(ctx, backfill.SeedValidProtectedResourceFixtureParams{ProjectID: env.projectID, OrganizationID: env.organizationID, ResourceIdentifier: current.resource, MetadataUrl: pgtype.Text{String: urls.DiagnosticURL(former.doc(true).MetadataURL), Valid: true}, AuthorizationServers: []string{"https://unrelated-issuer.example.test"}}))
			require.NoError(t, Record(ctx, env.conn, env.projectID, env.organizationID, current.resource, current.doc(true)))
			current.assertTyped(t, ctx, true)
			former.assertTyped(t, ctx, true)
		})
	}
}

func TestRecordEMA_PreservesExactResourceIdentity(t *testing.T) {
	t.Parallel()
	ctx, env := newProberEnv(t)
	f := seedMetadataEMA(t, ctx, env, "https://resource.example.test/mcp")
	q := backfill.New(env.conn)
	source, server := uuid.New(), uuid.New()
	require.NoError(t, q.SeedRemoteMCPServerFixture(ctx, backfill.SeedRemoteMCPServerFixtureParams{ID: source, ProjectID: env.projectID, Url: f.resource + "/"}))
	require.NoError(t, q.SeedMCPServerFixture(ctx, backfill.SeedMCPServerFixtureParams{ID: server, ProjectID: env.projectID, Slug: pgtype.Text{String: "server-" + server.String(), Valid: true}, RemoteMcpServerID: uuid.NullUUID{UUID: source, Valid: true}}))
	require.NoError(t, q.SetServerUserSessionIssuerFixture(ctx, backfill.SetServerUserSessionIssuerFixtureParams{ID: server, UserSessionIssuerID: uuid.NullUUID{UUID: f.sessionIssuer, Valid: true}}))
	require.NoError(t, Record(ctx, env.conn, env.projectID, env.organizationID, f.resource, f.doc(true)))
	// The same inbound issuer must not make a slash-distinct upstream eligible.
	f.assertTyped(t, ctx, true)
}
