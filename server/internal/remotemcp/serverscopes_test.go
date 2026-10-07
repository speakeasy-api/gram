package remotemcp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	unproxiedrepo "github.com/speakeasy-api/gram/server/internal/unproxiedmcp/repo"
)

type scopeServer struct {
	mcpServerID         string
	remoteServerID      uuid.UUID
	userSessionIssuerID uuid.UUID
	url                 string
}

func seedScopeServer(t *testing.T, ctx context.Context, ti *testInstance, url string) scopeServer {
	t.Helper()
	result, err := ti.service.CreateServerAndMcpServer(ctx, &gen.CreateServerAndMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                new("Scoped"),
		URL:                 url,
		TransportType:       "streamable-http",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result.McpServer.UserSessionIssuerID)
	return scopeServer{mcpServerID: result.McpServer.ID, remoteServerID: uuid.MustParse(result.RemoteMcpServer.ID), userSessionIssuerID: uuid.MustParse(*result.McpServer.UserSessionIssuerID), url: url}
}

// seedScopeIssuer creates a project authorization server advertising scopesSupported.
func seedScopeIssuer(t *testing.T, ctx context.Context, ti *testInstance, scopesSupported, scopeOverride []string) remotesessionsrepo.RemoteSessionIssuer {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              "as-" + uuid.NewString()[:8],
		Issuer:                            "https://as-" + uuid.NewString()[:8] + ".example.test",
		AuthorizationEndpoint:             conv.ToPGText("https://as.example.test/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://as.example.test/token"),
		ScopesSupported:                   scopesSupported,
		ScopeOverride:                     scopeOverride,
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
	require.NoError(t, err)
	return issuer
}

// seedScopeClient registers a client with its own scope on issuer and binds it to each user session issuer.
func seedScopeClient(t *testing.T, ctx context.Context, ti *testInstance, issuer remotesessionsrepo.RemoteSessionIssuer, scope []string, userSessionIssuerIDs ...uuid.UUID) uuid.UUID {
	t.Helper()
	q := remotesessionsrepo.New(ti.conn)
	client, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             issuer.ProjectID,
		OrganizationID:        issuer.OrganizationID,
		RemoteSessionIssuerID: issuer.ID,
		ClientID:              "client-" + uuid.NewString(),
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
		Scope:                 scope,
	})
	require.NoError(t, err)
	for _, id := range userSessionIssuerIDs {
		require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
			RemoteSessionClientID: client.ID,
			UserSessionIssuerID:   id,
		}))
	}
	return client.ID
}

// recordResource writes a read of the resource's metadata advertising scopes, fetched at fetchedAt.
func recordResource(t *testing.T, ctx context.Context, ti *testInstance, url string, scopes []string, fetchedAt time.Time) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := repo.New(ti.conn)
	_, err := q.UpsertRemoteProtectedResource(ctx, repo.UpsertRemoteProtectedResourceParams{
		ProjectID:              *authCtx.ProjectID,
		OrganizationID:         authCtx.ActiveOrganizationID,
		ResourceIdentifier:     url,
		MetadataUrl:            "",
		AuthorizationServers:   []string{"https://as.example.test"},
		ScopesSupported:        scopes,
		BearerMethodsSupported: nil,
		ResourceName:           "",
		ResourceDocumentation:  "",
		ResourcePolicyUri:      "",
		ResourceTosUri:         "",
		Metadata:               "",
	})
	require.NoError(t, err)
	_, err = q.SetRemoteProtectedResourceMetadataTimestamps(ctx, repo.SetRemoteProtectedResourceMetadataTimestampsParams{
		MetadataFetchedAt:   pgtype.Timestamptz{Time: fetchedAt, Valid: true},
		MetadataLastErrorAt: pgtype.Timestamptz{},
		ProjectID:           *authCtx.ProjectID,
		ResourceIdentifier:  url,
	})
	require.NoError(t, err)
}

