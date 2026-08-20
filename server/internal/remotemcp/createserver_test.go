package remotemcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestCreateServerAndMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	beforeRemoteAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerCreate)
	require.NoError(t, err)
	beforeMcpAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateServerAndMcpServer(ctx, &gen.CreateServerAndMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             new("  Remote source  "),
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.RemoteMcpServer)
	require.NotNil(t, result.McpServer)

	remote := result.RemoteMcpServer
	mcpServer := result.McpServer
	require.NotNil(t, remote.Name)
	require.Equal(t, "Remote source", *remote.Name)
	require.Equal(t, remote.ID, *mcpServer.RemoteMcpServerID)
	require.Equal(t, "Remote source", *mcpServer.Name)
	require.Equal(t, types.McpServerVisibility("disabled"), mcpServer.Visibility)
	require.NotNil(t, mcpServer.UserSessionIssuerID)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	storedMcpServer, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        uuid.MustParse(mcpServer.ID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(remote.ID), storedMcpServer.RemoteMcpServerID.UUID)
	require.True(t, storedMcpServer.RemoteMcpServerID.Valid)
	require.Equal(t, "disabled", storedMcpServer.Visibility)
	require.True(t, storedMcpServer.UserSessionIssuerID.Valid)

	issuer, err := usersessionsrepo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:        storedMcpServer.UserSessionIssuerID.UUID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, issuer.Slug)

	afterRemoteAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerCreate)
	require.NoError(t, err)
	require.Equal(t, beforeRemoteAuditCount+1, afterRemoteAuditCount)
	afterMcpAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerCreate)
	require.NoError(t, err)
	require.Equal(t, beforeMcpAuditCount+1, afterMcpAuditCount)
}

func TestCreateServerAndMcpServer_AllowsSSE(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	result, err := ti.service.CreateServerAndMcpServer(ctx, &gen.CreateServerAndMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             new("SSE source"),
		URL:              "https://mcp.example.com/events",
		TransportType:    "sse",
	})
	require.NoError(t, err)
	require.Equal(t, "sse", result.RemoteMcpServer.TransportType)
	require.Equal(t, result.RemoteMcpServer.ID, *result.McpServer.RemoteMcpServerID)
}

func TestCreateServerAndMcpServer_RequiresLiveProjectInActiveOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	foreignAuthCtx := *authCtx
	foreignAuthCtx.ActiveOrganizationID = "another-organization"
	ctx = contextvalues.SetAuthContext(ctx, &foreignAuthCtx)

	_, err := ti.service.CreateServerAndMcpServer(ctx, &gen.CreateServerAndMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             new("Foreign organization source"),
		URL:              "https://mcp.example.com/foreign",
		TransportType:    "streamable-http",
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	remoteCount, mcpCount, issuerCount := provisionedResourceCounts(t, ctx, ti, *authCtx.ProjectID)
	require.Zero(t, remoteCount)
	require.Zero(t, mcpCount)
	require.Zero(t, issuerCount)
}

func TestCreateServerAndMcpServer_RollsBackAllResourcesWhenMaterializationFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.CreateRemoteMCPServerMaterializationFailureFunctionFixture(ctx))
	require.NoError(t, fixtures.CreateRemoteMCPServerMaterializationFailureTriggerFixture(ctx))

	remoteCount, mcpCount, issuerCount := provisionedResourceCounts(t, ctx, ti, *authCtx.ProjectID)

	_, err := ti.service.CreateServerAndMcpServer(ctx, &gen.CreateServerAndMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             new("Fails atomically"),
		URL:              "https://mcp.example.com/failing",
		TransportType:    "streamable-http",
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnexpected)

	afterRemoteCount, afterMcpCount, afterIssuerCount := provisionedResourceCounts(t, ctx, ti, *authCtx.ProjectID)
	require.Equal(t, remoteCount, afterRemoteCount)
	require.Equal(t, mcpCount, afterMcpCount)
	require.Equal(t, issuerCount, afterIssuerCount)
}

func provisionedResourceCounts(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID) (int, int, int) {
	t.Helper()

	remoteServers, err := repo.New(ti.conn).ListServersByProjectID(ctx, projectID)
	require.NoError(t, err)
	mcpServers, err := mcpserversrepo.New(ti.conn).ListMCPServersByProjectID(ctx, mcpserversrepo.ListMCPServersByProjectIDParams{ProjectID: projectID})
	require.NoError(t, err)
	issuers, err := usersessionsrepo.New(ti.conn).ListUserSessionIssuersByProjectID(ctx, usersessionsrepo.ListUserSessionIssuersByProjectIDParams{
		ProjectID: projectID, Cursor: uuid.NullUUID{}, LimitValue: 100,
	})
	require.NoError(t, err)

	return len(remoteServers), len(mcpServers), len(issuers)
}

func TestCreateServerAndMcpServer_UsesURLDisplayFallback(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.CreateServerAndMcpServer(ctx, &gen.CreateServerAndMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             nil,
		URL:              "https://mcp.example.com/remote",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.RemoteMcpServer)
	require.NotNil(t, result.McpServer)
	require.Nil(t, result.RemoteMcpServer.Name)
	require.NotNil(t, result.McpServer.Name)
	require.Equal(t, "mcp.example.com/remote", *result.McpServer.Name)
}

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
