package remotesessions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// FederatedRefreshFailure contains only local classifications, never upstream
// descriptions, request URLs, response bodies, or credentials.
type FederatedRefreshFailure string

const (
	FederatedRefreshInvalidGrant    FederatedRefreshFailure = "invalid_grant"
	FederatedRefreshConfiguration   FederatedRefreshFailure = "configuration"
	FederatedRefreshAmbiguous       FederatedRefreshFailure = "ambiguous"
	FederatedRefreshRetryable       FederatedRefreshFailure = "retryable"
	FederatedRefreshInvalidIdentity FederatedRefreshFailure = "invalid_identity"
)

type FederatedRefreshError struct{ Kind FederatedRefreshFailure }

func (e *FederatedRefreshError) Error() string { return "federated refresh: " + string(e.Kind) }

// FederatedRefreshResult distinguishes successful rotation without a portable
// assertion (Identity == nil) from a rejected grant. Persist rotation before
// exposing Identity. A missing refresh token preserves the caller's current
// token; this protocol layer does not merge or persist credentials.
type FederatedRefreshResult struct {
	Identity    *FederatedIdentity
	Credentials FederatedRefreshCredentials
}

// FederatedRefreshCredentials preserves rotation credentials for authorized
// encrypted retention, including when no ID token was issued. Token getters
// return secrets and must never be logged. Expiry is nil when unknown, not an
// infinite lifetime. Access tokens and raw provider responses are not retained.
type FederatedRefreshCredentials struct {
	EphemeralFederatedCredentials
}

func (c FederatedRefreshCredentials) String() string {
	return "[redacted federated refresh credentials]"
}
func (c FederatedRefreshCredentials) GoString() string             { return c.String() }
func (c FederatedRefreshCredentials) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (c FederatedRefreshCredentials) LogValue() slog.Value         { return slog.StringValue(c.String()) }
func (r FederatedRefreshResult) String() string                    { return "[federated refresh result]" }
func (r FederatedRefreshResult) GoString() string                  { return r.String() }
func (r FederatedRefreshResult) MarshalJSON() ([]byte, error)      { return []byte("{}"), nil }
func (r FederatedRefreshResult) LogValue() slog.Value              { return slog.StringValue(r.String()) }

// RefreshFederatedIdentity performs exactly one refresh POST. The owner must
// serialize rotating grants and check live configuration/authorization first.
// Ambiguous failures must not blindly replay a potentially spent refresh token.
func (m *ChallengeManager) RefreshFederatedIdentity(ctx context.Context, p *FederatedProvider, refreshToken, expectedSubject, expectedNonce string) (*FederatedRefreshResult, error) {
	if p == nil || refreshToken == "" || expectedSubject == "" {
		return nil, &FederatedRefreshError{Kind: FederatedRefreshConfiguration}
	}
	if err := m.preflightFederatedSigner(p); err != nil {
		return nil, federatedRefreshSetupError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := m.validateFederatedMetadataHosts(ctx, p.issuer, p.metadata); err != nil {
		return nil, &FederatedRefreshError{Kind: FederatedRefreshConfiguration}
	}
	doer, auth, err := m.federatedTokenClient(p)
	if err != nil {
		return nil, &FederatedRefreshError{Kind: FederatedRefreshConfiguration}
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
	req, err := newTokenEndpointRequest(ctx, p.metadata.TokenEndpoint, form, auth)
	if err != nil {
		return nil, federatedRefreshSetupError(classifyFederatedExchangeError(err))
	}
	tok, receivedAt, err := postFederatedRefresh(doer, req)
	if err != nil {
		return nil, err
	}
	result := &FederatedRefreshResult{Identity: nil, Credentials: federatedRefreshCredentials(tok, receivedAt)}
	if tok.IDToken == "" {
		return result, nil
	}
	identity, err := m.verifyFederatedIdentityMode(ctx, p, tok, "", expectedNonce, expectedSubject, doer, true)
	if err != nil {
		return nil, classifyFederatedRefreshVerificationError(err)
	}
	result.Identity = identity
	return result, nil
}

// A successful POST may already have spent the submitted refresh token. If
// verification could not fetch keys, quarantine that generation rather than
// treating an unverified assertion as a proven security violation or retrying.
func classifyFederatedRefreshVerificationError(err error) error {
	if errors.Is(err, ErrFederatedUnavailable) {
		return &FederatedRefreshError{Kind: FederatedRefreshAmbiguous}
	}
	return &FederatedRefreshError{Kind: FederatedRefreshInvalidIdentity}
}

// maxFederatedRefreshResponseBytes bounds untrusted token endpoint responses.
// 64 KiB accommodates JWTs and provider metadata without allowing an upstream
// response to cause unbounded allocation; one extra byte detects truncation.
const maxFederatedRefreshResponseBytes = 64 << 10

func postFederatedRefresh(doer httpDoer, req *http.Request) (tokenResponse, time.Time, error) {
	var zero tokenResponse
	resp, err := doer.Do(req)
	if err != nil {
		return zero, time.Time{}, &FederatedRefreshError{Kind: FederatedRefreshAmbiguous}
	}
	defer func() { _ = resp.Body.Close() }()
	receivedAt := time.Now()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFederatedRefreshResponseBytes+1))
	if err != nil || len(body) > maxFederatedRefreshResponseBytes {
		return zero, time.Time{}, &FederatedRefreshError{Kind: FederatedRefreshAmbiguous}
	}
	if resp.StatusCode/100 != 2 {
		return zero, time.Time{}, classifyFederatedRefreshResponse(resp.StatusCode, body)
	}
	var tok tokenResponse
	if json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		return zero, time.Time{}, &FederatedRefreshError{Kind: FederatedRefreshAmbiguous}
	}
	return tok, receivedAt, nil
}

