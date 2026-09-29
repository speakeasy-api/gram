package identitychaining

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testIdPIssuer       = "https://idp.example.test/tenant"
	testResourceIssuer  = "https://as.resource.example.test"
	testResource        = "https://api.resource.example.test/"
	testResourceClient  = "resource-client"
	testUpstreamSubject = "00u-upstream-subject"
)

func testSelection() selection {
	return selection{
		bindingID:        uuid.New(),
		generation:       1,
		remoteIssuerID:   uuid.New(),
		clientID:         uuid.New(),
		externalClientID: testResourceClient,
		issuer:           testResourceIssuer,
		resource:         testResource,
		scopes:           []string{"read", "write"},
		trustedIssuerID:  uuid.New(),
		trustedClientID:  uuid.New(),
	}
}

// recordingPoster answers every grant with one response and records the forms.
type recordingPoster struct {
	mu    sync.Mutex
	body  map[string]any
	err   error
	forms []url.Values
}

func (p *recordingPoster) Post(_ context.Context, form url.Values) (remotesessions.TokenResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.forms = append(p.forms, form)
	if p.err != nil {
		return remotesessions.TokenResponse{}, p.err
	}
	raw, err := json.Marshal(p.body)
	if err != nil {
		return remotesessions.TokenResponse{}, fmt.Errorf("encode fake token response: %w", err)
	}
	tok, err := remotesessions.DecodeTokenResponse(raw)
	if err != nil {
		return remotesessions.TokenResponse{}, fmt.Errorf("decode fake token response: %w", err)
	}
	return tok, nil
}

func (p *recordingPoster) form(t *testing.T) url.Values {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.forms, 1)
	return p.forms[0]
}

func testChainer(t *testing.T) (*Chainer, time.Time) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	return &Chainer{logger: testenv.NewLogger(t), db: nil, enc: nil, challenges: nil, delegation: nil, keys: nil, locks: nil, now: func() time.Time { return now }}, now
}

func TestExchange_RequestsIDJAGForTheResource(t *testing.T) {
	t.Parallel()
	idp := &recordingPoster{body: map[string]any{"access_token": "id-jag-value", "issued_token_type": oauthwire.TokenTypeIDJAG, "token_type": "N_A", "expires_in": 300}}
	grant, outcome := exchange(t.Context(), idp, "id-token-value", testSelection())
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, "id-jag-value", grant)

	form := idp.form(t)
	require.Equal(t, oauthwire.GrantTypeTokenExchange, form.Get("grant_type"))
	require.Equal(t, "id-token-value", form.Get("subject_token"))
	require.Equal(t, oauthwire.TokenTypeIDToken, form.Get("subject_token_type"))
	require.Equal(t, oauthwire.TokenTypeIDJAG, form.Get("requested_token_type"))
	require.Equal(t, testResourceIssuer, form.Get("audience"), "audience is the resource authorization server issuer")
	require.Equal(t, testResource, form.Get("resource"), "the canonical resource keeps its trailing slash")
	require.Equal(t, "read write", form.Get("scope"))
	require.NotContains(t, form.Encode(), testResourceClient, "the resource client is never sent to the identity provider")
}

func TestExchange_RejectsOtherIssuedTokenType(t *testing.T) {
	t.Parallel()
	idp := &recordingPoster{body: map[string]any{"access_token": "not-an-id-jag", "issued_token_type": "urn:ietf:params:oauth:token-type:access_token"}}
	_, outcome := exchange(t.Context(), idp, "id-token-value", testSelection())
	require.Equal(t, ReasonMalformedResponse, outcome.Reason)
	require.Equal(t, StageExchange, outcome.Stage)
}

func TestExchange_ClassifiesProviderRejection(t *testing.T) {
	t.Parallel()
	idp := &recordingPoster{err: &remotesessions.TokenEndpointError{StatusCode: http.StatusBadRequest, Code: "invalid_target", Transport: false, Signing: false}}
	_, outcome := exchange(t.Context(), idp, "id-token-value", testSelection())
	require.Equal(t, Outcome{Stage: StageExchange, Reason: ReasonInvalidTarget, Confidence: ConfidenceInferred, Retryable: false, Cached: false}, outcome)
}

