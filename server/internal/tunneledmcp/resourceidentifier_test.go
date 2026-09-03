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

func TestCreateServerResourceIdentifier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	writeCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))

	result, err := ti.service.CreateServer(writeCtx, &gen.CreateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Name:               "without-identifier",
		ResourceIdentifier: nil,
	})
	require.NoError(t, err)
	require.Nil(t, result.Server.ResourceIdentifier)

	// Blank means unset, same as omitting: there is no stored value to clear.
	result, err = ti.service.CreateServer(writeCtx, &gen.CreateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Name:               "blank-identifier",
		ResourceIdentifier: new(""),
	})
	require.NoError(t, err)
	require.Nil(t, result.Server.ResourceIdentifier)

	result, err = ti.service.CreateServer(writeCtx, &gen.CreateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Name:               "with-identifier",
		ResourceIdentifier: new(" https://tunneled.internal/mcp/ "),
	})
	require.NoError(t, err)
	require.Equal(t, "https://tunneled.internal/mcp", conv.PtrValOr(result.Server.ResourceIdentifier, ""),
		"the identifier is stored trimmed, without a trailing slash")
}

func TestCreateServerRejectsInvalidResourceIdentifier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	writeCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))

	_, err := ti.service.CreateServer(writeCtx, &gen.CreateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Name:               "invalid-identifier",
		ResourceIdentifier: new("tunneled.internal/mcp"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
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
		Name:               new(server.Name),
		AllowPublic:        nil,
		ResourceIdentifier: new(" https://tunneled.internal/mcp/ "),
	})
	require.NoError(t, err)
	require.Equal(t, "https://tunneled.internal/mcp", conv.PtrValOr(updated.ResourceIdentifier, ""),
		"the identifier is stored trimmed, without a trailing slash")

	// Omitting a field leaves the stored value untouched — name included, so a
	// section editing only the identifier cannot revert a concurrent rename.
	updated, err = ti.service.UpdateServer(writeCtx, &gen.UpdateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		ID:                 server.ID.String(),
		Name:               nil,
		AllowPublic:        new(false),
		ResourceIdentifier: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "https://tunneled.internal/mcp", conv.PtrValOr(updated.ResourceIdentifier, ""))
	require.Equal(t, server.Name, updated.Name)

	// An empty string clears it back to NULL.
	updated, err = ti.service.UpdateServer(writeCtx, &gen.UpdateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		ID:                 server.ID.String(),
		Name:               new(server.Name),
		AllowPublic:        nil,
		ResourceIdentifier: new(""),
	})
	require.NoError(t, err)
	require.Nil(t, updated.ResourceIdentifier)
}

// A trailing slash is syntax in a path and data in a query, so only the path
// is trimmed — otherwise one server would read as two routing identities.
func TestUpdateServerResourceIdentifierTrimsPathOnly(t *testing.T) {
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
		Name:               new(server.Name),
		AllowPublic:        nil,
		ResourceIdentifier: new("https://tunneled.internal/mcp/?tenant=a/"),
	})
	require.NoError(t, err)
	require.Equal(t, "https://tunneled.internal/mcp?tenant=a/", conv.PtrValOr(updated.ResourceIdentifier, ""))

	// An encoded separator must survive the trim: decoding it would collapse
	// this identifier onto the genuinely different .../a/b.
	updated, err = ti.service.UpdateServer(writeCtx, &gen.UpdateServerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		ID:                 server.ID.String(),
		Name:               new(server.Name),
		AllowPublic:        nil,
		ResourceIdentifier: new("https://tunneled.internal/a%2Fb/"),
	})
	require.NoError(t, err)
	require.Equal(t, "https://tunneled.internal/a%2Fb", conv.PtrValOr(updated.ResourceIdentifier, ""))
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
		"https://tunneled.internal/mcp#",     // bare fragment delimiter
		"/",                                  // normalizes to nothing: an error, not a silent clear
		"   x   ",                            // whitespace-padded garbage is not a clear either
	} {
		_, err := ti.service.UpdateServer(writeCtx, &gen.UpdateServerPayload{
			SessionToken:       nil,
			ApikeyToken:        nil,
			ProjectSlugInput:   nil,
			ID:                 server.ID.String(),
			Name:               new(server.Name),
			AllowPublic:        nil,
			ResourceIdentifier: new(invalid),
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}
