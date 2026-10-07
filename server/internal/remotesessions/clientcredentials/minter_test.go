package clientcredentials

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testResource = "https://api.upstream.example.test/mcp"

func TestCredential_ClientSecretBasic(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("basic-token") })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{"read", "write"})

	cred, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, testResource))
	require.NoError(t, err)
	require.Equal(t, "basic-token", cred.Value())
	require.Equal(t, SchemeBearer, cred.Scheme())
	require.WithinDuration(t, time.Now().Add(time.Hour-expirySkew), cred.ExpiresAt(), 10*time.Second)

	requests := f.tokens.received()
	require.Len(t, requests, 1)

	form := requests[0].form
	require.Equal(t, oauthwire.GrantTypeClientCredentials, form.Get(oauthwire.ParamGrantType))
	require.Equal(t, "read write", form.Get(oauthwire.ParamScope))
	require.Equal(t, testResource, form.Get(oauthwire.ParamResource))
	require.False(t, form.Has("client_secret"))

	id, secret, ok := (&http.Request{Header: requests[0].header}).BasicAuth()
	require.True(t, ok)
	require.Equal(t, "gram-client", id)
	require.Equal(t, "client-secret", secret)
}

func TestCredential_ClientSecretPost(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("post-token") })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretPost, []string{})

	cred, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, "post-token", cred.Value())

	requests := f.tokens.received()
	require.Len(t, requests, 1)

	form := requests[0].form
	require.Equal(t, "gram-client", form.Get("client_id"))
	require.Equal(t, "client-secret", form.Get("client_secret"))
	require.False(t, form.Has(oauthwire.ParamScope))
	require.False(t, form.Has(oauthwire.ParamResource))
	require.Empty(t, requests[0].header.Get("Authorization"))
}

func TestCredential_PrivateKeyJWT(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("jwt-token") })
	clientID, keySetID := f.keySetClient(t, "")

	cred, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, "jwt-token", cred.Value())

	signed := f.signer.received()
	require.Len(t, signed, 1)
	require.Equal(t, remotesessions.ClientAssertionRequest{
		RemoteSessionClientID: clientID, OrganizationID: f.org, JSONWebKeySetID: keySetID,
		ClientID: "gram-client", Audience: f.tokens.server.URL,
	}, signed[0])
	requests := f.tokens.received()
	require.Len(t, requests, 1)

	form := requests[0].form
	require.Equal(t, "gram-client", form.Get("client_id"))
	require.Equal(t, oauthwire.ClientAssertionTypeJWTBearer, form.Get("client_assertion_type"))
	require.Equal(t, "assertion-for-"+f.tokens.server.URL, form.Get("client_assertion"))
	require.False(t, form.Has("client_secret"))
}

func TestCredential_PrivateKeyJWTTokenEndpointAudience(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("jwt-token") })
	clientID, _ := f.keySetClient(t, string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint))

	_, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)

	signed := f.signer.received()
	require.Len(t, signed, 1)
	require.Equal(t, f.tokens.tokenURL(), signed[0].Audience)
}

func TestCredential_SendsClientAudience(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("token") })

	encrypted, err := f.enc.Encrypt([]byte("client-secret"))
	require.NoError(t, err)

	client, err := repo.New(f.db).CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		OrganizationID: pgtype.Text{String: f.org, Valid: true}, RemoteSessionIssuerID: f.issuerID, ClientID: "gram-client",
		ClientSecretEncrypted:   pgtype.Text{String: encrypted, Valid: true},
		TokenEndpointAuthMethod: pgtype.Text{String: oauthwire.AuthMethodClientSecretPost, Valid: true},
		Audience:                pgtype.Text{String: "https://api.upstream.example.test", Valid: true},
		Scope:                   []string{},
	})
	require.NoError(t, err)

	_, err = f.newMinter(t).Credential(t.Context(), f.request(client.ID, ""))
	require.NoError(t, err)

	requests := f.tokens.received()
	require.Len(t, requests, 1)
	require.Equal(t, "https://api.upstream.example.test", requests[0].form.Get(oauthwire.ParamAudience))
}

func TestCredential_SecondInstanceReusesCachedCredential(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(n int) tokenResponse { return bearerToken("token-" + strconv.Itoa(n)) })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{"read"})

	first, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, testResource))
	require.NoError(t, err)

	second, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, testResource))
	require.NoError(t, err)

	require.Equal(t, "token-0", first.Value())
	require.Equal(t, first.Value(), second.Value())
	require.Equal(t, first.ExpiresAt().UnixMilli(), second.ExpiresAt().UnixMilli())
	require.Len(t, f.tokens.received(), 1)
}

