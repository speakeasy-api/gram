package organizations

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	srv "github.com/speakeasy-api/gram/server/gen/http/organizations/server"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

const (
	defaultInviteExpiryDays = 7
	// workosOrgAdminRoleSlug is the WorkOS organization role slug that may manage
	// invites and membership when Gram enterprise RBAC is not authoritative.
	workosOrgAdminRoleSlug = "admin"
)

// OrganizationProvider is the WorkOS surface the organizations service uses.
// *workos.Client implements it.
type OrganizationProvider interface {
	// DeleteOrganizationMembership removes a user from the org in WorkOS using the
	// organization membership ID (not Gram user_id or WorkOS user id).
	DeleteOrganizationMembership(ctx context.Context, workosMembershipID string) error
	GetUserByEmail(ctx context.Context, email string) (*workos.User, error)
	SendInvitation(ctx context.Context, opts workos.SendInvitationOpts) (*workos.Invitation, error)
	ListInvitations(ctx context.Context, orgID string) ([]workos.Invitation, error)
	GetInvitation(ctx context.Context, invitationID string) (*workos.Invitation, error)
	RevokeInvitation(ctx context.Context, invitationID string) (*workos.Invitation, error)
	FindInvitationByToken(ctx context.Context, token string) (*workos.Invitation, error)
	GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*workos.Member, error)
}

var _ OrganizationProvider = (*workos.Client)(nil)

type orgFeatureChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

type Service struct {
	logger   *slog.Logger
	tracer   trace.Tracer
	db       *pgxpool.Pool
	auth     *auth.Auth
	authz    *authz.Engine
	orgs     OrganizationProvider
	features orgFeatureChecker
}

var _ gen.Service = (*Service)(nil)

var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, orgs OrganizationProvider, features orgFeatureChecker, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("organizations"))

	return &Service{
		logger:   logger,
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/organizations"),
		db:       db,
		auth:     auth.New(logger, db, sessions, authzEngine),
		authz:    authzEngine,
		orgs:     orgs,
		features: features,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) SendInvite(ctx context.Context, payload *gen.SendInvitePayload) (*gen.OrganizationInvitation, error) {
	ac, workosOrgID, err := s.orgContext(ctx)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
	)
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Conditions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireWorkOSOrgAdminRole(ctx, logger, ac, workosOrgID); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	inviterWorkosUserID, err := s.resolveGramUserWorkOSUserID(ctx, ac.UserID, logger,
		"inviting user not found",
		"inviter has no WorkOS user id; sign in via WorkOS first or link your account",
	)
	if err != nil {
		return nil, err
	}

	opts := workos.SendInvitationOpts{
		Email:          payload.Email,
		OrganizationID: workosOrgID,
		InviterUserID:  inviterWorkosUserID,
		ExpiresInDays:  defaultInviteExpiryDays,
		RoleSlug:       "",
	}
	if payload.RoleSlug != nil && *payload.RoleSlug != "" {
		opts.RoleSlug = *payload.RoleSlug
	}

	invite, err := s.orgs.SendInvitation(ctx, opts)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "send invitation").Log(ctx, logger)
	}

	out := invitationToGen(invite, &ac.UserID)
	return out, nil
}

func (s *Service) RevokeInvite(ctx context.Context, payload *gen.RevokeInvitePayload) error {
	ac, workosOrgID, err := s.orgContext(ctx)
	if err != nil {
		return err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
		attr.SlogOrganizationInviteID(payload.InvitationID),
	)
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Conditions: nil}); err != nil {
		return err
	}
	if err := s.requireWorkOSOrgAdminRole(ctx, logger, ac, workosOrgID); err != nil {
		return err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	invite, err := s.orgs.GetInvitation(ctx, payload.InvitationID)
	switch {
	case workos.IsNotFound(err):
		return oops.C(oops.CodeNotFound).Log(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get invitation").Log(ctx, logger)
	}
	if invite.OrganizationID != workosOrgID {
		return oops.E(oops.CodeForbidden, nil, "invitation does not belong to this organization").Log(ctx, logger)
	}

	_, err = s.orgs.RevokeInvitation(ctx, payload.InvitationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "revoke invitation").Log(ctx, logger)
	}

	return nil
}

func (s *Service) ListInvites(ctx context.Context, _ *gen.ListInvitesPayload) (*gen.ListInvitesResult, error) {
	ac, workosOrgID, err := s.orgContext(ctx)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
	)

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Conditions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireWorkOSOrgAdminRole(ctx, logger, ac, workosOrgID); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	invites, err := s.orgs.ListInvitations(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list invitations").Log(ctx, logger)
	}

	out := make([]*gen.OrganizationInvitation, 0, len(invites))
	for i := range invites {
		invite := &invites[i]
		if invite.State != workos.InvitationStatePending {
			continue
		}
		var inviterGram *string
		if invite.InviterUserID != "" {
			inviterGram = s.gramUserIDForWorkosID(ctx, invite.InviterUserID)
		}
		out = append(out, invitationToGen(invite, inviterGram))
	}
	return &gen.ListInvitesResult{Invitations: out}, nil
}

