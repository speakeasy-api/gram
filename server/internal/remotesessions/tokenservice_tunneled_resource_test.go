// The direct-surface consent derivation and tunneled backends: a tunneled
// server never contributes an RFC 8707 resource, whether or not it records an
// identifier (AIM-151). Its credentials route by the server's own derived
// remote_session_issuer and accept an unqualified grant, so stamping the
// identifier here would buy no routing — and would let an issuer fronting
// both kinds read as ambiguous, unqualifying a sibling remote server's grants.

package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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
		ID:        uuid.New(),
		ProjectID: *authCtx.ProjectID,
		Name:      slug,
		KeyHash:   "hash-" + slug,
		KeyPrefix: "pfx",
	})
	require.NoError(t, err)
	// The identifier is recorded after creation, once the tunnel is up.
	if resourceIdentifier != "" {
		_, err = tunneledmcprepo.New(ti.conn).UpdateServer(ctx, tunneledmcprepo.UpdateServerParams{
			ID:                 tunneled.ID,
			ProjectID:          *authCtx.ProjectID,
			Name:               slug,
			AllowPublic:        pgtype.Bool{Bool: false, Valid: false},
			ResourceIdentifier: conv.ToPGText(resourceIdentifier),
		})
		require.NoError(t, err)
	}

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

func TestFallbackResourceForClient_TunneledServerDerivesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-tunnel-rid")
	clientID, _ := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, authCtx.ActiveOrganizationID, "rsi-tunnel-rid")
	seedTunneledMCPServerForIssuer(t, ctx, ti, userIssuerID, "tunnel-rid-mcp", "https://tunneled.internal/mcp")

	// The grant stays unqualified, which is what the tunneled routing path
	// accepts under the server's own issuer key.
	resource, err := mgr.FallbackResourceForClient(ctx, clientID)
	require.NoError(t, err)
	require.Empty(t, resource)
}

// Recording an identifier on a tunneled server must not disturb a remote
// server that shares its issuer: the remote grant stays qualified to its own
// URL rather than collapsing to an ambiguous, unqualified derivation.
func TestFallbackResourceForClient_TunneledIdentifierLeavesRemoteSiblingQualified(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-tunnel-mixed")
	clientID, _ := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, authCtx.ActiveOrganizationID, "rsi-tunnel-mixed")
	seedRemoteMCPServerForIssuer(t, ctx, ti, userIssuerID, "mixed-remote-mcp", "https://remote.example.com/mcp")
	seedTunneledMCPServerForIssuer(t, ctx, ti, userIssuerID, "mixed-tunnel-mcp", "https://tunneled.internal/mcp")

	resource, err := mgr.FallbackResourceForClient(ctx, clientID)
	require.NoError(t, err)
	require.Equal(t, "https://remote.example.com/mcp", resource,
		"a tunneled sibling's identifier must not make the derivation ambiguous")
}
