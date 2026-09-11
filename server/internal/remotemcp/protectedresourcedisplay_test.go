package remotemcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// attachedResource is what the Platform MCP attachment leaves behind for one
// remote MCP server: a registration bound to a user-session issuer, and one
// client on an issuer bound to it, carrying the resource's display members.
type attachedResource struct {
	userSessionIssuerID uuid.UUID
	issuer              remotesessionsrepo.RemoteSessionIssuer
	client              remotesessionsrepo.RemoteSessionClient
}

// seedAttachedResource mirrors the attachment's rows for server. The issuer
// slug is deliberately not the attachment-derived one: nothing may depend on
// it. resourceIdentifier is what the client recorded its display for.
func seedAttachedResource(t *testing.T, ctx context.Context, ti *testInstance, server repo.RemoteMcpServer, resourceIdentifier string) attachedResource {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	usi, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		OrganizationID:     conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)

	registrations := platformrepo.New(ti.conn)
	registration, err := registrations.CreatePlatformMCPCatalogRegistration(ctx, platformrepo.CreatePlatformMCPCatalogRegistrationParams{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            projectID,
		SourceKind:           "remote_mcp",
		CatalogProvider:      "direct-remote-url-v1",
		CatalogReference:     server.Url,
		Status:               "registered",
		ConnectionID:         uuid.NullUUID{},
		ConnectionGeneration: uuid.NullUUID{},
		UserID:               conv.ToPGText(authCtx.UserID),
		ActingSurface:        conv.ToPGText("dashboard"),
	})
	require.NoError(t, err)
	_, err = registrations.UpdatePlatformMCPCatalogRegistrationComponents(ctx, platformrepo.UpdatePlatformMCPCatalogRegistrationComponentsParams{
		Status:                 "registered",
		RemoteMcpServerID:      conv.ToNullUUID(server.ID),
		RemoteMcpServerOwned:   true,
		UserSessionIssuerID:    conv.ToNullUUID(usi.ID),
		UserSessionIssuerOwned: true,
		McpServerID:            uuid.NullUUID{},
		McpServerOwned:         false,
		McpEndpointID:          uuid.NullUUID{},
		McpEndpointOwned:       false,
		ID:                     registration.ID,
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              projectID,
	})
	require.NoError(t, err)

	q := remotesessionsrepo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(projectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              "renamed-provider-" + uuid.NewString()[:8],
		Issuer:                            "https://auth.example.test",
		Name:                              conv.ToPGText("Remote identity provider"),
		AuthorizationEndpoint:             conv.ToPGText("https://auth.example.test/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://auth.example.test/token"),
		RegistrationEndpoint:              conv.ToPGText("https://auth.example.test/register"),
		OpPolicyUri:                       conv.ToPGText("https://auth.example.test/as-policy"),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
	require.NoError(t, err)
	client := seedIssuerClient(t, ctx, ti, issuer, usi.ID, resourceIdentifier)
	return attachedResource{userSessionIssuerID: usi.ID, issuer: issuer, client: client}
}

// seedIssuerClient registers one client on issuer, binds it to the
// user-session issuer, and records stale display members for
// resourceIdentifier (none when it is empty).
func seedIssuerClient(t *testing.T, ctx context.Context, ti *testInstance, issuer remotesessionsrepo.RemoteSessionIssuer, userSessionIssuerID uuid.UUID, resourceIdentifier string) remotesessionsrepo.RemoteSessionClient {
	t.Helper()
	q := remotesessionsrepo.New(ti.conn)
	client, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             issuer.ProjectID,
		OrganizationID:        issuer.OrganizationID,
		RemoteSessionIssuerID: issuer.ID,
		ClientID:              "client-" + uuid.NewString(),
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userSessionIssuerID,
	}))
	if resourceIdentifier == "" {
		return client
	}
	client, err = q.UpdateRemoteSessionClientResourceDisplay(ctx, remotesessionsrepo.UpdateRemoteSessionClientResourceDisplayParams{
		ResourceIdentifier:    conv.ToPGText(resourceIdentifier),
		ResourceName:          "Stale MCP",
		ResourceDocumentation: "",
		ResourcePolicyUri:     "https://stale.example.test/policy",
		ResourceTosUri:        "",
		ID:                    client.ID,
		ProjectID:             issuer.ProjectID,
	})
	require.NoError(t, err)
	return client
}

