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
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
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
		auth:     auth.New(logger, db, sessions),
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

// requireWorkOS returns the WorkOS client or a proper service error if not configured.
func (s *Service) requireWorkOS() (*workos.WorkOS, error) {
	w := s.sessions.WorkOS()
	if w == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "WorkOS integration is not configured")
	}
	return w, nil
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

// validateOrgAccess checks that the payload org ID matches the session org.
func (s *Service) validateOrgAccess(payloadOrgID, activeOrgID string) error {
	if payloadOrgID != activeOrgID {
		return oops.E(oops.CodeForbidden, nil, "organization_id does not match active organization")
	}
	return nil
}

// verifyInviteBelongsToOrg fetches an invitation and verifies it belongs to the caller's org.
func (s *Service) verifyInviteBelongsToOrg(ctx context.Context, w *workos.WorkOS, inviteID, workosOrgID string) (usermanagement.Invitation, error) {
	inv, err := w.GetInvitation(ctx, inviteID)
	if err != nil {
		return usermanagement.Invitation{}, oops.E(oops.CodeNotFound, err, "invitation not found").Log(ctx, s.logger)
	}

	if inv.OrganizationID != workosOrgID {
		return usermanagement.Invitation{}, oops.E(oops.CodeNotFound, nil, "invitation not found")
	}

	return inv, nil
}

func workosDisplayName(firstName, lastName, email string) string {
	displayName := firstName
	if lastName != "" {
		if displayName != "" {
			displayName += " "
		}
		displayName += lastName
	}
	if displayName == "" {
		displayName = email
	}
	return displayName
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

func (s *Service) resolveInviterName(ctx context.Context, w *workos.WorkOS, inviterUserID string) string {
	if inviterUserID == "" {
		return ""
	}
	inviter, err := w.GetUser(ctx, inviterUserID)
	if err != nil {
		return ""
	}
	return workosDisplayName(inviter.FirstName, inviter.LastName, inviter.Email)
}

func (s *Service) ListMembers(ctx context.Context, payload *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.validateOrgAccess(payload.OrganizationID, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	wos, err := s.requireWorkOS()
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.getOrgWorkOSID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	users, err := wos.ListUsersInOrg(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "failed to list organization members from WorkOS").Log(ctx, s.logger)
	}

	members := make([]*gen.TeamMember, 0, len(users))
	for _, u := range users {
		// Resolve WorkOS user to Gram user for consistent ID space
		gramUser, err := s.userRepo.GetUserByWorkosID(ctx, pgtype.Text{String: u.ID, Valid: true})
		if err != nil {
			// User exists in WorkOS but not synced to Gram yet — use WorkOS data
			s.logger.WarnContext(ctx, "WorkOS user not found in Gram DB, skipping",
				attr.SlogError(err),
			)
			continue
		}

		var photoURL *string
		if gramUser.PhotoUrl.Valid && gramUser.PhotoUrl.String != "" {
			photoURL = &gramUser.PhotoUrl.String
		}
		member := &gen.TeamMember{
			ID:          gramUser.ID,
			Email:       gramUser.Email,
			DisplayName: gramUser.DisplayName,
			PhotoURL:    photoURL,
			JoinedAt:    u.CreatedAt,
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

	if err := s.validateOrgAccess(payload.OrganizationID, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	wos, err := s.requireWorkOS()
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

	inv, err := wos.SendInvitation(ctx, usermanagement.SendInvitationOpts{
		Email:          payload.Email,
		OrganizationID: workosOrgID,
		InviterUserID:  inviterWorkOSID,
		ExpiresInDays:  7,
		RoleSlug:       "",
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

	if err := s.validateOrgAccess(payload.OrganizationID, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	wos, err := s.requireWorkOS()
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.getOrgWorkOSID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	invitations, err := wos.ListInvitations(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "failed to list invitations from WorkOS").Log(ctx, s.logger)
	}

	invites := make([]*gen.TeamInvite, 0, len(invitations))
	for _, inv := range invitations {
		inviterName := s.resolveInviterName(ctx, wos, inv.InviterUserID)
		invites = append(invites, invitationToGenInvite(inv, inviterName))
	}

	return &gen.ListInvitesResult{Invites: invites}, nil
}

func (s *Service) CancelInvite(ctx context.Context, payload *gen.CancelInvitePayload) error {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return err
	}

	wos, err := s.requireWorkOS()
	if err != nil {
		return err
	}

	workosOrgID, err := s.getOrgWorkOSID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return err
	}

	if _, err := s.verifyInviteBelongsToOrg(ctx, wos, payload.InviteID, workosOrgID); err != nil {
		return err
	}

	if _, err := wos.RevokeInvitation(ctx, payload.InviteID); err != nil {
		return oops.E(oops.CodeGatewayError, err, "failed to revoke invitation via WorkOS").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) ResendInvite(ctx context.Context, payload *gen.ResendInvitePayload) (*gen.ResendInviteResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	wos, err := s.requireWorkOS()
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.getOrgWorkOSID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	if _, err := s.verifyInviteBelongsToOrg(ctx, wos, payload.InviteID, workosOrgID); err != nil {
		return nil, err
	}

	inv, err := wos.ResendInvitation(ctx, payload.InviteID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "failed to resend invitation via WorkOS").Log(ctx, s.logger)
	}

	inviterName := s.resolveInviterName(ctx, wos, inv.InviterUserID)

	return &gen.ResendInviteResult{
		Invite: invitationToGenInvite(inv, inviterName),
	}, nil
}

func (s *Service) GetInviteInfo(ctx context.Context, payload *gen.GetInviteInfoPayload) (*gen.InviteInfoResult, error) {
	wos, err := s.requireWorkOS()
	if err != nil {
		return nil, err
	}

	inv, err := wos.FindInvitationByToken(ctx, payload.Token)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "invitation not found").Log(ctx, s.logger)
	}

	inviterName := s.resolveInviterName(ctx, wos, inv.InviterUserID)

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

	if err := s.validateOrgAccess(payload.OrganizationID, authCtx.ActiveOrganizationID); err != nil {
		return err
	}

	if payload.UserID == authCtx.UserID {
		return oops.E(oops.CodeBadRequest, nil, "cannot remove yourself from the organization")
	}

	wos, err := s.requireWorkOS()
	if err != nil {
		return err
	}

	workosOrgID, err := s.getOrgWorkOSID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return err
	}

	memberWorkOSID, err := s.getUserWorkOSID(ctx, payload.UserID)
	if err != nil {
		return err
	}

	membership, err := wos.GetOrgMembership(ctx, memberWorkOSID, workosOrgID)
	if err != nil {
		return oops.E(oops.CodeGatewayError, err, "failed to find membership in WorkOS").Log(ctx, s.logger)
	}
	if membership == nil {
		return oops.E(oops.CodeNotFound, nil, "user is not a member of this organization")
	}

	if err := wos.DeleteOrganizationMembership(ctx, membership.ID); err != nil {
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
