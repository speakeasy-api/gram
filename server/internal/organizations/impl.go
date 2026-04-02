package organizations

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	srv "github.com/speakeasy-api/gram/server/gen/http/organizations/server"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"go.opentelemetry.io/otel/trace"

	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

const (
	defaultInviteExpiryDays = 7
)

type OrganizationProvider interface {
	ListUsers(ctx context.Context, orgID string) ([]workos.User, error)
	RemoveUser(ctx context.Context, orgID, userID string) error
	SendInvitation(ctx context.Context, opts workos.SendInvitationOpts) (*workos.Invitation, error)
	ListInvitations(ctx context.Context, orgID string) ([]workos.Invitation, error)
	RevokeInvitation(ctx context.Context, invitationID string) (*workos.Invitation, error)
	FindInvitationByToken(ctx context.Context, token string) (*workos.Invitation, error)
}

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

	opts := workos.SendInvitationOpts{
		Email:          payload.Email,
		OrganizationID: workosOrgID,
		InviterUserID:  ac.UserID,
		ExpiresInDays:  defaultInviteExpiryDays,
	}
	if payload.RoleSlug != nil && *payload.RoleSlug != "" {
		opts.RoleSlug = *payload.RoleSlug
	}

	invite, err := s.orgs.SendInvitation(ctx, opts)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "send invitation").Log(ctx, logger)
	}

	out := invitationToGen(invite)
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
	for _, invite := range invites {
		if invite.State != workos.InvitationStatePending {
			continue
		}
		out = append(out, invitationToGen(&invite))
	}
	return &gen.ListInvitesResult{Invitations: out}, nil
}

func (s *Service) GetInviteByToken(ctx context.Context, payload *gen.GetInviteByTokenPayload) (*gen.OrganizationInvitationAccept, error) {
	ac, _, err := s.orgContext(ctx)
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

	invite, err := s.orgs.FindInvitationByToken(ctx, payload.Token)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "find invitation by token").Log(ctx, logger)
	}

	return invitationToGenAccept(invite), nil
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
	defer tx.Rollback(ctx)

	qtx := orgrepo.New(tx)

	member, err := qtx.HasOrganizationUserRelationship(ctx, orgrepo.HasOrganizationUserRelationshipParams{
		OrganizationID: ac.ActiveOrganizationID,
		UserID:         payload.UserID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check organization membership").Log(ctx, logger)
	}
	if !member {
		return oops.E(oops.CodeNotFound, nil, "user is not a member of this organization").Log(ctx, logger)
	}

	if err := qtx.DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: ac.ActiveOrganizationID,
		UserID:         payload.UserID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete organization user relationship").Log(ctx, logger)
	}

	if err := s.orgs.RemoveUser(ctx, workosOrgID, payload.UserID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "remove user").Log(ctx, logger)
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

func invitationToGen(inv *workos.Invitation) *gen.OrganizationInvitation {
	if inv == nil {
		return nil
	}
	return &gen.OrganizationInvitation{
		ID:             inv.ID,
		Email:          inv.Email,
		State:          string(inv.State),
		AcceptedAt:     optionalTimeString(inv.AcceptedAt),
		RevokedAt:      optionalTimeString(inv.RevokedAt),
		RoleSlug:       nil,
		OrganizationID: inv.OrganizationID,
		InviterUserID:  optionalTimeString(inv.InviterUserID),
		ExpiresAt:      optionalTimeString(inv.ExpiresAt),
		CreatedAt:      inv.CreatedAt,
		UpdatedAt:      inv.UpdatedAt,
	}
}

func invitationToGenAccept(inv *workos.Invitation) *gen.OrganizationInvitationAccept {
	if inv == nil {
		return nil
	}
	return &gen.OrganizationInvitationAccept{
		Email:               inv.Email,
		State:               string(inv.State),
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