func TestCredential_ResourceSelectsCredential(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(n int) tokenResponse { return bearerToken("token-" + strconv.Itoa(n)) })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)

	first, err := minter.Credential(t.Context(), f.request(clientID, testResource))
	require.NoError(t, err)

	second, err := minter.Credential(t.Context(), f.request(clientID, "https://other.upstream.example.test"))
	require.NoError(t, err)

	require.NotEqual(t, first.Value(), second.Value())
	require.Len(t, f.tokens.received(), 2)
}

func TestCredential_SecretRotationRequestsNewToken(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(n int) tokenResponse { return bearerToken("token-" + strconv.Itoa(n)) })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretPost, []string{})
	minter := f.newMinter(t)

	first, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)

	f.rotateSecret(t, clientID, "rotated-secret")

	second, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)

	require.Equal(t, "token-0", first.Value())
	require.Equal(t, "token-1", second.Value())

	requests := f.tokens.received()
	require.Len(t, requests, 2)
	require.Equal(t, "rotated-secret", requests[1].form.Get("client_secret"))
}

func TestCredential_ActiveKeyRotationRequestsNewToken(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(n int) tokenResponse { return bearerToken("token-" + strconv.Itoa(n)) })
	clientID, keySetID := f.keySetClient(t, "")
	minter := f.newMinter(t)

	first, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)

	reused, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)

	f.activateKey(t, keySetID, "kid-2")

	rotated, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)

	require.Equal(t, first.Value(), reused.Value())
	require.Equal(t, "token-1", rotated.Value())
	require.Len(t, f.tokens.received(), 2)
}

func TestCredential_ConcurrentCallersShareOneGrant(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("shared-token") })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	const callers = 8
	minters := []*Minter{f.newMinter(t), f.newMinter(t)}

	// The test holds the lease, as a holder on another replica would, so every
	// caller waits until it is released and then one of them takes it over.
	keys := f.cacheKeys(t, clientID, testResource)
	leases := minters[0].leases

	held, err := leases.AcquireLease(t.Context(), keys.lease, "test-holder", leaseTTL)
	require.NoError(t, err)
	require.True(t, held)

	var wg sync.WaitGroup
	values := make([]string, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Go(func() {
			cred, err := minters[i%len(minters)].Credential(t.Context(), f.request(clientID, testResource))
			values[i], errs[i] = cred.Value(), err
		})
	}

	released, err := leases.ReleaseLeaseIfOwner(t.Context(), keys.lease, "test-holder")
	require.NoError(t, err)
	require.True(t, released)

	wg.Wait()

	for i := range callers {
		require.NoError(t, errs[i])
		require.Equal(t, "shared-token", values[i])
	}

	require.Len(t, f.tokens.received(), 1)
}

func TestCredential_CachesRejectionUntilCredentialsChange(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(n int) tokenResponse {
		if n == 0 {
			return oauthError(http.StatusUnauthorized, oautherr.CodeInvalidClient)
		}
		return bearerToken("token-after-rotation")
	})
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)

	for range 2 {
		_, err := minter.Credential(t.Context(), f.request(clientID, ""))
		rejected, ok := errors.AsType[*remotesessions.TokenEndpointError](err)
		require.True(t, ok, "error: %v", err)
		require.Equal(t, oautherr.CodeInvalidClient, rejected.Code)
		require.Equal(t, http.StatusUnauthorized, rejected.StatusCode)
	}

	require.Len(t, f.tokens.received(), 1)

	_, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, ""))
	require.Error(t, err)
	require.Len(t, f.tokens.received(), 1)

	f.rotateSecret(t, clientID, "rotated-secret")

	cred, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, "token-after-rotation", cred.Value())
	require.Len(t, f.tokens.received(), 2)
}

func TestCredential_CachesConfigurationFailure(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse {
		return tokenResponse{status: http.StatusOK, body: map[string]any{"access_token": "dpop-token", "token_type": "DPoP", "expires_in": 3600}}
	})
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)

	for range 2 {
		_, err := minter.Credential(t.Context(), f.request(clientID, ""))
		require.ErrorIs(t, err, remotesessions.ErrTokenEndpointConfiguration)
	}

	require.Len(t, f.tokens.received(), 1)
}

func TestCredential_DoesNotCacheServerErrors(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(n int) tokenResponse {
		if n == 0 {
			return oauthError(http.StatusServiceUnavailable, oautherr.CodeTemporarilyUnavailable)
		}
		return bearerToken("token-after-outage")
	})
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)

	_, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.Error(t, err)

	cred, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, "token-after-outage", cred.Value())
	require.Len(t, f.tokens.received(), 2)
}

