package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// seedAnonymousRefresh stores a refreshable session for a new anonymous subject.
func seedAnonymousRefresh(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) (urn.SessionSubject, usersessions_repo.UserSession, usersessions_repo.UserSessionClient, string) {
	t.Helper()

	queries := usersessions_repo.New(ti.conn)
	client, err := queries.CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                "anonymous-client-" + uuid.NewString()[:8],
		ClientName:              "anonymous client",
		RedirectUris:            []string{"http://localhost:3000/callback"},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)

	subject := urn.NewAnonymousSubject(uuid.NewString())
	refreshToken := "refresh-" + uuid.NewString()
	refreshHash := sha256.Sum256([]byte(refreshToken))
	session, err := queries.CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: issuerID,
		UserSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true},
		SubjectUrn:          subject,
		Jti:                 uuid.NewString(),
		RefreshTokenHash:    conv.ToPGText(base64.RawURLEncoding.EncodeToString(refreshHash[:])),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		SignInID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	return subject, session, client, refreshToken
}

// seedAnonymousConnection stores a provider connection expiring after access,
// renewable only when refreshable is set.
func seedAnonymousConnection(t *testing.T, ctx context.Context, ti *testInstance, issuerID, clientID uuid.UUID, subject urn.SessionSubject, access time.Duration, refreshable bool) {
	t.Helper()

	accessCiphertext, err := ti.enc.Encrypt([]byte("upstream-access"))
	require.NoError(t, err)
	params := remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   issuerID,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  accessCiphertext,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(access)),
		Scopes:                []string{},
	}
	if refreshable {
		refreshCiphertext, err := ti.enc.Encrypt([]byte("upstream-refresh"))
		require.NoError(t, err)
		params.RefreshTokenEncrypted = conv.ToPGText(refreshCiphertext)
		params.RefreshExpiresAt = conv.ToPGTimestamptz(time.Now().Add(time.Hour))
	}
	_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, params)
	require.NoError(t, err)
}

func TestServePublic_IssuerGate_AnonymousMissingRemoteSessionChallengesInvalidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-anon-reconnect")

	sessionToken := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, urn.NewAnonymousSubject(uuid.NewString()))

	w, err := servePublicHTTP(t, t.Context(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), sessionToken, nil)
	require.Equal(t, reconnectDescription, requireOopsCode(t, err, oops.CodeUnauthorized))

	challenge := w.Header().Get("WWW-Authenticate")
	require.Contains(t, challenge, `error="invalid_token"`, "RFC 6750 section 3.1: the request carried a token that can no longer be used")
	require.Contains(t, challenge, `error_description="`+reconnectDescription+`"`)
}

// Enforced auth clients offer an anonymous subject no personal connection, so
// it must not be told to drop a token that reauthorizing could not replace.
func TestServePublic_IssuerGate_AnonymousKeepsBareChallengeUnderEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-anon-enforced")
	_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureUpstreamAuthEnforced),
	})
	require.NoError(t, err)

	sessionToken := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, urn.NewAnonymousSubject(uuid.NewString()))

	w, err := servePublicHTTP(t, t.Context(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), sessionToken, nil)
	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.NotContains(t, w.Header().Get("WWW-Authenticate"), "error=")
}

func TestHandleToken_AnonymousRefreshChecksStoredUpstreamCredentials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		noConnection  bool
		access        time.Duration
		refreshable   bool
		wantReconnect bool
	}{
		{name: "grant cleared after the provider refused it", access: -time.Hour, wantReconnect: true},
		{name: "no connection", noConnection: true, wantReconnect: true},
		{name: "live access with no refresh token", access: time.Hour},
		{name: "expired access that can still refresh", access: -time.Hour, refreshable: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "refresh-anon")
			subject, _, client, refreshToken := seedAnonymousRefresh(t, ctx, ti, fixture.UserSessionIssuer.ID)

			var upstreamRefreshes atomic.Int32
			pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, fixture.RemoteSessionClient.ID, func(w http.ResponseWriter, _ *http.Request) {
				upstreamRefreshes.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			})
			if !tc.noConnection {
				seedAnonymousConnection(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject, tc.access, tc.refreshable)
			}

			res := performRefreshRequest(ctx, ti, fixture.Toolset.McpSlug.String, client.ClientID, refreshToken)
			require.NoError(t, res.err)
			if tc.wantReconnect {
				requireRefreshReconnect(t, res)
			} else {
				require.Equal(t, http.StatusOK, res.code, res.body)
			}
			require.Zero(t, upstreamRefreshes.Load(), "refresh judges stored state and never contacts the provider")
		})
	}
}