func TestRedeem_PresentsGrantAndDiscardsRefreshToken(t *testing.T) {
	t.Parallel()
	resourceAS := &recordingPoster{body: map[string]any{"access_token": "downstream-token", "token_type": "Bearer", "expires_in": 3600, "refresh_token": "downstream-refresh", "scope": "read write extra"}}
	c, now := testChainer(t)
	cred, outcome := c.redeem(t.Context(), resourceAS, "id-jag-value", testSelection())
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)

	form := resourceAS.form(t)
	require.Equal(t, oauthwire.GrantTypeJWTBearer, form.Get("grant_type"))
	require.Equal(t, "id-jag-value", form.Get("assertion"))
	require.Empty(t, form.Get("subject_token"), "the ID token never reaches the resource authorization server")

	require.Equal(t, "downstream-token", cred.accessToken)
	require.Equal(t, now.Add(time.Hour), cred.expiresAt)
	require.True(t, cred.refreshObserved, "a returned refresh token is recorded as observed")
	require.Equal(t, []string{"read", "write", "extra"}, cred.grantedScopes)
}

func TestRedeem_RejectsUnusableResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		body   map[string]any
		reason Reason
	}{
		{"narrowed scope", map[string]any{"access_token": "t", "token_type": "Bearer", "expires_in": 3600, "scope": "read"}, ReasonInsufficientScope},
		{"proof of possession token", map[string]any{"access_token": "t", "token_type": "DPoP", "expires_in": 3600}, ReasonMalformedResponse},
		{"no access token", map[string]any{"token_type": "Bearer", "expires_in": 3600}, ReasonMalformedResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, _ := testChainer(t)
			_, outcome := c.redeem(t.Context(), &recordingPoster{body: tc.body}, "id-jag-value", testSelection())
			require.Equal(t, tc.reason, outcome.Reason)
			require.Equal(t, StageRedemption, outcome.Stage)
		})
	}
}

func TestRedeem_BoundsReportedLifetime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body map[string]any
		want time.Duration
	}{
		{"unknown expiry is capped", map[string]any{"access_token": "opaque-token", "token_type": "Bearer"}, unknownExpiryLifetime},
		{"short-lived token is usable once", map[string]any{"access_token": "t", "token_type": "Bearer", "expires_in": 30}, 30 * time.Second},
		{"long-lived token is capped", map[string]any{"access_token": "t", "token_type": "Bearer", "expires_in": 86400}, maxAccessLifetime},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, now := testChainer(t)
			cred, outcome := c.redeem(t.Context(), &recordingPoster{body: tc.body}, "id-jag-value", testSelection())
			require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
			require.Equal(t, now.Add(tc.want), cred.expiresAt)
		})
	}
}

func TestRedeem_OmittedScopeGrantsRequested(t *testing.T) {
	t.Parallel()
	c, _ := testChainer(t)
	cred, outcome := c.redeem(t.Context(), &recordingPoster{body: map[string]any{"access_token": "t", "token_type": "Bearer", "expires_in": 3600}}, "id-jag-value", testSelection())
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.False(t, cred.refreshObserved)
	require.Equal(t, []string{"read", "write"}, cred.grantedScopes)
}

