package usersessions

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RevokeMySignIns signs the caller out everywhere: every SSO sign-in in every
// organization they belong to, and every MCP grant made from them.
func (s *Service) RevokeMySignIns(ctx context.Context, _ *gen.RevokeMySignInsPayload) (*types.RevokeSignInsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	orgs, err := orgrepo.New(s.db).ListOrganizationsForUser(ctx, conv.ToPGText(authCtx.UserID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organizations for user").LogError(ctx, s.logger)
	}
	organizationIDs := make([]string, 0, len(orgs))
	for _, org := range orgs {
		organizationIDs = append(organizationIDs, org.ID)
	}
	return s.revokeSignIns(ctx, s.logger.With(attr.SlogUserID(authCtx.UserID)), authCtx, organizationIDs, authCtx.UserID, ssosignins.RevokeReasonSignOutEverywhere)
}

// RevokeMemberSignIns revokes a member's SSO sign-ins in the caller's
// organization and every MCP grant made from them.
func (s *Service) RevokeMemberSignIns(ctx context.Context, payload *gen.RevokeMemberSignInsPayload) (*types.RevokeSignInsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	if payload.UserID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "user_id is required").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(payload.UserID))
	return s.revokeSignIns(ctx, logger, authCtx, []string{authCtx.ActiveOrganizationID}, payload.UserID, ssosignins.RevokeReasonAdmin)
}

func (s *Service) revokeSignIns(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, organizationIDs []string, userID string, reason ssosignins.RevokeReason) (*types.RevokeSignInsResult, error) {
	result := &types.RevokeSignInsResult{RevokedSignIns: 0, RevokedSessions: 0}
	if len(organizationIDs) == 0 {
		return result, nil
	}

	dbtx, err := s.db.BeginTx(ctx, pgx.TxOptions{}) //nolint:exhaustruct // default transaction options
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	revoked, err := ssosignins.RevokeUser(ctx, dbtx, organizationIDs, userID, reason)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke sso sign-ins").LogError(ctx, logger)
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	for _, session := range revoked.Sessions {
		if err := s.audit.LogUserSessionRevoke(ctx, dbtx, audit.LogUserSessionRevokeEvent{
			OrganizationID:   session.OrganizationID,
			ProjectID:        session.ProjectID,
			Actor:            actor,
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			UserSessionURN:   urn.NewUserSession(session.ID),
			Principal:        session.Subject,
			Jti:              session.JTI,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log user session revocation").LogError(ctx, logger)
		}
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Push after commit so a cached jti always matches a soft-deleted row.
	var pushErr error
	for _, session := range revoked.Sessions {
		if session.JTI == "" {
			continue
		}
		if err := s.chatSessions.RevokeToken(ctx, session.JTI); err != nil && pushErr == nil {
			pushErr = err
		}
	}
	if pushErr != nil {
		return nil, oops.E(oops.CodeUnexpected, pushErr, "push jti into revocation cache").LogError(ctx, logger)
	}

	result.RevokedSignIns = revoked.SignIns
	result.RevokedSessions = len(revoked.Sessions)
	logger.InfoContext(ctx, "revoked sso sign-ins", attr.SlogReason(string(reason)))
	return result, nil
}
