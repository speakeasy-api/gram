package tunneledmcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// strPtr builds the optional-string payload fields the tri-state form takes.
//
//go:fix inline
func strPtr(s string) *string { return new(s) }

func TestCreateServerRecordsResourceIdentifier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	writeCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))

	result, err := ti.service.CreateServer(writeCtx, &gen.CreateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Name:               "with-identifier",
		ResourceIdentifier: new("https://tunneled.internal/mcp/"),
	})
	require.NoError(t, err)
	require.Equal(t, "https://tunneled.internal/mcp", conv.PtrValOr(result.Server.ResourceIdentifier, ""),
		"the identifier is stored without a trailing slash")
}

func TestUpdateServerResourceIdentifierTriState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	writeCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))

	updated, err := ti.service.UpdateServer(writeCtx, &gen.UpdateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		ID:                 server.ID.String(),
		Name:               server.Name,
		AllowPublic:        nil,
		ResourceIdentifier: new(" https://tunneled.internal/mcp/ "),
	})
	require.NoError(t, err)
	require.Equal(t, "https://tunneled.internal/mcp", conv.PtrValOr(updated.ResourceIdentifier, ""),
		"the identifier is stored trimmed, without a trailing slash")

	// Omitting the field leaves the stored value untouched.
	updated, err = ti.service.UpdateServer(writeCtx, &gen.UpdateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		ID:                 server.ID.String(),
		Name:               server.Name,
		AllowPublic:        nil,
		ResourceIdentifier: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "https://tunneled.internal/mcp", conv.PtrValOr(updated.ResourceIdentifier, ""))

	// An empty string clears it back to NULL.
	updated, err = ti.service.UpdateServer(writeCtx, &gen.UpdateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		ID:                 server.ID.String(),
		Name:               server.Name,
		AllowPublic:        nil,
		ResourceIdentifier: new(""),
	})
	require.NoError(t, err)
	require.Nil(t, updated.ResourceIdentifier)
}

func TestUpdateServerRejectsInvalidResourceIdentifier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	writeCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))

	for _, invalid := range []string{
		"tunneled.internal/mcp",              // not absolute
		"ftp://tunneled.internal/mcp",        // wrong scheme
		"https:///mcp",                       // no host
		"https://tunneled.internal/mcp#frag", // fragment (RFC 8707)
	} {
		_, err := ti.service.UpdateServer(writeCtx, &gen.UpdateServerPayload{
			SessionToken:       nil,
			ApikeyToken:        nil,
			ProjectSlugInput:   nil,
			ID:                 server.ID.String(),
			Name:               server.Name,
			AllowPublic:        nil,
			ResourceIdentifier: new(invalid),
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}