func loadClient(t *testing.T, ctx context.Context, ti *testInstance, client remotesessionsrepo.RemoteSessionClient) remotesessionsrepo.RemoteSessionClient {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row, err := remotesessionsrepo.New(ti.conn).GetRemoteSessionClientByID(ctx, remotesessionsrepo.GetRemoteSessionClientByIDParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             client.ID,
	})
	require.NoError(t, err)
	return row.RemoteSessionClient
}

func requireStaleDisplay(t *testing.T, client remotesessionsrepo.RemoteSessionClient, resourceIdentifier string) {
	t.Helper()
	require.Equal(t, resourceIdentifier, conv.FromPGTextOrEmpty[string](client.ResourceIdentifier))
	require.Equal(t, "Stale MCP", conv.FromPGTextOrEmpty[string](client.ResourceName))
	require.Equal(t, "https://stale.example.test/policy", conv.FromPGTextOrEmpty[string](client.ResourcePolicyUri))
	require.False(t, client.ResourceDocumentation.Valid)
}

func updateServerURL(t *testing.T, ctx context.Context, ti *testInstance, server repo.RemoteMcpServer, url string) {
	t.Helper()
	_, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               server.ID.String(),
		URL:              new(url),
		TransportType:    nil,
	})
	require.NoError(t, err)
}

func launchResourceMetadata(t *testing.T, name, documentation, tos string) (*httptest.Server, *wellknown.OAuthProtectedResourceMetadata) {
	t.Helper()
	metadata := &wellknown.OAuthProtectedResourceMetadata{
		Resource:               "",
		AuthorizationServers:   []string{"https://auth.example.test"},
		ScopesSupported:        nil,
		BearerMethodsSupported: nil,
		ResourceDocumentation:  documentation,
		ResourceName:           name,
		ResourcePolicyURI:      "",
		ResourceTosURI:         tos,
		Raw:                    nil,
	}
	upstream := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{Metadata: metadata, StatusCode: 0, Body: nil})
	metadata.Resource = upstream.URL
	return upstream, metadata
}

// The re-probe finds the resource's client through its registration, not
// through the issuer's slug, and rewrites that client's display members.
func TestUpdateServer_ReprobesResourceDisplayOnClientRegardlessOfIssuerSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))
	upstream, _ := launchResourceMetadata(t, "Example MCP", "https://docs.example.test/mcp", "https://example.test/tos")

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://old.example.test")
	attached := seedAttachedResource(t, ctx, ti, server, "https://old.example.test")

	beforeAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)
	updateServerURL(t, ctx, ti, server, upstream.URL)

	client := loadClient(t, ctx, ti, attached.client)
	require.Equal(t, upstream.URL, conv.FromPGTextOrEmpty[string](client.ResourceIdentifier))
	require.Equal(t, "Example MCP", conv.FromPGTextOrEmpty[string](client.ResourceName))
	require.Equal(t, "https://docs.example.test/mcp", conv.FromPGTextOrEmpty[string](client.ResourceDocumentation))
	require.False(t, client.ResourcePolicyUri.Valid, "a link the resource no longer advertises clears")
	require.Equal(t, "https://example.test/tos", conv.FromPGTextOrEmpty[string](client.ResourceTosUri))

	issuer, err := remotesessionsrepo.New(ti.conn).GetRemoteSessionIssuerBySlug(ctx, remotesessionsrepo.GetRemoteSessionIssuerBySlugParams{Slug: attached.issuer.Slug, ProjectID: attached.issuer.ProjectID})
	require.NoError(t, err)
	require.Equal(t, "Remote identity provider", issuer.Name.String, "the issuer row is authorization-server data only")
	require.Equal(t, "https://auth.example.test/as-policy", conv.FromPGTextOrEmpty[string](issuer.OpPolicyUri))

	afterAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit+2, afterAudit, "one row for the claim on the new URL, one for the members discovery fills")
}