func setPin(ctx context.Context, ti *testInstance, mcpServerID string, scopes ...string) (*gen.RemoteMcpServerScopes, error) {
	if scopes == nil {
		scopes = []string{}
	}
	return ti.service.SetServerScopePin(ctx, &gen.SetServerScopePinPayload{McpServerID: mcpServerID, Scopes: scopes, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}) //nolint:wrapcheck // returned for oops code assertions
}

func getScopes(ctx context.Context, ti *testInstance, mcpServerID string) (*gen.RemoteMcpServerScopes, error) {
	return ti.service.GetServerScopes(ctx, &gen.GetServerScopesPayload{McpServerID: mcpServerID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}) //nolint:wrapcheck // returned for oops code assertions
}

func clientEntry(t *testing.T, result *gen.RemoteMcpServerScopes, clientID uuid.UUID) *gen.RemoteMcpServerClientScopes {
	t.Helper()
	for _, c := range result.Clients {
		if c.ClientID == clientID.String() {
			return c
		}
	}
	t.Fatalf("client %s not in result", clientID)
	return nil
}

func enableDiscovery(t *testing.T, ctx context.Context, ti *testInstance, on bool) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.features.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, authCtx.ActiveOrganizationID, on)
}

func storedPin(t *testing.T, ctx context.Context, ti *testInstance, url string) []string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row, err := repo.New(ti.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: *authCtx.ProjectID, ResourceIdentifier: url})
	require.NoError(t, err)
	return row.ScopeOverride
}

func TestSetServerScopePin_CreatesReplacesAndClears(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	srv := seedScopeServer(t, ctx, ti, "https://pin-create.example.com/mcp")

	got, err := setPin(ctx, ti, srv.mcpServerID, " read ", "write", "", "read")
	require.NoError(t, err)
	require.Equal(t, srv.url, got.ResourceURL)
	require.Equal(t, []string{"read", "write"}, got.PinnedScopes, "trimmed, blanks and duplicates dropped, order kept")
	require.Equal(t, []string{"read", "write"}, storedPin(t, ctx, ti, srv.url), "the row is created for a never-read resource")
	require.False(t, got.AdvertisedScopesKnown)
	require.Nil(t, got.AdvertisedScopes)

	got, err = setPin(ctx, ti, srv.mcpServerID, "admin")
	require.NoError(t, err)
	require.Equal(t, []string{"admin"}, got.PinnedScopes)

	got, err = setPin(ctx, ti, srv.mcpServerID)
	require.NoError(t, err)
	require.Empty(t, got.PinnedScopes)
	require.Nil(t, storedPin(t, ctx, ti, srv.url), "an empty list stores NULL")
}

func TestSetServerScopePin_KeepsDiscoveredMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	srv := seedScopeServer(t, ctx, ti, "https://pin-keep.example.com/mcp")
	recordResource(t, ctx, ti, srv.url, []string{"read", "write"}, time.Now())

	got, err := setPin(ctx, ti, srv.mcpServerID, "read")
	require.NoError(t, err)
	require.True(t, got.AdvertisedScopesKnown)
	require.Equal(t, []string{"read", "write"}, got.AdvertisedScopes)

	recordResource(t, ctx, ti, srv.url, []string{"read"}, time.Now())
	require.Equal(t, []string{"read"}, storedPin(t, ctx, ti, srv.url), "discovery leaves the pin untouched")
}

func TestSetServerScopePin_RejectsInvalidScopes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	srv := seedScopeServer(t, ctx, ti, "https://pin-invalid.example.com/mcp")

	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = "s" + uuid.NewString()[:8]
	}
	for name, scopes := range map[string][]string{
		"interior space": {"read write"},
		"quote":          {`re"ad`},
		"backslash":      {`re\ad`},
		"control":        {"read\x01"},
		"non-ascii":      {"lecture-é"},
		"too long":       {strings.Repeat("a", 257)},
		"too many":       tooMany,
	} {
		_, err := setPin(ctx, ti, srv.mcpServerID, scopes...)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr, name)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code, name)
	}
	_, err := getScopes(ctx, ti, srv.mcpServerID)
	require.NoError(t, err)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	_, err = repo.New(ti.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: *authCtx.ProjectID, ResourceIdentifier: srv.url})
	require.Error(t, err, "a rejected pin writes nothing")
}

