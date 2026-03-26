package teams

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/teams/server"
	gen "github.com/speakeasy-api/gram/server/gen/teams"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	sessions *sessions.Manager
	auth     *auth.Auth
	orgRepo  *orgRepo.Queries
	userRepo *userRepo.Queries
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("teams"))

	return &Service{
		tracer:   otel.Tracer("github.com/speakeasy-api/gram/server/internal/teams"),
		logger:   logger,
		sessions: sessions,
		auth:     auth.New(logger, nil, sessions),
		orgRepo:  orgRepo.New(db),
		userRepo: userRepo.New(db),
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

// getAuthContext extracts and validates the auth context from the request.
func (s *Service) getAuthContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	return authCtx, nil
}

// getOrgWorkOSID looks up the WorkOS organization ID for a Gram organization.
func (s *Service) getOrgWorkOSID(ctx context.Context, orgID string) (string, error) {
	org, err := s.orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to get organization metadata").Log(ctx, s.logger)
	}

	if !org.WorkosID.Valid || org.WorkosID.String == "" {
		return "", oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS")
	}

	return org.WorkosID.String, nil
}

// getUserWorkOSID looks up the WorkOS user ID for a Gram user.
func (s *Service) getUserWorkOSID(ctx context.Context, userID string) (string, error) {
	user, err := s.userRepo.GetUser(ctx, userID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to get user").Log(ctx, s.logger)
	}

	if !user.WorkosID.Valid || user.WorkosID.String == "" {
		return "", oops.E(oops.CodeBadRequest, nil, "user is not linked to WorkOS")
	}

	return user.WorkosID.String, nil
}

func invitationToGenInvite(inv usermanagement.Invitation, inviterName string) *gen.TeamInvite {
	return &gen.TeamInvite{
		ID:        inv.ID,
		Email:     inv.Email,
		Status:    string(inv.State),
		InvitedBy: inviterName,
		CreatedAt: inv.CreatedAt,
		ExpiresAt: inv.ExpiresAt,
	}
}

func (s *Service) ListMembers(ctx context.Context, payload *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.getOrgWorkOSID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	workos := s.sessions.WorkOS()
	users, err := workos.ListUsersInOrg(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "failed to list organization members from WorkOS").Log(ctx, s.logger)
	}

	members := make([]*gen.TeamMember, 0, len(users))
	for _, u := range users {
		displayName := u.FirstName
		if u.LastName != "" {
			if displayName != "" {
				displayName += " "
			}
			displayName += u.LastName
		}
		if displayName == "" {
			displayName = u.Email
		}

		member := &gen.TeamMember{
			ID:          u.ID,
			Email:       u.Email,
			DisplayName: displayName,
			JoinedAt:    u.CreatedAt,
		}
		if u.ProfilePictureURL != "" {
			member.PhotoURL = &u.ProfilePictureURL
		}
		members = append(members, member)
	}

	return &gen.ListMembersResult{Members: members}, nil
}

func (s *Service) InviteMember(ctx context.Context, payload *gen.InviteMemberPayload) (*gen.InviteMemberResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.getOrgWorkOSID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	inviterWorkOSID, err := s.getUserWorkOSID(ctx, authCtx.UserID)
	if err != nil {
		return nil, err
	}

	workos := s.sessions.WorkOS()
	inv, err := workos.SendInvitation(ctx, usermanagement.SendInvitationOpts{
		Email:          payload.Email,
		OrganizationID: workosOrgID,
		InviterUserID:  inviterWorkOSID,
		ExpiresInDays:  7,
	})
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "failed to send invitation via WorkOS").Log(ctx, s.logger)
	}

	inviterName := ""
	if authCtx.Email != nil {
		inviterName = *authCtx.Email
	}

	return &gen.InviteMemberResult{
		Invite: invitationToGenInvite(inv, inviterName),
	}, nil
}

