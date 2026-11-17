package remotesessions_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/clientsecrets/clientsecretstest"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/upstreamtokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestOrganizationKeySealsSignInAndRefresh(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	ctx, env := newSyntheticExpiryEnv(t, "org-key-seal", unreadableTokenProvider(&refreshes, healingRefresh), withOrganizationEncryption())
	require.Equal(t, conv.ToPGText(env.organizationID), env.session.OrganizationID)
	require.True(t, env.session.AccessTokenEncrypted.Valid, "dual_write keeps the platform-key copy for a rollback")
	require.True(t, env.session.AccessTokenOrgEncrypted.Valid, "dual_write seals the sign-in under the organization's key")
	require.True(t, env.session.RefreshTokenOrgEncrypted.Valid)

	token, err := env.mgr.ResolveAccessToken(ctx, remotesessions.RefreshAttribution{}, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "live-access", token)

	require.NoError(t, testrepo.New(env.db).ExpireRemoteSessionAccessTokenFixture(ctx, env.session.ID))
	token, err = env.mgr.ResolveAccessToken(ctx, remotesessions.RefreshAttribution{}, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "healed-access", token)
	require.EqualValues(t, 1, refreshes.Load())

	refreshed, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	require.True(t, refreshed.AccessTokenOrgEncrypted.Valid)
	require.NotEqual(t, env.session.AccessTokenOrgEncrypted, refreshed.AccessTokenOrgEncrypted, "the refreshed access token replaced both copies")
	require.NotEqual(t, env.session.RefreshTokenOrgEncrypted, refreshed.RefreshTokenOrgEncrypted, "the rotated refresh token replaced both copies")
}

func TestSuspendedOrganizationKeyIsRetryableAndNeverRefreshes(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int32
	ctx, env := newSyntheticExpiryEnv(t, "org-key-suspended", unreadableTokenProvider(&refreshes, healingRefresh), withOrganizationEncryption())
	require.NoError(t, testrepo.New(env.db).ExpireRemoteSessionAccessTokenFixture(ctx, env.session.ID))
	require.NoError(t, env.orgEncryption.fixture.Provisioner.Suspend(ctx, env.organizationID, "customer disabled the key", orgencryption.SystemActor()))

	_, err := resolveAll(t, env)
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable, "a suspended key clears on its own, so the request is retryable")
	require.NotErrorIs(t, err, remotesessions.ErrStoredSignInUnreadable, "a suspended key never asks anyone to sign in again")
	require.False(t, remotesessions.StoredTokenUnreadable(err))
	require.True(t, resolveAvailable(t, env).SessionUnavailable(), "an outage to retry")
	require.False(t, needsReconnect(t, env), "nobody is sent to reconnect")
	require.Zero(t, refreshes.Load(), "nothing is asked of the provider while the result could not be stored")

	states, err := env.mgr.RemoteSessionStatuses(ctx, env.subject, env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, states[env.clientID].Status, "the session still reads as connected")
	require.False(t, states[env.clientID].CanRefresh, "but nothing offers a refresh the key cannot serve")

	stored, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	require.Equal(t, env.session.RefreshTokenOrgEncrypted, stored.RefreshTokenOrgEncrypted, "the grant is left as it was")
}

func TestRevocationOpensTheOrganizationKeyCopy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	upstream := newRevocationUpstream(t, spy)
	fx := seedRevocableSession(t, ctx, ti, "revoke-org-key", upstream.URL+"/revoke", "s3cret", true)

	enc := testenv.NewEncryptionClient(t)
	fixture := orgencryptiontest.New(t, ti.conn)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, audit.NewLogger(), fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	tokens := upstreamtokens.New(testenv.NewLogger(t), orgencryption.NewCipher(testenv.NewLogger(t), enc, fixture.Service, transitions), enc)
	_, err := transitions.Enroll(ctx, fx.organizationID, orgencryption.UseCaseOAuthSessionTokens, orgencryption.SystemActor())
	require.NoError(t, err)

	sealed, err := tokens.SetRemoteSession(ctx, ti.conn, fx.sessionID, conv.ToPGText(fx.organizationID), upstreamtokens.RemoteSessionTokens{AccessToken: "org-access", RefreshToken: "org-refresh", KeepRefreshToken: false})
	require.NoError(t, err)
	require.True(t, sealed.RefreshTokenOrgEncrypted.Valid)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	revoker := remotesessions.NewUpstreamRevoker(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, enc, clientsecretstest.New(t, ti.conn, enc), tokens, policy, nil)

	// Only the organization-key copies, as a finalized organization stores them.
	creds := remotesessions.RevokedCredentials{
		RemoteSessionClientID: fx.clientID,
		OrganizationID:        sealed.OrganizationID,
		AccessToken:           upstreamtokens.Stored(pgtype.Text{String: "", Valid: false}, sealed.AccessTokenOrgEncrypted),
		RefreshToken:          upstreamtokens.Stored(pgtype.Text{String: "", Valid: false}, sealed.RefreshTokenOrgEncrypted),
	}
	revoker.RevokeDetached(ctx, creds)

	calls, form, _ := spy.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, "org-refresh", form.Get("token"), "the refresh token opened with the organization's key")
	require.Equal(t, "refresh_token", form.Get("token_type_hint"))

	require.NoError(t, fixture.Provisioner.Suspend(ctx, fx.organizationID, "customer disabled the key", orgencryption.SystemActor()))
	revoker.RevokeDetached(ctx, creds)

	calls, _, _ = spy.snapshot()
	require.Equal(t, 1, calls, "a token the key cannot open right now is never sent, and nothing falls back to the platform key")
}