func TestSetServerScopePin_WritesAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	srv := seedScopeServer(t, ctx, ti, "https://pin-audit.example.com/mcp")

	_, err := setPin(ctx, ti, srv.mcpServerID, "read")
	require.NoError(t, err)
	_, err = setPin(ctx, ti, srv.mcpServerID, "read", "write")
	require.NoError(t, err)

	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerScopePinUpdate)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerScopePinUpdate)
	require.NoError(t, err)
	require.Equal(t, srv.mcpServerID, record.SubjectID)
	require.Equal(t, "mcp_server", record.SubjectType)
	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{"read"}, before["pinned_scopes"])
	require.Equal(t, []any{"read", "write"}, after["pinned_scopes"])
	require.Equal(t, srv.url, metadata["resource_url"])
	require.Equal(t, []any{srv.mcpServerID}, metadata["mcp_server_ids"])
}

func TestGetServerScopes_ReportsResourceAndPerClientResolution(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	enableDiscovery(t, ctx, ti, true)
	srv := seedScopeServer(t, ctx, ti, "https://scopes-a.example.com/mcp")
	other := seedScopeServer(t, ctx, ti, "https://scopes-b.example.com/mcp")
	issuer := seedScopeIssuer(t, ctx, ti, []string{"openid", "iss:read"}, nil)

	own := seedScopeClient(t, ctx, ti, issuer, []string{"own:read"}, srv.userSessionIssuerID)
	owner := seedScopeClient(t, ctx, ti, issuer, nil, srv.userSessionIssuerID)
	// Bound to two servers' issuers, its grant is not qualified to this resource while owner serves it.
	shared := seedScopeClient(t, ctx, ti, issuer, nil, srv.userSessionIssuerID, other.userSessionIssuerID)

	recordResource(t, ctx, ti, srv.url, []string{"read", "write"}, time.Now())
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	_, err := repo.New(ti.conn).RecordRemoteProtectedResourceChallengeScopes(ctx, repo.RecordRemoteProtectedResourceChallengeScopesParams{ChallengeScopes: []string{"challenged"}, ProjectID: *authCtx.ProjectID, ResourceIdentifier: srv.url})
	require.NoError(t, err)
	_, err = setPin(ctx, ti, srv.mcpServerID, "read", "admin")
	require.NoError(t, err)

	got, err := getScopes(ctx, ti, srv.mcpServerID)
	require.NoError(t, err)
	require.Equal(t, srv.url, got.ResourceURL)
	require.True(t, got.DiscoveryEnabled)
	require.Equal(t, []string{"read", "admin"}, got.PinnedScopes)
	require.True(t, got.AdvertisedScopesKnown)
	require.Equal(t, []string{"read", "write"}, got.AdvertisedScopes)
	require.Equal(t, []string{"challenged"}, got.ChallengeScopes)
	require.Len(t, got.Clients, 3)

	c := clientEntry(t, got, own)
	require.Equal(t, "client_scope", c.ScopeSource)
	require.Equal(t, []string{"own:read", "openid"}, c.RequestedScopes)
	require.Empty(t, c.UnadvertisedPinnedScopes)

	// The challenge outranks the pin.
	c = clientEntry(t, got, owner)
	require.Equal(t, "challenge_scope", c.ScopeSource)
	require.Equal(t, []string{"challenged", "openid"}, c.RequestedScopes)

	c = clientEntry(t, got, shared)
	require.Equal(t, "issuer_catalogue", c.ScopeSource, "a client that does not own the resource ignores the pin")
	require.Equal(t, []string{"openid", "iss:read"}, c.RequestedScopes)

	_, err = repo.New(ti.conn).RecordRemoteProtectedResourceChallengeScopes(ctx, repo.RecordRemoteProtectedResourceChallengeScopesParams{ChallengeScopes: nil, ProjectID: *authCtx.ProjectID, ResourceIdentifier: srv.url})
	require.NoError(t, err)
	got, err = getScopes(ctx, ti, srv.mcpServerID)
	require.NoError(t, err)
	require.Empty(t, got.ChallengeScopes)
	c = clientEntry(t, got, owner)
	require.Equal(t, "resource_pin", c.ScopeSource)
	require.Equal(t, []string{"read", "admin", "openid"}, c.RequestedScopes)
	require.Equal(t, []string{"admin"}, c.UnadvertisedPinnedScopes)
}

