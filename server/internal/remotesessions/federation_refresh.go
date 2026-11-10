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

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
)

// FederatedRefreshFailure contains only local classifications, never upstream
// descriptions, request URLs, response bodies, or credentials.
type FederatedRefreshFailure string

const (
	FederatedRefreshInvalidGrant    FederatedRefreshFailure = "invalid_grant"
	FederatedRefreshConfiguration   FederatedRefreshFailure = "configuration"
	FederatedRefreshSetup           FederatedRefreshFailure = "setup"
	FederatedRefreshClientRejected  FederatedRefreshFailure = "client_rejected"
	FederatedRefreshAmbiguous       FederatedRefreshFailure = "ambiguous"
	FederatedRefreshRateLimited     FederatedRefreshFailure = "rate_limited"
	FederatedRefreshRetryable       FederatedRefreshFailure = "retryable"
	FederatedRefreshSetupRetryable  FederatedRefreshFailure = "setup_retryable"
	FederatedRefreshInvalidIdentity FederatedRefreshFailure = "invalid_identity"
)

type FederatedRefreshError struct {
	Kind FederatedRefreshFailure

	// RetryAfter is a 429's Retry-After, zero when absent.
	RetryAfter time.Duration
}

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
		return nil, &FederatedRefreshError{Kind: FederatedRefreshSetup, RetryAfter: 0}
	}
	if err := m.preflightFederatedSigner(p); err != nil {
		return nil, federatedRefreshSetupError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := m.validateFederatedMetadataHosts(ctx, p.issuer, p.metadata); err != nil {
		return nil, federatedRefreshSetupError(err)
	}
	doer, auth, err := m.federatedTokenClient(p)
	if err != nil {
		return nil, federatedRefreshSetupError(err)
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
	req, err := NewTokenEndpointRequest(ctx, p.metadata.TokenEndpoint, form, auth)
	if err != nil {
		return nil, federatedRefreshSetupError(classifyFederatedExchangeError(err))
	}
	logger := m.logger.With(attr.SlogOrganizationID(p.organizationID), attr.SlogRemoteSessionIssuerID(p.issuer.ID.String()), attr.SlogRemoteSessionClientID(p.client.ID.String()))
	tok, receivedAt, err := postFederatedRefresh(logger, doer, req)
	if err != nil {
		var failure *FederatedRefreshError
		if errors.As(err, &failure) && failure.Kind == FederatedRefreshClientRejected {
			m.pauseFederatedClient(ctx, logger, p)
		}
		return nil, err
	}
	return m.verifyFederatedRefresh(ctx, p, tok, receivedAt, expectedSubject, expectedNonce, doer)
}

// Keep the exchange result even if verification fails: its rotated credential
// can be quarantined, but neither its identity nor assertion is yet trusted.
func (m *ChallengeManager) verifyFederatedRefresh(ctx context.Context, p *FederatedProvider, tok tokenResponse, receivedAt time.Time, expectedSubject, expectedNonce string, doer httpDoer) (*FederatedRefreshResult, error) {
	result := &FederatedRefreshResult{Identity: nil, Credentials: federatedRefreshCredentials(tok, receivedAt)}
	if tok.IDToken == "" {
		return result, nil
	}
	identity, err := m.verifyFederatedIdentityMode(ctx, p, tok, "", expectedNonce, expectedSubject, doer, true)
	if err != nil {
		return result, classifyFederatedRefreshVerificationError(err)
	}
	result.Identity = identity
	return result, nil
}

// A successful POST may already have spent the submitted refresh token. If
// verification could not fetch keys, quarantine that generation rather than
// treating an unverified assertion as a proven security violation or retrying.
func classifyFederatedRefreshVerificationError(err error) error {
	if errors.Is(err, ErrFederatedUnavailable) {
		return &FederatedRefreshError{Kind: FederatedRefreshAmbiguous, RetryAfter: 0}
	}
	return &FederatedRefreshError{Kind: FederatedRefreshInvalidIdentity, RetryAfter: 0}
}

// maxFederatedRefreshResponseBytes bounds untrusted token endpoint responses.
// 64 KiB accommodates JWTs and provider metadata without allowing an upstream
// response to cause unbounded allocation; one extra byte detects truncation.
const maxFederatedRefreshResponseBytes = 64 << 10

func postFederatedRefresh(logger *slog.Logger, doer httpDoer, req *http.Request) (tokenResponse, time.Time, error) {
	var zero tokenResponse
	resp, err := doer.Do(req)
	if err != nil {
		return zero, time.Time{}, &FederatedRefreshError{Kind: FederatedRefreshAmbiguous, RetryAfter: 0}
	}
	defer func() { _ = resp.Body.Close() }()
	receivedAt := time.Now()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFederatedRefreshResponseBytes+1))
	if err != nil || len(body) > maxFederatedRefreshResponseBytes {
		return zero, time.Time{}, &FederatedRefreshError{Kind: FederatedRefreshAmbiguous, RetryAfter: 0}
	}
	if resp.StatusCode/100 != 2 {
		kind, code := classifyFederatedRefreshResponse(resp.StatusCode, body)
		logger.WarnContext(req.Context(), "federated refresh rejected",
			attr.SlogOutcome(string(kind)),
			attr.SlogHTTPResponseStatusCode(resp.StatusCode),
			attr.SlogOAuthError(loggableOAuthErrorCode(code)),
		)
		var retryAfter time.Duration
		if kind == FederatedRefreshRateLimited {
			retryAfter = oautherr.ParseRetryAfter(resp.Header.Get("Retry-After"), receivedAt)
		}
		return zero, time.Time{}, &FederatedRefreshError{Kind: kind, RetryAfter: retryAfter}
	}
	var tok tokenResponse
	if json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		return zero, time.Time{}, &FederatedRefreshError{Kind: FederatedRefreshAmbiguous, RetryAfter: 0}
	}
	return tok, receivedAt, nil
}

