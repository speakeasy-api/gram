// OAuth 2.1 token endpoint (RFC 6749 §4.1.3 / §6) for the issuer-gated
// authn-challenge surface. HandleToken dispatches on grant_type to one of
// the two grant handlers below; both funnel through mintSessionAndRespond
// to write the RFC 6749 §5.1 response.

package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// tokenResponse is the RFC 6749 §5.1 successful token response shape, plus
// `refresh_token` since we issue them on every grant.
//
// `scope` is intentionally absent: RFC 6749 §5.1 says the returned `scope`
// is the scope of the issued access token, and our access-token JWT
// carries no scope claim — no enforcement, no persistence. Emitting a
// `scope` field here would assert token state we don't hold. Restore it
// when /token mints scope-bearing access tokens.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// HandleToken implements the OAuth 2.1 token endpoint (RFC 6749 §4.1.3 /
// §6). Mounted at `POST /mcp/{mcpSlug}/token`. Performs the common upfront
// work — parse form, load toolset, authenticate the client — then
// dispatches on grant_type to handleTokenAuthorizationCodeGrant or
// handleTokenRefreshTokenGrant. Both grant handlers funnel through
// mintSessionAndRespond which writes the RFC 6749 §5.1 response.
func (s *Service) HandleToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		return writeTokenError(ctx, w, s.logger, http.StatusBadRequest, "invalid_request", "failed to parse form")
	}

	toolset, _, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}
	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	clientID, clientSecret, _ := extractClientCredentials(r)
	if clientID == "" {
		return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "client_id is required")
	}
	clientRow, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            clientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").Log(ctx, logger)
	}
	// Public clients (token_endpoint_auth_method=none) have a NULL hash:
	// PKCE / refresh-token possession is the integrity proof, no secret check.
	// Confidential clients MUST present a matching secret.
	if clientRow.ClientSecretHash.Valid {
		if err := bcrypt.CompareHashAndPassword([]byte(clientRow.ClientSecretHash.String), []byte(clientSecret)); err != nil {
			return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "client secret mismatch")
		}
	}

	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		return s.handleTokenAuthorizationCodeGrant(ctx, w, r, toolset, &clientRow, mcpSlug, logger)
	case "refresh_token":
		return s.handleTokenRefreshTokenGrant(ctx, w, r, toolset, &clientRow, mcpSlug, logger)
	default:
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
	}
}

// handleTokenAuthorizationCodeGrant implements RFC 6749 §4.1.3. Reads the
// authorization code from the form, atomically consumes the
// UserSessionGrant from Redis (single-use), validates redirect_uri + the
// S256 PKCE verifier, then mints a new session via mintSessionAndRespond.
//
// No re-check of user_session_consents: possession of a valid grant IS
// proof of consent. The grant was minted by HandleConsent's POST after
// writing the consent row, and we atomically consumed it here.
func (s *Service) handleTokenAuthorizationCodeGrant(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	toolset *toolsets_repo.Toolset,
	clientRow *usersessions_repo.UserSessionClient,
	mcpSlug string,
	logger *slog.Logger,
) error {
	req := usersessions.AuthCodeTokenRequestFromForm(r.PostForm)
	req.SetDefaults()
	if err := req.Validate(); err != nil {
		return writeTokenOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	grantKey := "userSessionGrant:" + toolset.UserSessionIssuerID.UUID.String() + ":" + req.Code
	grant, err := s.userSessionGrantCache.Get(ctx, grantKey)
	if err != nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code not found or expired")
	}
	if err := s.userSessionGrantCache.Delete(ctx, grant); err != nil {
		// Failed to delete -- another process may redeem. Refuse to continue.
		return oops.E(oops.CodeUnexpected, err, "consume user session grant").Log(ctx, logger)
	}

	if grant.ClientID != clientRow.ClientID {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code was issued to a different client")
	}
	if grant.RedirectURI != req.RedirectURI {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the original request")
	}
	if !verifyPKCES256(req.CodeVerifier, grant.CodeChallenge) {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
	}

	return s.mintSessionAndRespond(ctx, w, toolset, clientRow, grant.Subject, mcpSlug, logger)
}

// handleTokenRefreshTokenGrant implements RFC 6749 §6 (and OAuth 2.1's
// refresh-token rotation guidance). Hashes the supplied refresh token,
// atomically soft-deletes the matching user_sessions row (single-use:
// concurrent refreshes race for the slot), pushes the old access token's
// JTI into the revocation cache, then mints a new session via
// mintSessionAndRespond.
//
// Client binding: the soft-deleted row's user_session_client_id MUST match
// the authenticated client. This blocks Client B from refreshing tokens
// issued to Client A even if B somehow obtains the opaque refresh token.
func (s *Service) handleTokenRefreshTokenGrant(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	toolset *toolsets_repo.Toolset,
	clientRow *usersessions_repo.UserSessionClient,
	mcpSlug string,
	logger *slog.Logger,
) error {
	req := usersessions.RefreshTokenRequestFromForm(r.PostForm)
	req.SetDefaults()
	if err := req.Validate(); err != nil {
		return writeTokenOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	// Soft-delete by hash claims the single-use slot atomically. If the row
	// is already gone (unknown / replayed / revoked), pgx.ErrNoRows surfaces
	// here as invalid_grant.
	oldSession, err := usersessions_repo.New(s.db).RevokeUserSessionByRefreshTokenHash(ctx, usersessions_repo.RevokeUserSessionByRefreshTokenHashParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		RefreshTokenHash:    sha256Hex(req.RefreshToken),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token is unknown or already used")
		}
		return oops.E(oops.CodeUnexpected, err, "revoke old refresh token").Log(ctx, logger)
	}

	// Client binding: refuse if the original session was minted for a
	// different client. We've already soft-deleted the row -- that's
	// intentional, the alternative would let a leaking client poke at others'
	// refresh tokens without invalidating them.
	if !oldSession.UserSessionClientID.Valid || oldSession.UserSessionClientID.UUID != clientRow.ID {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token was issued to a different client")
	}

	if oldSession.RefreshExpiresAt.Valid && time.Now().After(oldSession.RefreshExpiresAt.Time) {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token has expired")
	}

	// Best-effort: invalidate any access token still floating around from
	// the prior session row. If Redis is down, the access token will expire
	// naturally on its own clock; we'd rather mint than fail the refresh.
	if err := s.chatSessionsManager.RevokeToken(ctx, oldSession.Jti); err != nil {
		logger.WarnContext(ctx, "failed to revoke old access token jti on refresh", attr.SlogError(err))
	}

	return s.mintSessionAndRespond(ctx, w, toolset, clientRow, oldSession.SubjectUrn, mcpSlug, logger)
}