func TestGetServerScopes_StaleAdvertisedListIsUnknown(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	enableDiscovery(t, ctx, ti, true)
	srv := seedScopeServer(t, ctx, ti, "https://scopes-stale.example.com/mcp")
	issuer := seedScopeIssuer(t, ctx, ti, []string{}, nil)
	owner := seedScopeClient(t, ctx, ti, issuer, nil, srv.userSessionIssuerID)
	recordResource(t, ctx, ti, srv.url, []string{"read"}, time.Now().Add(-8*24*time.Hour))
	_, err := setPin(ctx, ti, srv.mcpServerID, "admin")
	require.NoError(t, err)

	got, err := getScopes(ctx, ti, srv.mcpServerID)
	require.NoError(t, err)
	require.False(t, got.AdvertisedScopesKnown, "a read older than the last good window is not trusted")
	require.Nil(t, got.AdvertisedScopes)
	c := clientEntry(t, got, owner)
	require.Equal(t, "resource_pin", c.ScopeSource)
	require.Empty(t, c.UnadvertisedPinnedScopes, "nothing is flagged against an unknown list")
}

func TestGetServerScopes_FlagOffIgnoresPin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	enableDiscovery(t, ctx, ti, false)
	srv := seedScopeServer(t, ctx, ti, "https://scopes-off.example.com/mcp")
	issuer := seedScopeIssuer(t, ctx, ti, []string{"iss:read"}, []string{"iss:override"})
	owner := seedScopeClient(t, ctx, ti, issuer, []string{"own:read"}, srv.userSessionIssuerID)
	_, err := setPin(ctx, ti, srv.mcpServerID, "read")
	require.NoError(t, err)

	got, err := getScopes(ctx, ti, srv.mcpServerID)
	require.NoError(t, err)
	require.False(t, got.DiscoveryEnabled)
	require.Equal(t, []string{"read"}, got.PinnedScopes)
	c := clientEntry(t, got, owner)
	require.Equal(t, "issuer_override", c.ScopeSource, "without discovery the issuer override beats even the client scope")
	require.Equal(t, []string{"iss:override"}, c.RequestedScopes)
}

func TestServerScopes_RefusesServerWithoutRemoteBackend(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	backend, err := unproxiedrepo.New(ti.conn).CreateServer(ctx, unproxiedrepo.CreateServerParams{
		ID:          uuid.Must(uuid.NewV7()),
		ProjectID:   *authCtx.ProjectID,
		Name:        conv.ToPGText("Unproxied"),
		Slug:        conv.ToPGText("unproxied-" + uuid.NewString()[:8]),
		Url:         "https://unproxied.example.com/mcp",
		Description: pgtype.Text{},
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                   uuid.Must(uuid.NewV7()),
		ProjectID:            *authCtx.ProjectID,
		Name:                 conv.ToPGText("Unproxied"),
		Slug:                 conv.ToPGText("unproxied-" + uuid.NewString()[:8]),
		UnproxiedMcpServerID: conv.ToNullUUID(backend.ID),
		Visibility:           "private",
	})
	require.NoError(t, err)

	_, err = getScopes(ctx, ti, server.ID.String())
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = setPin(ctx, ti, server.ID.String(), "read")
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestServerScopes_CrossProjectServerIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	remote := seedOtherProjectServer(t, ctx, ti)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                uuid.Must(uuid.NewV7()),
		ProjectID:         remote.ProjectID,
		Name:              conv.ToPGText("Other"),
		Slug:              conv.ToPGText("other-" + uuid.NewString()[:8]),
		RemoteMcpServerID: conv.ToNullUUID(remote.ID),
		Visibility:        "private",
	})
	require.NoError(t, err)

	_, err = getScopes(ctx, ti, server.ID.String())
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = setPin(ctx, ti, server.ID.String(), "read")
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = repo.New(ti.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: remote.ProjectID, ResourceIdentifier: remote.Url})
	require.Error(t, err, "nothing is written to the other project")
}