func classifyFederatedRefreshResponse(status int, body []byte) error {
	var response struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &response)
	kind := FederatedRefreshConfiguration
	switch {
	// An intermediary can return a timeout, rate limit or 5xx after the issuer rotated.
	// The HTTP error is not evidence that the submitted refresh token is unspent.
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		kind = FederatedRefreshAmbiguous
	case response.Error == "invalid_grant":
		kind = FederatedRefreshInvalidGrant
	case response.Error == "invalid_client" || response.Error == "unauthorized_client":
		kind = FederatedRefreshConfiguration
	case response.Error == "temporarily_unavailable" || response.Error == "server_error":
		kind = FederatedRefreshRetryable
	}
	return &FederatedRefreshError{Kind: kind}
}

func federatedRefreshCredentials(tok tokenResponse, receivedAt time.Time) FederatedRefreshCredentials {
	c := FederatedRefreshCredentials{EphemeralFederatedCredentials: EphemeralFederatedCredentials{refreshExpiresAt: nil, idToken: tok.IDToken, refreshToken: tok.RefreshToken, expiresIn: tok.ExpiresIn, refreshExpiresIn: tok.RefreshExpiresIn, receivedAt: receivedAt}}
	// Use the shortest reported bound, never the access-token expires_in.
	// Standard explicit zero is distinct from absent; provider aliases follow
	// the shared parser and count only when positive.
	idle, hasIdle := tok.RefreshTokenTimeoutSeconds()
	absolute, hasAbsolute := tok.AuthorizationLifetimeSeconds()
	for _, bound := range []struct {
		seconds  int64
		reported bool
	}{{idle, hasIdle}, {absolute, hasAbsolute}} {
		seconds := bound.seconds
		if !bound.reported || seconds > int64((1<<63-1)/time.Second) {
			continue
		}
		if seconds < 0 {
			seconds = 0
		}
		expiry := receivedAt.Add(time.Duration(seconds) * time.Second)
		if c.refreshExpiresAt == nil || expiry.Before(*c.refreshExpiresAt) {
			c.refreshExpiresAt = &expiry
		}
	}
	return c
}

// Is permits errors.Is against another sanitized classification without exposing
// any upstream error through an unwrap chain.
func (e *FederatedRefreshError) Is(target error) bool {
	other, ok := target.(*FederatedRefreshError)
	return ok && other != nil && e.Kind == other.Kind
}

func federatedRefreshSetupError(err error) error {
	kind := FederatedRefreshConfiguration
	if errors.Is(err, ErrFederatedUnavailable) {
		kind = FederatedRefreshRetryable
	}
	return &FederatedRefreshError{Kind: kind}
}
