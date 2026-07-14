package remotemcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateServer_ServerFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerUpdate)
	require.NoError(t, err)

	// Update server fields only, leave headers unchanged (nil)
	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		URL:              new("https://mcp-v2.example.com"),
		TransportType:    new("sse"),
	})
	require.NoError(t, err)
	require.Equal(t, "https://mcp-v2.example.com", updated.URL)
	require.Equal(t, "sse", updated.TransportType)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestUpdateServer_PartialServerFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)

	// Only update URL, leave transport_type unchanged
	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		URL:              new("https://mcp-new.example.com"),
	})
	require.NoError(t, err)
	require.Equal(t, "https://mcp-new.example.com", updated.URL)
	require.Equal(t, "streamable-http", updated.TransportType)
}

// requireUpdateServerInvalidURL creates a remote MCP server with a valid URL
// and then asserts that updating it to the given URL fails with
// [oops.CodeBadRequest], returning the error so the caller can make
// additional assertions on the error chain.
func requireUpdateServerInvalidURL(t *testing.T, url string) error {
	t.Helper()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		URL:              &url,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
	return err //nolint:wrapcheck // returned for ErrorIs assertions on the chain
}

func TestUpdateServer_InvalidURL_BlockedIPv4LiteralLoopback(t *testing.T) {
	t.Parallel()
	err := requireUpdateServerInvalidURL(t, "http://127.0.0.1")
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestUpdateServer_InvalidURL_BlockedIPv4LiteralPrivate(t *testing.T) {
	t.Parallel()
	err := requireUpdateServerInvalidURL(t, "http://10.0.0.1")
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestUpdateServer_InvalidURL_BlockedIPv6LiteralLoopback(t *testing.T) {
	t.Parallel()
	err := requireUpdateServerInvalidURL(t, "http://[::1]")
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestUpdateServer_InvalidURL_HostnameResolvesToBlockedIP(t *testing.T) {
	t.Parallel()
	err := requireUpdateServerInvalidURL(t, "http://"+blockedTestHost)
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestUpdateServer_InvalidURL_HostnameFailsToResolve(t *testing.T) {
	t.Parallel()
	err := requireUpdateServerInvalidURL(t, "http://"+unresolvableTestHost)
	require.ErrorIs(t, err, guardian.ErrBadHost)
}

func TestUpdateServer_InvalidURL_UnsupportedScheme(t *testing.T) {
	t.Parallel()
	_ = requireUpdateServerInvalidURL(t, "ftp://mcp.example.com")
}

func TestUpdateServer_InvalidURL_MissingHost(t *testing.T) {
	t.Parallel()
	_ = requireUpdateServerInvalidURL(t, "https://")
}

func TestUpdateServer_AllowsPublicIPLiteral(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		URL:              new("http://8.8.8.8"),
	})
	require.NoError(t, err)
	require.Equal(t, "http://8.8.8.8", updated.URL)
}

func TestUpdateServer_NameSet(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Name:             new("New Name"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, "New Name", *updated.Name)
}

func TestUpdateServer_NameClearedWithEmptyString(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             new("Initial"),
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)
	require.NotNil(t, created.Name)

	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Name:             new(""),
	})
	require.NoError(t, err)
	require.Nil(t, updated.Name)
}

func TestUpdateServer_NameNilLeavesUnchanged(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             new("Keep Me"),
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		URL:              new("https://other.example.com"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, "Keep Me", *updated.Name)
}

func TestUpdateServer_SlugRecomputedOnURLChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://api.example.com/mcp",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)
	require.NotNil(t, created.Slug)
	require.Contains(t, *created.Slug, "api-example-com-mcp-")

	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		URL:              new("https://other.test.com/v2"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Slug)
	require.Contains(t, *updated.Slug, "other-test-com-v2-")
	// Suffix (last 4 chars of ID) is stable across the URL change.
	require.Equal(t, (*created.Slug)[len(*created.Slug)-4:], (*updated.Slug)[len(*updated.Slug)-4:])
}

func TestUpdateServer_SlugStableOnUnchangedURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)
	require.NotNil(t, created.Slug)

	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Name:             new("Renamed"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Slug)
	require.Equal(t, *created.Slug, *updated.Slug)
}
