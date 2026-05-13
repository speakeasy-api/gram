// RFC 7009 token revocation handler for the issuer-gated authn-challenge
// surface. Per RFC 7009 §2.2 we MUST NOT leak whether the token existed —
// the response is HTTP 200 on success, unknown, already-revoked, and
// never-valid alike.

package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// HandleRevoke implements RFC 7009 token revocation. Mounted at
// `POST /mcp/{mcpSlug}/revoke`.
//
// Per RFC 7009 §2.2: the response is HTTP 200 unconditionally on success or
// when the token is unknown / already revoked / was never valid -- the spec
// is explicit that we MUST NOT leak which case applies. Only authentication
// or malformed-request failures return non-200.
//
// Token-type handling:
//   - If `token_type_hint=refresh_token` (or the hint is missing and the
//     token is opaque): hash with sha256, soft-delete the user_sessions row
//     matching the hash + issuer, and push the row's stored jti into the
//     unified revocation cache so the still-live access token is invalidated
//     too.
//   - If `token_type_hint=access_token` (or the hint is missing and the
//     token parses as a JWT): extract the jti without verifying the
//     signature -- the client_secret check above establishes authenticity --
//     and push it into the revocation cache. We do NOT have to find a
//     matching user_sessions row to honour the request.
func (s *Service) HandleRevoke(w http.ResponseWriter, r *http.Request) error {
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
	if err := s.requireUserSessionIssuer(ctx, toolset); err != nil {
		return err
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
	// Public clients (NULL hash) skip the secret check; their possession of
	// the token alone authenticates the revoke per RFC 7009 §2.1.
	if clientRow.ClientSecretHash.Valid {
		if err := bcrypt.CompareHashAndPassword([]byte(clientRow.ClientSecretHash.String), []byte(clientSecret)); err != nil {
			return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "client secret mismatch")
		}
	}

	token := r.PostForm.Get("token")
	if token == "" {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "token is required")
	}
	hint := r.PostForm.Get("token_type_hint")

	// Try the hinted type first; on miss, fall through to the other. Per
	// RFC 7009 §2.1 each path verifies the token belongs to the
	// authenticated client before revoking; ownership mismatches look like
	// the "unknown token" success path to the caller (§2.2 — don't leak
	// ownership).
	issuerID := toolset.UserSessionIssuerID.UUID
	clientUUID := clientRow.ID
	switch hint {
	case "refresh_token":
		if s.tryRevokeRefreshToken(ctx, logger, issuerID, clientUUID, token) {
			break
		}
		s.tryRevokeAccessToken(ctx, logger, issuerID, clientUUID, token)
	case "access_token":
		if s.tryRevokeAccessToken(ctx, logger, issuerID, clientUUID, token) {
			break
		}
		s.tryRevokeRefreshToken(ctx, logger, issuerID, clientUUID, token)
	default:
		// No hint: try access_token first (JWT shape is recognisable), then
		// refresh_token. Either may match; per RFC 7009 we don't tell the
		// client which.
		if !s.tryRevokeAccessToken(ctx, logger, issuerID, clientUUID, token) {
			s.tryRevokeRefreshToken(ctx, logger, issuerID, clientUUID, token)
		}
	}

	// RFC 7009 §2.2: 200 OK with empty body, regardless of whether the token
	// existed. Headers per RFC 6749 §5.1 (no-store / no-cache).
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	return nil
}

// tryRevokeAccessToken parses the token as a JWT, looks up the
// user_sessions row by jti, and — when the row belongs to the
// authenticated client — pushes the jti into the revocation cache.
// Signature verification on the JWT is intentionally skipped; the
// client_secret check in HandleRevoke establishes authenticity per RFC
// 7009 §2.1. Returns true only when the row was found AND owned by the
// caller; ownership mismatches return false so the dispatch falls through
// to the refresh-token attempt (and ultimately surfaces as the success
// silent-no-op per §2.2).
func (s *Service) tryRevokeAccessToken(ctx context.Context, logger *slog.Logger, issuerID, clientID uuid.UUID, token string) bool {
	jti, err := s.userSessionSigner.ParseUnverifiedJTI(token)
	if err != nil || jti == "" {
		return false
	}
	row, err := usersessions_repo.New(s.db).GetUserSessionByJTI(ctx, usersessions_repo.GetUserSessionByJTIParams{
		UserSessionIssuerID: issuerID,
		Jti:                 jti,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.ErrorContext(ctx, "failed to look up user session by jti", attr.SlogError(err))
		}
		return false
	}
	if !row.UserSessionClientID.Valid || row.UserSessionClientID.UUID != clientID {
		// Token exists but isn't this client's — RFC 7009 §2.1 forbids
		// honouring the revoke; §2.2 says we MUST NOT leak that mismatch.
		return false
	}
	if err := s.chatSessionsManager.RevokeToken(ctx, jti); err != nil {
		logger.ErrorContext(ctx, "failed to push jti into revocation cache", attr.SlogError(err))
	}
	return true
}

// tryRevokeRefreshToken hashes the token, looks up the user_sessions row,
// and — when the row belongs to the authenticated client — soft-deletes it
// and pushes the jti into the revocation cache. Ownership mismatches return
// false so the dispatch keeps the row intact and falls through. (Critical:
// the peek-then-delete pattern is required here because a soft-delete-
// regardless approach would let a malicious client wipe another client's
// refresh token by presenting it to /revoke.)
func (s *Service) tryRevokeRefreshToken(ctx context.Context, logger *slog.Logger, issuerID, clientID uuid.UUID, token string) bool {
	hash := sha256Hex(token)
	row, err := usersessions_repo.New(s.db).GetUserSessionByRefreshTokenHash(ctx, usersessions_repo.GetUserSessionByRefreshTokenHashParams{
		UserSessionIssuerID: issuerID,
		RefreshTokenHash:    hash,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.ErrorContext(ctx, "failed to look up user session by refresh token", attr.SlogError(err))
		}
		return false
	}
	if !row.UserSessionClientID.Valid || row.UserSessionClientID.UUID != clientID {
		return false
	}
	deleted, err := usersessions_repo.New(s.db).RevokeUserSessionByRefreshTokenHash(ctx, usersessions_repo.RevokeUserSessionByRefreshTokenHashParams{
		UserSessionIssuerID: issuerID,
		RefreshTokenHash:    hash,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.ErrorContext(ctx, "failed to soft-delete user session by refresh token", attr.SlogError(err))
		}
		return false
	}
	if err := s.chatSessionsManager.RevokeToken(ctx, deleted.Jti); err != nil {
		logger.ErrorContext(ctx, "failed to push jti into revocation cache", attr.SlogError(err))
	}
	return true
}