func (s *Service) ListInvites(ctx context.Context, payload *gen.ListInvitesPayload) (*gen.ListInvitesResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.getOrgWorkOSID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	workos := s.sessions.WorkOS()
	invitations, err := workos.ListInvitations(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "failed to list invitations from WorkOS").Log(ctx, s.logger)
	}

	invites := make([]*gen.TeamInvite, 0, len(invitations))
	for _, inv := range invitations {
		inviterName := ""
		if inv.InviterUserID != "" {
			if inviter, err := workos.GetUser(ctx, inv.InviterUserID); err == nil {
				inviterName = inviter.FirstName
				if inviter.LastName != "" {
					if inviterName != "" {
						inviterName += " "
					}
					inviterName += inviter.LastName
				}
				if inviterName == "" {
					inviterName = inviter.Email
				}
			}
		}

		invites = append(invites, invitationToGenInvite(inv, inviterName))
	}

	return &gen.ListInvitesResult{Invites: invites}, nil
}

func (s *Service) CancelInvite(ctx context.Context, payload *gen.CancelInvitePayload) error {
	_, err := s.getAuthContext(ctx)
	if err != nil {
		return err
	}

	workos := s.sessions.WorkOS()
	_, err = workos.RevokeInvitation(ctx, payload.InviteID)
	if err != nil {
		return oops.E(oops.CodeGatewayError, err, "failed to revoke invitation via WorkOS").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) ResendInvite(ctx context.Context, payload *gen.ResendInvitePayload) (*gen.ResendInviteResult, error) {
	_, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	workos := s.sessions.WorkOS()
	inv, err := workos.ResendInvitation(ctx, payload.InviteID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "failed to resend invitation via WorkOS").Log(ctx, s.logger)
	}

	return &gen.ResendInviteResult{
		Invite: invitationToGenInvite(inv, ""),
	}, nil
}

func (s *Service) GetInviteInfo(ctx context.Context, payload *gen.GetInviteInfoPayload) (*gen.InviteInfoResult, error) {
	workos := s.sessions.WorkOS()
	inv, err := workos.FindInvitationByToken(ctx, payload.Token)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "invitation not found").Log(ctx, s.logger)
	}

	inviterName := ""
	if inv.InviterUserID != "" {
		if inviter, err := workos.GetUser(ctx, inv.InviterUserID); err == nil {
			inviterName = inviter.FirstName
			if inviter.LastName != "" {
				if inviterName != "" {
					inviterName += " "
				}
				inviterName += inviter.LastName
			}
			if inviterName == "" {
				inviterName = inviter.Email
			}
		}
	}

	orgName := ""
	if inv.OrganizationID != "" {
		org, err := s.orgRepo.GetOrganizationMetadataByWorkosID(ctx, pgtype.Text{String: inv.OrganizationID, Valid: true})
		if err == nil {
			orgName = org.Name
		}
	}

	return &gen.InviteInfoResult{
		InviterName:      inviterName,
		OrganizationName: orgName,
		Email:            inv.Email,
		Status:           string(inv.State),
	}, nil
}

func (s *Service) RemoveMember(ctx context.Context, payload *gen.RemoveMemberPayload) error {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return err
	}

	if payload.UserID == authCtx.UserID {
		return oops.E(oops.CodeBadRequest, nil, "cannot remove yourself from the organization")
	}

	workosOrgID, err := s.getOrgWorkOSID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return err
	}

	memberWorkOSID, err := s.getUserWorkOSID(ctx, payload.UserID)
	if err != nil {
		return err
	}

	workos := s.sessions.WorkOS()
	membership, err := workos.GetOrgMembership(ctx, memberWorkOSID, workosOrgID)
	if err != nil {
		return oops.E(oops.CodeGatewayError, err, "failed to find membership in WorkOS").Log(ctx, s.logger)
	}
	if membership == nil {
		return oops.E(oops.CodeNotFound, nil, "user is not a member of this organization")
	}

	if err := workos.DeleteOrganizationMembership(ctx, membership.ID); err != nil {
		return oops.E(oops.CodeGatewayError, err, "failed to remove member via WorkOS").Log(ctx, s.logger)
	}

	// Also soft-delete the local relationship
	if err := s.orgRepo.DeleteOrganizationUserRelationship(ctx, orgRepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         payload.UserID,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to delete local org-user relationship after WorkOS removal",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
	}

	return nil
}
