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
//   - ResolveOneAccessToken: the variant the MCP serving path calls.
//     Wraps ResolveAccessToken with an inv.Check that the
//     user_session_issuer has exactly one remote_session_client bound,
//     and errors if not.
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
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newTokenEndpointRequest places client_id/client_secret per the
// registered auth method. With client_secret_basic, client_id is NOT
// echoed in the form body — WorkOS-fronted token endpoints return
// "unauthorized" when client_id appears in both Basic auth and the body.
func newTokenEndpointRequest(ctx context.Context, endpoint string, form url.Values, method TokenEndpointAuthMethod, clientID, clientSecret string) (*http.Request, error) {
	useBasic := clientSecret != "" && method == TokenEndpointAuthMethodBasic
	if !useBasic {
		form.Set("client_id", clientID)
	}
	if clientSecret != "" && method == TokenEndpointAuthMethodPost {
		form.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token endpoint request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if useBasic {
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

// ResolveOneAccessToken is the variant the MCP serving path calls.
// It asserts via inv.Check that the user_session_issuer has exactly
// one remote_session_client bound, and errors if not — this guards
// against the case where someone has wired multiple clients to a
// single issuer, which today's product config explicitly forbids and
// which would otherwise force the resolver to arbitrarily pick a
// binding.
//
//   - Issuer has no remote_session_clients: returns ("", nil). The
//     toolset has no remote-session requirement to satisfy.
//   - Issuer has exactly one bound client and a usable token exists:
//     returns the access token.
//   - Issuer has exactly one bound client but no usable token:
//     returns ("", ErrNoValidToken).
//   - Issuer has more than one bound client: returns an
//     inv.InvariantError. The MCP runtime treats this as an internal
//     error, not a re-auth path.
func (m *ChallengeManager) ResolveOneAccessToken(
	ctx context.Context,
	projectID, userSessionIssuerID uuid.UUID,
	subject urn.SessionSubject,
) (string, error) {
	clients, err := remotesessions_repo.New(m.db).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerParams{
		ProjectID:           projectID,
		UserSessionIssuerID: userSessionIssuerID,
	})
	if err != nil {
		return "", fmt.Errorf("list remote_session_clients: %w", err)
	}
	if len(clients) == 0 {
		return "", nil
	}
	if err := inv.Check("remotesessions.ResolveOneAccessToken",
		"single remote_session_client per user_session_issuer", len(clients) == 1,
	); err != nil {
		return "", fmt.Errorf("invariant: %w", err)
	}

	tok, err := m.ResolveAccessToken(ctx, clients[0].ClientID, subject)
	if err != nil {
		return "", err
	}
	if tok == "" {
		return "", ErrNoValidToken
	}
	return tok, nil
}

// validateAndRefresh returns the upstream access token for sess,
// refreshing via the upstream /token endpoint when the stored access
// token has expired and a refresh_token is present.
func (m *ChallengeManager) validateAndRefresh(
	ctx context.Context,
	sess remotesessions_repo.RemoteSession,
) (string, error) {
	if sess.AccessExpiresAt.Valid && sess.AccessExpiresAt.Time.After(time.Now()) {
		plain, err := m.enc.Decrypt(sess.AccessTokenEncrypted)
		if err != nil {
			return "", fmt.Errorf("decrypt access token: %w", err)
		}
		return plain, nil
	}
	if !sess.RefreshTokenEncrypted.Valid || sess.RefreshTokenEncrypted.String == "" {
		return "", ErrNoValidToken
	}
	return m.refreshAccessToken(ctx, sess)
}

// refreshAccessToken POSTs grant_type=refresh_token to the upstream
// token endpoint and persists the new token pair on success, then
// returns the new access token. Mirrors exchangeCode's request shape
// but with the refresh grant.
func (m *ChallengeManager) refreshAccessToken(
	ctx context.Context,
	sess remotesessions_repo.RemoteSession,
) (string, error) {
	q := remotesessions_repo.New(m.db)
	client, err := q.GetRemoteSessionClientWithIssuerByID(ctx, sess.RemoteSessionClientID)
	if err != nil {
		return "", fmt.Errorf("load remote_session_client for refresh: %w", err)
	}
	if !client.TokenEndpoint.Valid || client.TokenEndpoint.String == "" {
		return "", errors.New("remote_session_issuer has no token endpoint configured")
	}

	refreshToken, err := m.enc.Decrypt(sess.RefreshTokenEncrypted.String)
	if err != nil {
		return "", fmt.Errorf("decrypt refresh token: %w", err)
	}

	var clientSecret string
	if client.ClientSecretEncrypted.Valid {
		clientSecret, err = m.enc.Decrypt(client.ClientSecretEncrypted.String)
		if err != nil {
			return "", fmt.Errorf("decrypt client secret: %w", err)
		}
	}

	authMethod := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String)

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if audience := conv.FromPGTextOrEmpty[string](client.ClientAudience); audience != "" {
		form.Set("audience", audience)
	}

	req, err := newTokenEndpointRequest(ctx, client.TokenEndpoint.String, form, authMethod, client.ExternalClientID, clientSecret)
	if err != nil {
		return "", fmt.Errorf("new refresh request: %w", err)
	}

	resp, err := m.policy.PooledClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("post refresh: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("read refresh response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("refresh endpoint %s: %s", resp.Status, string(body))
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("decode refresh response: %w", err)
	}
	if tok.AccessToken == "" {
		return "", errors.New("refresh endpoint returned no access_token")
	}

	accessEnc, err := m.enc.Encrypt([]byte(tok.AccessToken))
	if err != nil {
		return "", fmt.Errorf("encrypt new access token: %w", err)
	}
	newRefreshEnc := sess.RefreshTokenEncrypted
	if tok.RefreshToken != "" {
		v, eerr := m.enc.Encrypt([]byte(tok.RefreshToken))
		if eerr != nil {
			return "", fmt.Errorf("encrypt new refresh token: %w", eerr)
		}
		newRefreshEnc = conv.PtrToPGText(&v)
	}

	accessExpires := time.Now().Add(1 * time.Hour)
	if tok.ExpiresIn > 0 {
		accessExpires = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	refreshExpires := sess.RefreshExpiresAt
	if tok.RefreshExpiresIn > 0 {
		v := time.Now().Add(time.Duration(tok.RefreshExpiresIn) * time.Second)
		refreshExpires = conv.ToPGTimestamptz(v)
	}

	if _, err := q.UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            sess.SubjectUrn,
		UserSessionIssuerID:   sess.UserSessionIssuerID,
		RemoteSessionClientID: sess.RemoteSessionClientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       conv.ToPGTimestamptz(accessExpires),
		RefreshTokenEncrypted: newRefreshEnc,
		RefreshExpiresAt:      refreshExpires,
		Scopes:                sess.Scopes,
	}); err != nil {
		return "", fmt.Errorf("persist refreshed session: %w", err)
	}

	return tok.AccessToken, nil
}
