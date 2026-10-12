package auth

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SetTokenRevoker wires the access-token revocation cache used when signing
// out everywhere.
func (s *Service) SetTokenRevoker(tokens ssosignins.TokenRevoker) { s.tokens = tokens }

// LogoutEverywhere ends every dashboard and identity provider session the
// caller holds, revokes their SSO sign-ins in every organization they ever
// signed in to, and revokes every MCP grant made from them.
func (s *Service) LogoutEverywhere(ctx context.Context, _ *gen.LogoutEverywherePayload) (*gen.LogoutEverywhereResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil || authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogUserID(authCtx.UserID))
	organizationIDs, err := s.userSignInOrganizations(ctx, authCtx.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organizations for sign-out").LogError(ctx, logger)
	}

	dbtx, err := s.db.BeginTx(ctx, pgx.TxOptions{}) //nolint:exhaustruct // default transaction options
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	revoked, err := ssosignins.RevokeUser(ctx, dbtx, organizationIDs, authCtx.UserID, ssosignins.RevokeReasonSignOutEverywhere)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke sso sign-ins").LogError(ctx, logger)
	}
	actor := ssosignins.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), DisplayName: authCtx.Email}
	if err := ssosignins.AuditRevocation(ctx, dbtx, s.auditLogger, actor, authCtx.UserID, conv.PtrValOr(authCtx.Email, ""), ssosignins.RevokeReasonSignOutEverywhere, organizationIDs, revoked); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit sign-in revocation").LogError(ctx, logger)
	}
	jtis, err := ssosignins.JTIsToPush(ctx, dbtx, organizationIDs, authCtx.UserID, revoked, time.Now())
	if err != nil {
		logger.WarnContext(ctx, "list recently revoked grants", attr.SlogError(err))
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	ssosignins.RecordRevoked(ctx, ssosignins.RevokeReasonSignOutEverywhere, revoked.SignIns())

	// The caller's own WorkOS session is ended too, even if it has no sign-in row.
	idpSessions := revoked.WorkOSSessionIDs
	if current, err := s.sessions.GetSession(ctx, *authCtx.SessionID); err == nil && current.WorkOSSessionID != "" && !slices.Contains(idpSessions, current.WorkOSSessionID) {
		idpSessions = append(idpSessions, current.WorkOSSessionID)
	}
	ssosignins.FinishRevocation(ctx, logger, s.tokens, s.sessions, jtis, idpSessions)
	if err := s.sessions.EndUserSessions(ctx, authCtx.UserID, nil); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "end dashboard sessions").LogError(ctx, logger)
	}
	if err := s.sessions.InvalidateUserInfoCache(ctx, authCtx.UserID); err != nil {
		logger.WarnContext(ctx, "invalidate user info after sign-out everywhere", attr.SlogError(err))
	}

	logger.InfoContext(ctx, "signed out everywhere", attr.SlogValueInt(revoked.SignIns()))
	return &gen.LogoutEverywhereResult{
		SessionCookie:   "",
		RevokedSignIns:  revoked.SignIns(),
		RevokedSessions: len(revoked.Sessions),
	}, nil
}

// userSignInOrganizations is every organization the user belongs to or has a
// recorded sign-in in.
func (s *Service) userSignInOrganizations(ctx context.Context, userID string) ([]string, error) {
	orgs, err := s.orgRepo.ListOrganizationsForUser(ctx, conv.ToPGText(userID))
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by callers
	}
	organizationIDs := make([]string, 0, len(orgs))
	for _, org := range orgs {
		organizationIDs = append(organizationIDs, org.ID)
	}
	recorded, err := ssosignins.SignInOrganizations(ctx, s.db, userID)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by callers
	}
	for _, id := range recorded {
		if !slices.Contains(organizationIDs, id) {
			organizationIDs = append(organizationIDs, id)
		}
	}
	return organizationIDs, nil
}

// revokeSignedOutSSOSignIn revokes the sign-ins recorded for the WorkOS
// session behind a dashboard session being signed out. Grants made from them
// keep working but stop qualifying for vouching. Best effort.
func (s *Service) revokeSignedOutSSOSignIn(ctx context.Context, sessionID string, authCtx *contextvalues.AuthContext) {
	session, err := s.sessions.GetSession(ctx, sessionID)
	if err != nil {
		s.logger.WarnContext(ctx, "load session for sign-out sign-in revocation", attr.SlogError(err))
		return
	}
	if session.WorkOSSessionID == "" || session.UserID != authCtx.UserID {
		return
	}
	organizationIDs, err := s.userSignInOrganizations(ctx, authCtx.UserID)
	if err != nil {
		s.logger.ErrorContext(ctx, "list organizations for sign-out", attr.SlogError(err))
		return
	}

	dbtx, err := s.db.BeginTx(ctx, pgx.TxOptions{}) //nolint:exhaustruct // default transaction options
	if err != nil {
		s.logger.ErrorContext(ctx, "begin sign-out sign-in revocation", attr.SlogError(err))
		return
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	revokedIn, err := ssosignins.RevokeSession(ctx, dbtx, organizationIDs, authCtx.UserID, session.WorkOSSessionID)
	if err != nil {
		s.logger.ErrorContext(ctx, "revoke sso sign-in on sign-out", attr.SlogError(err))
		return
	}
	revoked := ssosignins.Revoked{SignInsByOrganization: map[string]int{}, WorkOSSessionIDs: nil, Sessions: nil}
	for _, id := range revokedIn {
		revoked.SignInsByOrganization[id]++
	}
	actor := ssosignins.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), DisplayName: authCtx.Email}
	if err := ssosignins.AuditRevocation(ctx, dbtx, s.auditLogger, actor, authCtx.UserID, conv.PtrValOr(authCtx.Email, ""), ssosignins.RevokeReasonSignOut, revokedIn, revoked); err != nil {
		s.logger.ErrorContext(ctx, "audit sign-out sign-in revocation", attr.SlogError(err))
		return
	}
	if err := dbtx.Commit(ctx); err != nil {
		s.logger.ErrorContext(ctx, "commit sign-out sign-in revocation", attr.SlogError(err))
		return
	}
	ssosignins.RecordRevoked(ctx, ssosignins.RevokeReasonSignOut, revoked.SignIns())
}