func (s *Service) GetInviteByToken(ctx context.Context, payload *gen.GetInviteByTokenPayload) (*gen.OrganizationInvitationAccept, error) {
	invite, err := s.orgs.FindInvitationByToken(ctx, payload.Token)
	switch {
	case workos.IsNotFound(err):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "find invitation by token").Log(ctx, s.logger)
	}

	orgName := ""
	if invite.OrganizationID != "" {
		name, qerr := orgrepo.New(s.db).GetOrganizationNameByWorkosID(ctx, conv.ToPGText(invite.OrganizationID))
		switch {
		case qerr == nil:
			orgName = name
		case errors.Is(qerr, pgx.ErrNoRows):
			// Gram has no row for this WorkOS org yet.
		default:
			return nil, oops.E(oops.CodeUnexpected, qerr, "get organization name").Log(ctx, s.logger)
		}
	}

	return invitationToGenAccept(invite, orgName), nil
}

// ListUsers returns Gram organization members from organization_user_relationships.
// That table is the in-app source of truth for roster and RemoveUser; WorkOS owns
// invite/membership lifecycle but the dashboard "team" list should match what Gram authorizes.
func (s *Service) ListUsers(ctx context.Context, _ *gen.ListUsersPayload) (*gen.ListUsersResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
	)

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get organization metadata").Log(ctx, logger)
	}
	workosOrgID := ""
	if org.WorkosID.Valid {
		workosOrgID = org.WorkosID.String
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Conditions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireWorkOSOrgAdminRole(ctx, logger, ac, workosOrgID); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	rows, err := orgrepo.New(s.db).ListOrganizationUsers(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization users").Log(ctx, logger)
	}

	out := make([]*gen.OrganizationUser, 0, len(rows))
	for i := range rows {
		out = append(out, organizationUserToGen(&rows[i]))
	}
	return &gen.ListUsersResult{Users: out}, nil
}

func (s *Service) RemoveUser(ctx context.Context, payload *gen.RemoveUserPayload) error {
	ac, workosOrgID, err := s.orgContext(ctx)
	if err != nil {
		return err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
	)
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Conditions: nil}); err != nil {
		return err
	}
	if err := s.requireWorkOSOrgAdminRole(ctx, logger, ac, workosOrgID); err != nil {
		return err
	}

	if payload.UserID == ac.UserID {
		return oops.E(oops.CodeBadRequest, nil, "cannot remove yourself from the organization").Log(ctx, logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	qtx := orgrepo.New(tx)

	rel, err := qtx.GetOrganizationUserRelationship(ctx, orgrepo.GetOrganizationUserRelationshipParams{
		OrganizationID: ac.ActiveOrganizationID,
		UserID:         payload.UserID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "user is not a member of this organization").Log(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get organization user relationship").Log(ctx, logger)
	}

	if err := qtx.DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: ac.ActiveOrganizationID,
		UserID:         payload.UserID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete organization user relationship").Log(ctx, logger)
	}

	if rel.WorkosMembershipID.Valid && rel.WorkosMembershipID.String != "" {
		if err := s.orgs.DeleteOrganizationMembership(ctx, rel.WorkosMembershipID.String); err != nil {
			return oops.E(oops.CodeUnexpected, err, "remove user").Log(ctx, logger)
		}
	} else {
		s.logger.DebugContext(ctx, "skipping WorkOS membership delete: no workos_membership_id on relationship",
			attr.SlogOrganizationID(ac.ActiveOrganizationID),
			attr.SlogUserID(payload.UserID),
			attr.SlogWorkOSOrganizationID(workosOrgID),
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}

func (s *Service) authContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil {
		return nil, oops.E(oops.CodeUnauthorized, errors.New("missing auth context"), "missing auth context").Log(ctx, s.logger)
	}

	return ac, nil
}

func (s *Service) orgContext(ctx context.Context) (*contextvalues.AuthContext, string, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, "", oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, "", oops.E(oops.CodeUnexpected, err, "get organization metadata").Log(ctx, s.logger)
	}
	if !org.WorkosID.Valid || org.WorkosID.String == "" {
		return nil, "", oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS").Log(ctx, s.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	return ac, org.WorkosID.String, nil
}

// requireWorkOSOrgAdminRole checks that the current user holds the admin role in WorkOS.
// It is a no-op for enterprise accounts with the RBAC feature enabled, since those orgs
// are gated by s.authz.Require instead.
func (s *Service) requireWorkOSOrgAdminRole(ctx context.Context, logger *slog.Logger, ac *contextvalues.AuthContext, workosOrgID string) error {
	if s.features == nil {
		return oops.E(oops.CodeUnexpected, errors.New("product features checker not configured"), "internal configuration error").Log(ctx, logger)
	}

	enabled, err := s.features.IsFeatureEnabled(ctx, ac.ActiveOrganizationID, productfeatures.FeatureRBAC)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check RBAC feature").Log(ctx, logger)
	}

	if enabled && ac.AccountType == "enterprise" {
		return nil
	}

	if workosOrgID == "" {
		return oops.E(oops.CodeForbidden, nil, "organization administrator privileges required").Log(ctx, logger)
	}

	workosUserID, err := s.resolveGramUserWorkOSUserID(ctx, ac.UserID, logger, "user not found", "user has no WorkOS user id; sign in via WorkOS first or link your account")
	if err != nil {
		return err
	}

	mem, err := s.orgs.GetOrgMembership(ctx, workosUserID, workosOrgID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check organization membership role").Log(ctx, logger)
	}
	if mem == nil || !strings.EqualFold(mem.RoleSlug, workosOrgAdminRoleSlug) {
		return oops.C(oops.CodeForbidden).Log(ctx, logger)
	}

	return nil
}

// resolveGramUserWorkOSUserID returns the WorkOS user id for a Gram user, optionally persisting it from email lookup.
func (s *Service) resolveGramUserWorkOSUserID(ctx context.Context, gramUserID string, logger *slog.Logger, notFoundMsg, noWorkOSMsg string) (string, error) {
	u, err := userrepo.New(s.db).GetUser(ctx, gramUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", oops.E(oops.CodeNotFound, nil, "%s", notFoundMsg).Log(ctx, logger)
		}
		return "", oops.E(oops.CodeUnexpected, err, "get user").Log(ctx, logger)
	}
	if u.WorkosID.Valid && strings.TrimSpace(u.WorkosID.String) != "" {
		return strings.TrimSpace(u.WorkosID.String), nil
	}

	wu, err := s.orgs.GetUserByEmail(ctx, u.Email)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "lookup WorkOS user by email").Log(ctx, logger)
	}
	if wu == nil {
		return "", oops.E(oops.CodeBadRequest, nil, "%s", noWorkOSMsg).Log(ctx, logger)
	}

	if err := userrepo.New(s.db).SetUserWorkosID(ctx, userrepo.SetUserWorkosIDParams{
		ID:       gramUserID,
		WorkosID: conv.ToPGText(wu.ID),
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to persist WorkOS user id", attr.SlogError(err))
	}
	return wu.ID, nil
}

