// tokenservice.go is the MCP-runtime side of the remote-session flow.
// challenge.go drives the *login* leg (build authz URL, exchange code,
// persist tokens). This file drives the *use* leg: given a subject the
// MCP runtime has just authenticated via a Gram user-session JWT, find
// the upstream access token to forward on the request.
//
// Three entry points exposed:
//
//   - ResolveAccessToken: per-client primitive. Given a
//     remote_session_client id and a subject, returns the stored
//     upstream access token (refreshing if necessary) or empty string
//     if no usable token exists.
//   - ResolveAuthorization: resolves one tenant-qualified reviewed issuer
//     binding and returns its ephemeral access token plus safe identity
//     metadata for callers that must record authorization freshness.
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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newTokenEndpointRequest assembles a request and owns client identification:
// callers must not put client_id or client_secret in form themselves. RFC 6749
// §2.3 allows exactly one placement for client credentials: Basic-auth clients
// identify via the Authorization header, everyone else (client_secret_post and
// public clients) via the body. Double-sending client_id is rejected by some
// upstreams (e.g. Pylon) as ambiguous client identification.
//
// method must come from ResolveTokenEndpointAuthMethod, which guarantees a
// Basic or Post client carries a non-empty secret and a secret-less client is
// public.
func newTokenEndpointRequest(ctx context.Context, endpoint string, form url.Values, method TokenEndpointAuthMethod, clientID, clientSecret string) (*http.Request, error) {
	switch method {
	case TokenEndpointAuthMethodBasic:
		// Credentials ride the Authorization header only, set below once req
		// exists. Strip any body copies so a caller-seeded client_id cannot
		// reintroduce the double-send this function exists to prevent.
		form.Del("client_id")
		form.Del("client_secret")
	case TokenEndpointAuthMethodPost:
		form.Set("client_id", clientID)
		form.Set("client_secret", clientSecret)
	case TokenEndpointAuthMethodNone:
		form.Set("client_id", clientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token endpoint request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if method == TokenEndpointAuthMethodBasic {
		// RFC 6749 §2.3.1: client credentials must be form-urlencoded before
		// going into the Basic authorization header. Upstreams that decode per
		// spec (e.g. Snowflake) reject raw credentials containing '+' or '%'.
		req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
	}
	return req, nil
}

// ErrNoValidToken signals "there is a remote-session requirement for
// this toolset but the subject has no usable token." Callers (the MCP
// runtime) surface this as a fresh auth challenge so the user can
// re-link upstream.
var (
	ErrNoValidToken                 = errors.New("remotesessions: no valid token for subject")
	ErrNoRemoteSessionClientBinding = errors.New("remotesessions: no client binding for remote issuer")
	ErrInvalidAuthorizationRequest  = errors.New("remotesessions: invalid authorization request")
)

// ResolvedAuthorization is the ephemeral result of resolving one reviewed
// remote-session issuer for a subject. AccessToken must remain in process and
// must never be persisted or logged by callers.
type ResolvedAuthorization struct {
	AccessToken            string
	RemoteSessionID        uuid.UUID
	RemoteSessionUpdatedAt time.Time
	RemoteSessionClientID  uuid.UUID
	RemoteSessionIssuerID  uuid.UUID
}

// remoteSessionLastUsedCutoff coalesces the last_used_at stamp so a busy
// binding writes at most one row per window. Matches the window used for
// user_sessions, so both legs of a brokered connection report liveness at the
// same resolution and a chain view can compare them directly.
const remoteSessionLastUsedCutoff = 5 * time.Minute

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
	resource string,
) (string, error) {
	resolved, err := m.resolveUpstreamToken(ctx, clientID, subject, resource)
	return resolved.Token, err
}

// resolveUpstreamToken is ResolveAccessToken's implementation. It returns the
// token together with the grant-time metadata of the row it was resolved
// from, so callers that need the recorded RFC 8707 resource read it from the
// same row load as the token instead of re-reading the row (which would cost
// a round trip and could pair the token with a different row's resource
// across a disconnect+reconnect). A zero-valued Token means "no usable
// token", exactly as ResolveAccessToken's empty string does.
func (m *ChallengeManager) resolveUpstreamToken(
	ctx context.Context,
	clientID uuid.UUID,
	subject urn.SessionSubject,
	resource string,
) (UpstreamToken, error) {
	var zero UpstreamToken

	sess, err := remotesessions_repo.New(m.db).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, nil
	case err != nil:
		return zero, fmt.Errorf("get active remote_session: %w", err)
	}

	tok, err := m.validateAndRefresh(ctx, sess, resource)
	if err != nil {
		// A known-expired access token with no usable refresh grant is the
		// ordinary reconnect path. The downstream 401 carries the challenge;
		// logging every retry here would turn one stale session into noise.
		if errors.Is(err, ErrNoValidToken) {
			return zero, nil
		}
		// validateAndRefresh errors only when a refresh was required (the
		// stored access token is past its usable window) and could not be
		// completed — the upstream rejected the refresh token, or the stored
		// token could not be decrypted. That is a broken link needing a
		// re-connect, distinct from "never linked" (the pgx.ErrNoRows case
		// above). Both collapse to the same empty-token signal downstream and
		// then to a byte-identical 401, so log the reason here — using the
		// public-safe TokenRefreshError.Reason when present — instead of
		// discarding it silently.
		reason := "upstream token refresh failed"
		var refreshErr *TokenRefreshError
		if errors.As(err, &refreshErr) {
			reason = refreshErr.Reason
		}
		m.logger.WarnContext(ctx, "remote session unusable: upstream token refresh failed",
			attr.SlogRemoteSessionClientID(clientID.String()),
			attr.SlogUserSessionIssuerID(sess.UserSessionIssuerID.String()),
			attr.SlogOAuthFailureReason(reason),
			attr.SlogError(err),
		)
		return zero, nil
	}

	// Stamped only on the success path: a resolved token is one that is about
	// to be spent on a proxied call, which is precisely what "used" means here.
	// Best-effort — bookkeeping must not fail a call that has a valid token.
	now := time.Now()
	if err := remotesessions_repo.New(m.db).TouchRemoteSessionLastUsed(ctx, remotesessions_repo.TouchRemoteSessionLastUsedParams{
		NowTs:                 pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
		UsedCutoff:            pgtype.Timestamptz{Time: now.Add(-remoteSessionLastUsedCutoff), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		m.logger.WarnContext(ctx, "failed to stamp remote session last_used_at",
			attr.SlogRemoteSessionClientID(clientID.String()),
			attr.SlogError(err),
		)
	}

	return UpstreamToken{
		Token:                 tok,
		Resource:              conv.FromPGTextOrEmpty[string](sess.Resource),
		RemoteSessionClientID: clientID,
	}, nil
}

// ResolveAuthorization resolves exactly one remote-session issuer binding for a
// project user-session issuer. It selects the client through the tenant-scoped
// attachment, then reuses ResolveAccessToken's refresh and revocation behavior.
//
// ErrNoRemoteSessionClientBinding means the reviewed issuer is not configured
// for this user-session issuer. ErrNoValidToken means the binding exists but the
// subject has no usable authorization.
func (m *ChallengeManager) ResolveAuthorization(
	ctx context.Context,
	projectID uuid.UUID,
	organizationID string,
	userSessionIssuerID uuid.UUID,
	remoteSessionIssuerID uuid.UUID,
	subject urn.SessionSubject,
	resource string,
) (ResolvedAuthorization, error) {
	if projectID == uuid.Nil || organizationID == "" || userSessionIssuerID == uuid.Nil || remoteSessionIssuerID == uuid.Nil || subject.IsZero() {
		return ResolvedAuthorization{}, ErrInvalidAuthorizationRequest
	}

	clients, err := m.listRemoteSessionClientRowsForUserSessionIssuer(ctx, projectID, organizationID, userSessionIssuerID)
	if err != nil {
		return ResolvedAuthorization{}, fmt.Errorf("list remote_session_clients: %w", err)
	}

	var clientID uuid.UUID
	for _, client := range clients {
		if client.RemoteSessionIssuerID != remoteSessionIssuerID {
			continue
		}
		if err := inv.Check("remotesessions.ResolveAuthorization",
			"at most one remote_session_client per (user_session_issuer, remote_session_issuer)", clientID == uuid.Nil,
		); err != nil {
			return ResolvedAuthorization{}, fmt.Errorf("invariant: %w", err)
		}
		clientID = client.ClientID
	}
	if clientID == uuid.Nil {
		return ResolvedAuthorization{}, ErrNoRemoteSessionClientBinding
	}

	token, err := m.ResolveAccessToken(ctx, clientID, subject, resource)
	if err != nil {
		return ResolvedAuthorization{}, fmt.Errorf("resolve remote-session access token: %w", err)
	}
	if token == "" {
		return ResolvedAuthorization{}, ErrNoValidToken
	}

	session, err := remotesessions_repo.New(m.db).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedAuthorization{}, ErrNoValidToken
	}
	if err != nil {
		return ResolvedAuthorization{}, fmt.Errorf("load resolved remote_session: %w", err)
	}

	return ResolvedAuthorization{
		AccessToken:            token,
		RemoteSessionID:        session.ID,
		RemoteSessionUpdatedAt: session.UpdatedAt.Time,
		RemoteSessionClientID:  clientID,
		RemoteSessionIssuerID:  remoteSessionIssuerID,
	}, nil
}

// UpstreamToken is one resolved upstream credential entry, qualified by the
// identity recorded at grant time so callers can route it to the upstream it
// was minted for instead of selecting broadly or arbitrarily.
type UpstreamToken struct {
	// Token is the plaintext upstream bearer access token. It must remain in
	// process and must never be persisted or logged.
	Token string

	// Resource is the RFC 8707 resource indicator recorded on the credential
	// at code exchange — the upstream the grant was minted for. Empty when
	// the connect flow carried no resource indicator.
	Resource string

	// RemoteSessionClientID is the remote_session_client the credential
	// belongs to.
	RemoteSessionClientID uuid.UUID
}

// ResolveAccessTokens is the variant the MCP serving path calls. It
// resolves one upstream access token per remote_session_issuer the
// subject has linked under the user_session_issuer, keyed by
// remote_session_issuer_id, so downstream tool dispatch can forward the
// right token per upstream. Each entry carries the RFC 8707 resource
// recorded at grant time as its qualified upstream identity.
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
// is missing or invalid, even when the request only needs a different one.
// Toolset dispatch is what still requires this — it has no per-tool
// remote_session_issuer mapping (AIS-152), so a partial map would silently
// dispatch a tool with no credential instead of challenging. Proxied backends
// no longer need it: routeUpstreamToken picks by the backend's own resource.
// One resolver serves both, so it stays all-or-nothing here; relaxing it for
// the proxied path is a follow-up, not a behavior change this PR makes.
// The cost is that one expired upstream blocks every tool on the issuer, and
// a proxied request to a still-linked upstream, until it is re-linked.
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
	resource string,
) (map[uuid.UUID]UpstreamToken, error) {
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

	tokens := make(map[uuid.UUID]UpstreamToken, len(clients))
	for _, c := range clients {
		// The grant-time metadata (the recorded RFC 8707 resource) comes from
		// the same row load that produced the token, so a disconnect+reconnect
		// between two reads can never pair an old token with a new row's
		// resource.
		resolved, err := m.resolveUpstreamToken(ctx, c.ClientID, subject, resource)
		if err != nil {
			return nil, fmt.Errorf("resolve access token: %w", err)
		}
		if resolved.Token == "" {
			return nil, ErrNoValidToken
		}
		tokens[c.RemoteSessionIssuerID] = resolved
	}
	return tokens, nil
}

