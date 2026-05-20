// issuer_gated_mcp_server_test.go provides the [createIssuerGatedMcpServer]
// helper that wires up a full /x/mcp/{slug} resolution chain plus the
// upstream-IDP plumbing (user_session_issuer + remote_session_issuer +
// DCR-registered remote_session_client). The shape mirrors
// [oauthtest.CreateIssuerGatedToolset] but operates on the
// mcp_servers / mcp_endpoints model used by /x/mcp rather than the
// legacy toolsets-keyed model used by /mcp.
//
// Kept as test-internal because /x/mcp integration tests are today the
// only consumer; promote to a public xmcptest package if a second consumer
// shows up.
package xmcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// issuerGatedBackend selects which backend the seeded mcp_server should
// point at.
type issuerGatedBackend int

const (
	// issuerGatedBackendToolset wires the mcp_server.toolset_id to a
	// fresh toolsets row.
	issuerGatedBackendToolset issuerGatedBackend = iota
	// issuerGatedBackendRemote wires the mcp_server.remote_mcp_server_id
	// to a fresh remote_mcp_servers row.
	issuerGatedBackendRemote
)

// issuerGatedMcpServerOpts configures [createIssuerGatedMcpServer].
type issuerGatedMcpServerOpts struct {
	// Backend selects toolset-backed vs remote-backed.
	Backend issuerGatedBackend
	// Slug prefix for the mcp_endpoints.slug. A UUID suffix is appended.
	// For toolset-backed servers the toolsets.mcp_slug uses the same value
	// so resolution lines up with the production assumption.
	Slug string
	// Visibility is "public", "private", or "disabled". Required.
	Visibility string
	// UpstreamMetadata is RFC 8414 JSON describing the remote authorization
	// server (e.g. devidptest.Instance.OAuth21Metadata(t)). The helper reads
	// issuer / authorization_endpoint / token_endpoint / registration_endpoint
	// out of this document and DCR-registers a remote_session_client.
	UpstreamMetadata []byte
	// RemoteSessionCallbackBaseURL, when set, registers the static Gram
	// /x/mcp/remote_login_callback URL. Tests that drive a real upstream
	// authorize flow should set this to the Gram server URL.
	RemoteSessionCallbackBaseURL string
	// AuthnChallengeMode is "chain" or "interactive". Default "interactive".
	AuthnChallengeMode string
	// RemoteUpstreamURL is the upstream URL stored on the remote_mcp_servers
	// row for issuerGatedBackendRemote. Required for that backend.
	RemoteUpstreamURL string
	// CustomDomainID, when Valid, scopes the resulting mcp_endpoint to a
	// custom_domains row so resolution only succeeds for requests carrying
	// a matching customdomains.Context.
	CustomDomainID uuid.NullUUID
}

// issuerGatedMcpServerResult holds the rows created by [createIssuerGatedMcpServer].
type issuerGatedMcpServerResult struct {
	// Slug is the mcp_endpoints.slug (also the toolsets.mcp_slug for the
	// toolset backend).
	Slug string
	// McpEndpoint is the mcp_endpoints row exposing the server via Slug.
	McpEndpoint mcpendpoints_repo.McpEndpoint
	// McpServer is the issuer-gated mcp_servers row.
	McpServer mcpservers_repo.McpServer
	// UserSessionIssuer gates the mcp_server.
	UserSessionIssuer usersessions_repo.UserSessionIssuer
	// RemoteSessionIssuer is the upstream-IDP discovery row.
	RemoteSessionIssuer remotesessions_repo.RemoteSessionIssuer
	// RemoteSessionClient is the DCR-registered upstream client bound to
	// the UserSessionIssuer.
	RemoteSessionClient remotesessions_repo.RemoteSessionClient
	// Toolset is populated only for the toolset backend.
	Toolset *toolsets_repo.Toolset
	// RemoteMcpServer is populated only for the remote backend.
	RemoteMcpServer *remotemcp_repo.RemoteMcpServer
}