// invitationToGen maps a WorkOS invitation into the public API shape. gramOrganizationID
// and inviterGramUserID are Gram identifiers; WorkOS IDs are not exposed for those fields.
func invitationToGen(inv *workos.Invitation, inviterGramUserID *string) *gen.OrganizationInvitation {
	if inv == nil {
		return nil
	}
	var inviter *string
	if inviterGramUserID != nil && *inviterGramUserID != "" {
		inviter = inviterGramUserID
	}
	return &gen.OrganizationInvitation{
		ID:            inv.ID,
		Email:         inv.Email,
		State:         string(inv.State),
		AcceptedAt:    conv.PtrEmpty(inv.AcceptedAt),
		RevokedAt:     conv.PtrEmpty(inv.RevokedAt),
		InviterUserID: inviter,
		ExpiresAt:     conv.PtrEmpty(inv.ExpiresAt),
		CreatedAt:     inv.CreatedAt,
		UpdatedAt:     inv.UpdatedAt,
	}
}

func (s *Service) gramUserIDForWorkosID(ctx context.Context, workosUserID string) *string {
	if workosUserID == "" {
		return nil
	}
	gramID, err := userrepo.New(s.db).GetUserIDByWorkosID(ctx, pgtype.Text{String: workosUserID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		s.logger.WarnContext(ctx, "lookup gram user by workos user id",
			attr.SlogError(err),
			attr.SlogWorkOSUserID(workosUserID),
		)
		return nil
	}
	if gramID == "" {
		return nil
	}
	return &gramID
}

func invitationToGenAccept(inv *workos.Invitation, organizationName string) *gen.OrganizationInvitationAccept {
	if inv == nil {
		return nil
	}
	return &gen.OrganizationInvitationAccept{
		Email:               inv.Email,
		State:               string(inv.State),
		OrganizationName:    organizationName,
		AcceptInvitationURL: inv.AcceptInvitationURL,
	}
}

func organizationUserToGen(row *orgrepo.ListOrganizationUsersRow) *gen.OrganizationUser {
	if row == nil {
		return nil
	}
	createdAt := ""
	if row.CreatedAt.Valid {
		createdAt = row.CreatedAt.Time.UTC().Format(time.RFC3339)
	}
	updatedAt := ""
	if row.UpdatedAt.Valid {
		updatedAt = row.UpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	return &gen.OrganizationUser{
		ID:                 strconv.FormatInt(row.ID, 10),
		OrganizationID:     row.OrganizationID,
		UserID:             row.UserID,
		Name:               row.UserDisplayName,
		Email:              row.UserEmail,
		PhotoURL:           conv.FromPGText[string](row.UserPhotoUrl),
		WorkosMembershipID: conv.FromPGText[string](row.WorkosMembershipID),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}
}