func TestCredential_RetriesWithoutRejectedResource(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(n int) tokenResponse {
		if n == 0 {
			return oauthError(http.StatusBadRequest, oautherr.CodeInvalidTarget)
		}
		return bearerToken("token-without-resource")
	})
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})

	cred, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, testResource))
	require.NoError(t, err)
	require.Equal(t, "token-without-resource", cred.Value())

	requests := f.tokens.received()
	require.Len(t, requests, 2)
	require.Equal(t, testResource, requests[0].form.Get(oauthwire.ParamResource))
	require.False(t, requests[1].form.Has(oauthwire.ParamResource))
}

func TestCredential_OmitsResourceForIssuerWithoutIndicators(t *testing.T) {
	t.Parallel()

	f := newFixtureWithIssuer(t, func(int) tokenResponse { return bearerToken("token") }, issuerOptions{resourceIndicatorSupported: pgtype.Bool{Bool: false, Valid: true}})
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)

	first, err := minter.Credential(t.Context(), f.request(clientID, testResource))
	require.NoError(t, err)

	second, err := minter.Credential(t.Context(), f.request(clientID, "https://other.upstream.example.test"))
	require.NoError(t, err)

	require.Equal(t, first.Value(), second.Value())

	requests := f.tokens.received()
	require.Len(t, requests, 1)
	require.False(t, requests[0].form.Has(oauthwire.ParamResource))
}

func TestCredential_RejectsPublicClient(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("token") })

	client, err := repo.New(f.db).CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		OrganizationID: pgtype.Text{String: f.org, Valid: true}, RemoteSessionIssuerID: f.issuerID, ClientID: "public-client",
		TokenEndpointAuthMethod: pgtype.Text{String: oauthwire.AuthMethodNone, Valid: true},
		Scope:                   []string{},
	})
	require.NoError(t, err)

	_, err = f.newMinter(t).Credential(t.Context(), f.request(client.ID, ""))
	require.ErrorIs(t, err, remotesessions.ErrTokenEndpointConfiguration)
	require.Empty(t, f.tokens.received())
}

func TestCredential_RejectsExpiredClientSecret(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("token") })

	encrypted, err := f.enc.Encrypt([]byte("client-secret"))
	require.NoError(t, err)

	client, err := repo.New(f.db).CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		OrganizationID: pgtype.Text{String: f.org, Valid: true}, RemoteSessionIssuerID: f.issuerID, ClientID: "gram-client",
		ClientSecretEncrypted:   pgtype.Text{String: encrypted, Valid: true},
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true, InfinityModifier: pgtype.Finite},
		TokenEndpointAuthMethod: pgtype.Text{String: oauthwire.AuthMethodClientSecretBasic, Valid: true},
		Scope:                   []string{},
	})
	require.NoError(t, err)

	_, err = f.newMinter(t).Credential(t.Context(), f.request(client.ID, ""))
	require.ErrorIs(t, err, remotesessions.ErrTokenEndpointConfiguration)
	require.Empty(t, f.tokens.received())
}

func TestCredential_RejectsNonBearerToken(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse {
		return tokenResponse{status: http.StatusOK, body: map[string]any{"access_token": "dpop-token", "token_type": "DPoP", "expires_in": 3600}}
	})
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})

	_, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, ""))
	require.ErrorIs(t, err, remotesessions.ErrTokenEndpointConfiguration)
}

func TestCredential_TreatsMissingTokenTypeAsBearer(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse {
		return tokenResponse{status: http.StatusOK, body: map[string]any{"access_token": "untyped-token"}}
	})
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})

	cred, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, SchemeBearer, cred.Scheme())
	require.WithinDuration(t, time.Now().Add(unknownExpiryLifetime-expirySkew), cred.ExpiresAt(), 10*time.Second)
}

func TestCredential_RejectsTokenExpiredDuringGrant(t *testing.T) {
	t.Parallel()

	startedAt := time.Now()
	var elapsed atomic.Int64
	f := newFixture(t, func(n int) tokenResponse {
		if n == 0 {
			// Advance the minter's clock while the request is in flight.
			elapsed.Store(int64(11 * time.Second))
			response := bearerToken("expired-token")
			response.body["expires_in"] = 10

			return response
		}

		return bearerToken("usable-token")
	})
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)
	minter.now = func() time.Time { return startedAt.Add(time.Duration(elapsed.Load())) }

	cred, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.ErrorContains(t, err, "already expired")
	require.Empty(t, cred.Value())

	_, err = minter.credentials.Get(t.Context(), f.cacheKeys(t, clientID, "").credential)
	require.Error(t, err, "an expired credential must not be cached")

	cred, err = minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, "usable-token", cred.Value())
	require.Len(t, f.tokens.received(), 2)
}