// createIssuerGatedMcpServer wires up a full /x/mcp/{slug} resolution chain
// for an issuer-gated mcp_server: a user_session_issuer + one
// remote_session_issuer + one DCR-registered remote_session_client + a
// backend row (toolset or remote_mcp_server) + the mcp_server pointing at
// both the issuer and the backend + the mcp_endpoint exposing it via the
// returned slug. Intentionally analogous to
// [oauthtest.CreateIssuerGatedToolset] so /x/mcp integration tests drive
// the same upstream-IDP-backed OAuth dance against /x/mcp/{slug} that the
// /mcp tests already drive against the toolset-keyed surface.
func createIssuerGatedMcpServer(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	enc *encryption.Client,
	authCtx *contextvalues.AuthContext,
	opts issuerGatedMcpServerOpts,
) issuerGatedMcpServerResult {
	t.Helper()

	require.NotEmpty(t, opts.Visibility, "Visibility is required")
	require.NotNil(t, opts.UpstreamMetadata, "UpstreamMetadata is required")
	if opts.Backend == issuerGatedBackendRemote {
		require.NotEmpty(t, opts.RemoteUpstreamURL, "RemoteUpstreamURL is required for the remote backend")
	}

	// Reuse oauthtest's issuer-gated bootstrapping to mint the user_session_issuer,
	// remote_session_issuer, and DCR-registered remote_session_client. The
	// toolset it produces is reused as the toolset backend; for the
	// remote backend the toolset row is harmless overhead but ensures the
	// upstream-IDP wiring stays identical across backends.
	base := oauthtest.CreateIssuerGatedToolset(t, ctx, conn, enc, authCtx, oauthtest.IssuerGatedToolsetOpts{
		Slug:                         opts.Slug,
		IsPublic:                     opts.Visibility == mcpservers.VisibilityPublic,
		UpstreamMetadata:             opts.UpstreamMetadata,
		RemoteSessionCallbackBaseURL: opts.RemoteSessionCallbackBaseURL,
		RouteBase:                    "x/mcp",
		AuthnChallengeMode:           opts.AuthnChallengeMode,
	})

	mcpSlug := base.Toolset.McpSlug.String
	if mcpSlug == "" {
		mcpSlug = base.Toolset.Slug
	}

	var (
		toolsetID       uuid.NullUUID
		remoteServerID  uuid.NullUUID
		toolsetOut      *toolsets_repo.Toolset
		remoteServerOut *remotemcp_repo.RemoteMcpServer
		endpointSlug    string
	)

	switch opts.Backend {
	case issuerGatedBackendToolset:
		toolsetID = uuid.NullUUID{UUID: base.Toolset.ID, Valid: true}
		tk := base.Toolset
		toolsetOut = &tk
		endpointSlug = mcpSlug
	case issuerGatedBackendRemote:
		remote := remotemcptest.SeedServer(t, ctx, conn, remotemcp_repo.CreateServerParams{
			ID:            uuid.New(),
			ProjectID:     *authCtx.ProjectID,
			Name:          pgtype.Text{String: "xmcp-issuer-gated-remote", Valid: true},
			Slug:          pgtype.Text{String: "xmcp-issuer-gated-" + uuid.New().String()[:8], Valid: true},
			TransportType: "streamable-http",
			Url:           opts.RemoteUpstreamURL,
		})
		remoteServerID = uuid.NullUUID{UUID: remote.ID, Valid: true}
		remoteServerOut = &remote
		// Remote-backed slugs intentionally don't reuse the toolset's
		// mcp_slug — the /x/mcp endpoint is identified by its own slug
		// and is independent of any toolset row.
		endpointSlug = "xmcp-remote-" + uuid.New().String()[:8]
	}

	mcpServerID, err := uuid.NewV7()
	require.NoError(t, err)
	mcpServer, err := mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  mcpServerID,
		ProjectID:           *authCtx.ProjectID,
		Name:                pgtype.Text{String: "xmcp issuer-gated", Valid: true},
		Slug:                pgtype.Text{String: "xmcp-issuer-gated-" + mcpServerID.String()[len(mcpServerID.String())-4:], Valid: true},
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: base.UserSessionIssuer.ID, Valid: true},
		RemoteMcpServerID:   remoteServerID,
		ToolsetID:           toolsetID,
		Visibility:          opts.Visibility,
	})
	require.NoError(t, err)

	endpoint, err := mcpendpoints_repo.New(conn).CreateMCPEndpoint(ctx, mcpendpoints_repo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: opts.CustomDomainID,
		McpServerID:    mcpServer.ID,
		Slug:           endpointSlug,
	})
	require.NoError(t, err)

	return issuerGatedMcpServerResult{
		Slug:                endpointSlug,
		McpEndpoint:         endpoint,
		McpServer:           mcpServer,
		UserSessionIssuer:   base.UserSessionIssuer,
		RemoteSessionIssuer: base.RemoteSessionIssuer,
		RemoteSessionClient: base.RemoteSessionClient,
		Toolset:             toolsetOut,
		RemoteMcpServer:     remoteServerOut,
	}
}
