package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestServePublic_IssuerGate_AnonymousMissingRemoteSessionChallengesInvalidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-anon-reconnect")

	subject := urn.NewAnonymousSubject(uuid.NewString())
	sessionToken := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

	w, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), sessionToken, nil)
	require.Equal(t, reconnectDescription, requireOopsCode(t, err, oops.CodeUnauthorized))

	challenge := w.Header().Get("WWW-Authenticate")
	require.Contains(t, challenge, `error="invalid_token"`, "RFC 6750 section 3.1: the request carried a token that can no longer be used")
	require.Contains(t, challenge, `error_description="`+reconnectDescription+`"`)
}

func TestHandleToken_AnonymousRefreshChecksStoredUpstreamCredentials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		noRemote      bool
		access        time.Duration
		refreshable   bool
		wantReconnect bool
	}{
		{name: "grant cleared after the provider refused it", access: -time.Hour, wantReconnect: true},
		{name: "no connection", noRemote: true, wantReconnect: true},
		{name: "live access with no refresh token", access: time.Hour},
		{name: "expired access that can still refresh", access: -time.Hour, refreshable: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			remoteClient := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)
			subject := urn.NewAnonymousSubject(uuid.NewString())

			refreshToken := "refresh-" + uuid.NewString()
			refreshHash := sha256.Sum256([]byte(refreshToken))
			_, err := usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
				UserSessionIssuerID: issuer.ID,
				UserSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true},
				SubjectUrn:          subject,
				Jti:                 uuid.NewString(),
				RefreshTokenHash:    conv.ToPGText(base64.RawURLEncoding.EncodeToString(refreshHash[:])),
				RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
				ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
				SignInID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			})
			require.NoError(t, err)

			var upstreamRefreshes atomic.Int32
			pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, remoteClient.ID, func(w http.ResponseWriter, _ *http.Request) {
				upstreamRefreshes.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			})

			if !tc.noRemote {
				accessCiphertext, err := ti.enc.Encrypt([]byte("upstream-access"))
				require.NoError(t, err)
				params := remotesessions_repo.UpsertRemoteSessionParams{
					SubjectUrn:            subject,
					UserSessionIssuerID:   issuer.ID,
					RemoteSessionClientID: remoteClient.ID,
					AccessTokenEncrypted:  accessCiphertext,
					AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(tc.access)),
					Scopes:                []string{},
				}
				if tc.refreshable {
					refreshCiphertext, err := ti.enc.Encrypt([]byte("upstream-refresh"))
					require.NoError(t, err)
					params.RefreshTokenEncrypted = conv.ToPGText(refreshCiphertext)
					params.RefreshExpiresAt = conv.ToPGTimestamptz(time.Now().Add(time.Hour))
				}
				_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, params)
				require.NoError(t, err)
			}

			res := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
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
