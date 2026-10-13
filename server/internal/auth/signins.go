package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// epochTimeout bounds the epoch write, detached from the caller's
// cancellation.
const epochTimeout = 5 * time.Second

// SetTokenRevoker wires the access-token revocation cache used when signing
// out, and the deliverer that pushes revocations durably.
func (s *Service) SetTokenRevoker(tokens ssosignins.TokenRevoker, meterProvider metric.MeterProvider) {
	s.tokens = tokens
	s.deliverer = ssosignins.NewDeliverer(s.logger, meterProvider, s.db, tokens, s.sessions, s.sessions)
}

// LogoutEverywhere ends every dashboard and identity provider session the
// caller holds, revokes their SSO sign-ins in every organization they ever
// signed in to, and revokes every MCP grant made from them. The revocation,
// including the dashboard epoch, commits durably first; the epoch is then
// raised right away and retried from the delivery table if that fails.
func (s *Service) LogoutEverywhere(ctx context.Context, _ *gen.LogoutEverywherePayload) (*gen.LogoutEverywhereResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil || authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if contextvalues.IsLegacyImpersonatedSession(ctx) || contextvalues.IsSupportSession(ctx) || authCtx.SupportOrganizationID != "" {
		return nil, oops.E(oops.CodeForbidden, nil, "sign out everywhere is not available from an impersonated or support session")
	}
	// Gated on the caller's active organization. Once available it signs the
	// user out of every organization: the user's own action over their own
	// sessions.
	if !s.sessions.SSOSignInEnforced(ctx, authCtx.ActiveOrganizationID) {
		return nil, oops.E(oops.CodeForbidden, nil, "sign out everywhere is not available")
	}
	logger := s.logger.With(attr.SlogUserID(authCtx.UserID))
	organizationIDs, err := s.userSignInOrganizations(ctx, authCtx.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organizations for sign-out").LogError(ctx, logger)
	}
	idpSessions, err := s.sessions.ListUserIDPSessionIDs(ctx, authCtx.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list identity provider sessions").LogError(ctx, logger)
	}
	if current, err := s.sessions.GetSession(ctx, *authCtx.SessionID); err == nil && current.WorkOSSessionID != "" && !slices.Contains(idpSessions, current.WorkOSSessionID) {
		idpSessions = append(idpSessions, current.WorkOSSessionID)
	}
	deliveryOrg := authCtx.ActiveOrganizationID
	if deliveryOrg == "" && len(organizationIDs) > 0 {
		deliveryOrg = organizationIDs[0]
	}
	epochAt := time.Now()
	if deliveryOrg == "" {
		// Nothing to anchor a durable delivery to: raise the epoch first so
		// a failure leaves the caller signed in to retry.
		if err := s.raiseEpoch(ctx, authCtx.UserID, epochAt); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "end dashboard sessions").LogError(ctx, logger)
		}
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
	deliveryIDs := revoked.DeliveryIDs
	if deliveryOrg != "" {
		listed, err := ssosignins.EnqueueWorkOSSessionRevocations(ctx, dbtx, deliveryOrg, idpSessions)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "enqueue identity provider sign-out").LogError(ctx, logger)
		}
		epoch, err := ssosignins.EnqueueUserEpoch(ctx, dbtx, deliveryOrg, authCtx.UserID, epochAt)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "enqueue dashboard sign-out").LogError(ctx, logger)
		}
		deliveryIDs = slices.Concat(deliveryIDs, listed, []uuid.UUID{epoch})
	}
	actor := ssosignins.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), DisplayName: authCtx.Email}
	if err := ssosignins.AuditRevocation(ctx, dbtx, s.auditLogger, actor, authCtx.UserID, conv.PtrValOr(authCtx.Email, ""), ssosignins.RevokeReasonSignOutEverywhere, organizationIDs, revoked); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit sign-in revocation").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	ssosignins.RecordRevoked(ctx, ssosignins.RevokeReasonSignOutEverywhere, revoked.SignIns())

	if deliveryOrg != "" {
		if err := s.raiseEpoch(ctx, authCtx.UserID, epochAt); err != nil {
			logger.WarnContext(ctx, "raise session epoch after sign-out everywhere; left for retry", attr.SlogError(err))
		}
		deliveryOrgs := organizationIDs
		if !slices.Contains(deliveryOrgs, deliveryOrg) {
			deliveryOrgs = append(slices.Clone(deliveryOrgs), deliveryOrg)
		}
		s.deliverer.DeliverNow(ctx, deliveryOrgs, deliveryIDs)
	} else {
		// No organization to anchor a durable delivery; end them directly.
		for _, id := range idpSessions {
			if err := s.sessions.RevokeSession(ctx, id); err != nil {
				logger.WarnContext(ctx, "revoke identity provider session", attr.SlogError(err))
			}
		}
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

// raiseEpoch raises the user's dashboard epoch detached from the caller's
// cancellation, bounded by epochTimeout.
func (s *Service) raiseEpoch(ctx context.Context, userID string, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), epochTimeout)
	defer cancel()
	if err := s.sessions.RaiseUserEpoch(ctx, userID, at); err != nil {
		return fmt.Errorf("raise user epoch: %w", err)
	}
	return nil
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