func TestCredential_DoesNotFindClientOutsideOrganization(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("token") })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})

	_, err := f.newMinter(t).Credential(t.Context(), Request{OrganizationID: "org-" + uuid.NewString(), ClientID: clientID, Resource: ""})
	require.ErrorIs(t, err, ErrClientNotFound)
	require.Empty(t, f.tokens.received())
}

func TestCredential_DoesNotFindGlobalClient(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("token") })
	q := repo.New(f.db)

	issuer, err := q.CreateRemoteSessionIssuer(t.Context(), repo.CreateRemoteSessionIssuerParams{
		Slug: "global-" + uuid.NewString()[:8], Issuer: f.tokens.server.URL + "/global",
		TokenEndpoint:       pgtype.Text{String: f.tokens.tokenURL(), Valid: true},
		GrantTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{},
		ScopesSupported: []string{}, ResponseTypesSupported: []string{}, CodeChallengeMethodsSupported: []string{},
		IntrospectionEndpointAuthMethodsSupported: []string{}, IDTokenSigningAlgValuesSupported: []string{}, ClaimsSupported: []string{},
	})
	require.NoError(t, err)

	encrypted, err := f.enc.Encrypt([]byte("client-secret"))
	require.NoError(t, err)

	client, err := q.CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		RemoteSessionIssuerID: issuer.ID, ClientID: "global-client",
		ClientSecretEncrypted:   pgtype.Text{String: encrypted, Valid: true},
		TokenEndpointAuthMethod: pgtype.Text{String: oauthwire.AuthMethodClientSecretBasic, Valid: true},
		Scope:                   []string{},
	})
	require.NoError(t, err)

	_, err = f.newMinter(t).Credential(t.Context(), f.request(client.ID, ""))
	require.ErrorIs(t, err, ErrClientNotFound)
	require.Empty(t, f.tokens.received())
}

func TestAwait_TakesOverReleasedLease(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("token") })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)
	keys := f.cacheKeys(t, clientID, "")

	held, err := minter.leases.AcquireLease(t.Context(), keys.lease, "holder", leaseTTL)
	require.NoError(t, err)
	require.True(t, held)

	type result struct {
		acquired bool
		err      error
	}

	done := make(chan result, 1)
	go func() {
		_, acquired, err := minter.await(t.Context(), testenv.NewLogger(t), keys, "waiter")
		done <- result{acquired: acquired, err: err}
	}()

	// The holder gives up without storing a credential or a failure, as after
	// a transport or server error.
	released, err := minter.leases.ReleaseLeaseIfOwner(t.Context(), keys.lease, "holder")
	require.NoError(t, err)
	require.True(t, released)

	got := <-done
	require.NoError(t, got.err)
	require.True(t, got.acquired)

	reacquired, err := minter.leases.AcquireLease(t.Context(), keys.lease, "holder", leaseTTL)
	require.NoError(t, err)
	require.False(t, reacquired, "the waiter holds the lease")
	require.Empty(t, f.tokens.received())
}

func TestAwait_AdoptsHolderRejection(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("token") })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)
	keys := f.cacheKeys(t, clientID, "")

	held, err := minter.leases.AcquireLease(t.Context(), keys.lease, "holder", leaseTTL)
	require.NoError(t, err)
	require.True(t, held)

	done := make(chan error, 1)
	go func() {
		_, _, err := minter.await(t.Context(), testenv.NewLogger(t), keys, "waiter")
		done <- err
	}()

	require.NoError(t, minter.failures.Store(t.Context(), failureEntry{Key: keys.failure, Configuration: false, StatusCode: http.StatusUnauthorized, Code: oautherr.CodeInvalidClient}))

	err = <-done
	rejected, ok := errors.AsType[*remotesessions.TokenEndpointError](err)
	require.True(t, ok, "error: %v", err)
	require.Equal(t, oautherr.CodeInvalidClient, rejected.Code)
	require.Empty(t, f.tokens.received())
}