// federatedClientPause is how long a client rejection stops every person's
// refresh for that client, so one request per interval tests the fix.
const federatedClientPause = 5 * time.Minute

func federatedClientPauseKey(p *FederatedProvider) string {
	return "federatedRefreshClientPause:" + p.client.ID.String() + ":" + p.DelegationConfigurationHash()
}

// A cache failure must not stop refreshes, so it reads as not paused.
func (m *ChallengeManager) federatedClientPaused(ctx context.Context, p *FederatedProvider) bool {
	if m.locks == nil {
		return false
	}
	var paused bool
	err := m.locks.Get(ctx, federatedClientPauseKey(p), &paused)
	if err != nil && !errors.Is(err, redisCache.ErrCacheMiss) {
		m.logger.WarnContext(ctx, "read federated client pause", attr.SlogRemoteSessionClientID(p.client.ID.String()), attr.SlogError(err))
	}
	return err == nil && paused
}

func (m *ChallengeManager) pauseFederatedClient(ctx context.Context, logger *slog.Logger, p *FederatedProvider) {
	if m.locks == nil {
		return
	}
	// The first rejection sets the window, so late rejections cannot extend it.
	var err error
	if conditional, ok := m.locks.(cache.ConditionalCache); ok {
		_, err = conditional.SetIfAbsent(ctx, federatedClientPauseKey(p), true, federatedClientPause)
	} else {
		err = m.locks.Set(ctx, federatedClientPauseKey(p), true, federatedClientPause)
	}
	if err != nil {
		logger.WarnContext(ctx, "record federated client pause", attr.SlogError(err))
	}
}

// Only invalid_grant (RFC 6749 section 5.2) or a sign-in code (OIDC Core section 3.1.2.6) deletes the token.
func classifyFederatedRefreshResponse(status int, body []byte) (FederatedRefreshFailure, string) {
	parsed, _ := oautherr.ParseTokenError(body)
	code := oautherr.CanonicalTokenErrorCode(parsed.Code)
	failure := oautherr.ClassifyRefreshFailure(status, code)
	switch {
	// An intermediary can return a timeout, rate limit or 5xx after the issuer rotated.
	// The HTTP error is not evidence that the submitted refresh token is unspent.
	case failure.Status == oautherr.RefreshStatusRateLimited:
		return FederatedRefreshRateLimited, code
	case failure.Status == oautherr.RefreshStatusTimeout || failure.Status == oautherr.RefreshStatusServerError:
		return FederatedRefreshAmbiguous, code
	case failure.Code == oautherr.RefreshCodeInvalidGrant || failure.Code == oautherr.RefreshCodeSignInRequired:
		return FederatedRefreshInvalidGrant, code
	// Keeps the token: an administrator configured this client, so a sign-in can't fix it.
	case failure.Code == oautherr.RefreshCodeInvalidClient || failure.Code == oautherr.RefreshCodeUnauthorizedClient:
		return FederatedRefreshClientRejected, code
	case failure.Code == oautherr.RefreshCodeTransient:
		return FederatedRefreshRetryable, code
	}
	return FederatedRefreshConfiguration, code
}

// oauthErrorCodeUnrecognized replaces a code outside the IETF registry in logs.
const oauthErrorCodeUnrecognized = "unrecognized"

// The "error" member is provider-controlled text, so only registered codes reach logs.
func loggableOAuthErrorCode(code string) string {
	if code == "" || oautherr.IsIETFRegisteredCode(code) || oautherr.IsOIDCSignInCode(code) {
		return code
	}
	return oauthErrorCodeUnrecognized
}

func federatedRefreshCredentials(tok tokenResponse, receivedAt time.Time) FederatedRefreshCredentials {
	c := FederatedRefreshCredentials{EphemeralFederatedCredentials: EphemeralFederatedCredentials{refreshExpiresAt: nil, idToken: tok.IDToken, refreshToken: tok.RefreshToken, expiresIn: tok.ExpiresIn, refreshExpiresIn: tok.RefreshExpiresIn, receivedAt: receivedAt}}
	// Use the shortest reported bound, never the access-token expires_in.
	idle, hasIdle := tok.RefreshTokenTimeoutSeconds()
	absolute, hasAbsolute := tok.AuthorizationLifetimeSeconds()
	for _, bound := range []struct {
		seconds  int64
		reported bool
	}{{idle, hasIdle}, {absolute, hasAbsolute}} {
		if !bound.reported {
			continue
		}
		expiry := receivedAt.Add(time.Duration(bound.seconds) * time.Second)
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

// Nothing was sent, so the provider has said nothing about the token.
func federatedRefreshSetupError(err error) error {
	kind := FederatedRefreshSetup
	if errors.Is(err, ErrFederatedUnavailable) {
		kind = FederatedRefreshSetupRetryable
	}
	failure := &FederatedRefreshError{Kind: kind, RetryAfter: 0}
	if errors.Is(err, ErrFederatedSigning) {
		// Preserve only the safe owner marker, never a signing service's raw error.
		return errors.Join(failure, ErrFederatedSigning)
	}
	return failure
}