// A client that already shows what the document advertises is neither
// written nor audited.
func TestUpdateServer_ReprobeUnchangedDocumentWritesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))
	upstream, _ := launchResourceMetadata(t, "Example MCP", "https://docs.example.test/mcp", "")

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://old.example.test")
	attached := seedAttachedResource(t, ctx, ti, server, "")
	_, err := remotesessionsrepo.New(ti.conn).UpdateRemoteSessionClientResourceDisplay(ctx, remotesessionsrepo.UpdateRemoteSessionClientResourceDisplayParams{
		ResourceIdentifier:    conv.ToPGText(upstream.URL),
		ResourceName:          "Example MCP",
		ResourceDocumentation: "https://docs.example.test/mcp",
		ResourcePolicyUri:     "",
		ResourceTosUri:        "",
		ID:                    attached.client.ID,
		ProjectID:             attached.client.ProjectID,
	})
	require.NoError(t, err)

	beforeAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)
	updateServerURL(t, ctx, ti, server, upstream.URL)
	afterAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit, afterAudit)
	require.Equal(t, "Example MCP", conv.FromPGTextOrEmpty[string](loadClient(t, ctx, ti, attached.client).ResourceName))
}

// A client that never recorded a resource is still the registration's only
// client, so it takes the first probe.
func TestUpdateServer_ReprobeFillsClientWithoutRecordedResource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))
	upstream, _ := launchResourceMetadata(t, "Example MCP", "https://docs.example.test/mcp", "")

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://old.example.test")
	attached := seedAttachedResource(t, ctx, ti, server, "")
	updateServerURL(t, ctx, ti, server, upstream.URL)

	client := loadClient(t, ctx, ti, attached.client)
	require.Equal(t, "Example MCP", conv.FromPGTextOrEmpty[string](client.ResourceName))
	require.Equal(t, upstream.URL, conv.FromPGTextOrEmpty[string](client.ResourceIdentifier))
}

// A save that leaves the URL alone never dials the upstream.
func TestUpdateServer_NoURLChangeSkipsReprobe(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	var probed atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probed.Store(true)
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)
	attached := seedAttachedResource(t, ctx, ti, server, upstream.URL)

	_, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               server.ID.String(),
		URL:              new(upstream.URL),
		TransportType:    new("sse"),
	})
	require.NoError(t, err)
	_, err = ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               server.ID.String(),
		URL:              nil,
		TransportType:    new("streamable-http"),
	})
	require.NoError(t, err)

	require.False(t, probed.Load(), "a save that keeps the URL must not dial the upstream")
	requireStaleDisplay(t, loadClient(t, ctx, ti, attached.client), upstream.URL)
}

// requireClaimWithoutDisplay asserts the client records resourceIdentifier
// with nothing to show: what a failed or mismatched probe leaves behind.
func requireClaimWithoutDisplay(t *testing.T, client remotesessionsrepo.RemoteSessionClient, resourceIdentifier string) {
	t.Helper()
	require.Equal(t, resourceIdentifier, conv.FromPGTextOrEmpty[string](client.ResourceIdentifier))
	require.False(t, client.ResourceName.Valid)
	require.False(t, client.ResourceDocumentation.Valid)
	require.False(t, client.ResourcePolicyUri.Valid)
	require.False(t, client.ResourceTosUri.Valid)
}

// A failed probe replaces what the client recorded for the old URL — its
// name and links are not the new resource's — with a claim on the new URL
// that shows nothing, and the server update still lands.
func TestUpdateServer_FailedReprobeClearsStaleDisplay(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))
	upstream := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{Metadata: nil, StatusCode: 0, Body: nil})

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://old.example.test")
	attached := seedAttachedResource(t, ctx, ti, server, "https://old.example.test")

	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               server.ID.String(),
		URL:              new(upstream.URL),
		TransportType:    nil,
	})
	require.NoError(t, err)
	require.Equal(t, upstream.URL, updated.URL)
	requireClaimWithoutDisplay(t, loadClient(t, ctx, ti, attached.client), upstream.URL)
}