// RFC 6750 section 3.1 then RFC 6749 section 5.2: the provider refuses the
// grant, the request is told its token is invalid, and the next refresh is
// refused, which sends the client through reauthorization.
func TestIssuerGate_AnonymousUpstreamInvalidGrantRefusesTheFollowingRefresh(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-anon-sequence")
	subject, _, client, refreshToken := seedAnonymousRefresh(t, ctx, ti, fixture.UserSessionIssuer.ID)
	seedAnonymousConnection(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject, -time.Hour, true)
	pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, fixture.RemoteSessionClient.ID, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	slug := fixture.Toolset.McpSlug.String

	refreshed := performRefreshRequest(ctx, ti, slug, client.ClientID, refreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
	var tokens tokenResponseFixture
	require.NoError(t, json.Unmarshal([]byte(refreshed.body), &tokens))

	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeInitializeBody(), tokens.AccessToken, nil)
	require.Equal(t, reconnectDescription, requireOopsCode(t, err, oops.CodeUnauthorized))
	require.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)

	refused := performRefreshRequest(ctx, ti, slug, client.ClientID, tokens.RefreshToken)
	require.NoError(t, refused.err)
	requireRefreshReconnect(t, refused)
}

// A proxied server is served through the session's chosen client, so only that
// client's stored grant decides.
func TestHandleToken_AnonymousRefreshJudgesTheChosenClientOnAProxiedServer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		access        time.Duration
		wantReconnect bool
	}{
		{name: "chosen connection gone", access: -time.Hour, wantReconnect: true},
		{name: "chosen connection live", access: time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			ti.service.manual = true
			authCtx := requireProjectAuthContext(t, ctx)
			_, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
			chosen := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)
			slug := "anon-proxied-" + uuid.NewString()[:8]
			serverID := attachConsentRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID, issuer.ID, slug, "https://anon-proxied.example.com/mcp")
			_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
				ProjectID:       *authCtx.ProjectID,
				CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				McpServerID:     conv.ToNullUUID(serverID),
				MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				Slug:            slug,
			})
			require.NoError(t, err)
			_, err = upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
				OrganizationID:        authCtx.ActiveOrganizationID,
				RemoteSessionClientID: chosen.ID,
				McpServerID:           serverID,
				ProjectID:             *authCtx.ProjectID,
			})
			require.NoError(t, err)

			subject, session, client, refreshToken := seedAnonymousRefresh(t, ctx, ti, issuer.ID)
			_, err = usersessions_repo.New(ti.conn).RecordUpstreamClientSelections(ctx, usersessions_repo.RecordUpstreamClientSelectionsParams{
				UserSessionIssuerID: issuer.ID,
				CredentialKind:      upstreamclients.CredentialKindUserSession,
				CredentialID:        session.ID,
				Source:              upstreamclients.SelectionSourceIssuance,
				PickedMcpServerIds:  []uuid.UUID{serverID},
				PickedClientIds:     []uuid.UUID{chosen.ID},
			})
			require.NoError(t, err)
			seedAnonymousConnection(t, ctx, ti, issuer.ID, chosen.ID, subject, tc.access, false)

			res := performRefreshRequest(ctx, ti, slug, client.ClientID, refreshToken)
			require.NoError(t, res.err)
			if tc.wantReconnect {
				requireRefreshReconnect(t, res)
			} else {
				require.Equal(t, http.StatusOK, res.code, res.body)
			}
		})
	}
}
