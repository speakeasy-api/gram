package remotesessions_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// clientCredentialAnswer is one canned answer from fakeClientCredentials.
type clientCredentialAnswer struct {
	token string
	err   error
}

// fakeClientCredentials answers Credential from canned answers, in order with
// the last repeating, and records every request and Forget.
type fakeClientCredentials struct {
	mu       sync.Mutex
	answers  []clientCredentialAnswer
	requests []remotesessions.ClientCredentialRequest
	forgets  int

	// forgettable is what Forget reports.
	forgettable bool
}

func (f *fakeClientCredentials) Credential(_ context.Context, req remotesessions.ClientCredentialRequest) (remotesessions.ClientCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	answer := f.answers[min(len(f.requests), len(f.answers)-1)]
	f.requests = append(f.requests, req)
	if answer.err != nil {
		return remotesessions.ClientCredential{}, answer.err
	}

	return remotesessions.NewClientCredential(answer.token, remotesessions.ClientCredentialSchemeBearer, time.Now().Add(time.Hour), func(context.Context) (bool, error) {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.forgets++
		return f.forgettable, nil
	}), nil
}

func (f *fakeClientCredentials) recorded() ([]remotesessions.ClientCredentialRequest, int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]remotesessions.ClientCredentialRequest(nil), f.requests...), f.forgets
}

func newClientCredentialManager(t *testing.T, conn *pgxpool.Pool, enc *encryption.Client, source remotesessions.ClientCredentialSource) *remotesessions.ChallengeManager {
	t.Helper()

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	return remotesessions.NewChallengeManager(
		testenv.NewLogger(t),
		tracerProvider,
		testenv.NewMeterProvider(t),
		conn,
		enc,
		policy,
		nil,
		cache.NoopCache,
		mustURL(t, "http://localhost"),
		remotesessions.WithClientCredentialSource(func(*remotesessions.ChallengeManager) remotesessions.ClientCredentialSource {
			return source
		}),
	)
}

// selfClientFixture is a self client bound to a user session issuer.
type selfClientFixture struct {
	projectID      uuid.UUID
	organizationID string
	userIssuerID   uuid.UUID
	clientID       uuid.UUID
	remoteIssuerID uuid.UUID
}

func seedSelfClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug string) selfClientFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	userIssuerID := createUserSessionIssuer(t, ctx, conn, "usi-"+slug)
	clientID, remoteIssuerID := seedActiveClient(t, ctx, conn, *authCtx.ProjectID, userIssuerID, authCtx.ActiveOrganizationID, "rsi-"+slug)

	rows, err := testrepo.New(conn).ForceRemoteSessionClientCredentialOwnerFixture(ctx, testrepo.ForceRemoteSessionClientCredentialOwnerFixtureParams{
		CredentialOwner: string(remotesessions.CredentialOwnerSelf),
		ID:              clientID,
		ProjectID:       conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:  conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	return selfClientFixture{
		projectID:      *authCtx.ProjectID,
		organizationID: authCtx.ActiveOrganizationID,
		userIssuerID:   userIssuerID,
		clientID:       clientID,
		remoteIssuerID: remoteIssuerID,
	}
}

func TestResolveAccessTokens_SelfClientPresentsClientCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{token: "self-token"}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-resolve")

	tokens, err := mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewUserSubject("self-resolve-user"))
	require.NoError(t, err)

	require.Len(t, tokens, 1)
	got := tokens[fx.remoteIssuerID]
	require.Equal(t, "self-token", got.Token)
	require.Empty(t, got.Resource, "the grant requested no resource")
	require.Equal(t, remotesessions.CredentialOwnerSelf, got.CredentialOwner)
	require.Equal(t, fx.clientID, got.RemoteSessionClientID)
	require.Equal(t, uuid.Nil, got.RemoteSessionID)
	require.NoError(t, got.ClientCredentialErr)

	requests, _ := source.recorded()
	require.Equal(t, []remotesessions.ClientCredentialRequest{{OrganizationID: fx.organizationID, ClientID: fx.clientID, Resource: ""}}, requests)
}