func TestAwait_AdoptsHolderCredential(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(int) tokenResponse { return bearerToken("token") })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)
	keys := f.cacheKeys(t, clientID, "")

	held, err := minter.leases.AcquireLease(t.Context(), keys.lease, "holder", leaseTTL)
	require.NoError(t, err)
	require.True(t, held)

	type result struct {
		cred     Credential
		acquired bool
		err      error
	}

	done := make(chan result, 1)
	go func() {
		cred, acquired, err := minter.await(t.Context(), testenv.NewLogger(t), keys, "waiter")
		done <- result{cred: cred, acquired: acquired, err: err}
	}()

	encrypted, err := f.enc.Encrypt([]byte("holder-token"))
	require.NoError(t, err)

	now := time.Now()
	require.NoError(t, minter.credentials.Store(t.Context(), credentialEntry{
		Key: keys.credential, AccessTokenEncrypted: encrypted, Scheme: SchemeBearer,
		ExpiresAt: now.Add(time.Hour), MintedAt: now, ttl: time.Hour,
	}))

	got := <-done
	require.NoError(t, got.err)
	require.False(t, got.acquired)
	require.Equal(t, "holder-token", got.cred.Value())
	require.Empty(t, f.tokens.received())
}

// heldLease is a lease cache whose every lease another holder keeps.
type heldLease struct{}

func (heldLease) AcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}

func (heldLease) ReleaseLeaseIfOwner(context.Context, string, string) (bool, error) {
	return false, nil
}

func TestAwait_ExpiresWhenHolderKeepsLease(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logger := testenv.NewLogger(t)
		minter := &Minter{
			logger:      logger,
			db:          nil,
			enc:         nil,
			endpoints:   nil,
			leases:      heldLease{},
			credentials: cache.NewTypedObjectCache[credentialEntry](logger, cache.NoopCache, cache.SuffixNone),
			failures:    cache.NewTypedObjectCache[failureEntry](logger, cache.NoopCache, cache.SuffixNone),
			now:         time.Now,
		}
		keys := cacheKeys{credential: "credential", failure: "failure", lease: "lease"}
		startedAt := time.Now()

		_, acquired, err := minter.await(t.Context(), logger, keys, "waiter")

		require.ErrorIs(t, err, errWaitExpired)
		require.False(t, acquired)
		require.Equal(t, waitBudget, time.Since(startedAt))
	})
}

func TestForget_DropsRejectedCredential(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(n int) tokenResponse { return bearerToken("token-" + strconv.Itoa(n)) })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)

	first, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)

	later := f.newMinter(t)
	later.now = func() time.Time { return time.Now().Add(forgetMinAge) }

	cached, err := later.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, first.Value(), cached.Value())

	forgotten, err := later.Forget(t.Context(), cached)
	require.NoError(t, err)
	require.True(t, forgotten)

	replaced, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, "token-1", replaced.Value())
	require.Len(t, f.tokens.received(), 2)
}

func TestForget_KeepsJustMintedCredential(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(n int) tokenResponse { return bearerToken("token-" + strconv.Itoa(n)) })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})
	minter := f.newMinter(t)

	cred, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)

	forgotten, err := minter.Forget(t.Context(), cred)
	require.NoError(t, err)
	require.False(t, forgotten)

	again, err := minter.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, cred.Value(), again.Value())
	require.Len(t, f.tokens.received(), 1)
}

func TestForget_KeepsReplacementFromAnotherReplica(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(n int) tokenResponse { return bearerToken("token-" + strconv.Itoa(n)) })
	clientID := f.secretClient(t, oauthwire.AuthMethodClientSecretBasic, []string{})

	original, err := f.newMinter(t).Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)

	later := func() time.Time { return time.Now().Add(forgetMinAge) }
	peer := f.newMinter(t)
	peer.now = later
	stale := f.newMinter(t)
	stale.now = later

	forgotten, err := peer.Forget(t.Context(), original)
	require.NoError(t, err)
	require.True(t, forgotten)

	replacement, err := peer.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, "token-1", replacement.Value())

	forgotten, err = stale.Forget(t.Context(), original)
	require.NoError(t, err)
	require.True(t, forgotten)

	current, err := stale.Credential(t.Context(), f.request(clientID, ""))
	require.NoError(t, err)
	require.Equal(t, replacement.Value(), current.Value())
	require.Len(t, f.tokens.received(), 2)
}

func TestServedUntil(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		reported *time.Time
		want     time.Time
	}{
		{name: "reported expiry less skew", reported: new(now.Add(30 * time.Minute)), want: now.Add(29 * time.Minute)},
		{name: "unknown expiry", reported: nil, want: now.Add(unknownExpiryLifetime - expirySkew)},
		{name: "long expiry capped", reported: new(now.Add(24 * time.Hour)), want: now.Add(maxCredentialLifetime - expirySkew)},
		{name: "short expiry uses half its lifetime", reported: new(now.Add(time.Minute)), want: now.Add(30 * time.Second)},
		{name: "expired", reported: new(now.Add(-time.Second)), want: now.Add(-time.Second)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, servedUntil(now, tc.reported))
		})
	}
}