func TestClassifyEndpointError(t *testing.T) {
	t.Parallel()
	rejection := func(status int, code string) error {
		return &remotesessions.TokenEndpointError{StatusCode: status, Code: code, Transport: false, Signing: false}
	}
	for _, tc := range []struct {
		name       string
		err        error
		reason     Reason
		confidence Confidence
		retryable  bool
	}{
		{"transport", &remotesessions.TokenEndpointError{StatusCode: 0, Code: "", Transport: true, Signing: false}, ReasonTransientFailure, ConfidenceUnknown, true},
		{"signing", &remotesessions.TokenEndpointError{StatusCode: 0, Code: "", Transport: false, Signing: true}, ReasonTransientFailure, ConfidenceUnknown, true},
		{"server error", rejection(http.StatusBadGateway, ""), ReasonTransientFailure, ConfidenceInferred, true},
		{"rate limited", rejection(http.StatusTooManyRequests, ""), ReasonTransientFailure, ConfidenceInferred, true},
		{"invalid target", rejection(http.StatusBadRequest, "invalid_target"), ReasonInvalidTarget, ConfidenceInferred, false},
		{"unauthorized client", rejection(http.StatusBadRequest, "unauthorized_client"), ReasonScopePolicyDenied, ConfidenceInferred, false},
		{"access denied", rejection(http.StatusBadRequest, "access_denied"), ReasonAccessDenied, ConfidenceInferred, false},
		{"invalid client", rejection(http.StatusUnauthorized, "invalid_client"), ReasonInvalidClient, ConfidenceInferred, false},
		{"invalid grant stays ambiguous", rejection(http.StatusBadRequest, "invalid_grant"), ReasonInvalidGrant, ConfidenceUnknown, false},
		{"unknown code", rejection(http.StatusBadRequest, "something_else"), ReasonUnknownRejection, ConfidenceUnknown, false},
		{"undecodable success", errors.New("decode token response"), ReasonMalformedResponse, ConfidenceVerified, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, Outcome{Stage: StageRedemption, Reason: tc.reason, Confidence: tc.confidence, Retryable: tc.retryable, Cached: false}, classifyEndpointError(StageRedemption, tc.err))
		})
	}
}

func TestOutcome_CacheableAndApplicable(t *testing.T) {
	t.Parallel()
	require.True(t, newOutcome(StageExchange, ReasonInvalidTarget, ConfidenceInferred, false).cacheable())
	require.True(t, newOutcome(StageValidation, ReasonMalformedAssertion, ConfidenceVerified, false).cacheable())
	require.False(t, newOutcome(StageExchange, ReasonTransientFailure, ConfidenceInferred, true).cacheable(), "transient failures retry immediately")
	require.False(t, newOutcome(StageDelegation, ReasonReauthenticationRequired, ConfidenceVerified, false).cacheable(), "a fresh sign-in must take effect immediately")
	require.False(t, notApplicable.Applicable())
	require.False(t, bindingNotReady.Applicable())
	require.True(t, success.Applicable())
}

// claimsVerifier stands in for the identity provider's signature check: it
// decodes fixed claims under a fixed typ header, or fails with err.
type claimsVerifier struct {
	typ    string
	claims map[string]any
	err    error
}

func (v claimsVerifier) VerifyAssertion(_ context.Context, _ string, dest ...any) (jose.Header, error) {
	if v.err != nil {
		return jose.Header{}, v.err
	}
	raw, err := json.Marshal(v.claims)
	if err != nil {
		return jose.Header{}, fmt.Errorf("encode fake claims: %w", err)
	}
	for _, d := range dest {
		if err := json.Unmarshal(raw, d); err != nil {
			return jose.Header{}, fmt.Errorf("decode fake claims: %w", err)
		}
	}
	return jose.Header{ExtraHeaders: map[jose.HeaderKey]any{jose.HeaderType: v.typ}}, nil
}

func validGrantClaims(sel selection, now time.Time) map[string]any {
	return map[string]any{
		"iss": testIdPIssuer, "sub": testUpstreamSubject, "aud": sel.issuer,
		"client_id": sel.externalClientID, "resource": sel.resource, "scope": "read write",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "jti": uuid.NewString(),
	}
}

func TestValidateGrant_AcceptsBoundGrant(t *testing.T) {
	t.Parallel()
	c, now := testChainer(t)
	sel := testSelection()
	for _, typ := range []string{"oauth-id-jag+jwt", "application/oauth-id-jag+jwt"} {
		outcome := c.validateGrant(t.Context(), testenv.NewLogger(t), claimsVerifier{typ: typ, claims: validGrantClaims(sel, now), err: nil}, testIdPIssuer, "raw", testUpstreamSubject, sel)
		require.True(t, outcome.Succeeded(), "typ %q: %+v", typ, outcome)
	}
	arrayResource := validGrantClaims(sel, now)
	arrayResource["resource"] = []string{testResource}
	outcome := c.validateGrant(t.Context(), testenv.NewLogger(t), claimsVerifier{typ: "oauth-id-jag+jwt", claims: arrayResource, err: nil}, testIdPIssuer, "raw", testUpstreamSubject, sel)
	require.True(t, outcome.Succeeded(), "a single-element resource array names the same resource: %+v", outcome)
}