// validateAndRefresh returns the upstream access token for sess, refreshing
// via the upstream /token endpoint when the token is past its usable window
// and a refresh_token is present.
//
// The usable window depends on what the upstream told us:
//   - access_expires_at set: the upstream-stated expiry governs.
//   - access_expires_at NULL: no expiry was reported, so the stored access
//     token is served as-is. A refresh token does not imply that access expires.
//
// Scheduled auto-refresh separately exercises unused refresh grants. Its
// failure must not turn an access token with no known expiry into a reconnect.
func (m *ChallengeManager) validateAndRefresh(
	ctx context.Context,
	sess remotesessions_repo.RemoteSession,
	resource string,
) (string, error) {
	now := time.Now()
	if sess.AuthorizationExpiresAt.Valid && !sess.AuthorizationExpiresAt.Time.After(now) {
		return "", ErrNoValidToken
	}

	hasRefresh := sess.RefreshTokenEncrypted.Valid && sess.RefreshTokenEncrypted.String != ""

	if !sess.AccessExpiresAt.Valid || sess.AccessExpiresAt.Time.After(now) {
		plain, err := m.enc.Decrypt(sess.AccessTokenEncrypted)
		if err != nil {
			return "", fmt.Errorf("decrypt access token: %w", err)
		}
		return plain, nil
	}

	if !hasRefresh {
		return "", ErrNoValidToken
	}

	res, err := m.refresher.RefreshNow(ctx, sess, resource)
	if err != nil {
		return "", err
	}
	// RefreshOutcomeSessionInactive lands here as an empty token, which the
	// caller treats the same as "never linked".
	return res.AccessToken, nil
}

