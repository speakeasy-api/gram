package admin

import (
	"context"
	"errors"
	"log/slog"

	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Verifier implements the Goa APIKeyAuth entry point for the admin service.
// It resolves the opaque admin session ID (either passed by Goa or taken
// from the request context by the admin-cookie middleware) to a server-side
// AdminSession, re-validates the stored OAuth access token with the OIDC
// provider on every call, refreshes it if necessary, and populates the request
// context with an AdminAuthContext on success.
type Verifier struct {
	logger   *slog.Logger
	sessions *SessionStore
	oidc     *OIDCClient
}

func NewVerifier(logger *slog.Logger, sessions *SessionStore, oidc *OIDCClient) *Verifier {
	return &Verifier{
		logger:   logger.With(attr.SlogComponent("adminauth")),
		sessions: sessions,
		oidc:     oidc,
	}
}

// Authorize is the entry point that Goa-generated admin endpoints call via
// the Auther interface on the admin service.
func (v *Verifier) Authorize(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	if scheme == nil || scheme.Name != constants.AdminAuthSecurityScheme {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "unsupported security scheme").Log(ctx, v.logger)
	}

	if key == "" {
		// Goa does not extract HTTP cookies into APIKey payloads natively.
		// The cookie value is forwarded by AdminSessionMiddleware.
		key, _ = contextvalues.GetAdminSessionTokenFromContext(ctx)
	}
	if key == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	session, err := v.sessions.Get(ctx, key)
	if err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	accessToken, err := v.sessions.DecryptAccessToken(session)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "decrypt admin session").Log(ctx, v.logger)
	}

	if NeedsRefresh(session.AccessTokenExpiresAt) {
		refreshToken, err := v.sessions.DecryptRefreshToken(session)
		if err != nil {
			return ctx, oops.E(oops.CodeUnexpected, err, "decrypt admin session").Log(ctx, v.logger)
		}
		if refreshToken == "" {
			// No refresh token captured at login; re-auth required.
			_ = v.sessions.Delete(ctx, session.SessionID)
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		tok, err := v.oidc.Refresh(ctx, refreshToken)
		if err != nil {
			_ = v.sessions.Delete(ctx, session.SessionID)
			return ctx, oops.C(oops.CodeUnauthorized).Log(ctx, v.logger, attr.SlogError(err))
		}
		session, err = v.sessions.UpdateAccessToken(ctx, session, tok.AccessToken, tok.Expiry)
		if err != nil {
			return ctx, oops.E(oops.CodeUnexpected, err, "persist refreshed admin session").Log(ctx, v.logger)
		}
		accessToken = tok.AccessToken
	}

	info, err := v.oidc.Userinfo(ctx, accessToken)
	switch {
	case errors.Is(err, ErrOIDCUnauthenticated), errors.Is(err, ErrAdminDomainNotAllowed):
		_ = v.sessions.Delete(ctx, session.SessionID)
		return ctx, oops.C(oops.CodeUnauthorized).Log(ctx, v.logger, attr.SlogError(err))
	case err != nil:
		return ctx, oops.E(oops.CodeUnexpected, err, "validate admin session with oidc provider").Log(ctx, v.logger)
	}

	if info.OIDCSubject != session.OIDCSubject {
		// Token belongs to a different user than the cached session —
		// treat as hostile and invalidate the session immediately.
		_ = v.sessions.Delete(ctx, session.SessionID)
		return ctx, oops.C(oops.CodeUnauthorized).Log(ctx, v.logger)
	}

	authCtx := &contextvalues.AdminAuthContext{
		SessionID:   session.SessionID,
		Email:       session.Email,
		OIDCSubject: session.OIDCSubject,
		Name:        session.Name,
		HD:          session.HD,
	}
	ctx = contextvalues.SetAdminAuthContext(ctx, authCtx)

	v.logger.InfoContext(ctx, "admin auth check passed",
		attr.SlogAuthScheme(constants.AdminAuthSecurityScheme),
		attr.SlogAuthUserEmail(session.Email),
	)

	return ctx, nil
}
