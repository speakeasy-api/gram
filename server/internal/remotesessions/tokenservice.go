// tokenservice.go is the MCP-runtime side of the remote-session flow.
// challenge.go drives the *login* leg (build authz URL, exchange code,
// persist tokens). This file drives the *use* leg: given a subject the
// MCP runtime has just authenticated via a Gram user-session JWT, find
// the upstream access token to forward on the request.
//
// Two entry points exposed:
//
//   - ResolveAccessToken: per-client primitive. Given a
//     remote_session_client id and a subject, returns the stored
//     upstream access token (refreshing if necessary) or empty string
//     if no usable token exists.
//   - ResolveAccessTokens: the variant the MCP serving path calls.
//     Resolves one upstream token per remote_session_issuer the subject
//     has linked under the user_session_issuer, returning them as a
//     remote_session_issuer_id -> token map.
//
// Refresh is invoked only when the stored access_expires_at is in the
// past. A still-valid access token short-circuits: no upstream token
// endpoint is contacted.

package remotesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newTokenEndpointRequest assembles a request and handles encoding
// credentials based on the configuration set by the client.
//
// CIMD invariant: a Client ID Metadata Document client is public — its row
// carries no client_secret (enforced by the remote_session_clients
// client_id_metadata_uri CHECK constraint) and its token_endpoint_auth_method
// is "none". Both credential branches below are gated on a non-empty
// clientSecret, so a CIMD client never puts a secret in the body and never
// reaches HTTP Basic auth. The guard that matters is the empty secret, not the
// method: ResolveTokenEndpointAuthMethod maps unknown values to Basic, so it is
// the absent secret — not method=none — that keeps the public CIMD path off
// Basic auth.
func newTokenEndpointRequest(ctx context.Context, endpoint string, form url.Values, method TokenEndpointAuthMethod, clientID, clientSecret string) (*http.Request, error) {
	if clientSecret != "" && method == TokenEndpointAuthMethodPost {
		form.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token endpoint request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// clientSecret is always empty for CIMD/public clients, so this never runs
	// for them regardless of the resolved method.
	if clientSecret != "" && method == TokenEndpointAuthMethodBasic {
		req.SetBasicAuth(clientID, clientSecret)
	}
	return req, nil
}

// ErrNoValidToken signals "there is a remote-session requirement for
// this toolset but the subject has no usable token." Callers (the MCP
// runtime) surface this as a fresh auth challenge so the user can
// re-link upstream.
var ErrNoValidToken = errors.New("remotesessions: no valid token for subject")

// ResolveAccessToken returns the upstream access token stored for the
// (client, subject) pair, refreshing via the upstream /token endpoint
// when the stored access_expires_at is in the past and a
// refresh_token is present.
//
// Returns ("", nil) when there is no usable token for this binding —
// no row, expired with no refresh path, refresh failed, decryption
// failed. The empty string is the "no token" signal; the caller
// decides whether absence is a challenge or a no-op.
//
// Returns a non-nil error only for unexpected failures (database
// errors). "No token available" is not an error.
//
// The (subject, remote_session_client_id) pair is uniqueness-enforced
// by a partial index — at most one active row exists per binding, so
// the lookup is exact.
func (m *ChallengeManager) ResolveAccessToken(
	ctx context.Context,
	clientID uuid.UUID,
	subject urn.SessionSubject,
) (string, error) {
	sess, err := remotesessions_repo.New(m.db).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("get active remote_session: %w", err)
	}

	tok, err := m.validateAndRefresh(ctx, sess)
	if err != nil {
		return "", nil
	}
	return tok, nil
}