// mintSessionAndRespond resolves the issuer's session_duration, mints a new
// SessionClaims JWT (HS256, audience = toolset slug) and an opaque refresh
// token, persists a fresh user_sessions row, and writes the RFC 6749 §5.1
// response. Shared by the authorization_code and refresh_token grant
// handlers since both produce identical token responses.
func (s *Service) mintSessionAndRespond(
	ctx context.Context,
	w http.ResponseWriter,
	toolset *toolsets_repo.Toolset,
	clientRow *usersessions_repo.UserSessionClient,
	subject urn.SessionSubject,
	mcpSlug string,
	logger *slog.Logger,
) error {
	// Resolve the issuer's session_duration. Microseconds-only: the issuer
	// create handler stores via conv.PtrToPGInterval which never sets
	// Months/Days; if we ever see those here, raw SQL bypassed the writer
	// and the conversion is calendar-dependent — fail with 500 rather than
	// silently approximate.
	issuer, err := usersessions_repo.New(s.db).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
		ID:        toolset.UserSessionIssuerID.UUID,
		ProjectID: toolset.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user_session_issuer not found")
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session issuer").Log(ctx, logger)
	}
	if !issuer.SessionDuration.Valid {
		return oops.E(oops.CodeUnexpected, nil, "issuer session_duration is not set").Log(ctx, logger)
	}
	if issuer.SessionDuration.Months != 0 || issuer.SessionDuration.Days != 0 {
		return oops.E(oops.CodeUnexpected, nil, "issuer session_duration carries Months/Days; only Microseconds intervals are supported").Log(ctx, logger)
	}
	lifetime := time.Duration(issuer.SessionDuration.Microseconds) * time.Microsecond
	if lifetime <= 0 {
		return oops.E(oops.CodeUnexpected, nil, "issuer session_duration is non-positive").Log(ctx, logger)
	}

	issuerURL := s.serverURL.String() + "/mcp/" + mcpSlug
	access, jti, err := s.userSessionSigner.Mint(subject, toolset.Slug, issuerURL, lifetime)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "mint session jwt").Log(ctx, logger)
	}

	refreshTokenRaw, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate refresh token").Log(ctx, logger)
	}

	expiresAt := time.Now().Add(lifetime)
	if _, err := usersessions_repo.New(s.db).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		UserSessionClientID: uuid.NullUUID{UUID: clientRow.ID, Valid: true},
		SubjectUrn:          subject,
		Jti:                 jti,
		RefreshTokenHash:    sha256Hex(refreshTokenRaw),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: expiresAt, InfinityModifier: 0, Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: expiresAt, InfinityModifier: 0, Valid: true},
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "persist user session").Log(ctx, logger)
	}

	body, err := json.Marshal(tokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int64(lifetime.Seconds()),
		RefreshToken: refreshTokenRaw,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "marshal token response").Log(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write token response").Log(ctx, logger)
	}
	return nil
}

// writeTokenOAuthError unwraps a *usersessions.OAuthError to its code +
// description and forwards to writeTokenError. Falls back to a generic
// invalid_request if err is something else (shouldn't happen — Validate
// returns *OAuthError).
func writeTokenOAuthError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, err error) error {
	var oauthErr *usersessions.OAuthError
	if errors.As(err, &oauthErr) {
		return writeTokenError(ctx, w, logger, status, oauthErr.Code, oauthErr.Description)
	}
	return writeTokenError(ctx, w, logger, status, "invalid_request", err.Error())
}

// writeTokenError emits an RFC 6749 §5.2 token error response: 4xx with a
// JSON body { "error": "<code>", "error_description": "..." } and the
// no-store headers required by RFC 6749 §5.1.
func writeTokenError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, code, description string) error {
	body, err := json.Marshal(map[string]string{
		"error":             code,
		"error_description": description,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "marshal token error").Log(ctx, logger)
	}

	logger.InfoContext(ctx, "token request rejected",
		attr.SlogOAuthError(code),
		attr.SlogOAuthErrorDescription(description),
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	if _, werr := w.Write(body); werr != nil {
		return oops.E(oops.CodeUnexpected, werr, "write token error body").Log(ctx, logger)
	}
	return nil
}

// verifyPKCES256 reports whether code_verifier matches the stored
// code_challenge under the S256 method (RFC 7636 §4.6):
// BASE64URL-NO-PAD(SHA256(ASCII(code_verifier))) == code_challenge.
func verifyPKCES256(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}