func TestResolveAccessTokens_SelfClientRequestsAttachedUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{token: "self-token"}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-upstream")
	attachRemoteMcpServerToIssuer(t, ctx, ti.conn, fx.projectID, fx.userIssuerID, "self-upstream", "https://upstream.example.com/mcp")

	tokens, err := mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewUserSubject("self-upstream-user"))
	require.NoError(t, err)
	require.Equal(t, "https://upstream.example.com/mcp", tokens[fx.remoteIssuerID].Resource, "the token records the audience it was minted for")

	requests, _ := source.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, "https://upstream.example.com/mcp", requests[0].Resource)
}

func TestResolveAccessTokens_SelfClientServesAgentWithoutAttachment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{token: "self-token"}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-agent")

	// No principal binding exists: no subject owns a self credential.
	tokens, err := mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewAgentSubject(uuid.New()))
	require.NoError(t, err)
	require.Equal(t, "self-token", tokens[fx.remoteIssuerID].Token)
}

func TestResolveAccessTokens_SelfClientServesAnonymousCaller(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{token: "self-token"}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-anonymous")

	// A public endpoint admits anonymous callers; the MCP server's own
	// credential serves them as it serves everyone else.
	tokens, err := mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewAnonymousSubject(uuid.NewString()))
	require.NoError(t, err)
	require.Equal(t, "self-token", tokens[fx.remoteIssuerID].Token)
}

func TestResolveAccessTokens_SelfClientRejectedIsMisconfigured(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{err: &remotesessions.TokenEndpointError{StatusCode: http.StatusUnauthorized, Code: oautherr.CodeInvalidClient}}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-rejected")

	_, err := mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewUserSubject("self-rejected-user"))
	require.ErrorIs(t, err, remotesessions.ErrClientCredentialMisconfigured)
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)
}

func TestResolveAccessTokens_SelfClientUnreachableIsUnavailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{err: &remotesessions.TokenEndpointError{Transport: true}}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-unreachable")

	_, err := mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewUserSubject("self-unreachable-user"))
	require.ErrorIs(t, err, remotesessions.ErrClientCredentialUnavailable)
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionUnavailable)
	require.NotErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
}

func TestResolveAccessTokens_SelfClientWithoutSourceIsMisconfigured(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	fx := seedSelfClient(t, ctx, ti.conn, "self-no-source")

	_, err := mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewUserSubject("self-no-source-user"))
	require.ErrorIs(t, err, remotesessions.ErrClientCredentialMisconfigured)
}

func TestResolveAvailableAccessTokens_KeepsUnavailableSelfClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{err: &remotesessions.TokenEndpointError{StatusCode: http.StatusBadRequest, Code: oautherr.CodeInvalidScope}}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-available")

	tokens, err := mgr.ResolveAvailableAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewUserSubject("self-available-user"))
	require.NoError(t, err)

	got, ok := tokens[fx.remoteIssuerID]
	require.True(t, ok, "routing needs the entry to name the remedy")
	require.Empty(t, got.Token)
	require.Equal(t, remotesessions.CredentialOwnerSelf, got.CredentialOwner)
	require.Equal(t, fx.clientID, got.RemoteSessionClientID)
	require.ErrorIs(t, got.ClientCredentialErr, remotesessions.ErrClientCredentialMisconfigured)
}

func TestCheckAccessTokens_SkipsSelfClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{err: errors.New("must not be asked")}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-check")

	err := mgr.CheckAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewAgentSubject(uuid.New()))
	require.NoError(t, err)

	requests, _ := source.recorded()
	require.Empty(t, requests)
}

func TestRemoteSessionsNeedReconnect_SkipsSelfClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{token: "self-token"}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-reconnect")

	reconnect, err := mgr.RemoteSessionsNeedReconnect(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewUserSubject("self-reconnect-user"))
	require.NoError(t, err)
	require.False(t, reconnect)
}

