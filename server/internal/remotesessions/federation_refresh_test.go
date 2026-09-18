package remotesessions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

func TestFederatedRefreshVerification(t *testing.T) {
	t.Parallel()
	key, keys, _ := newRSAKeyPolicyFixture(t, 2048)
	p := federatedFixture(t)
	p.metadata.JwksURI = rsaKeyPolicyJWKSURI
	m := &ChallengeManager{idTokens: NewIDTokenVerifier(keys)}
	for _, tc := range []struct {
		name, claim, value string
		valid              bool
	}{
		{name: "same nonce", valid: true},
		{name: "invalid signature"},
		{name: "omitted nonce", claim: "nonce", valid: true},
		{name: "email not required", claim: "email", valid: true},
		{name: "changed nonce", claim: "nonce", value: `"different"`},
		{name: "null nonce", claim: "nonce", value: `null`},
		{name: "changed subject", claim: "sub", value: `"different"`},
		{name: "changed issuer", claim: "iss", value: `"https://other.example.test"`},
		{name: "changed audience", claim: "aud", value: `"other"`},
		{name: "wrong azp", claim: "azp", value: `"other"`},
		{name: "missing azp", claim: "aud", value: `["upstream-client","other"]`},
		{name: "expired", claim: "exp", value: `1`},
		{name: "missing iat", claim: "iat"},
		{name: "future issued", claim: "iat", value: `4102444800`},
		{name: "not yet valid", claim: "nbf", value: `4102444800`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := federatedClaims(t, p, time.Now())
			if tc.claim != "" {
				if tc.value == "" {
					delete(claims, tc.claim)
				} else {
					claims[tc.claim] = json.RawMessage(tc.value)
				}
			}
			payload, err := json.Marshal(claims)
			require.NoError(t, err)
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "example-key"))
			require.NoError(t, err)
			signed, err := signer.Sign(payload)
			require.NoError(t, err)
			raw, err := signed.CompactSerialize()
			require.NoError(t, err)
			if tc.name == "invalid signature" {
				parts := strings.Split(raw, ".")
				parts[2] = strings.Repeat("A", len(parts[2]))
				raw = strings.Join(parts, ".")
			}
			identity, err := m.verifyFederatedIdentityMode(t.Context(), p, tokenResponse{IDToken: raw}, "", "nonce", "foreign-subject", nil, true)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, "foreign-subject", identity.Subject)
				require.False(t, identity.ExpiresAt.IsZero())
				if tc.claim == "nonce" {
					require.Empty(t, identity.Nonce)
				} else {
					require.Equal(t, "nonce", identity.Nonce)
				}
			} else {
				require.ErrorIs(t, err, ErrFederatedIdentity)
				require.Nil(t, identity)
			}
		})
	}
}

func TestFederatedRefreshResponse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		transport error
		kind      FederatedRefreshFailure
	}{
		{name: "rotation without assertion", status: 200, body: `{"access_token":"secret-access","refresh_token":"secret-rotated"}`},
		{name: "omitted rotation", status: 200, body: `{"access_token":"secret-access"}`},
		{name: "invalid grant", status: 400, body: `{"error":"invalid_grant","error_description":"secret-upstream"}`, kind: FederatedRefreshInvalidGrant},
		{name: "invalid client", status: 401, body: `{"error":"invalid_client","error_description":"secret-upstream"}`, kind: FederatedRefreshConfiguration},
		{name: "rate limited", status: 429, body: `secret-upstream`, kind: FederatedRefreshRetryable},
		{name: "unavailable", status: 503, body: `secret-upstream`, kind: FederatedRefreshAmbiguous},
		{name: "timeout", transport: context.DeadlineExceeded, kind: FederatedRefreshAmbiguous},
		{name: "timeout status", status: 408, kind: FederatedRefreshAmbiguous},
		{name: "gateway timeout", status: 504, kind: FederatedRefreshAmbiguous},
		{name: "internal error", status: 500, body: `{"error":"server_error"}`, kind: FederatedRefreshAmbiguous},
		{name: "bad gateway", status: 502, kind: FederatedRefreshAmbiguous},
		{name: "malformed success", status: 200, body: `secret-upstream`, kind: FederatedRefreshAmbiguous},
		{name: "oversized success", status: 200, body: strings.Repeat("x", (64<<10)+1), kind: FederatedRefreshAmbiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			doer := federatedHTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if tc.transport != nil {
					return nil, tc.transport
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://idp.example.test/token", nil)
			require.NoError(t, err)
			tok, received, err := postFederatedRefresh(doer, req)
			require.Equal(t, 1, calls)
			if tc.kind != "" {
				var failure *FederatedRefreshError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, tc.kind, failure.Kind)
				require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), "secret-")
				return
			}
			require.NoError(t, err)
			require.Empty(t, tok.IDToken)
			c := federatedRefreshCredentials(tok, received)
			require.Empty(t, tok.raw)
			require.Empty(t, c.IDToken())
			if tc.name == "rotation without assertion" {
				require.Equal(t, "secret-rotated", c.RefreshToken())
			} else {
				require.Empty(t, c.RefreshToken())
			}
			require.Nil(t, c.RefreshExpiresAt())
			for _, v := range []any{c, &c, FederatedRefreshResult{Credentials: c}} {
				b, err := json.Marshal(v)
				require.NoError(t, err)
				require.NotContains(t, string(b), "secret-")
				require.NotContains(t, fmt.Sprintf("%v %+v %#v", v, v, v), "secret-")
			}
		})
	}
}

func TestFederatedRefreshExpiry(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		body    string
		seconds int64
	}{
		{body: `{"expires_in":3600}`},
		{body: `{"refresh_expires_in":null}`},

		{body: `{"refresh_expires_in":-1}`},
		{body: `{"refresh_expires_in":600}`, seconds: 600},
		{body: `{"refresh_token_timeout":600,"authorization_expires_in":300}`, seconds: 300},
	} {
		var tok tokenResponse
		require.NoError(t, json.Unmarshal([]byte(tc.body), &tok))
		expiry := federatedRefreshCredentials(tok, now).RefreshExpiresAt()
		if tc.seconds == 0 {
			require.Nil(t, expiry)
		} else {
			require.Equal(t, now.Add(time.Duration(tc.seconds)*time.Second), *expiry)
		}
	}
}

func TestFederatedRefreshExplicitZeroExpiry(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	var tok tokenResponse
	require.NoError(t, json.Unmarshal([]byte(`{"refresh_token_timeout":0}`), &tok))
	expiry := federatedRefreshCredentials(tok, now).RefreshExpiresAt()
	require.NotNil(t, expiry)
	require.Equal(t, now, *expiry)
}
