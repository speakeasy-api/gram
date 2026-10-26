package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestConnectionCatalogExpiryAndIsolation(t *testing.T) {
	t.Parallel()
	now := time.Now()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	service := &Service{connectionCatalogCache: cache.NewRedisCacheAdapter(client)}
	endpoint := &ResolvedMcpEndpoint{OrganizationID: "org_test", UserSessionIssuerID: uuid.New(), Slug: "gateway", RouteBase: mcproute.MCP, MetaMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	subject := urn.NewUserSubject("test-user")
	ctx := contextvalues.SetOAuthClientID(t.Context(), "test-client")
	catalog := connectionCatalog{Version: connectionCatalogVersion, ID: uuid.New(), OrganizationID: endpoint.OrganizationID, IssuerID: endpoint.UserSessionIssuerID, Resource: endpointToolSelectionResource(endpoint), ClientID: "test-client", Subject: subject, Members: map[string]connectionCatalogMember{}, Tools: []*toolListEntry{}, ExpiresAt: now.Add(time.Hour)}
	raw, err := json.Marshal(catalog)
	require.NoError(t, err)
	conditional, ok := service.connectionCatalogCache.(cache.ConditionalCache)
	require.True(t, ok)
	stored, err := conditional.SetIfAbsent(ctx, catalog.CacheKey(), raw, catalog.ExpiresAt.Sub(now))
	require.NoError(t, err)
	require.True(t, stored)
	reference := &toolfilter.CatalogReference{ID: catalog.ID, Mode: toolfilter.CatalogModeCached, AllTools: true}
	loaded, err := service.loadConnectionCatalog(ctx, endpoint, subject, reference, now)
	require.NoError(t, err)
	require.Equal(t, catalog.ID, loaded.ID)
	require.Equal(t, []string{catalog.CacheKey()}, mr.Keys())
	require.Equal(t, time.Hour, mr.TTL(catalog.CacheKey()))

	require.Equal(t, string(raw), mrGet(t, mr, catalog.CacheKey()), "store the JSON bytes without a second codec")
	for _, corrupt := range []string{"{invalid", `{"version":999}`, "\xc1\x00"} {
		require.NoError(t, service.connectionCatalogCache.Set(ctx, catalog.CacheKey(), []byte(corrupt), time.Hour))
		_, err = service.loadConnectionCatalog(ctx, endpoint, subject, reference, now)
		require.ErrorIs(t, err, errConnectionCatalogExpired)
	}
	require.NoError(t, service.connectionCatalogCache.Set(ctx, catalog.CacheKey(), raw, time.Hour))
	mr.SetError("catalog store unavailable")
	_, err = service.loadConnectionCatalog(ctx, endpoint, subject, reference, now)
	require.Error(t, err)
	require.NotErrorIs(t, err, errConnectionCatalogExpired)
	mr.SetError("")

	// The session owner validates request authority. The catalog binds the
	// connection identity, so resuming consent on another surface cannot pin
	// that surface onto later discovery requests.
	mintAuthority := networkingress.Authority{Surface: requestorigin.SurfacePrivateNetwork, BaseURL: "https://gateway.example.test", OrganizationID: endpoint.OrganizationID, NetworkIngressID: uuid.New(), NamespaceKind: networkingress.NamespacePlatform}
	continuation := requestorigin.WithContext(ctx, requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: "https://auth.example.test"})
	require.Error(t, mintAuthority.ValidateRequest(continuation))
	loaded, err = service.loadConnectionCatalog(continuation, endpoint, subject, reference, now)
	require.NoError(t, err)
	require.Equal(t, catalog.ID, loaded.ID)
	require.NotContains(t, string(raw), `"endpoint"`)

	wrongOrg := *endpoint
	wrongOrg.OrganizationID = "org_other"
	_, err = service.loadConnectionCatalog(ctx, &wrongOrg, subject, reference, now)
	require.ErrorIs(t, err, errConnectionCatalogExpired)
	wrongIssuer := *endpoint
	wrongIssuer.UserSessionIssuerID = uuid.New()
	_, err = service.loadConnectionCatalog(ctx, &wrongIssuer, subject, reference, now)
	require.ErrorIs(t, err, errConnectionCatalogExpired)
	wrongResource := *endpoint
	wrongResource.MetaMcpServerID.UUID = uuid.New()
	_, err = service.loadConnectionCatalog(ctx, &wrongResource, subject, reference, now)
	require.ErrorIs(t, err, errConnectionCatalogExpired)
	_, err = service.loadConnectionCatalog(ctx, endpoint, urn.NewUserSubject("another-user"), reference, now)
	require.ErrorIs(t, err, errConnectionCatalogExpired)
	_, err = service.loadConnectionCatalog(contextvalues.SetOAuthClientID(ctx, "other-client"), endpoint, subject, reference, now)
	require.ErrorIs(t, err, errConnectionCatalogExpired)
	_, err = service.loadConnectionCatalog(ctx, endpoint, subject, reference, catalog.ExpiresAt)
	require.ErrorIs(t, err, errConnectionCatalogExpired)

	mr.FastForward(time.Hour)
	_, err = service.loadConnectionCatalog(ctx, endpoint, subject, reference, now)
	require.ErrorIs(t, err, errConnectionCatalogExpired, "eviction cannot fall back to upstream discovery")
}

func mrGet(t *testing.T, mr *miniredis.Miniredis, key string) string {
	t.Helper()
	value, err := mr.Get(key)
	require.NoError(t, err)
	return value
}

// A derived gateway qualifies a slug only while another member shares it, so
// approval is matched by server and project identity and each approved member
// keeps the slug its stored tool names use.
func TestConnectionCatalogApprovedMembersMatchStableIdentity(t *testing.T) {
	t.Parallel()
	projectA, projectB := uuid.New(), uuid.New()
	docs, wiki, newcomer, moved := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	catalog := &connectionCatalog{Members: map[string]connectionCatalogMember{
		"docs":   {ServerID: docs, ProjectID: projectA},
		"b.wiki": {ServerID: wiki, ProjectID: projectB},
		"moved":  {ServerID: moved, ProjectID: projectA},
	}}
	current := []metaMember{
		// Another project added "docs" after consent, qualifying both.
		{serverID: docs, projectID: projectA, slug: "a.docs"},
		{serverID: newcomer, projectID: projectB, slug: "b.docs"},
		// The collision that qualified "wiki" at consent is gone.
		{serverID: wiki, projectID: projectB, slug: "wiki"},
		// The same server now resolves in another project.
		{serverID: moved, projectID: projectB, slug: "moved"},
	}
	approved, unapproved := catalog.approvedMembers(current)
	slugs := map[uuid.UUID]string{}
	for _, member := range approved {
		slugs[member.serverID] = member.slug
	}
	require.Equal(t, map[uuid.UUID]string{docs: "docs", wiki: "b.wiki"}, slugs)
	require.Len(t, unapproved, 2)
	require.Equal(t, newcomer, unapproved[0].serverID)
	require.Equal(t, "b.docs", unapproved[0].slug, "status reports an unapproved member by its current slug")
	require.Equal(t, moved, unapproved[1].serverID)
}
