package organizations

import (
	"context"
	"errors"
	"log/slog"

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
	GetInvitation(ctx context.Context, invitationID string) (*workos.Invitation, error)
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
		return nil, err
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

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	_, err = s.orgs.RevokeInvitation(ctx, payload.InvitationID)
	if err != nil {
		return err
	}

	return nil
}

func (s *Service) ListInvites(context.Context, *gen.ListInvitesPayload) (*gen.ListInvitesResult, error) {
	return nil, errNotImplemented
}

func (s *Service) GetInviteByID(context.Context, *gen.GetInviteByIDPayload) (*gen.OrganizationInvitation, error) {
	return nil, errNotImplemented
}

func (s *Service) GetInviteByToken(context.Context, *gen.GetInviteByTokenPayload) (*gen.OrganizationInvitationAccept, error) {
	return nil, errNotImplemented
}

func (s *Service) ListUsers(context.Context, *gen.ListUsersPayload) (*gen.ListUsersResult, error) {
	return nil, errNotImplemented
}

func (s *Service) RemoveUser(context.Context, *gen.RemoveUserPayload) error {
	return errNotImplemented
}

var errNotImplemented = errors.New("not implemented")

func (s *Service) authContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil {
		return nil, errors.New("missing auth context")
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
		ID:                  inv.ID,
		Email:               inv.Email,
		State:               string(inv.State),
		OrganizationID:      inv.OrganizationID,
		ExpiresAt:           optionalTimeString(inv.ExpiresAt),
		AcceptInvitationURL: inv.AcceptInvitationURL,
		CreatedAt:           inv.CreatedAt,
		UpdatedAt:           inv.UpdatedAt,
	}
}