func TestResolveAuthorization_SelfClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{token: "self-token"}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-authorization")

	authorization, err := mgr.ResolveAuthorization(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, fx.remoteIssuerID, urn.NewUserSubject("self-authorization-user"), "https://provider.example.com/mcp")
	require.NoError(t, err)
	require.Equal(t, remotesessions.ResolvedAuthorization{
		AccessToken:            "self-token",
		RemoteSessionID:        uuid.Nil,
		RemoteSessionUpdatedAt: time.Time{},
		RemoteSessionClientID:  fx.clientID,
		RemoteSessionIssuerID:  fx.remoteIssuerID,
		CredentialOwner:        remotesessions.CredentialOwnerSelf,
	}, authorization)

	requests, _ := source.recorded()
	require.Equal(t, []remotesessions.ClientCredentialRequest{{OrganizationID: fx.organizationID, ClientID: fx.clientID, Resource: "https://provider.example.com/mcp"}}, requests)
}

func TestRenewClientCredential_ReplacesForgottenCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{token: "first"}, {token: "second"}}, forgettable: true}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-renew")

	tokens, err := mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewUserSubject("self-renew-user"))
	require.NoError(t, err)

	renewed, ok, err := mgr.RenewClientCredential(ctx, tokens[fx.remoteIssuerID])
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "second", renewed.Token)
	require.Equal(t, remotesessions.CredentialOwnerSelf, renewed.CredentialOwner)

	requests, forgets := source.recorded()
	require.Len(t, requests, 2)
	require.Equal(t, requests[0], requests[1], "a renewal repeats the original request")
	require.Equal(t, 1, forgets)
}

func TestRenewClientCredential_KeepsCredentialSourceRetains(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{token: "first"}}, forgettable: false}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "self-renew-final")

	tokens, err := mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.userIssuerID, urn.NewUserSubject("self-renew-final-user"))
	require.NoError(t, err)

	_, ok, err := mgr.RenewClientCredential(ctx, tokens[fx.remoteIssuerID])
	require.NoError(t, err)
	require.False(t, ok, "a credential the source keeps makes the rejection final")

	requests, forgets := source.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, 1, forgets)
}

func TestRenewClientCredential_IgnoresSubjectToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{token: "unused"}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)

	_, ok, err := mgr.RenewClientCredential(ctx, remotesessions.UpstreamToken{Token: "subject-token", CredentialOwner: remotesessions.CredentialOwnerSubject})
	require.NoError(t, err)
	require.False(t, ok)

	requests, _ := source.recorded()
	require.Empty(t, requests)
}

func TestListClients_ReportsSelfCredentialOwner(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	fx := seedSelfClient(t, ctx, ti.conn, "self-list")

	clients, err := mgr.ListClients(ctx, fx.projectID, fx.organizationID, fx.userIssuerID)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	require.Equal(t, remotesessions.CredentialOwnerSelf, clients[0].CredentialOwner)
}

func TestBuildAuthorizationUrl_RefusesSelfClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	fx := seedSelfClient(t, ctx, ti.conn, "self-authorize")

	clients, err := mgr.ListClients(ctx, fx.projectID, fx.organizationID, fx.userIssuerID)
	require.NoError(t, err)
	require.Len(t, clients, 1)

	_, err = mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{}, clients[0])
	require.ErrorIs(t, err, remotesessions.ErrSelfCredentialClient)
}

func TestRotate_RefusesAutomaticRotationOfSelfClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fx := seedSelfClient(t, ctx, ti.conn, "self-rotate")
	rotator := remotesessions.NewClientRotator(testenv.NewLogger(t), ti.conn, nil, nil, nil, ti.redisCache, nil, nil, nil, nil)

	_, err := rotator.Rotate(ctx, remotesessions.RotateClientRegistrationParams{
		ClientID:       fx.clientID,
		Trigger:        remotesessions.RotationTriggerUpstreamRejected,
		OrganizationID: fx.organizationID,
		Actor:          urn.NewSystemPrincipal("rotation-test"),
	})
	require.ErrorIs(t, err, remotesessions.ErrSelfCredentialClient)
}