func TestServerScopes_RequiresWriteOnTheServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	srv := seedScopeServer(t, ctx, ti, "https://scopes-rbac.example.com/mcp")
	other := seedScopeServer(t, ctx, ti, "https://scopes-rbac-other.example.com/mcp")
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	readOnly := withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPRead, authCtx.ProjectID.String()),
		authz.NewGrant(authz.ScopeMCPWrite, other.mcpServerID),
	)
	_, err := getScopes(readOnly, ti, srv.mcpServerID)
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = setPin(readOnly, ti, srv.mcpServerID, "read")
	requireOopsCode(t, err, oops.CodeForbidden)

	writer := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, srv.mcpServerID))
	_, err = setPin(writer, ti, srv.mcpServerID, "read")
	require.NoError(t, err)
	_, err = getScopes(writer, ti, srv.mcpServerID)
	require.NoError(t, err)
}

// seedSiblingServer adds a second MCP server on the same remote MCP server row.
func seedSiblingServer(t *testing.T, ctx context.Context, ti *testInstance, srv scopeServer) string {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                uuid.Must(uuid.NewV7()),
		ProjectID:         *authCtx.ProjectID,
		Name:              conv.ToPGText("Sibling"),
		Slug:              conv.ToPGText("sibling-" + uuid.NewString()[:8]),
		RemoteMcpServerID: conv.ToNullUUID(srv.remoteServerID),
		Visibility:        "private",
	})
	require.NoError(t, err)
	return server.ID.String()
}

func auditCount(t *testing.T, ctx context.Context, ti *testInstance) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerScopePinUpdate)
	require.NoError(t, err)
	return count
}

func TestServerScopes_SharedUpstreamRequiresWriteOnEveryServer(t *testing.T) {
	t.Parallel()
	for name, separateRemote := range map[string]bool{"same remote row": false, "separate remote row": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			url := "https://shared-" + uuid.NewString()[:8] + ".example.com/mcp"
			srv := seedScopeServer(t, ctx, ti, url)
			var sibling string
			if separateRemote {
				sibling = seedScopeServer(t, ctx, ti, url).mcpServerID
			} else {
				sibling = seedSiblingServer(t, ctx, ti, srv)
			}

			for _, id := range []string{srv.mcpServerID, sibling} {
				oneOnly := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, id))
				_, err := getScopes(oneOnly, ti, id)
				requireOopsCode(t, err, oops.CodeForbidden)
				_, err = setPin(oneOnly, ti, id, "read")
				requireOopsCode(t, err, oops.CodeForbidden)
			}
			require.Zero(t, auditCount(t, ctx, ti))

			both := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, srv.mcpServerID), authz.NewGrant(authz.ScopeMCPWrite, sibling))
			got, err := setPin(both, ti, srv.mcpServerID, "read")
			require.NoError(t, err)
			require.Equal(t, 1, got.SharedServerCount)
			got, err = getScopes(both, ti, sibling)
			require.NoError(t, err)
			require.Equal(t, []string{"read"}, got.PinnedScopes, "the pin is shared through the resource")
			require.Equal(t, 1, got.SharedServerCount)

			record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerScopePinUpdate)
			require.NoError(t, err)
			metadata, err := audittest.DecodeAuditData(record.Metadata)
			require.NoError(t, err)
			require.ElementsMatch(t, []any{srv.mcpServerID, sibling}, metadata["mcp_server_ids"])
		})
	}
}

