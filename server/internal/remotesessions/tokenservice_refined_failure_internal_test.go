package remotesessions

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// Pins the remedy each request-path refresh failure maps onto. The MCP issuer
// gate answers ErrRemoteSessionUnavailable with a retryable 503,
// ErrRemoteSessionMisconfigured with a challenge that names an administrator,
// and a failure matching neither with the reconnect challenge, so a misfiled
// case either strands a user on retries that cannot succeed or signs them out
// over an upstream blip.
func TestRefreshErrorIs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// err is the failure as refresh produced it, before RefreshNow wraps
		// it in the *RefreshError the request path sees.
		err  error
		want error
	}{
		{
			name: "no refresh grant",
			err:  ErrNoValidToken,
			want: nil,
		},
		{
			name: "upstream invalid_grant",
			err:  newTokenRefreshErrorFromHTTP(http.StatusBadRequest, "400 Bad Request", []byte(`{"error":"invalid_grant"}`)),
			want: nil,
		},
		{
			name: "upstream invalid_client marks the client for re-registration",
			err:  newTokenRefreshErrorFromHTTP(http.StatusUnauthorized, "401 Unauthorized", []byte(`{"error":"invalid_client"}`)),
			want: nil,
		},
		{
			name: "stored refresh token unreadable",
			err:  newTokenRefreshError("the session's stored refresh token could not be read", errors.New("decrypt"), refreshRemedyReconnect),
			want: nil,
		},
		{
			name: "upstream rejects the client's request",
			err:  newTokenRefreshErrorFromHTTP(http.StatusBadRequest, "400 Bad Request", []byte(`{"error":"unauthorized_client"}`)),
			want: ErrRemoteSessionMisconfigured,
		},
		{
			name: "upstream rejects without a recognizable body",
			err:  newTokenRefreshErrorFromHTTP(http.StatusNotFound, "404 Not Found", []byte(`not found`)),
			want: ErrRemoteSessionMisconfigured,
		},
		{
			name: "client or issuer deleted under a live session",
			err:  refreshClientLoadError(pgx.ErrNoRows),
			want: ErrRemoteSessionMisconfigured,
		},
		{
			name: "client load hits a database failure",
			err:  refreshClientLoadError(errors.New("connection reset")),
			want: ErrRemoteSessionUnavailable,
		},
		{
			name: "no token endpoint configured",
			err:  newTokenRefreshError("the identity provider has no token endpoint configured", nil, refreshRemedyAdministrator),
			want: ErrRemoteSessionMisconfigured,
		},
		{
			name: "no access token returned",
			err:  newTokenRefreshError("the identity provider returned no access token", nil, refreshRemedyAdministrator),
			want: ErrRemoteSessionMisconfigured,
		},
		{
			name: "upstream server error",
			err:  newTokenRefreshErrorFromHTTP(http.StatusBadGateway, "502 Bad Gateway", []byte(`upstream down`)),
			want: ErrRemoteSessionUnavailable,
		},
		{
			name: "upstream rate limited",
			err:  newTokenRefreshErrorFromHTTP(http.StatusTooManyRequests, "429 Too Many Requests", []byte(`{"error":"temporarily_unavailable"}`)),
			want: ErrRemoteSessionUnavailable,
		},
		{
			name: "upstream unreachable",
			err:  fmt.Errorf("post refresh: %w: %w", errRefreshUpstreamUnreachable, errors.New("dial tcp: connection refused")),
			want: ErrRemoteSessionUnavailable,
		},
		{
			name: "caller canceled mid-POST",
			err:  fmt.Errorf("post refresh: %w: %w", errRefreshUpstreamUnreachable, context.Canceled),
			want: ErrRemoteSessionUnavailable,
		},
		{
			name: "lost compare-and-swap after the POST",
			err:  newTokenRefreshError("the session was rotated by another request", errRefreshNotApplied, refreshRemedyRetry),
			want: ErrRemoteSessionUnavailable,
		},
		{
			name: "tunnel transport unavailable",
			err:  newTokenRefreshError("the tunnel transport for this identity provider is unavailable", errors.New("no route"), refreshRemedyRetry),
			want: ErrRemoteSessionUnavailable,
		},
		{
			name: "database failure",
			err:  errors.New("re-read active remote_session: connection reset"),
			want: ErrRemoteSessionUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("refresh: %w", &RefreshError{IssuerURL: "", Outcome: refreshOutcomeForError(t.Context(), tc.err), err: tc.err})
			for _, remedy := range []error{ErrRemoteSessionUnavailable, ErrRemoteSessionMisconfigured} {
				require.Equal(t, errors.Is(tc.want, remedy), errors.Is(wrapped, remedy), remedy.Error())
			}
		})
	}
}

// A signing failure is an administrator's to repair only when the key
// configuration itself is broken; KMS or the database failing to answer may
// clear on retry.
func TestClientAssertionUnconfigured(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "no signer in this deployment", err: &tokenEndpointSigningError{err: errTokenEndpointSigningUnavailable}, want: true},
		{name: "incomplete key configuration", err: &tokenEndpointSigningError{err: fmt.Errorf("sign: %w", errClientAssertionKeyUnconfigured)}, want: true},
		{name: "key set or key deleted", err: &tokenEndpointSigningError{err: fmt.Errorf("load active client assertion key: %w", pgx.ErrNoRows)}, want: true},
		{name: "KMS failed to answer", err: &tokenEndpointSigningError{err: errors.New("kms: deadline exceeded")}, want: false},
		{name: "not a signing failure", err: fmt.Errorf("lookup: %w", pgx.ErrNoRows), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, clientAssertionUnconfigured(fmt.Errorf("new refresh request: %w", tc.err)))
		})
	}
}

// A stored public key that cannot be used fails every signing attempt the
// same way, so it takes the administrator remedy rather than a retry.
func TestClientAssertionUnconfigured_UnusableStoredPublicKey(t *testing.T) {
	t.Parallel()

	_, err := serializeClientAssertion(t.Context(), nil, "projects/p/locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/1", "kid", []byte("not a jwk"), "client", "https://issuer.example.com", time.Now())
	require.Error(t, err)
	require.True(t, clientAssertionUnconfigured(&tokenEndpointSigningError{err: fmt.Errorf("sign private_key_jwt client assertion: %w", err)}))
}

// Building the KMS signer only validates stored key data, so a key version
// name it rejects is a configuration error, not a retryable signing failure.
func TestClientAssertionUnconfigured_InvalidStoredKeyVersionName(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	jwk, err := (&jose.JSONWebKey{Key: key.Public(), KeyID: "kid", Algorithm: string(jose.ES256), Use: "sig"}).MarshalJSON()
	require.NoError(t, err)

	_, err = serializeClientAssertion(t.Context(), nil, "not-a-key-version", "kid", jwk, "client", "https://issuer.example.com", time.Now())
	require.ErrorContains(t, err, "build client assertion signer")
	require.True(t, clientAssertionUnconfigured(&tokenEndpointSigningError{err: fmt.Errorf("sign private_key_jwt client assertion: %w", err)}))
}