// refreshSessionTokens POSTs grant_type=refresh_token to the upstream token
// endpoint and persists the new token pair on success, returning the updated
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
	resource string,
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

	authMethod, err := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String, clientSecret)
	if err != nil {
		return zero, "", newTokenRefreshError("the client's authentication configuration is invalid; check the issuer's configuration", err)
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if audience := conv.FromPGTextOrEmpty[string](client.ClientAudience); audience != "" {
		form.Set("audience", audience)
	}
	if resource != "" {
		form.Set("resource", resource)
	}

	// Scoped to the exchange so an unresponsive upstream cannot outlive the
	// single-flight lease; the persist below still runs on ctx.
	postCtx, cancel := context.WithTimeout(ctx, refreshUpstreamTimeout)
	defer cancel()

	req, err := newTokenEndpointRequest(postCtx, client.TokenEndpoint.String, form, authMethod, client.ExternalClientID, clientSecret)
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
		return zero, "", newTokenRefreshErrorFromHTTP(resp.StatusCode, resp.Status, body)
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
	now := time.Now()
	var accessExpires *time.Time
	if tok.ExpiresIn > 0 {
		v := now.Add(time.Duration(tok.ExpiresIn) * time.Second)
		accessExpires = &v
	}
	refreshTimeout, refreshTimeoutReported := tok.RefreshTokenTimeoutSeconds()
	refreshExpires := conv.PtrToPGTimestamptz(expirationDeadline(now, refreshTimeout, refreshTimeoutReported))

	// authorization_expires_in is an absolute property of the grant. Preserve
	// the known deadline when a later response omits it; replace it when the
	// provider reports the remaining authorization lifetime again.
	authorizationExpires := sess.AuthorizationExpiresAt
	if lifetime, reported := tok.AuthorizationLifetimeSeconds(); reported {
		authorizationExpires = conv.PtrToPGTimestamptz(expirationDeadline(now, lifetime, true))
	}
	scopes := tok.Scopes()
	if len(scopes) == 0 {
		// RFC 6749 §6: an omitted scope means the refreshed token retains the
		// original grant's scope.
		scopes = sess.Scopes
	}

	// CAS on the updated_at read before the POST: overwriting a row someone
	// else rotated would persist a refresh token the provider has already
	// consumed. A revocation mid-POST drops the row out of scope too.
	updated, err := q.UpdateRemoteSessionTokensIfUnchanged(ctx, remotesessions_repo.UpdateRemoteSessionTokensIfUnchangedParams{
		SubjectUrn:             sess.SubjectUrn,
		RemoteSessionClientID:  sess.RemoteSessionClientID,
		AccessTokenEncrypted:   accessEnc,
		AccessExpiresAt:        conv.PtrToPGTimestamptz(accessExpires),
		RefreshTokenEncrypted:  newRefreshEnc,
		AuthorizationExpiresAt: authorizationExpires,
		RefreshExpiresAt:       refreshExpires,
		Scopes:                 scopes,
		ExpectedUpdatedAt:      sess.UpdatedAt,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, "", newTokenRefreshError("the session was rotated by another request or revoked while this refresh was in flight; reload to see its current state", errRefreshNotApplied)
		}
		return zero, "", fmt.Errorf("persist refreshed session: %w", err)
	}

	return updated, tok.AccessToken, nil
}