// signOutAttempts bounds retries of a sign-out revocation that lost a
// deadlock or serialization race.
const signOutAttempts = 3

// revokeSignedOutSSOSignIn durably revokes the sign-ins recorded for the
// WorkOS session behind a dashboard session being signed out and enqueues
// ending that WorkOS session. Grants made from them keep working but stop
// qualifying for vouching.
func (s *Service) revokeSignedOutSSOSignIn(ctx context.Context, sessionID string, authCtx *contextvalues.AuthContext) (ssosignins.SignedOut, error) {
	var none ssosignins.SignedOut
	session, err := s.sessions.GetSession(ctx, sessionID)
	if err != nil {
		return none, fmt.Errorf("load session: %w", err)
	}
	if session.WorkOSSessionID == "" || session.UserID != authCtx.UserID {
		return none, nil
	}
	organizationIDs, err := s.userSignInOrganizations(ctx, authCtx.UserID)
	if err != nil {
		return none, fmt.Errorf("list organizations: %w", err)
	}
	// The first organization holds the signed-out marker when nothing was
	// recorded yet.
	if i := slices.Index(organizationIDs, authCtx.ActiveOrganizationID); i > 0 {
		organizationIDs[0], organizationIDs[i] = organizationIDs[i], organizationIDs[0]
	}
	for attempt := 1; ; attempt++ {
		out, err := s.revokeSignedOutOnce(ctx, organizationIDs, session.WorkOSSessionID, authCtx)
		if err == nil || attempt == signOutAttempts || !retryableTxError(err) {
			return out, err
		}
	}
}

func (s *Service) revokeSignedOutOnce(ctx context.Context, organizationIDs []string, workosSessionID string, authCtx *contextvalues.AuthContext) (ssosignins.SignedOut, error) {
	var none ssosignins.SignedOut
	dbtx, err := s.db.BeginTx(ctx, pgx.TxOptions{}) //nolint:exhaustruct // default transaction options
	if err != nil {
		return none, fmt.Errorf("begin: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	out, err := ssosignins.RevokeSession(ctx, dbtx, organizationIDs, authCtx.UserID, workosSessionID)
	if err != nil {
		return none, fmt.Errorf("revoke: %w", err)
	}
	revoked := ssosignins.Revoked{SignInsByOrganization: map[string]int{}, WorkOSSessionIDs: nil, Sessions: nil, DeliveryIDs: nil}
	for _, id := range out.Organizations {
		revoked.SignInsByOrganization[id]++
	}
	actor := ssosignins.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), DisplayName: authCtx.Email}
	if err := ssosignins.AuditRevocation(ctx, dbtx, s.auditLogger, actor, authCtx.UserID, conv.PtrValOr(authCtx.Email, ""), ssosignins.RevokeReasonSignOut, out.Organizations, revoked); err != nil {
		return none, fmt.Errorf("audit: %w", err)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return none, fmt.Errorf("commit: %w", err)
	}
	ssosignins.RecordRevoked(ctx, ssosignins.RevokeReasonSignOut, revoked.SignIns())
	return out, nil
}

// retryableTxError reports a deadlock or serialization failure.
func retryableTxError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == pgerrcode.DeadlockDetected || pgErr.Code == pgerrcode.SerializationFailure)
}