// Discovery fails moving A to B, then recovers: saving B again (URL
// unchanged) re-probes, and B's metadata replaces A's rather than A's
// pinning the client.
func TestUpdateServer_ReprobeRetriesOnResaveAfterFailedMove(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	var origin string
	var available atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available.Load() || r.URL.Path != wellknown.OAuthProtectedResourcePath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              origin,
			"authorization_servers": []string{"https://auth.example.test"},
			"resource_name":         "Resource B",
			"resource_tos_uri":      "https://b.example.test/tos",
		})
	}))
	t.Cleanup(upstream.Close)
	origin = upstream.URL

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://a.example.test/mcp")
	attached := seedAttachedResource(t, ctx, ti, server, "https://a.example.test/mcp")

	updateServerURL(t, ctx, ti, server, origin)
	requireClaimWithoutDisplay(t, loadClient(t, ctx, ti, attached.client), origin)

	available.Store(true)
	updateServerURL(t, ctx, ti, server, origin)

	client := loadClient(t, ctx, ti, attached.client)
	require.Equal(t, origin, conv.FromPGTextOrEmpty[string](client.ResourceIdentifier))
	require.Equal(t, "Resource B", conv.FromPGTextOrEmpty[string](client.ResourceName))
	require.Equal(t, "https://b.example.test/tos", conv.FromPGTextOrEmpty[string](client.ResourceTosUri))
	require.False(t, client.ResourcePolicyUri.Valid)
}

// With two clients bound to the registration, a failed move leaves both
// claiming the new URL with nothing to show; once discovery recovers, saving
// again fills both — an empty identifier would have left neither selectable.
func TestUpdateServer_FailedMoveRecoversEveryBoundClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	var origin string
	var available atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available.Load() || r.URL.Path != wellknown.OAuthProtectedResourcePath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              origin,
			"authorization_servers": []string{"https://auth.example.test"},
			"resource_name":         "Resource B",
			"resource_policy_uri":   "https://b.example.test/policy",
		})
	}))
	t.Cleanup(upstream.Close)
	origin = upstream.URL

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://a.example.test/mcp")
	attached := seedAttachedResource(t, ctx, ti, server, "https://a.example.test/mcp")
	second := seedIssuerClient(t, ctx, ti, attached.issuer, attached.userSessionIssuerID, "https://a.example.test/mcp")

	updateServerURL(t, ctx, ti, server, origin)
	requireClaimWithoutDisplay(t, loadClient(t, ctx, ti, attached.client), origin)
	requireClaimWithoutDisplay(t, loadClient(t, ctx, ti, second), origin)

	available.Store(true)
	updateServerURL(t, ctx, ti, server, origin)

	for _, seeded := range []remotesessionsrepo.RemoteSessionClient{attached.client, second} {
		client := loadClient(t, ctx, ti, seeded)
		require.Equal(t, origin, conv.FromPGTextOrEmpty[string](client.ResourceIdentifier))
		require.Equal(t, "Resource B", conv.FromPGTextOrEmpty[string](client.ResourceName))
		require.Equal(t, "https://b.example.test/policy", conv.FromPGTextOrEmpty[string](client.ResourcePolicyUri))
	}
}

// For /mcp/b, a path-level 404 followed by origin metadata that names /mcp/a
// must not persist A's name or links for B; what the client recorded for the
// old URL gives way to a claim on B with nothing to show.
func TestUpdateServer_MismatchedResourceMetadataDoesNotOverwriteLinks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	var origin string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellknown.OAuthProtectedResourcePath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              origin + "/mcp/a",
			"authorization_servers": []string{"https://auth.example.test"},
			"resource_name":         "Resource A",
			"resource_policy_uri":   "https://a.example.test/policy",
		})
	}))
	t.Cleanup(upstream.Close)
	origin = upstream.URL

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://old.example.test")
	attached := seedAttachedResource(t, ctx, ti, server, "https://old.example.test")
	updateServerURL(t, ctx, ti, server, origin+"/mcp/b")

	client := loadClient(t, ctx, ti, attached.client)
	requireClaimWithoutDisplay(t, client, origin+"/mcp/b")
	require.NotEqual(t, "Resource A", conv.FromPGTextOrEmpty[string](client.ResourceName))
}

