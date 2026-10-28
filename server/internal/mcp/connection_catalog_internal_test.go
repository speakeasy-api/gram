package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestConnectionCatalogValidationDiagnostics(t *testing.T) {
	t.Parallel()
	upstreamError := errors.New("private upstream error")
	tests := []struct {
		name        string
		change      func(*metaGateContext, *metaMember)
		reason      string
		message     string
		comparisons map[string]any
	}{
		{name: "valid", change: func(*metaGateContext, *metaMember) {}},
		{name: "without catalog", change: func(g *metaGateContext, _ *metaMember) { g.catalog = nil }},
		{name: "cached discovery skips auth comparison", change: func(g *metaGateContext, _ *metaMember) {
			g.execute = false
			g.catalog.Members["source"] = connectionCatalogMember{ServerID: g.catalog.Members["source"].ServerID, ProjectID: g.catalog.Members["source"].ProjectID, BackendHash: "private configuration hash"}
		}},
		{name: "live discovery checks auth", change: func(g *metaGateContext, m *metaMember) {
			g.execute = false
			g.catalogRef.Mode = toolfilter.CatalogModeLive
			m.authClient = nil
		}, reason: "authentication_changed", message: "This server's authentication changed. Give consent again to approve it.", comparisons: map[string]any{"auth_recorded": true, "registration_id_matches": true, "auth_client_id_matches": false}},
		{name: "missing member", change: func(g *metaGateContext, _ *metaMember) { delete(g.catalog.Members, "source") }, reason: "member_not_approved", message: catalogMemberNotApproved, comparisons: map[string]any{"member_recorded": false, "server_id_matches": false, "project_id_matches": false}},
		{name: "changed server", change: func(_ *metaGateContext, m *metaMember) { m.serverID = uuid.New() }, reason: "member_not_approved", message: catalogMemberNotApproved, comparisons: map[string]any{"member_recorded": true, "server_id_matches": false, "project_id_matches": true}},
		{name: "changed project", change: func(_ *metaGateContext, m *metaMember) { m.projectID = uuid.New() }, reason: "member_not_approved", message: catalogMemberNotApproved, comparisons: map[string]any{"member_recorded": true, "server_id_matches": true, "project_id_matches": false}},
		{name: "missing recorded auth", change: func(g *metaGateContext, _ *metaMember) {
			a := g.catalog.Members["source"]
			a.Auth = nil
			g.catalog.Members["source"] = a
		}, reason: "authentication_changed", message: "This server's authentication changed. Give consent again to approve it.", comparisons: map[string]any{"auth_recorded": false, "registration_id_matches": false, "auth_client_id_matches": false}},
		{name: "changed registration", change: func(_ *metaGateContext, m *metaMember) {
			m.chosenClientID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
		}, reason: "authentication_changed", message: "This server's authentication changed. Give consent again to approve it.", comparisons: map[string]any{"auth_recorded": true, "registration_id_matches": false, "auth_client_id_matches": true}},
		{name: "changed auth client", change: func(_ *metaGateContext, m *metaMember) { m.authClient.clientID = uuid.New() }, reason: "authentication_changed", message: "This server's authentication changed. Give consent again to approve it.", comparisons: map[string]any{"auth_recorded": true, "registration_id_matches": true, "auth_client_id_matches": false}},
		{name: "changed configuration", change: func(_ *metaGateContext, m *metaMember) { m.configurationHash = "private changed hash" }, reason: "configuration_changed", message: "This server's configuration changed. Give consent again to approve it.", comparisons: map[string]any{"configuration_hash_present": true, "approved_hash_recorded": true}},
		{name: "missing configuration", change: func(_ *metaGateContext, m *metaMember) { m.configurationHash = "" }, reason: "configuration_changed", message: "This server's configuration changed. Give consent again to approve it.", comparisons: map[string]any{"configuration_hash_present": false, "approved_hash_recorded": true}},
		{name: "missing approved configuration", change: func(g *metaGateContext, _ *metaMember) {
			a := g.catalog.Members["source"]
			a.BackendHash = ""
			g.catalog.Members["source"] = a
		}, reason: "configuration_changed", message: "This server's configuration changed. Give consent again to approve it.", comparisons: map[string]any{"configuration_hash_present": true, "approved_hash_recorded": false}},
		{name: "upstream unavailable", change: func(_ *metaGateContext, m *metaMember) { m.upstreamUnavailable = upstreamError }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			s := &Service{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			member := metaMember{
				serverID: uuid.New(), projectID: uuid.New(), slug: "source", name: "private server name",
				backend: metaMemberBackendProxied, configurationHash: "private configuration hash",
				chosenClientID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, authClient: &authClientResolution{clientID: uuid.New()},
			}
			gate := &metaGateContext{
				projectID: uuid.New(), metaServerID: uuid.New(), execute: true, userID: "private user", sessionID: "private session",
				catalogRef: &toolfilter.CatalogReference{Mode: toolfilter.CatalogModeCached},
				catalog: &connectionCatalog{ID: uuid.New(), ClientID: "private OAuth client", Subject: urn.NewUserSubject("private subject"), Members: map[string]connectionCatalogMember{
					"source": {ServerID: member.serverID, ProjectID: member.projectID, BackendHash: member.configurationHash, Auth: &connectionCatalogAuth{ClientID: member.authClient.clientID, RegistrationID: member.chosenClientID}},
				}},
			}
			tt.change(gate, &member)
			err := s.validateCatalogMember(t.Context(), gate, member)
			if tt.reason == "" {
				if member.upstreamUnavailable != nil {
					require.ErrorIs(t, err, upstreamError)
				} else {
					require.NoError(t, err)
				}
				require.Empty(t, logs.String(), "success and unrelated failures must stay quiet")
				return
			}
			var consent catalogConsentError
			require.ErrorAs(t, err, &consent)
			require.EqualError(t, err, tt.message)
			entry := decodeLogEntry(t, &logs)
			require.Equal(t, "INFO", entry["level"])
			require.Equal(t, "gateway consent validation failed", entry["msg"])
			require.Equal(t, tt.reason, entry[string(attr.ReasonKey)])
			require.Equal(t, gate.metaServerID.String(), entry[string(attr.MetaMcpServerIDKey)])
			require.Equal(t, member.serverID.String(), entry[string(attr.McpServerIDKey)])
			require.Equal(t, gate.projectID.String(), entry[string(attr.ProjectIDKey)])
			require.Equal(t, member.projectID.String(), entry[string(attr.McpConsentMemberProjectIDKey)])
			require.Equal(t, gate.catalog.ID.String(), entry[string(attr.McpConsentCatalogIDKey)])
			require.Equal(t, string(gate.catalogRef.Mode), entry[string(attr.McpConsentModeKey)])
			require.Equal(t, gate.execute, entry[string(attr.McpConsentExecuteKey)])
			require.Equal(t, tt.comparisons, entry[string(attr.McpConsentComparisonsKey)])
			require.Len(t, entry, 12, "only bounded diagnostic fields may be logged")
			require.NotContains(t, logs.String(), "private", "never log configuration, user/session data, or upstream errors")
		})
	}
}

