// A direct MCP server configured with no upstream authorization server signs
// nobody in. The clients its user session issuer binds belong to the other
// servers sharing it, so they must neither block its requests nor reach its
// upstream (AIM-577).

package mcp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// nullProviderEndpoint is a server with no provider of its own, sharing a user
// session issuer with a sibling that has one. siblingConnected seeds a live
// upstream session for the sibling's client, which is the credential that must
// not be forwarded here.
func newNullProviderEndpoint(t *testing.T, ctx context.Context, ti *testInstance, upstreamURL string, siblingConnected bool) (slug, token string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug = "null-provider-" + uuid.NewString()[:8]
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, projectID, upstreamURL, slug, "private", issuerID)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, mcpServer.ID.String())

	// A sibling on the same issuer, configured with a provider of its own. Its
	// presence is also what keeps the reconcile from deriving that provider
	// onto the server under test.
	siblingSlug := "null-provider-sibling-" + uuid.NewString()[:8]
	sibling, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, projectID, upstreamURL, siblingSlug, "private", issuerID)
	siblingClient := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuerID)
	stamped, err := testrepo.New(ti.conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		ID:                    sibling.ID,
		ProjectID:             projectID,
		RemoteSessionIssuerID: conv.ToNullUUID(siblingClient.RemoteSessionIssuerID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped)

	served, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        mcpServer.ID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.False(t, served.RemoteSessionIssuerID.Valid, "the server under test must name no provider")

	if siblingConnected {
		encrypted, err := ti.enc.Encrypt([]byte("sibling-upstream-token"))
		require.NoError(t, err)
		_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
			SubjectUrn:             urn.NewUserSubject(mockidp.MockUserID),
			UserSessionIssuerID:    issuerID,
			RemoteSessionClientID:  siblingClient.ID,
			AccessTokenEncrypted:   encrypted,
			AccessExpiresAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
			RefreshTokenEncrypted:  pgtype.Text{String: "", Valid: false},
			AuthorizationExpiresAt: pgtype.Timestamptz{Valid: false},
			RefreshExpiresAt:       pgtype.Timestamptz{Valid: false},
			Scopes:                 []string{},
			Resource:               pgtype.Text{String: "", Valid: false},
			AutoRefresh:            true,
		})
		require.NoError(t, err)
	}

	minted, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject(mockidp.MockUserID),
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, urn.NewUserSubject(mockidp.MockUserID), jti)
	return slug, minted
}

func TestServePublic_NullProviderServerIsNotBlockedBySiblingCredentials(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gate := newAuthorizationGate(t, func(string) bool { return true })
	slug, token := newNullProviderEndpoint(t, ctx, ti, gate.URL, false)

	// The sibling's provider has no session for this member. Resolving the
	// whole issuer made that a 401 reconnect challenge for a server that needs
	// no sign-in at all.
	resp, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "initialize: %s", resp.Body.String())

	for _, authorization := range gate.authorizations() {
		require.Empty(t, authorization, "a server with no provider calls its upstream anonymously")
	}
}

func TestServePublic_NullProviderServerNeverForwardsASiblingToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gate := newAuthorizationGate(t, func(string) bool { return true })
	slug, token := newNullProviderEndpoint(t, ctx, ti, gate.URL, true)

	// The member has connected the sibling's provider. That credential is the
	// sibling's, consented for its upstream, and this server names no provider
	// to route by, so it must not be forwarded here.
	resp, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "initialize: %s", resp.Body.String())

	for _, authorization := range gate.authorizations() {
		require.Empty(t, authorization, "a sibling's credential must never reach this server's upstream")
	}
}