func TestGetServerScopes_SharedServerCount(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	srv := seedScopeServer(t, ctx, ti, "https://shared-count.example.com/mcp")
	seedSiblingServer(t, ctx, ti, srv)
	seedScopeServer(t, ctx, ti, srv.url)
	alone := seedScopeServer(t, ctx, ti, "https://shared-count-alone.example.com/mcp")

	got, err := getScopes(ctx, ti, srv.mcpServerID)
	require.NoError(t, err)
	require.Equal(t, 2, got.SharedServerCount)
	got, err = getScopes(ctx, ti, alone.mcpServerID)
	require.NoError(t, err)
	require.Zero(t, got.SharedServerCount)
}

func TestSetServerScopePin_NoOpWritesNothing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	srv := seedScopeServer(t, ctx, ti, "https://pin-noop.example.com/mcp")
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	got, err := setPin(ctx, ti, srv.mcpServerID)
	require.NoError(t, err)
	require.Empty(t, got.PinnedScopes)
	_, err = repo.New(ti.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: *authCtx.ProjectID, ResourceIdentifier: srv.url})
	require.Error(t, err, "clearing a never-set pin creates no row")
	require.Zero(t, auditCount(t, ctx, ti))

	_, err = setPin(ctx, ti, srv.mcpServerID, "read", "write")
	require.NoError(t, err)
	require.EqualValues(t, 1, auditCount(t, ctx, ti))

	got, err = setPin(ctx, ti, srv.mcpServerID, " read", "write", "read")
	require.NoError(t, err)
	require.Equal(t, []string{"read", "write"}, got.PinnedScopes)
	require.EqualValues(t, 1, auditCount(t, ctx, ti), "an unchanged pin writes no audit row")
}

func TestGetServerScopes_CachedResourceWithoutPin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	enableDiscovery(t, ctx, ti, true)
	srv := seedScopeServer(t, ctx, ti, "https://scopes-cached.example.com/mcp")
	issuer := seedScopeIssuer(t, ctx, ti, []string{}, nil)
	owner := seedScopeClient(t, ctx, ti, issuer, nil, srv.userSessionIssuerID)
	recordResource(t, ctx, ti, srv.url, []string{"read", "write"}, time.Now())

	got, err := getScopes(ctx, ti, srv.mcpServerID)
	require.NoError(t, err)
	require.Empty(t, got.PinnedScopes)
	c := clientEntry(t, got, owner)
	require.Equal(t, "cached_resource", c.ScopeSource)
	require.Equal(t, []string{"read", "write"}, c.RequestedScopes)
	require.Empty(t, c.UnadvertisedPinnedScopes)
}

func TestGetServerScopes_FlagOffClientScopeIgnoresPin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	enableDiscovery(t, ctx, ti, false)
	srv := seedScopeServer(t, ctx, ti, "https://scopes-off-client.example.com/mcp")
	issuer := seedScopeIssuer(t, ctx, ti, []string{"iss:read"}, nil)
	owner := seedScopeClient(t, ctx, ti, issuer, []string{"own:read"}, srv.userSessionIssuerID)
	_, err := setPin(ctx, ti, srv.mcpServerID, "read")
	require.NoError(t, err)

	got, err := getScopes(ctx, ti, srv.mcpServerID)
	require.NoError(t, err)
	require.False(t, got.DiscoveryEnabled)
	require.Equal(t, []string{"read"}, got.PinnedScopes)
	c := clientEntry(t, got, owner)
	require.Equal(t, "client_scope", c.ScopeSource)
	require.Equal(t, []string{"own:read"}, c.RequestedScopes)
	require.Empty(t, c.UnadvertisedPinnedScopes)
}
