package usersessions

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// mintAccessTokenLifetime mirrors mcp.authnchallenge_token.accessTokenLifetime
// so dashboard-minted JWTs and /token-minted JWTs have the same wall-clock
// validity. Hardcoded short — OAuth 2.1 best practice — because the dashboard
// doesn't have a refresh-token surface; the dashboard re-mints by calling
// this method again with a fresh dashboard session.
const mintAccessTokenLifetime = 1 * time.Hour

// MintUserSession issues a user-session JWT against an issuer-gated toolset
// on behalf of the authenticated dashboard user. The resulting JWT has the
// same shape as the one /mcp/{slug}/token would emit after a real OAuth
// dance, so the runtime gateway validates it through the existing
// validateUserSessionToken path with no special-casing.
//
// Auth posture: dashboard session only (see design.go, which scopes the
// method to security.Session). API-key callers are rejected at the security
// scheme layer. CSRF risk is bounded by the org-pinned CORS policy: a
// cross-origin caller could trigger the mint (cookie auto-attached) but
// cannot read the response body, so the resulting JWT cannot be exfiltrated.
//
// Persists a user_sessions row with user_session_client_id = NULL — the
// minted JWT has no DCR-registered OAuth client behind it. The row is
// otherwise identical to a /token-issued session so userSessions.list,
// userSessions.revoke, and the runtime revocation cache all work
// unchanged.
func (s *Service) MintUserSession(ctx context.Context, payload *gen.MintUserSessionPayload) (*gen.MintUserSessionResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if s.signer == nil || s.serverURL == "" {
		return nil, oops.E(oops.CodeUnexpected, nil, "user-session signer not configured").Log(ctx, s.logger)
	}

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        toolsetID,
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load toolset").Log(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, toolset.ID.String(), toolset.ProjectID.String())); err != nil {
		return nil, err
	}

	if !toolset.UserSessionIssuerID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "toolset is not issuer-gated; minting a user-session JWT is only meaningful for issuer-gated toolsets").Log(ctx, s.logger)
	}
	if toolset.McpSlug.String == "" {
		return nil, oops.E(oops.CodeInvariantViolation, nil, "issuer-gated toolset has no mcp slug").Log(ctx, s.logger)
	}

	issuer, err := repo.New(s.db).GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:        toolset.UserSessionIssuerID.UUID,
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "user_session_issuer not found").Log(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load user_session_issuer").Log(ctx, s.logger)
	}
	if !issuer.SessionDuration.Valid || issuer.SessionDuration.Months != 0 || issuer.SessionDuration.Days != 0 {
		return nil, oops.E(oops.CodeUnexpected, nil, "issuer session_duration is unset or carries Months/Days; only Microseconds intervals are supported").Log(ctx, s.logger)
	}
	refreshLifetime := time.Duration(issuer.SessionDuration.Microseconds) * time.Microsecond
	if refreshLifetime <= 0 {
		return nil, oops.E(oops.CodeUnexpected, nil, "issuer session_duration is non-positive").Log(ctx, s.logger)
	}

	// Issuer URL matches what /token would emit: <serverURL>/mcp/<mcpSlug>.
	// The JWT's iss claim is descriptive only — the gate validates audience,
	// not issuer — but matching the convention keeps minted JWTs
	// indistinguishable from /token output in audit trails.
	issuerURL, err := url.JoinPath(s.serverURL, "mcp", toolset.McpSlug.String)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build issuer URL").Log(ctx, s.logger)
	}

	subject := urn.NewUserSubject(authCtx.UserID)
	audience := urn.NewToolset(toolset.ID).String()

	access, jti, err := s.signer.Mint(subject, audience, issuerURL, mintAccessTokenLifetime)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "mint session jwt").Log(ctx, s.logger)
	}

	now := time.Now()
	if _, err := repo.New(s.db).CreateUserSession(ctx, repo.CreateUserSessionParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		// No DCR-registered client — this mint bypasses the OAuth dance.
		UserSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SubjectUrn:          subject,
		Jti:                 jti,
		// No refresh token issued: the dashboard re-mints by calling
		// this method again. Store a sentinel that satisfies the
		// NOT NULL + unique constraint without colliding with any real
		// sha256 hash; the column will be migrated to nullable separately.
		RefreshTokenHash: fmt.Sprintf("dashboard-mint:%s", jti),
		ExpiresAt:        pgtype.Timestamptz{Time: now.Add(mintAccessTokenLifetime), InfinityModifier: 0, Valid: true},
		RefreshExpiresAt: pgtype.Timestamptz{Time: now.Add(refreshLifetime), InfinityModifier: 0, Valid: true},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "persist user session").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "minted user session via dashboard",
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogUserID(authCtx.UserID),
		attr.SlogToolsetID(toolset.ID.String()),
	)

	return &gen.MintUserSessionResult{
		AccessToken: access,
		ExpiresIn:   int(mintAccessTokenLifetime.Seconds()),
	}, nil
}