// ResolveAccessTokens is the variant the MCP serving path calls. It
// resolves one upstream access token per remote_session_issuer the
// subject has linked under the user_session_issuer, keyed by
// remote_session_issuer_id, so downstream tool dispatch can forward the
// right token per upstream.
//
//   - Issuer has no bound remote_session_clients: returns (nil, nil). The
//     toolset has no remote-session requirement to satisfy.
//   - Every bound client has a usable token: returns the
//     remote_session_issuer_id -> token map.
//   - Any bound client lacks a usable token: returns ErrNoValidToken. The
//     MCP runtime surfaces this as a re-auth challenge so the user can
//     re-link the missing upstream via {routeBase}/{slug}/connect — the
//     "any attached remote session missing or invalid" rule from AIS-136.
//
// Current intent (all-or-nothing): resolution fails if ANY attached upstream
// is missing or invalid, even when the tool the caller is about to invoke
// only needs a different upstream. This is the safe default — the runtime
// never dispatches a half-authorized request — and matches AIS-136's wording.
// The cost is that one expired upstream blocks every tool on the issuer until
// it is re-linked.
//
// Future intent (AIS-152): once dispatch knows which remote_session_issuer a
// given tool requires, this can soften to challenging only when the upstream a
// tool actually needs is missing, so an expired Slack link doesn't 401 a
// Google-only tool call. Changing that behavior here without the per-tool
// linkage in place would let requests through unauthorized, so it is
// deliberately deferred to AIS-152.
//
// A runtime invariant asserts that no two bound clients target the same
// remote_session_issuer. This is the application-level counterpart to the
// attach-time guard in clienthandlers.go and keeps the map keys unambiguous.
func (m *ChallengeManager) ResolveAccessTokens(
	ctx context.Context,
	projectID uuid.UUID,
	organizationID string,
	userSessionIssuerID uuid.UUID,
	subject urn.SessionSubject,
) (map[uuid.UUID]string, error) {
	clients, err := m.listRemoteSessionClientRowsForUserSessionIssuer(ctx, projectID, organizationID, userSessionIssuerID)
	if err != nil {
		return nil, fmt.Errorf("list remote_session_clients: %w", err)
	}
	if len(clients) == 0 {
		return nil, nil
	}

	// Assert the per-(user_session_issuer, remote_session_issuer) uniqueness
	// invariant up front, before resolving any tokens. Folding this into the
	// token loop would let an unusable first client short-circuit with
	// ErrNoValidToken and hide a duplicate that comes later — masking the very
	// drift this backstop exists to surface.
	seen := make(map[uuid.UUID]bool, len(clients))
	for _, c := range clients {
		if err := inv.Check("remotesessions.ResolveAccessTokens",
			"at most one remote_session_client per (user_session_issuer, remote_session_issuer)", !seen[c.RemoteSessionIssuerID],
		); err != nil {
			return nil, fmt.Errorf("invariant: %w", err)
		}
		seen[c.RemoteSessionIssuerID] = true
	}

	tokens := make(map[uuid.UUID]string, len(clients))
	for _, c := range clients {
		tok, err := m.ResolveAccessToken(ctx, c.ClientID, subject)
		if err != nil {
			return nil, fmt.Errorf("resolve access token: %w", err)
		}
		if tok == "" {
			return nil, ErrNoValidToken
		}
		tokens[c.RemoteSessionIssuerID] = tok
	}
	return tokens, nil
}

// defaultNoExpiryRefreshInterval is the application-layer cadence at which a
// token whose upstream omitted expires_in but still handed us a refresh token
// is re-validated by attempting a refresh. The provider gave a renewal path
// without a stated lifetime, so we do not trust the token forever: it is
// served for this long past its last issuance (updated_at), then refreshed.
// Mirrors the historical fabricated now+1h expiry without persisting one in
// the database. A default for now; likely to become configurable.
const defaultNoExpiryRefreshInterval = time.Hour

// validateAndRefresh returns the upstream access token for sess, refreshing
// via the upstream /token endpoint when the token is past its usable window
// and a refresh_token is present.
//
// The usable window depends on what the upstream told us:
//   - access_expires_at set: the upstream-stated expiry governs.
//   - NULL with no refresh token: non-expiring (e.g. Slack non-rotating
//     xoxp) — served indefinitely.
//   - NULL with a refresh token: no stated lifetime but a renewal path, so
//     served until updated_at + defaultNoExpiryRefreshInterval, then refreshed.
func (m *ChallengeManager) validateAndRefresh(
	ctx context.Context,
	sess remotesessions_repo.RemoteSession,
) (string, error) {
	hasRefresh := sess.RefreshTokenEncrypted.Valid && sess.RefreshTokenEncrypted.String != ""

	// usableUntil is the instant after which we stop serving the stored access
	// token as-is; nil means "never" (non-expiring).
	var usableUntil *time.Time
	switch {
	case sess.AccessExpiresAt.Valid:
		usableUntil = &sess.AccessExpiresAt.Time
	case hasRefresh:
		deadline := sess.UpdatedAt.Time.Add(defaultNoExpiryRefreshInterval)
		usableUntil = &deadline
	default:
		usableUntil = nil
	}

	if usableUntil == nil || usableUntil.After(time.Now()) {
		plain, err := m.enc.Decrypt(sess.AccessTokenEncrypted)
		if err != nil {
			return "", fmt.Errorf("decrypt access token: %w", err)
		}
		return plain, nil
	}

	if !hasRefresh {
		return "", ErrNoValidToken
	}
	return m.refreshAccessToken(ctx, sess)
}

// refreshAccessToken is the lazy-path wrapper: it runs the shared refresh and
// returns just the new access token, discarding the persisted row.
func (m *ChallengeManager) refreshAccessToken(
	ctx context.Context,
	sess remotesessions_repo.RemoteSession,
) (string, error) {
	_, accessToken, err := refreshSessionTokens(ctx, remotesessions_repo.New(m.db), m.enc, m.policy, sess)
	if err != nil {
		return "", err
	}
	return accessToken, nil
}