func TestConnectionCatalogDiagnosticsDeduplicateConcurrentGateCopies(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	s := &Service{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	gate := metaGateContext{catalog: &connectionCatalog{ID: uuid.New(), Members: map[string]connectionCatalogMember{}}, consentDiagnostics: &consentDiagnostics{seen: sync.Map{}}}
	member := metaMember{serverID: uuid.New()}
	var calls sync.WaitGroup
	results := make(chan error, 32)
	for range cap(results) {
		calls.Go(func() {
			copiedGate := gate
			results <- s.validateCatalogMember(t.Context(), &copiedGate, member)
		})
	}
	calls.Wait()
	close(results)
	for err := range results {
		require.EqualError(t, err, catalogMemberNotApproved)
	}
	require.Equal(t, 1, bytes.Count(logs.Bytes(), []byte("\n")))
	// Different members and reasons remain distinguishable within an execution.
	other := member
	other.serverID = uuid.New()
	require.Error(t, s.validateCatalogMember(t.Context(), &gate, other))
	require.Error(t, s.rejectCatalogMember(t.Context(), &gate, member, "configuration_changed", "changed", nil))
	require.Equal(t, 3, bytes.Count(logs.Bytes(), []byte("\n")))
	// A fresh execution reports the still-stale member again.
	gate.consentDiagnostics = &consentDiagnostics{seen: sync.Map{}}
	require.Error(t, s.validateCatalogMember(t.Context(), &gate, member))
	require.Equal(t, 4, bytes.Count(logs.Bytes(), []byte("\n")))
}

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
