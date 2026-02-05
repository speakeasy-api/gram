package teams

import (
	"context"
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
	// DevMode skips the invite email match check for local development.
	DevMode bool
	// InviteExpiryDuration controls how long team invites remain valid.
	// Defaults to 7 days if zero.
	InviteExpiryDuration time.Duration
}

// inviteExpiry returns the configured invite expiry duration, falling back to
// 7 days when the value is zero (unconfigured).
func (c Config) inviteExpiry() time.Duration {
	if c.InviteExpiryDuration <= 0 {
		return 7 * 24 * time.Hour
	}
	return c.InviteExpiryDuration
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

// sendInviteEmail sends a team invite email via Loops.
func (s *Service) sendInviteEmail(ctx context.Context, email, token, teammateFirstName, teammateEmail, workspaceName string) error {
	transactionalID := loops.TransactionalID(loops.TemplateTeamInvite)
	if transactionalID == "" {
		s.logger.DebugContext(ctx, "skipping invite email: no transactional ID registered")
		return nil
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
		return fmt.Errorf("sending invite email via Loops: %w", err)
	}

	return nil
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

// maskEmail masks an email address for privacy, showing only the first character
// of the local part and domain. For example, "john@example.com" becomes "j***@e***.com".
func maskEmail(email string) string {
	localPart, rest, ok := strings.Cut(email, "@")
	if !ok || len(localPart) == 0 {
		return "***"
	}

	domain, tld, hasTLD := strings.Cut(rest, ".")
	if !hasTLD || len(domain) == 0 {
		return string(localPart[0]) + "***@***"
	}

	return string(localPart[0]) + "***@" + string(domain[0]) + "***." + tld
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

	// Normalize email to lowercase to ensure case-insensitive uniqueness at the DB level.
	// Note: RFC 5321 allows case-sensitive local parts, but this is extremely rare in practice.
	email := strings.ToLower(strings.TrimSpace(payload.Email))

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to start transaction").Log(ctx, s.logger)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback is a no-op after commit

	txRepo := s.repo.WithTx(tx)

	// Check if there's already a pending invite for this email
	existingInvite, err := txRepo.GetPendingInviteByEmail(ctx, repo.GetPendingInviteByEmailParams{
		OrganizationID: payload.OrganizationID,
		Email:          email,
	})
	if err == nil && existingInvite.ID != uuid.Nil {
		return nil, oops.E(oops.CodeConflict, nil, "an invite is already pending for this email")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to check existing invites").Log(ctx, s.logger)
	}

	// Check if user is already a member
	members, err := txRepo.ListOrganizationMembers(ctx, payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to check existing members").Log(ctx, s.logger)
	}
	for _, m := range members {
		if strings.EqualFold(m.Email, email) {
			return nil, oops.E(oops.CodeConflict, nil, "user is already a member of this organization")
		}
	}

	// Get the inviter's workspace slug for this org to include in the token.
	// This allows invite acceptance to work without depending on cache state.
	var workspaceSlug string
	for _, org := range userInfo.Organizations {
		if org.ID == payload.OrganizationID && len(org.UserWorkspaceSlugs) > 0 {
			workspaceSlug = org.UserWorkspaceSlugs[0]
			break
		}
	}
	if workspaceSlug == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "no workspace access to invite members").Log(ctx, s.logger,
			attr.SlogOrganizationID(payload.OrganizationID),
		)
	}

	token, err := auth.GenerateInviteToken(workspaceSlug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate invite token").Log(ctx, s.logger)
	}

	expiresAt := time.Now().Add(s.cfg.inviteExpiry())

	// Atomically insert the invite only if the rate limit has not been reached.
	// Returns pgx.ErrNoRows when the org already has >= 50 invites in the last 24h.
	invite, err := txRepo.CreateTeamInvite(ctx, repo.CreateTeamInviteParams{
		OrganizationID:  payload.OrganizationID,
		Email:           email,
		InvitedByUserID: pgtype.Text{String: userInfo.UserID, Valid: true},
		Token:           token,
		ExpiresAt:       pgtype.Timestamptz{Time: expiresAt, Valid: true, InfinityModifier: 0},
		MaxRecent:       50,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeInvalid, nil, "too many invites sent in the last 24 hours, please try again later")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create invite").Log(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID))
	}

	invitedByName := ""
	if userInfo.DisplayName != nil {
		invitedByName = *userInfo.DisplayName
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to commit invite").Log(ctx, s.logger)
	}

	if err := s.sendInviteEmail(ctx, email, token, firstName(invitedByName), userInfo.Email, orgName(userInfo, payload.OrganizationID)); err != nil {
		// Invite is committed but email failed — cancel it so it can be retried.
		if cancelErr := s.repo.CancelTeamInvite(ctx, invite.ID); cancelErr != nil {
			s.logger.ErrorContext(ctx, "failed to cancel invite after email failure",
				attr.SlogTeamInviteID(invite.ID.String()),
				attr.SlogError(cancelErr),
			)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to send invite email").Log(ctx, s.logger,
			attr.SlogTeamInviteID(invite.ID.String()),
		)
	}

	s.logger.InfoContext(ctx, "team invite created",
		attr.SlogOrganizationID(payload.OrganizationID),
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

	if invite.Status != "pending" {
		return oops.E(oops.CodeInvalid, nil, "can only cancel pending invites")
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

	// Prevent rapid resending: require at least 5 minutes between resends.
	if invite.UpdatedAt.Valid && time.Since(invite.UpdatedAt.Time) < 5*time.Minute {
		return nil, oops.E(oops.CodeInvalid, nil, "please wait at least 5 minutes before resending an invite")
	}

	// Get the resender's workspace slug for this org to include in the new token.
	var workspaceSlug string
	for _, org := range userInfo.Organizations {
		if org.ID == invite.OrganizationID && len(org.UserWorkspaceSlugs) > 0 {
			workspaceSlug = org.UserWorkspaceSlugs[0]
			break
		}
	}
	if workspaceSlug == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "no workspace access to resend invite").Log(ctx, s.logger,
			attr.SlogOrganizationID(invite.OrganizationID),
		)
	}

	// Rotate the token on resend so the old link is invalidated.
	newToken, err := auth.GenerateInviteToken(workspaceSlug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate new invite token").Log(ctx, s.logger)
	}

	newExpiry := time.Now().Add(s.cfg.inviteExpiry())
	updatedInvite, err := s.repo.UpdateTeamInviteExpiryAndToken(ctx, repo.UpdateTeamInviteExpiryAndTokenParams{
		ID:        inviteID,
		ExpiresAt: pgtype.Timestamptz{Time: newExpiry, Valid: true, InfinityModifier: 0},
		Token:     newToken,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update invite").Log(ctx, s.logger)
	}

	invitedByName := ""
	if userInfo.DisplayName != nil {
		invitedByName = *userInfo.DisplayName
	}

	if err := s.sendInviteEmail(ctx, invite.Email, newToken, firstName(invitedByName), userInfo.Email, orgName(userInfo, invite.OrganizationID)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to send invite email").Log(ctx, s.logger, attr.SlogTeamInviteID(invite.ID.String()))
	}

	s.logger.InfoContext(ctx, "team invite resent",
		attr.SlogOrganizationID(invite.OrganizationID),
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

func (s *Service) GetInviteInfo(ctx context.Context, payload *gen.GetInviteInfoPayload) (*gen.InviteInfoResult, error) {
	info, err := s.repo.GetInviteInfoByToken(ctx, payload.Token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "invite not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to look up invite").Log(ctx, s.logger)
	}

	status := info.Status
	if status == "pending" && info.ExpiresAt.Valid && time.Now().After(info.ExpiresAt.Time) {
		status = "expired"
	}

	// Mask the invite email unless the caller's email matches, to avoid
	// leaking the full address to unintended recipients.
	email := maskEmail(info.Email)
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.SessionID != nil {
		if userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID); err == nil {
			if strings.EqualFold(userInfo.Email, info.Email) {
				email = info.Email
			}
		}
	}

	return &gen.InviteInfoResult{
		InviterName:      info.InviterName,
		OrganizationName: info.OrganizationName,
		Email:            email,
		Status:           status,
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

	if !slices.ContainsFunc(members, func(m repo.ListOrganizationMembersRow) bool {
		return m.ID == payload.UserID
	}) {
		return oops.C(oops.CodeNotFound)
	}

	// Get the caller's workspace slugs for this org to remove the user from.
	// Speakeasy doesn't have an API to remove a user from an org directly—only
	// from workspaces. Removing from any workspace removes org access.
	var workspaceSlugs []string
	for _, org := range userInfo.Organizations {
		if org.ID == payload.OrganizationID {
			workspaceSlugs = org.UserWorkspaceSlugs
			break
		}
	}
	if len(workspaceSlugs) == 0 {
		return oops.E(oops.CodeInvalid, nil, "no workspace access to remove member from").Log(ctx, s.logger,
			attr.SlogOrganizationID(payload.OrganizationID),
		)
	}

	// Remove user from org workspaces via Speakeasy API.
	if err := s.sessions.RemoveUserFromOrg(ctx, workspaceSlugs, payload.UserID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to remove member from org via speakeasy").Log(ctx, s.logger)
	}

	// Soft-delete the local relationship so the member list updates immediately.
	// The Speakeasy API is the source of truth; this is a local cache optimisation.
	if err := s.repo.SoftDeleteOrganizationMember(ctx, repo.SoftDeleteOrganizationMemberParams{
		OrganizationID: payload.OrganizationID,
		UserID:         payload.UserID,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to soft-delete local org membership",
			attr.SlogError(err),
			attr.SlogUserID(payload.UserID),
			attr.SlogOrganizationID(payload.OrganizationID),
		)
	}

	// Invalidate the removed user's cache so their org list is refreshed.
	if err := s.sessions.InvalidateUserInfoCache(ctx, payload.UserID); err != nil {
		s.logger.ErrorContext(ctx, "failed to invalidate removed user's cache",
			attr.SlogError(err),
			attr.SlogUserID(payload.UserID),
		)
	}

	s.logger.InfoContext(ctx, "team member removed",
		attr.SlogOrganizationID(payload.OrganizationID),
		attr.SlogUserID(payload.UserID),
	)

	return nil
}
