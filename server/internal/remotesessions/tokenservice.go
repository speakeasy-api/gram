// tokenservice.go is the MCP-runtime side of the remote-session flow.
// challenge.go drives the *login* leg (build authz URL, exchange code,
// persist tokens). This file drives the *use* leg: given a subject the
// MCP runtime has just authenticated via a Gram user-session JWT, find
// the upstream access token to forward on the request.
//
// Two entry points exposed:
//
//   - ResolveAccessToken: multi-row variant. Walks every active row for
//     the (issuer, subject) pair, returns the first usable token. Kept
//     as a general utility.
//   - ResolveOneAccessToken: single-binding variant the MCP serving
//     path calls. Asserts the product invariants (one client per
//     issuer, one row per (issuer, subject)) via inv.Check so schema
//     drift fails loudly instead of silently picking arbitrary rows.
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

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ErrNoValidToken signals "there is a remote-session requirement for
// this toolset but the subject has no usable token for any of the
// bound clients." Callers surface this as a fresh auth challenge so
// the user can re-link upstream.
var ErrNoValidToken = errors.New("remotesessions: no valid token for subject")

// ResolveAccessToken returns an upstream access token for the given
// subject across every remote_session_client bound to the
// user_session_issuer.
//
//   - No clients bound: returns ("", nil). The toolset has no
//     remote-session requirement to satisfy on this request.
//   - Any active row yields a still-valid (or successfully refreshed)
//     access token: returns it.
//   - Otherwise returns ("", ErrNoValidToken).
func (m *ChallengeManager) ResolveAccessToken(
	ctx context.Context,
	projectID, userSessionIssuerID uuid.UUID,
	subject urn.SessionSubject,
) (string, error) {
	q := remotesessions_repo.New(m.db)

	clients, err := q.ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerParams{
		ProjectID:           projectID,
		UserSessionIssuerID: userSessionIssuerID,
	})
	if err != nil {
		return "", fmt.Errorf("list remote_session_clients: %w", err)
	}
	if len(clients) == 0 {
		return "", nil
	}

	sessions, err := q.ListRemoteSessionsForSubject(ctx, remotesessions_repo.ListRemoteSessionsForSubjectParams{
		UserSessionIssuerID: userSessionIssuerID,
		SubjectUrn:          subject,
	})
	if err != nil {
		return "", fmt.Errorf("list remote_sessions for subject: %w", err)
	}

	clientsByID := make(map[uuid.UUID]remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow, len(clients))
	for _, c := range clients {
		clientsByID[c.ClientID] = c
	}
	for _, sess := range sessions {
		client, ok := clientsByID[sess.RemoteSessionClientID]
		if !ok {
			continue
		}
		tok, err := m.validateAndRefresh(ctx, sess, client)
		if err == nil && tok != "" {
			return tok, nil
		}
	}
	return "", ErrNoValidToken
}

// ResolveOneAccessToken is the single-binding variant the MCP serving
// path calls. Encodes today's product invariants: a
// user_session_issuer has exactly one remote_session_client, and a
// (issuer, subject) pair has at most one active remote_sessions row.
//
// The second invariant is also enforced at the DB level by the
// partial unique index on (subject_urn, remote_session_client_id)
// WHERE deleted IS FALSE, so this assert mainly guards against schema
// drift (e.g. the WHERE clause being dropped, multiple clients being
// bound).
func (m *ChallengeManager) ResolveOneAccessToken(
	ctx context.Context,
	projectID, userSessionIssuerID uuid.UUID,
	subject urn.SessionSubject,
) (string, error) {
	q := remotesessions_repo.New(m.db)

	clients, err := q.ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerParams{
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

	sessions, err := q.ListRemoteSessionsForSubject(ctx, remotesessions_repo.ListRemoteSessionsForSubjectParams{
		UserSessionIssuerID: userSessionIssuerID,
		SubjectUrn:          subject,
	})
	if err != nil {
		return "", fmt.Errorf("list remote_sessions for subject: %w", err)
	}
	if err := inv.Check("remotesessions.ResolveOneAccessToken",
		"at most one active remote_sessions row per (issuer, subject)", len(sessions) <= 1,
	); err != nil {
		return "", fmt.Errorf("invariant: %w", err)
	}
	if len(sessions) == 0 {
		return "", ErrNoValidToken
	}

	tok, err := m.validateAndRefresh(ctx, sessions[0], clients[0])
	if err != nil || tok == "" {
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
	client remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow,
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
	return m.refreshAccessToken(ctx, sess, client)
}

// refreshAccessToken POSTs grant_type=refresh_token to the upstream
// token endpoint and persists the new token pair on success, then
// returns the new access token. Mirrors exchangeCode's request shape
// but with the refresh grant.
func (m *ChallengeManager) refreshAccessToken(
	ctx context.Context,
	sess remotesessions_repo.RemoteSession,
	client remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow,
) (string, error) {
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

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", client.ExternalClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.TokenEndpoint.String, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("new refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if clientSecret != "" {
		req.SetBasicAuth(client.ExternalClientID, clientSecret)
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

	if _, err := remotesessions_repo.New(m.db).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
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
