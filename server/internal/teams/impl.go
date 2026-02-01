package teams

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	genAuth "github.com/speakeasy-api/gram/server/gen/auth"
	srv "github.com/speakeasy-api/gram/server/gen/http/teams/server"
	gen "github.com/speakeasy-api/gram/server/gen/teams"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/teams/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
)

type Config struct {
	// SiteURL is the base URL of the frontend (e.g. "https://app.gram.sh").
	SiteURL string
}

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	repo     *repo.Queries
	sessions *sessions.Manager
	auth     *auth.Auth
	loops    *loops.Client
	cfg      Config
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, loopsClient *loops.Client, cfg Config) *Service {
	logger = logger.With(attr.SlogComponent("teams"))

	return &Service{
		tracer:   otel.Tracer("github.com/speakeasy-api/gram/server/internal/teams"),
		logger:   logger,
		db:       db,
		repo:     repo.New(db),
		sessions: sessions,
		auth:     auth.New(logger, db, sessions),
		loops:    loopsClient,
		cfg:      cfg,
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

// sendInviteEmail sends a team invite email via Loops. Failures are logged but
// not propagated — the invite is still created in the database regardless.
func (s *Service) sendInviteEmail(ctx context.Context, email, token, teammateFirstName, teammateEmail, workspaceName string) {
	transactionalID := loops.TransactionalID(loops.TemplateTeamInvite)
	if transactionalID == "" {
		s.logger.DebugContext(ctx, "skipping invite email: no transactional ID registered")
		return
	}

	inviteURL := fmt.Sprintf("%s/invite?token=%s", strings.TrimRight(s.cfg.SiteURL, "/"), token)

	if err := s.loops.SendTransactionalEmail(ctx, loops.SendTransactionalEmailInput{
		TransactionalID: transactionalID,
		Email:           email,
		DataVariables: map[string]string{
			"invite_link":    inviteURL,
			"teammate_fn":    teammateFirstName,
			"teammate_email": teammateEmail,
			"workspace_name": workspaceName,
		},
		AddToAudience: true,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to send invite email via Loops",
			attr.SlogError(err),
			attr.SlogTeamInviteEmail(email),
		)
	}
}

// firstName extracts the first name from a display name.
func firstName(displayName string) string {
	if name, _, ok := strings.Cut(displayName, " "); ok {
		return name
	}
	return displayName
}

// orgName looks up the organization name from the user's cached org list.
func orgName(userInfo *sessions.CachedUserInfo, organizationID string) string {
	for _, org := range userInfo.Organizations {
		if org.ID == organizationID {
			return org.Name
		}
	}
	return ""
}

// generateToken creates a secure random token for invites
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// hasOrgAccess checks if the current user has access to the organization
func (s *Service) hasOrgAccess(ctx context.Context, organizationID string) (*sessions.CachedUserInfo, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get user info")
	}

	if orgIdx := slices.IndexFunc(userInfo.Organizations, func(org genAuth.OrganizationEntry) bool {
		return org.ID == organizationID
	}); orgIdx == -1 {
		return nil, oops.C(oops.CodeForbidden)
	}

	return userInfo, nil
}

func (s *Service) ListMembers(ctx context.Context, payload *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	_, err := s.hasOrgAccess(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	members, err := s.repo.ListOrganizationMembers(ctx, payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list members").Log(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID))
	}

	result := make([]*gen.TeamMember, 0, len(members))
	for _, m := range members {
		var photoURL *string
		if m.PhotoUrl.Valid {
			photoURL = &m.PhotoUrl.String
		}
		result = append(result, &gen.TeamMember{
			ID:          m.ID,
			Email:       m.Email,
			DisplayName: m.DisplayName,
			PhotoURL:    photoURL,
			JoinedAt:    m.JoinedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListMembersResult{
		Members: result,
	}, nil
}

func (s *Service) InviteMember(ctx context.Context, payload *gen.InviteMemberPayload) (*gen.InviteMemberResult, error) {
	userInfo, err := s.hasOrgAccess(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	// Check if there's already a pending invite for this email
	existingInvite, err := s.repo.GetPendingInviteByEmail(ctx, repo.GetPendingInviteByEmailParams{
		OrganizationID: payload.OrganizationID,
		Email:          payload.Email,
	})
	if err == nil && existingInvite.ID != uuid.Nil {
		return nil, oops.E(oops.CodeConflict, nil, "an invite is already pending for this email")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to check existing invites").Log(ctx, s.logger)
	}

	// Check if user is already a member
	members, err := s.repo.ListOrganizationMembers(ctx, payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to check existing members").Log(ctx, s.logger)
	}
	for _, m := range members {
		if m.Email == payload.Email {
			return nil, oops.E(oops.CodeConflict, nil, "user is already a member of this organization")
		}
	}

	token, err := generateToken()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate invite token").Log(ctx, s.logger)
	}

	// Invite expires in 7 days
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	invite, err := s.repo.CreateTeamInvite(ctx, repo.CreateTeamInviteParams{
		OrganizationID:  payload.OrganizationID,
		Email:           payload.Email,
		InvitedByUserID: userInfo.UserID,
		Token:           token,
		ExpiresAt:       pgtype.Timestamptz{Time: expiresAt, Valid: true, InfinityModifier: 0},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create invite").Log(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID))
	}

	invitedByName := ""
	if userInfo.DisplayName != nil {
		invitedByName = *userInfo.DisplayName
	}

	s.sendInviteEmail(ctx, payload.Email, token, firstName(invitedByName), userInfo.Email, orgName(userInfo, payload.OrganizationID))

	s.logger.InfoContext(ctx, "team invite created",
		attr.SlogOrganizationID(payload.OrganizationID),
		attr.SlogTeamInviteEmail(payload.Email),
		attr.SlogTeamInviteID(invite.ID.String()),
	)

	return &gen.InviteMemberResult{
		Invite: &gen.TeamInvite{
			ID:        invite.ID.String(),
			Email:     invite.Email,
			Status:    invite.Status,
			InvitedBy: invitedByName,
			CreatedAt: invite.CreatedAt.Time.Format(time.RFC3339),
			ExpiresAt: invite.ExpiresAt.Time.Format(time.RFC3339),
		},
	}, nil
}

func (s *Service) ListInvites(ctx context.Context, payload *gen.ListInvitesPayload) (*gen.ListInvitesResult, error) {
	_, err := s.hasOrgAccess(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	invites, err := s.repo.ListPendingTeamInvites(ctx, payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list invites").Log(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID))
	}

	result := make([]*gen.TeamInvite, 0, len(invites))
	for _, inv := range invites {
		result = append(result, &gen.TeamInvite{
			ID:        inv.ID.String(),
			Email:     inv.Email,
			Status:    inv.Status,
			InvitedBy: inv.InvitedByName,
			CreatedAt: inv.CreatedAt.Time.Format(time.RFC3339),
			ExpiresAt: inv.ExpiresAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListInvitesResult{
		Invites: result,
	}, nil
}

func (s *Service) CancelInvite(ctx context.Context, payload *gen.CancelInvitePayload) error {
	inviteID, err := uuid.Parse(payload.InviteID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid invite ID")
	}

	invite, err := s.repo.GetTeamInviteByID(ctx, inviteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "failed to get invite").Log(ctx, s.logger)
	}

	_, err = s.hasOrgAccess(ctx, invite.OrganizationID)
	if err != nil {
		return err
	}

	if err := s.repo.CancelTeamInvite(ctx, inviteID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to cancel invite").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) ResendInvite(ctx context.Context, payload *gen.ResendInvitePayload) (*gen.ResendInviteResult, error) {
	inviteID, err := uuid.Parse(payload.InviteID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid invite ID")
	}

	invite, err := s.repo.GetTeamInviteByID(ctx, inviteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get invite").Log(ctx, s.logger)
	}

	userInfo, err := s.hasOrgAccess(ctx, invite.OrganizationID)
	if err != nil {
		return nil, err
	}

	if invite.Status != "pending" {
		return nil, oops.E(oops.CodeInvalid, nil, "can only resend pending invites")
	}

	// Extend expiry by 7 days from now
	newExpiry := time.Now().Add(7 * 24 * time.Hour)
	updatedInvite, err := s.repo.UpdateTeamInviteExpiry(ctx, repo.UpdateTeamInviteExpiryParams{
		ID:        inviteID,
		ExpiresAt: pgtype.Timestamptz{Time: newExpiry, Valid: true, InfinityModifier: 0},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update invite expiry").Log(ctx, s.logger)
	}

	invitedByName := ""
	if userInfo.DisplayName != nil {
		invitedByName = *userInfo.DisplayName
	}

	s.sendInviteEmail(ctx, invite.Email, invite.Token, firstName(invitedByName), userInfo.Email, orgName(userInfo, invite.OrganizationID))

	s.logger.InfoContext(ctx, "team invite resent",
		attr.SlogOrganizationID(invite.OrganizationID),
		attr.SlogTeamInviteEmail(invite.Email),
		attr.SlogTeamInviteID(invite.ID.String()),
	)

	return &gen.ResendInviteResult{
		Invite: &gen.TeamInvite{
			ID:        updatedInvite.ID.String(),
			Email:     updatedInvite.Email,
			Status:    updatedInvite.Status,
			InvitedBy: invitedByName,
			CreatedAt: updatedInvite.CreatedAt.Time.Format(time.RFC3339),
			ExpiresAt: updatedInvite.ExpiresAt.Time.Format(time.RFC3339),
		},
	}, nil
}

func (s *Service) AcceptInvite(ctx context.Context, payload *gen.AcceptInvitePayload) (*gen.AcceptInviteResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get user info").Log(ctx, s.logger)
	}

	invite, err := s.repo.GetTeamInviteByToken(ctx, payload.Token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "invite not found or already used")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to look up invite").Log(ctx, s.logger)
	}

	if invite.Status != "pending" {
		return nil, oops.E(oops.CodeInvalid, nil, "invite is no longer pending")
	}

	if invite.ExpiresAt.Valid && time.Now().After(invite.ExpiresAt.Time) {
		return nil, oops.E(oops.CodeInvalid, nil, "invite has expired")
	}

	if !strings.EqualFold(invite.Email, userInfo.Email) {
		return nil, oops.E(oops.CodeForbidden, nil, "invite was sent to a different email address")
	}

	if err := s.repo.AddOrganizationMember(ctx, repo.AddOrganizationMemberParams{
		OrganizationID: invite.OrganizationID,
		UserID:         authCtx.UserID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to add member to organization").Log(ctx, s.logger)
	}

	if err := s.repo.AcceptTeamInvite(ctx, invite.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to accept invite").Log(ctx, s.logger)
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, authCtx.UserID); err != nil {
		s.logger.ErrorContext(ctx, "failed to invalidate user info cache",
			attr.SlogError(err),
			attr.SlogUserID(authCtx.UserID),
		)
	}

	orgSlug, err := s.repo.GetOrganizationSlug(ctx, invite.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get organization slug").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "team invite accepted",
		attr.SlogOrganizationID(invite.OrganizationID),
		attr.SlogUserID(authCtx.UserID),
		attr.SlogTeamInviteID(invite.ID.String()),
	)

	return &gen.AcceptInviteResult{
		OrganizationSlug: orgSlug,
	}, nil
}

func (s *Service) RemoveMember(ctx context.Context, payload *gen.RemoveMemberPayload) error {
	userInfo, err := s.hasOrgAccess(ctx, payload.OrganizationID)
	if err != nil {
		return err
	}

	// Prevent removing yourself
	if payload.UserID == userInfo.UserID {
		return oops.E(oops.CodeInvalid, nil, "cannot remove yourself from the organization")
	}

	// Check that the target user is actually a member
	members, err := s.repo.ListOrganizationMembers(ctx, payload.OrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to check members").Log(ctx, s.logger)
	}

	found := false
	for _, m := range members {
		if m.ID == payload.UserID {
			found = true
			break
		}
	}
	if !found {
		return oops.C(oops.CodeNotFound)
	}

	if err := s.repo.RemoveOrganizationMember(ctx, repo.RemoveOrganizationMemberParams{
		OrganizationID: payload.OrganizationID,
		UserID:         payload.UserID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to remove member").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "team member removed",
		attr.SlogOrganizationID(payload.OrganizationID),
		attr.SlogUserID(payload.UserID),
	)

	return nil
}
