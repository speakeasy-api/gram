package remotemcp_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerCreate)
	require.NoError(t, err)

	payload := &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	}

	result, err := ti.service.CreateServer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotEmpty(t, result.ID)
	require.NotEmpty(t, result.ProjectID)
	require.Equal(t, "https://mcp.example.com", result.URL)
	require.Equal(t, "streamable-http", result.TransportType)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

// requireCreateServerInvalidURL asserts that creating a remote MCP server
// with the given URL fails with [oops.CodeBadRequest], and returns the error
// so the caller can make additional assertions on the error chain.
func requireCreateServerInvalidURL(t *testing.T, url string) error {
	t.Helper()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              url,
		TransportType:    "streamable-http",
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
	return err //nolint:wrapcheck // returned for ErrorIs assertions on the chain
}

func TestCreateServer_InvalidURL_BlockedIPv4LiteralLoopback(t *testing.T) {
	t.Parallel()
	err := requireCreateServerInvalidURL(t, "http://127.0.0.1")
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestCreateServer_InvalidURL_BlockedIPv4LiteralPrivate(t *testing.T) {
	t.Parallel()
	err := requireCreateServerInvalidURL(t, "http://10.0.0.1")
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestCreateServer_InvalidURL_BlockedIPv6LiteralLoopback(t *testing.T) {
	t.Parallel()
	err := requireCreateServerInvalidURL(t, "http://[::1]")
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestCreateServer_InvalidURL_HostnameResolvesToBlockedIP(t *testing.T) {
	t.Parallel()
	err := requireCreateServerInvalidURL(t, "http://"+blockedTestHost)
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestCreateServer_InvalidURL_HostnameFailsToResolve(t *testing.T) {
	t.Parallel()
	err := requireCreateServerInvalidURL(t, "http://"+unresolvableTestHost)
	require.ErrorIs(t, err, guardian.ErrBadHost)
}

func TestCreateServer_InvalidURL_UnsupportedScheme(t *testing.T) {
	t.Parallel()
	_ = requireCreateServerInvalidURL(t, "ftp://mcp.example.com")
}

func TestCreateServer_InvalidURL_MissingHost(t *testing.T) {
	t.Parallel()
	_ = requireCreateServerInvalidURL(t, "https://")
}

func TestCreateServer_AllowsPublicIPLiteral(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "http://8.8.8.8",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://8.8.8.8", result.URL)
}

func TestCreateServer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, authCtx.ProjectID.String())})

	payload := &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	}

	_, err := ti.service.CreateServer(ctx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateServer_NameStored(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             new("My MCP Server"),
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)
	require.NotNil(t, result.Name)
	require.Equal(t, "My MCP Server", *result.Name)
}

func TestCreateServer_NameTrimmed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             new("  Trimmed Name  "),
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)
	require.NotNil(t, result.Name)
	require.Equal(t, "Trimmed Name", *result.Name)
}

func TestCreateServer_NameEmptyTreatedAsNull(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             new("   "),
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)
	require.Nil(t, result.Name)
}

func TestCreateServer_SlugComputed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://api.example.com/mcp",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)
	require.NotNil(t, result.Slug)
	// Format: <host-and-path-slug>-<last 4 chars of UUID>
	require.True(t, strings.HasPrefix(*result.Slug, "api-example-com-mcp-"), "got %s", *result.Slug)
	require.True(t, strings.HasSuffix(*result.Slug, result.ID[len(result.ID)-4:]), "got %s", *result.Slug)
}
