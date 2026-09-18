package remotesessions

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/stretchr/testify/require"
)

func TestFederatedRefreshVerificationDependencies(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "example-key", Algorithm: "RS256", Use: "sig"}}})
	require.NoError(t, err)
	for _, tc := range []struct {
		name                     string
		fetchError               error
		status                   int
		badSignature, unknownKey bool
		want                     FederatedRefreshFailure
	}{
		{name: "JWKS timeout", fetchError: context.DeadlineExceeded, want: FederatedRefreshAmbiguous},
		{name: "JWKS cancellation", fetchError: context.Canceled, want: FederatedRefreshAmbiguous},
		{name: "JWKS outage", status: 503, want: FederatedRefreshAmbiguous},
		{name: "bad signature", badSignature: true, want: FederatedRefreshInvalidIdentity},
		{name: "unknown signing key", unknownKey: true, want: FederatedRefreshInvalidIdentity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "submitted-refresh", 30*time.Second), true))
			before, err := store.load(t.Context(), b)
			require.NoError(t, err)
			logger := testenv.NewLogger(t)
			keys, err := jwks.NewKeyResolver(jwks.NewResolver(federatedPublicPolicy(t), testenv.NewMeterProvider(t), logger), jwks.NewMemoryCache(), ratelimit.New(nil, "federated-refresh-dependency-test", ratelimit.PerMinute(10)), nil, logger)
			require.NoError(t, err)
			manager := &ChallengeManager{idTokens: NewIDTokenVerifier(keys)}
			kid := "example-key"
			if tc.unknownKey {
				kid = "unknown-key"
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", kid))
			require.NoError(t, err)
			claims := federatedClaims(t, p, time.Now())
			claims["sub"] = json.RawMessage(`"secret-subject"`)
			claims["nonce"] = json.RawMessage(`"secret-nonce"`)
			payload, err := json.Marshal(claims)
			require.NoError(t, err)
			signed, err := signer.Sign(payload)
			require.NoError(t, err)
			raw, err := signed.CompactSerialize()
			require.NoError(t, err)
			if tc.badSignature {
				parts := strings.Split(raw, ".")
				parts[2] = strings.Repeat("A", len(parts[2]))
				raw = strings.Join(parts, ".")
			}
			body, err := json.Marshal(map[string]any{"access_token": "secret-access", "id_token": raw, "refresh_token": "secret-rotated"})
			require.NoError(t, err)
			posts, fetches := 0, 0
			doer := federatedHTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodPost {
					posts++
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
				}
				fetches++
				if tc.fetchError != nil {
					return nil, fmt.Errorf("secret-provider-details: %w", tc.fetchError)
				}
				if tc.status != 0 {
					return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("secret-provider-details")), Header: http.Header{}}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(document))), Header: http.Header{"Content-Type": {"application/json"}}}, nil
			})
			s.refreshIdentity = func(ctx context.Context, p *FederatedProvider, refresh, subject, nonce string) (*FederatedRefreshResult, error) {
				// Exercise the production POST decoder, real JWKS resolver and common
				// verifier before the exact mapper used by RefreshFederatedIdentity.
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.metadata.TokenEndpoint, nil)
				require.NoError(t, err)
				tok, _, err := postFederatedRefresh(doer, req)
				require.NoError(t, err)
				require.Equal(t, "secret-rotated", tok.RefreshToken)
				identity, err := manager.verifyFederatedIdentityMode(ctx, p, tok, "", nonce, subject, doer, true)
				require.Nil(t, identity)
				require.Error(t, err)
				if tc.want == FederatedRefreshAmbiguous {
					require.ErrorIs(t, err, ErrFederatedUnavailable)
				} else {
					require.ErrorIs(t, err, ErrFederatedIdentity)
				}
				mapped := classifyFederatedRefreshVerificationError(err)
				var failure *FederatedRefreshError
				require.ErrorAs(t, mapped, &failure)
				require.Equal(t, tc.want, failure.Kind)
				require.NoError(t, errors.Unwrap(mapped))
				require.NotContains(t, fmt.Sprintf("%v %+v %#v", mapped, mapped, mapped), "secret-")
				return nil, mapped
			}
			assertion, err := s.Resolve(t.Context(), b, allow)
			require.Empty(t, assertion.Value())
			require.Equal(t, 1, posts)
			require.Positive(t, fetches)
			after, loadErr := store.load(t.Context(), b)
			require.NoError(t, loadErr)
			if tc.want == FederatedRefreshAmbiguous {
				require.ErrorIs(t, err, ErrDelegationTemporary)
				require.Equal(t, before.refresh, after.refresh)
				require.Equal(t, before.subject, after.subject)
				require.Equal(t, before.nonce, after.nonce)
				require.NotEqual(t, uuid.Nil, after.claim)
				// Even well after retryAfter, never resubmit the potentially spent token.
				later := s.now().Add(24 * time.Hour)
				s.now = func() time.Time { return later }
				_, err = s.Resolve(t.Context(), b, allow)
				require.ErrorIs(t, err, ErrDelegationTemporary)
			} else {
				require.ErrorIs(t, err, ErrDelegationConfiguration)
				require.Empty(t, after.refresh)
				require.Empty(t, after.assertion)
				require.Empty(t, after.subject)
				_, err = s.Resolve(t.Context(), b, allow)
				require.ErrorIs(t, err, ErrDelegationConfiguration)
			}
			require.Equal(t, 1, posts)
		})
	}
}