func TestValidateGrant_RejectsUnboundClaims(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		typ    string
		mutate func(claims map[string]any, now time.Time)
	}{
		{"plain jwt type", "JWT", func(map[string]any, time.Time) {}},
		{"other issuer", "oauth-id-jag+jwt", func(c map[string]any, _ time.Time) { c["iss"] = "https://attacker.example.test" }},
		{"other audience", "oauth-id-jag+jwt", func(c map[string]any, _ time.Time) { c["aud"] = "https://other-as.example.test" }},
		{"several audiences", "oauth-id-jag+jwt", func(c map[string]any, _ time.Time) {
			c["aud"] = []string{testResourceIssuer, "https://other-as.example.test"}
		}},
		{"other client", "oauth-id-jag+jwt", func(c map[string]any, _ time.Time) { c["client_id"] = "other-client" }},
		{"other resource", "oauth-id-jag+jwt", func(c map[string]any, _ time.Time) { c["resource"] = "https://api.resource.example.test" }},
		{"another human", "oauth-id-jag+jwt", func(c map[string]any, _ time.Time) { c["sub"] = "00u-someone-else" }},
		{"scope escalation", "oauth-id-jag+jwt", func(c map[string]any, _ time.Time) { c["scope"] = "read admin" }},
		{"expired", "oauth-id-jag+jwt", func(c map[string]any, now time.Time) { c["exp"] = now.Add(-5 * time.Minute).Unix() }},
		{"missing iat", "oauth-id-jag+jwt", func(c map[string]any, _ time.Time) { delete(c, "iat") }},
		{"excessive lifetime", "oauth-id-jag+jwt", func(c map[string]any, now time.Time) { c["exp"] = now.Add(time.Hour).Unix() }},
		{"issued long before expiry", "oauth-id-jag+jwt", func(c map[string]any, now time.Time) { c["iat"] = now.Add(-2 * time.Hour).Unix() }},
		{"null resource", "oauth-id-jag+jwt", func(c map[string]any, _ time.Time) { c["resource"] = nil }},
		{"narrowed scope", "oauth-id-jag+jwt", func(c map[string]any, _ time.Time) { c["scope"] = "read" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, now := testChainer(t)
			sel := testSelection()
			claims := validGrantClaims(sel, now)
			tc.mutate(claims, now)
			outcome := c.validateGrant(t.Context(), testenv.NewLogger(t), claimsVerifier{typ: tc.typ, claims: claims, err: nil}, testIdPIssuer, "raw", testUpstreamSubject, sel)
			require.Equal(t, Outcome{Stage: StageValidation, Reason: ReasonMalformedAssertion, Confidence: ConfidenceVerified, Retryable: false, Cached: false}, outcome)
		})
	}
}

func TestValidateGrant_ClassifiesVerificationFailures(t *testing.T) {
	t.Parallel()
	c, _ := testChainer(t)
	sel := testSelection()
	forged := c.validateGrant(t.Context(), testenv.NewLogger(t), claimsVerifier{typ: "", claims: nil, err: errors.New("verify jwt signature")}, testIdPIssuer, "raw", testUpstreamSubject, sel)
	require.Equal(t, ReasonMalformedAssertion, forged.Reason)
	require.False(t, forged.Retryable)

	unavailable := c.validateGrant(t.Context(), testenv.NewLogger(t), claimsVerifier{typ: "", claims: nil, err: remotesessions.ErrJWTKeySetUnavailable}, testIdPIssuer, "raw", testUpstreamSubject, sel)
	require.Equal(t, ReasonTransientFailure, unavailable.Reason, "an unreadable key set says nothing about the grant")
	require.True(t, unavailable.Retryable)
}
