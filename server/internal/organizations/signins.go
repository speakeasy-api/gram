package organizations

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/metric"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/signins"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SetTokenRevoker wires the access-token revocation cache used when revoking
// a member's sign-ins.
func (s *Service) SetTokenRevoker(tokens signins.TokenRevoker, meterProvider metric.MeterProvider) {
	s.deliverer = signins.NewDeliverer(s.logger, meterProvider, s.db, tokens, s.sessions, s.sessions)
}

// DrainDeliveries waits, bounded by ctx, for revocation pushes in flight.
func (s *Service) DrainDeliveries(ctx context.Context) { s.deliverer.Drain(ctx) }

// RevokeMemberSignIns revokes a member's sign-ins in the active
// organization, ends their identity provider sessions and revokes every MCP
// grant made from those sign-ins.
func (s *Service) RevokeMemberSignIns(ctx context.Context, payload *gen.RevokeMemberSignInsPayload) (*types.RevokeSignInsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}
	if ac.UserID == "" || ac.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	if !s.sessions.SignInEnforced(ctx, ac.ActiveOrganizationID) {
		return nil, oops.E(oops.CodeForbidden, nil, "sign-in revocation is not available")
	}
	if payload.UserID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "user_id is required").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID), attr.SlogUserID(payload.UserID))

	dbtx, err := s.db.BeginTx(ctx, pgx.TxOptions{}) //nolint:exhaustruct // default transaction options
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	revoked, err := s.revokeMember(ctx, dbtx, ac, payload.UserID, s.memberEmail(ctx, payload.UserID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke sign-ins").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	s.afterMemberRevoked(ctx, ac.ActiveOrganizationID, revoked)

	logger.InfoContext(ctx, "revoked member sign-ins", attr.SlogValueInt(revoked.SignIns()))
	return &types.RevokeSignInsResult{RevokedSignIns: revoked.SignIns(), RevokedSessions: len(revoked.Sessions)}, nil
}

// revokeMember revokes a member's sign-ins and grants in the active
// organization inside the caller's transaction, audited as the admin.
// Resolve memberEmail before opening the transaction.
func (s *Service) revokeMember(ctx context.Context, dbtx pgx.Tx, ac *contextvalues.AuthContext, userID, memberEmail string) (signins.Revoked, error) {
	organizationIDs := []string{ac.ActiveOrganizationID}
	revoked, err := signins.RevokeUser(ctx, dbtx, organizationIDs, userID, signins.RevokeReasonAdmin)
	if err != nil {
		return revoked, fmt.Errorf("revoke sign-ins: %w", err)
	}
	actor := signins.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), DisplayName: ac.Email}
	if err := signins.AuditRevocation(ctx, dbtx, s.audit, actor, userID, memberEmail, signins.RevokeReasonAdmin, organizationIDs, revoked); err != nil {
		return revoked, fmt.Errorf("audit sign-in revocation: %w", err)
	}
	return revoked, nil
}

// afterMemberRevoked records and delivers a committed member revocation.
func (s *Service) afterMemberRevoked(ctx context.Context, organizationID string, revoked signins.Revoked) {
	signins.RecordRevoked(ctx, signins.RevokeReasonAdmin, revoked.SignIns())
	s.deliverer.DeliverNow(ctx, []string{organizationID}, revoked.DeliveryIDs)
}

// memberEmail is the member's email for the audit entry, or empty. It may
// read through to the database, so callers resolve it outside a transaction.
func (s *Service) memberEmail(ctx context.Context, userID string) string {
	if info, _, err := s.sessions.GetUserInfo(ctx, userID); err == nil && info != nil {
		return info.Email
	}
	return ""
}
