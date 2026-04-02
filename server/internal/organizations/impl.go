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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

const (
	defaultInviteExpiryDays = 7
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
	RevokeInvitation(ctx context.Context, invitationID string) (*workos.Invitation, error)
	FindInvitationByToken(ctx context.Context, token string) (*workos.Invitation, error)
}

var _ OrganizationProvider = (*workos.Client)(nil)

type Service struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
	auth   *auth.Auth
	orgs   OrganizationProvider
}

var _ gen.Service = (*Service)(nil)

var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, orgs OrganizationProvider) *Service {
	logger = logger.With(attr.SlogComponent("organizations"))

	return &Service{
		logger: logger,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/organizations"),
		db:     db,
		auth:   auth.New(logger, db, sessions),
		orgs:   orgs,
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

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	inviterWorkosUserID, err := s.workosInviterUserID(ctx, ac.UserID, logger)
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

	out := invitationToGen(invite, ac.ActiveOrganizationID, &ac.UserID)
	if payload.RoleSlug != nil && *payload.RoleSlug != "" {
		rs := *payload.RoleSlug
		out.RoleSlug = &rs
	}
	return out, nil
}

func (s *Service) RevokeInvite(ctx context.Context, payload *gen.RevokeInvitePayload) error {
	ac, _, err := s.orgContext(ctx)
	if err != nil {
		return err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
		attr.SlogOrganizationInviteID(payload.InvitationID),
	)

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

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
		out = append(out, invitationToGen(invite, ac.ActiveOrganizationID, inviterGram))
	}
	return &gen.ListInvitesResult{Invitations: out}, nil
}

func (s *Service) GetInviteByToken(ctx context.Context, payload *gen.GetInviteByTokenPayload) (*gen.OrganizationInvitationAccept, error) {
	invite, err := s.orgs.FindInvitationByToken(ctx, payload.Token)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "find invitation by token").Log(ctx, s.logger)
	}

	orgName := ""
	if invite != nil && invite.OrganizationID != "" {
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
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}

	if ac.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
	)

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

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := orgrepo.New(tx)

	rel, err := qtx.GetOrganizationUserRelationship(ctx, orgrepo.GetOrganizationUserRelationshipParams{
		OrganizationID: ac.ActiveOrganizationID,
		UserID:         payload.UserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "user is not a member of this organization").Log(ctx, logger)
		}
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

	if ac.ActiveOrganizationID == "" {
		return nil, "", oops.C(oops.CodeUnauthorized)
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

func optionalTimeString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// invitationToGen maps a WorkOS invitation into the public API shape. gramOrganizationID
// and inviterGramUserID are Gram identifiers; WorkOS IDs are not exposed for those fields.
func invitationToGen(inv *workos.Invitation, gramOrganizationID string, inviterGramUserID *string) *gen.OrganizationInvitation {
	if inv == nil {
		return nil
	}
	var inviter *string
	if inviterGramUserID != nil && *inviterGramUserID != "" {
		inviter = inviterGramUserID
	}
	return &gen.OrganizationInvitation{
		ID:             inv.ID,
		Email:          inv.Email,
		State:          string(inv.State),
		AcceptedAt:     optionalTimeString(inv.AcceptedAt),
		RevokedAt:      optionalTimeString(inv.RevokedAt),
		RoleSlug:       nil,
		OrganizationID: gramOrganizationID,
		InviterUserID:  inviter,
		ExpiresAt:      optionalTimeString(inv.ExpiresAt),
		CreatedAt:      inv.CreatedAt,
		UpdatedAt:      inv.UpdatedAt,
	}
}

// workosInviterUserID returns the WorkOS user id for the Gram user sending an invitation.
func (s *Service) workosInviterUserID(ctx context.Context, gramUserID string, logger *slog.Logger) (string, error) {
	u, err := userrepo.New(s.db).GetUser(ctx, gramUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", oops.E(oops.CodeNotFound, nil, "inviting user not found").Log(ctx, logger)
		}
		return "", oops.E(oops.CodeUnexpected, err, "get inviting user").Log(ctx, logger)
	}
	if u.WorkosID.Valid && strings.TrimSpace(u.WorkosID.String) != "" {
		return strings.TrimSpace(u.WorkosID.String), nil
	}

	wu, err := s.orgs.GetUserByEmail(ctx, u.Email)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "lookup WorkOS user by email").Log(ctx, logger)
	}
	if wu == nil {
		return "", oops.E(oops.CodeBadRequest, nil, "inviter has no WorkOS user id; sign in via WorkOS first or link your account").Log(ctx, logger)
	}

	if err := userrepo.New(s.db).SetUserWorkosID(ctx, userrepo.SetUserWorkosIDParams{
		ID:       gramUserID,
		WorkosID: conv.ToPGText(wu.ID),
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to persist WorkOS user id for inviter", attr.SlogError(err))
	}
	return wu.ID, nil
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

func organizationUserToGen(row *orgrepo.OrganizationUserRelationship) *gen.OrganizationUser {
	if row == nil {
		return nil
	}
	var workosMem *string
	if row.WorkosMembershipID.Valid {
		s := row.WorkosMembershipID.String
		workosMem = &s
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
		WorkosMembershipID: workosMem,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}
}
