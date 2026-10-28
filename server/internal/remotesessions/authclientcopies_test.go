package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// A credential source's non-Authorization headers are copied onto the auth
// client of the upstream OAuth client its servers offer, and the serving path
// sends those copies: it loads them by auth client id and organization alone
// and revalidates no source ownership. Associating a client with a server on a
// second source therefore invalidates the copies the first source left, and
// detaching the server that made the client sole-source does the same. Both
// writers have to retire them in their own transaction; until they do, the
// first source's secret goes out to the second source's upstream.

// sourceBackedTarget creates a remote source with an MCP server in front of
// it, the shape a server identity configuration is committed against, and
// returns both ids.
func sourceBackedTarget(t *testing.T, ctx context.Context, ti *testInstance, slug string, userIssuerID uuid.UUID) (serverID, sourceID uuid.UUID) {
	t.Helper()

	projectID := projectIDFromContext(t, ctx)
	source, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          conv.ToPGText(slug),
		Slug:          conv.ToPGText(slug),
		TransportType: "streamable-http",
		Url:           "http://127.0.0.1:0/" + slug,
	})
	require.NoError(t, err)

	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		UserSessionIssuerID: conv.ToNullUUID(userIssuerID),
		RemoteMcpServerID:   conv.ToNullUUID(source.ID),
		Visibility:          "private",
	})
	require.NoError(t, err)

	return server.ID, source.ID
}

// seedAuxiliaryHeader puts a secret credential header other than Authorization
// on a remote source. Only these are copied onto an upstream client.
func seedAuxiliaryHeader(t *testing.T, ctx context.Context, ti *testInstance, sourceID uuid.UUID, name string) {
	t.Helper()

	_, err := authclientbackfill.New(ti.conn).SeedRemoteHeaderFixture(ctx, authclientbackfill.SeedRemoteHeaderFixtureParams{
		RemoteMcpServerID:      sourceID,
		Name:                   name,
		IsSecret:               true,
		Value:                  pgtype.Text{String: "upstream-secret", Valid: true},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
}

// seedDerivedAuthClient records the auth client the backfill derives from an
// upstream OAuth client, carrying the copy it took while the client reached one
// source alone. This is the state a later association write invalidates.
func seedDerivedAuthClient(t *testing.T, ctx context.Context, ti *testInstance, oauthClientID uuid.UUID, headerName string) uuid.UUID {
	t.Helper()

	orgID := activeOrganizationID(t, ctx)
	fixtures := authclientbackfill.New(ti.conn)
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{
		OrganizationID:        orgID,
		ProjectID:             conv.ToNullUUID(projectIDFromContext(t, ctx)),
		ClientType:            "remote_session",
		Source:                "backfill",
		RemoteSessionClientID: conv.ToNullUUID(oauthClientID),
	})
	require.NoError(t, err)

	require.NoError(t, fixtures.SeedAuthClientHeaderFixture(ctx, authclientbackfill.SeedAuthClientHeaderFixtureParams{
		AuthClientID:           authClientID,
		OrganizationID:         orgID,
		Name:                   headerName,
		IsRequired:             false,
		IsSecret:               true,
		Value:                  pgtype.Text{String: "upstream-secret", Valid: true},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	}))

	return authClientID
}

// copiedHeaderNames lists the live copies on an auth client, which are exactly
// what the serving path sends with the upstream request.
func copiedHeaderNames(t *testing.T, ctx context.Context, ti *testInstance, authClientID uuid.UUID) []string {
	t.Helper()

	copies, err := authclientbackfill.New(ti.conn).ListAuthClientHeadersFixture(ctx, authClientID)
	require.NoError(t, err)

	names := []string{}
	for _, copied := range copies {
		names = append(names, copied.Name)
	}

	return names
}

// Committing a server identity associates that server with the client, so a
// client that reached one source alone now also reaches this server's source
// and must stop carrying the first source's credentials.
func TestCommitServerIdentityConfigurationRetiresCopiesItInvalidates(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "copies-commit-issuer")
	_, soleSource := sourceBackedTarget(t, ctx, ti, "copies-commit-sole", userIssuerID)
	arriving, _ := sourceBackedTarget(t, ctx, ti, "copies-commit-arriving", userIssuerID)
	seedAuxiliaryHeader(t, ctx, ti, soleSource, "X-Tenant-Key")

	providerID := createServerIdentityProvider(t, ctx, ti, "copies-commit-provider", "", false, []string{"client_secret_basic"})
	oauthClientID := createServerIdentityClient(t, ctx, ti, "copies-commit-client", providerID, false)
	authClientID := seedDerivedAuthClient(t, ctx, ti, oauthClientID, "X-Tenant-Key")
	require.Equal(t, []string{"X-Tenant-Key"}, copiedHeaderNames(t, ctx, ti, authClientID))

	// The real writer: configuring the arriving server puts the client on a
	// second source.
	_, err := ti.service.CommitServerIdentityConfiguration(ctx, existingServerIdentityPayload(arriving, providerID, oauthClientID))
	require.NoError(t, err)

	require.Empty(t, copiedHeaderNames(t, ctx, ti, authClientID),
		"a client shared across sources still carries the first source's credentials")
}

// Detaching the server whose source lent the client its headers leaves the
// client reaching other sources only, so the copies go with the association.
func TestDetachServerClientAssociationRetiresCopiesItInvalidates(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	orgID := activeOrganizationID(t, ctx)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "copies-detach-issuer")
	leaving, lendingSource := sourceBackedTarget(t, ctx, ti, "copies-detach-leaving", userIssuerID)
	staying, _ := sourceBackedTarget(t, ctx, ti, "copies-detach-staying", userIssuerID)
	seedAuxiliaryHeader(t, ctx, ti, lendingSource, "X-Tenant-Key")

	providerID := createServerIdentityProvider(t, ctx, ti, "copies-detach-provider", "", false, []string{"client_secret_basic"})
	oauthClientID := createServerIdentityClient(t, ctx, ti, "copies-detach-client", providerID, false)
	associate(t, ctx, ti.conn, orgID, projectID, leaving, oauthClientID)
	associate(t, ctx, ti.conn, orgID, projectID, staying, oauthClientID)

	authClientID := seedDerivedAuthClient(t, ctx, ti, oauthClientID, "X-Tenant-Key")
	require.Equal(t, []string{"X-Tenant-Key"}, copiedHeaderNames(t, ctx, ti, authClientID))

	// The real writer: the server whose source lent the headers goes away.
	require.NoError(t, ti.service.DetachServerClientAssociation(ctx, &gen.DetachServerClientAssociationPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		McpServerID:           leaving.String(),
		RemoteSessionClientID: oauthClientID.String(),
	}))

	require.Empty(t, copiedHeaderNames(t, ctx, ti, authClientID),
		"the client still carries the credentials of the source it no longer reaches")
}