// refreshSessionTokens POSTs grant_type=refresh_token to the upstream token
// endpoint and persists the new token pair on success, returning the upserted
// remote_session row and the new plaintext access token.
//
// It is shared by the lazy MCP resolution path (ChallengeManager) and the
// explicit org-admin refresh handler. The upstream token POST is an external
// call, so q must be a pool-bound querier, never a transaction-bound one — the
// POST must not run inside an open database transaction.
//
// Operator-actionable failures (unreadable stored token, missing token
// endpoint, an upstream rejection, no access token returned) come back as a
// *TokenRefreshError carrying a public-safe Reason, so callers can distinguish
// them from internal infrastructure errors and surface the Reason.
func refreshSessionTokens(
	ctx context.Context,
	q *remotesessions_repo.Queries,
	enc *encryption.Client,
	policy *guardian.Policy,
	sess remotesessions_repo.RemoteSession,
) (remotesessions_repo.RemoteSession, string, error) {
	var zero remotesessions_repo.RemoteSession

	client, err := q.GetRemoteSessionClientWithIssuerByID(ctx, sess.RemoteSessionClientID)
	if err != nil {
		return zero, "", fmt.Errorf("load remote_session_client for refresh: %w", err)
	}
	if !client.TokenEndpoint.Valid || client.TokenEndpoint.String == "" {
		return zero, "", newTokenRefreshError("the identity provider has no token endpoint configured", nil)
	}

	refreshToken, err := enc.Decrypt(sess.RefreshTokenEncrypted.String)
	if err != nil {
		return zero, "", newTokenRefreshError("the session's stored refresh token could not be read; revoke and re-link the session", err)
	}

	var clientSecret string
	if client.ClientSecretEncrypted.Valid {
		clientSecret, err = enc.Decrypt(client.ClientSecretEncrypted.String)
		if err != nil {
			return zero, "", newTokenRefreshError("the client secret could not be read; check the issuer's configuration", err)
		}
	}

	authMethod := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String)

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", client.ExternalClientID)
	if audience := conv.FromPGTextOrEmpty[string](client.ClientAudience); audience != "" {
		form.Set("audience", audience)
	}
	// RFC 8707: repeat the resource indicator on refresh so rotated access
	// tokens keep the same audience binding as the original grant.
	if resource := conv.FromPGTextOrEmpty[string](client.ClientResource); resource != "" {
		form.Set("resource", resource)
	}

	req, err := newTokenEndpointRequest(ctx, client.TokenEndpoint.String, form, authMethod, client.ExternalClientID, clientSecret)
	if err != nil {
		return zero, "", fmt.Errorf("new refresh request: %w", err)
	}

	resp, err := policy.PooledClient().Do(req)
	if err != nil {
		return zero, "", fmt.Errorf("post refresh: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return zero, "", fmt.Errorf("read refresh response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return zero, "", newTokenRefreshErrorFromHTTP(resp.Status, body)
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return zero, "", fmt.Errorf("decode refresh response: %w", err)
	}
	if tok.AccessToken == "" {
		return zero, "", newTokenRefreshError("the identity provider returned no access token", nil)
	}

	accessEnc, err := enc.Encrypt([]byte(tok.AccessToken))
	if err != nil {
		return zero, "", fmt.Errorf("encrypt new access token: %w", err)
	}
	newRefreshEnc := sess.RefreshTokenEncrypted
	if tok.RefreshToken != "" {
		v, eerr := enc.Encrypt([]byte(tok.RefreshToken))
		if eerr != nil {
			return zero, "", fmt.Errorf("encrypt new refresh token: %w", eerr)
		}
		newRefreshEnc = conv.PtrToPGText(&v)
	}

	// expires_in absent ⇒ NULL (no known expiry), matching exchangeCode. Never
	// fabricate a deadline the upstream did not assert.
	var accessExpires *time.Time
	if tok.ExpiresIn > 0 {
		v := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
		accessExpires = &v
	}
	refreshExpires := sess.RefreshExpiresAt
	if tok.RefreshExpiresIn > 0 {
		v := time.Now().Add(time.Duration(tok.RefreshExpiresIn) * time.Second)
		refreshExpires = conv.ToPGTimestamptz(v)
	}

	updated, err := q.UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            sess.SubjectUrn,
		UserSessionIssuerID:   sess.UserSessionIssuerID,
		RemoteSessionClientID: sess.RemoteSessionClientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       conv.PtrToPGTimestamptz(accessExpires),
		RefreshTokenEncrypted: newRefreshEnc,
		RefreshExpiresAt:      refreshExpires,
		Scopes:                sess.Scopes,
	})
	if err != nil {
		return zero, "", fmt.Errorf("persist refreshed session: %w", err)
	}

	return updated, tok.AccessToken, nil
}