// A second client bound to the same user-session issuer for another resource
// keeps its own display members when this server's URL changes.
func TestUpdateServer_ReprobeLeavesOtherResourceClientUntouched(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))
	upstream, _ := launchResourceMetadata(t, "Resource A", "https://a.example.test/docs", "")

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://a.example.test/mcp")
	attached := seedAttachedResource(t, ctx, ti, server, "https://a.example.test/mcp")
	other := seedIssuerClient(t, ctx, ti, attached.issuer, attached.userSessionIssuerID, "https://b.example.test/mcp")

	updateServerURL(t, ctx, ti, server, upstream.URL)

	require.Equal(t, "Resource A", conv.FromPGTextOrEmpty[string](loadClient(t, ctx, ti, attached.client).ResourceName))
	requireStaleDisplay(t, loadClient(t, ctx, ti, other), "https://b.example.test/mcp")
}

// Moving A to A/ (a different resource, trailing slash aside) and then to B:
// each URL claims the client exactly and obtains its own metadata. A
// slash-insensitive match would skip the populated client on A/ and leave it
// recording A, where the later move to B does not find it.
func TestUpdateServer_TrailingSlashVariantIsAnotherResource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	var origin string
	var served atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resource := served.Load()
		if resource == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              *resource,
			"authorization_servers": []string{"https://auth.example.test"},
			"resource_name":         "Resource " + strings.TrimPrefix(*resource, origin),
		})
	}))
	t.Cleanup(upstream.Close)
	origin = upstream.URL

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://old.example.test")
	attached := seedAttachedResource(t, ctx, ti, server, "https://old.example.test")

	for _, step := range []string{origin + "/mcp", origin + "/mcp/", origin + "/b"} {
		served.Store(new(step))
		updateServerURL(t, ctx, ti, server, step)
		client := loadClient(t, ctx, ti, attached.client)
		require.Equal(t, step, conv.FromPGTextOrEmpty[string](client.ResourceIdentifier))
		require.Equal(t, "Resource "+strings.TrimPrefix(step, origin), conv.FromPGTextOrEmpty[string](client.ResourceName))
	}
}

// Overlapping moves: A to B commits and B's probe stalls; B to C commits and
// fills C's metadata while B is still in flight. The claim on C landed with
// the URL, so C's save found the client; B's late result is discarded.
func TestUpdateServer_OverlappingMoveKeepsOwnershipWithLatestURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	probeStarted := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var originB string
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(probeStarted) })
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              originB,
			"authorization_servers": []string{"https://auth.example.test"},
			"resource_name":         "Resource B",
		})
	}))
	t.Cleanup(upstreamB.Close)
	var releaseOnce sync.Once
	releaseB := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseB)
	originB = upstreamB.URL
	upstreamC, _ := launchResourceMetadata(t, "Resource C", "https://c.example.test/docs", "")

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://a.example.test/mcp")
	attached := seedAttachedResource(t, ctx, ti, server, "https://a.example.test/mcp")

	moveToB := make(chan error, 1)
	go func() {
		_, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			ID:               server.ID.String(),
			URL:              new(originB),
			TransportType:    nil,
		})
		moveToB <- err
	}()
	select {
	case <-probeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("B's probe never started")
	}
	requireClaimWithoutDisplay(t, loadClient(t, ctx, ti, attached.client), originB)

	updateServerURL(t, ctx, ti, server, upstreamC.URL)
	releaseB()
	require.NoError(t, <-moveToB)

	client := loadClient(t, ctx, ti, attached.client)
	require.Equal(t, upstreamC.URL, conv.FromPGTextOrEmpty[string](client.ResourceIdentifier))
	require.Equal(t, "Resource C", conv.FromPGTextOrEmpty[string](client.ResourceName))
	require.Equal(t, "https://c.example.test/docs", conv.FromPGTextOrEmpty[string](client.ResourceDocumentation))
}

