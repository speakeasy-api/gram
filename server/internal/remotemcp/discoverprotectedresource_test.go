package remotemcp_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newTestServiceForProbe builds a test service with an unsafe guardian.Policy
// so handlers can probe httptest.NewServer instances on 127.0.0.1.
func newTestServiceForProbe(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return newTestServiceWithPolicy(t, policy)
}

// seedRemoteMcpServerWithURL inserts a remote_mcp_server row pointing at url
// and returns the inserted row. The slug is derived from the row id so tests
// can seed multiple rows in the same database without collisions.
func seedRemoteMcpServerWithURL(t *testing.T, ctx context.Context, ti *testInstance, url string) repo.RemoteMcpServer {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	id := uuid.Must(uuid.NewV7())
	server, err := repo.New(ti.conn).CreateServer(ctx, repo.CreateServerParams{
		ID:            id,
		ProjectID:     *authCtx.ProjectID,
		Name:          pgtype.Text{String: "", Valid: false},
		Slug:          conv.ToPGText("probe-" + id.String()[:8]),
		TransportType: "streamable-http",
		Url:           url,
	})
	require.NoError(t, err)
	return server
}

func TestDiscoverProtectedResourceMetadata_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	// LaunchProtectedResourceServer JSON-encodes opts.Metadata on each
	// request, so mutating it after the server is up (but before any probe
	// fires) is safe and lets us advertise the upstream's own URL.
	metadata := &wellknown.OAuthProtectedResourceMetadata{
		Resource:               "",
		AuthorizationServers:   []string{"https://auth.example.com"},
		ScopesSupported:        []string{"read"},
		BearerMethodsSupported: []string{"header"},
		ResourceDocumentation:  "https://docs.example.com",
	}
	server := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{
		Metadata:   metadata,
		StatusCode: 0,
		Body:       nil,
	})
	metadata.Resource = server.URL

	row := seedRemoteMcpServerWithURL(t, ctx, ti, server.URL)

	out, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: row.ID.String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.NoError(t, err)
	require.True(t, out.Available)
	require.Nil(t, out.Unavailable)
	require.NotNil(t, out.Metadata)
	require.Empty(t, out.DiscoveryWarnings)
	require.Equal(t, []string{"https://auth.example.com"}, out.Metadata.AuthorizationServers)
	require.Equal(t, []string{"read"}, out.Metadata.ScopesSupported)
	require.Equal(t, []string{"header"}, out.Metadata.BearerMethodsSupported)
	require.NotNil(t, out.Metadata.ResourceDocumentation)
	require.Equal(t, "https://docs.example.com", *out.Metadata.ResourceDocumentation)
	require.NotNil(t, out.Metadata.Resource)
	require.Equal(t, server.URL, *out.Metadata.Resource)
}

func TestDiscoverProtectedResourceMetadata_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	// Empty opts → upstream 404s at the well-known path.
	server := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{
		Metadata:   nil,
		StatusCode: 0,
		Body:       nil,
	})
	row := seedRemoteMcpServerWithURL(t, ctx, ti, server.URL)

	out, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: row.ID.String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.NoError(t, err)
	require.False(t, out.Available)
	require.Nil(t, out.Metadata)
	require.Empty(t, out.DiscoveryWarnings)
	require.NotNil(t, out.Unavailable)
	require.Equal(t, "not_found", out.Unavailable.Code)
	require.Contains(t, out.Unavailable.Message, "not advertised")
}

func TestDiscoverProtectedResourceMetadata_ServerError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	server := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{
		Metadata:   nil,
		StatusCode: http.StatusInternalServerError,
		Body:       nil,
	})
	row := seedRemoteMcpServerWithURL(t, ctx, ti, server.URL)

	out, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: row.ID.String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.NoError(t, err)
	require.False(t, out.Available)
	require.NotNil(t, out.Unavailable)
	require.Equal(t, "http_error", out.Unavailable.Code)
	require.Contains(t, out.Unavailable.Message, "500")
}

func TestDiscoverProtectedResourceMetadata_Malformed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	server := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{
		Metadata:   nil,
		StatusCode: 0,
		Body:       []byte("not-json"),
	})
	row := seedRemoteMcpServerWithURL(t, ctx, ti, server.URL)

	out, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: row.ID.String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.NoError(t, err)
	require.False(t, out.Available)
	require.NotNil(t, out.Unavailable)
	require.Equal(t, "malformed", out.Unavailable.Code)
}

func TestDiscoverProtectedResourceMetadata_TransportError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	// Stand up a server, capture its URL, then close so the probe transport-errors.
	server := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{
		Metadata:   nil,
		StatusCode: 0,
		Body:       nil,
	})
	deadURL := server.URL
	server.Close()
	row := seedRemoteMcpServerWithURL(t, ctx, ti, deadURL)

	out, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: row.ID.String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.NoError(t, err)
	require.False(t, out.Available)
	require.NotNil(t, out.Unavailable)
	require.Equal(t, "transport_error", out.Unavailable.Code)
}

func TestDiscoverProtectedResourceMetadata_DiscoveryWarnings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	// Upstream advertises authorization_servers but no resource field; the
	// probe still succeeds and the missing field surfaces as a warning.
	server := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{
		Metadata: &wellknown.OAuthProtectedResourceMetadata{
			Resource:               "",
			AuthorizationServers:   []string{"https://auth.example.com"},
			ScopesSupported:        nil,
			BearerMethodsSupported: nil,
			ResourceDocumentation:  "",
		},
		StatusCode: 0,
		Body:       nil,
	})
	row := seedRemoteMcpServerWithURL(t, ctx, ti, server.URL)

	out, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: row.ID.String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.NoError(t, err)
	require.True(t, out.Available)
	require.NotEmpty(t, out.DiscoveryWarnings)
	requireAnyContains(t, out.DiscoveryWarnings, "resource field missing")
}

func TestDiscoverProtectedResourceMetadata_BadServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	_, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: "not-a-uuid",
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestDiscoverProtectedResourceMetadata_ServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	_, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: uuid.Must(uuid.NewV7()).String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDiscoverProtectedResourceMetadata_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	// Grant only ScopeMCPRead — ScopeMCPWrite is strictly required.
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, getProjectID(t, ctx)))

	server := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{
		Metadata:   nil,
		StatusCode: 0,
		Body:       nil,
	})
	row := seedRemoteMcpServerWithURL(t, ctx, ti, server.URL)

	_, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: row.ID.String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// getProjectID pulls the active project id out of the auth context for grant
// construction. Fails the test if absent.
func getProjectID(t *testing.T, ctx context.Context) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return authCtx.ProjectID.String()
}

func requireAnyContains(t *testing.T, haystack []string, needle string) {
	t.Helper()
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return
		}
	}
	require.Failf(t, "missing warning", "expected one of %v to contain %q", haystack, needle)
}
