// The direct-surface consent derivation for tunneled backends: a client whose
// attached MCP server is tunneled derives the tunneled server's recorded
// resource identifier as its RFC 8707 resource (AIM-151), exactly as a
// remote-backed client derives the remote URL. A tunneled server recording no
// identifier derives nothing, minting unqualified grants.

package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// seedTunneledMCPServerForIssuer binds one tunneled MCP server to the user
// session issuer, recording resourceIdentifier (empty records none).
func seedTunneledMCPServerForIssuer(t *testing.T, ctx context.Context, ti *testInstance, userIssuerID uuid.UUID, slug, resourceIdentifier string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	tunneled, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:                 uuid.New(),
		ProjectID:          *authCtx.ProjectID,
		Name:               slug,
		KeyHash:            "hash-" + slug,
		KeyPrefix:          "pfx",
		ResourceIdentifier: conv.ToPGTextEmpty(resourceIdentifier),
	})
	require.NoError(t, err)

	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		TunneledMcpServerID: conv.ToNullUUID(tunneled.ID),
		Visibility:          "private",
		UserSessionIssuerID: conv.ToNullUUID(userIssuerID),
	})
	require.NoError(t, err)
}

func TestFallbackResourceForClient_DerivesTunneledResourceIdentifier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-tunnel-rid")
	clientID, _ := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, authCtx.ActiveOrganizationID, "rsi-tunnel-rid")
	seedTunneledMCPServerForIssuer(t, ctx, ti, userIssuerID, "tunnel-rid-mcp", "https://tunneled.internal/mcp")

	resource, err := mgr.FallbackResourceForClient(ctx, clientID)
	require.NoError(t, err)
	require.Equal(t, "https://tunneled.internal/mcp", resource)
}

func TestFallbackResourceForClient_TunneledWithoutIdentifierDerivesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-tunnel-norid")
	clientID, _ := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, authCtx.ActiveOrganizationID, "rsi-tunnel-norid")
	seedTunneledMCPServerForIssuer(t, ctx, ti, userIssuerID, "tunnel-norid-mcp", "")

	resource, err := mgr.FallbackResourceForClient(ctx, clientID)
	require.NoError(t, err)
	require.Empty(t, resource)
}