// Two saves overlap on A: A to B holds the update transaction open while A to
// C starts. The row lock serializes them, so the second reads B as the
// previous URL and moves the client B's claim left on to C rather than
// searching for a client still recording A.
func TestUpdateServer_OverlappingSavesReadPreviousURLUnderLock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	upstreamB := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{Metadata: nil, StatusCode: 0, Body: nil})
	upstreamC, _ := launchResourceMetadata(t, "Resource C", "https://c.example.test/docs", "")

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://a.example.test/mcp")
	attached := seedAttachedResource(t, ctx, ti, server, "https://a.example.test/mcp")

	firstClaiming := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var previous []string
	var calls atomic.Int32
	ti.service.SetBeforeClaim(func(previousURL string) {
		mu.Lock()
		previous = append(previous, previousURL)
		mu.Unlock()
		if calls.Add(1) == 1 {
			close(firstClaiming)
			<-release
		}
	})

	moveToB := make(chan error, 1)
	go func() {
		_, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			ID:               server.ID.String(),
			URL:              new(upstreamB.URL),
			TransportType:    nil,
		})
		moveToB <- err
	}()
	select {
	case <-firstClaiming:
	case <-time.After(5 * time.Second):
		t.Fatal("first save never reached the claim")
	}

	moveToC := make(chan error, 1)
	go func() {
		_, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			ID:               server.ID.String(),
			URL:              new(upstreamC.URL),
			TransportType:    nil,
		})
		moveToC <- err
	}()
	testenv.WaitForBlockedBackend(t, ctx, ti.conn)
	close(release)
	require.NoError(t, <-moveToB)
	require.NoError(t, <-moveToC)

	mu.Lock()
	require.Equal(t, []string{"https://a.example.test/mcp", upstreamB.URL}, previous)
	mu.Unlock()

	client := loadClient(t, ctx, ti, attached.client)
	require.Equal(t, upstreamC.URL, conv.FromPGTextOrEmpty[string](client.ResourceIdentifier))
	require.Equal(t, "Resource C", conv.FromPGTextOrEmpty[string](client.ResourceName))
}

// A bound client deleted between the claim's list and its re-read is stale,
// not a failed save: the move lands and the surviving client is claimed.
func TestUpdateServer_ClientDeletedDuringClaimIsSkipped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))
	upstream, _ := launchResourceMetadata(t, "Resource B", "", "https://b.example.test/tos")

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://a.example.test/mcp")
	attached := seedAttachedResource(t, ctx, ti, server, "https://a.example.test/mcp")
	doomed := seedIssuerClient(t, ctx, ti, attached.issuer, attached.userSessionIssuerID, "https://a.example.test/mcp")

	ti.service.SetBeforeClaim(func(string) {
		_, err := remotesessionsrepo.New(ti.conn).DeleteRemoteSessionClient(ctx, remotesessionsrepo.DeleteRemoteSessionClientParams{
			ID:        doomed.ID,
			ProjectID: doomed.ProjectID,
		})
		require.NoError(t, err)
	})

	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               server.ID.String(),
		URL:              new(upstream.URL),
		TransportType:    nil,
	})
	require.NoError(t, err)
	require.Equal(t, upstream.URL, updated.URL)

	client := loadClient(t, ctx, ti, attached.client)
	require.Equal(t, upstream.URL, conv.FromPGTextOrEmpty[string](client.ResourceIdentifier))
	require.Equal(t, "Resource B", conv.FromPGTextOrEmpty[string](client.ResourceName))
	require.Equal(t, "https://b.example.test/tos", conv.FromPGTextOrEmpty[string](client.ResourceTosUri))
}
