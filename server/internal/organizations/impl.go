package organizations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/organizations/server"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	defaultInviteExpiryDays = 7
)

// OrganizationProvider is the WorkOS surface the organizations service uses.
// *workos.Client implements it.
type OrganizationProvider interface {
	DeleteOrganizationMembership(ctx context.Context, workosMembershipID string) error
	CreatePasswordlessSession(ctx context.Context, opts workos.CreatePasswordlessSessionOpts) (*workos.PasswordlessSession, error)
	AuthenticateWithInviteLink(ctx context.Context, code string) (*workos.InviteLinkProfile, error)
}

var _ OrganizationProvider = (*workos.Client)(nil)

type orgFeatureChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

type Service struct {
	logger      *slog.Logger
	tracer      trace.Tracer
	db          *pgxpool.Pool
	auth        *auth.Auth
	authz       *authz.Engine
	orgs        OrganizationProvider
	features    orgFeatureChecker
	email       *email.Service
	siteURL     string // dashboard URL; used for invite callback RedirectURI and post-callback redirect
	registryURL string // registry (speakeasy server) URL; used to redirect invitees through OIDC login after accepting
}

var _ gen.Service = (*Service)(nil)

var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, orgs OrganizationProvider, features orgFeatureChecker, authzEngine *authz.Engine, emailService *email.Service, siteURL string, registryURL string) *Service {
	logger = logger.With(attr.SlogComponent("organizations"))

	return &Service{
		logger:      logger,
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/organizations"),
		db:          db,
		auth:        auth.New(logger, db, sessions, authzEngine),
		authz:       authzEngine,
		orgs:        orgs,
		features:    features,
		email:       emailService,
		siteURL:     siteURL,
		registryURL: registryURL,
	}
}

const inviteCallbackPath = "/v1/auth/invite/callback"

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)

	// Raw HTTP handler for the passwordless magic-link invite callback.
	// WorkOS redirects here after the invitee clicks the magic link.
	mux.Handle("GET", inviteCallbackPath, service.handleInviteCallback)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) SendInvite(ctx context.Context, payload *gen.SendInvitePayload) (*gen.OrganizationInvitation, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
	)
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	rawToken, tokenHash, err := generateInviteToken()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate invite token").Log(ctx, logger)
	}

	roleSlug := pgtype.Text{}
	if payload.RoleID != nil && *payload.RoleID != "" {
		roleSlug = conv.ToPGText(*payload.RoleID)
	}

	row, err := orgrepo.New(s.db).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: ac.ActiveOrganizationID,
		Email:          strings.ToLower(strings.TrimSpace(payload.Email)),
		TokenHash:      tokenHash,
		InviterUserID:  conv.ToPGText(ac.UserID),
		RoleSlug:       roleSlug,
		ExpiresInDays:  int32(defaultInviteExpiryDays),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create invitation").Log(ctx, logger)
	}

	// Create a WorkOS passwordless magic-link session for the invitee.
	// RedirectURI points to our invite callback which exchanges the code,
	// accepts the invite, and redirects to the dashboard.
	pwlSess, pwlErr := s.orgs.CreatePasswordlessSession(ctx, workos.CreatePasswordlessSessionOpts{
		Email:       row.Email,
		RedirectURI: s.siteURL + inviteCallbackPath,
		ExpiresIn:   defaultInviteExpiryDays * 24 * 60 * 60,
		State:       fmt.Sprintf("invite_token=%s", rawToken),
	})
	if pwlErr != nil {
		logger.WarnContext(ctx, "failed to create passwordless session; invite saved but email not sent",
			attr.SlogError(pwlErr),
		)
	} else if s.email != nil {
		// Look up inviter display name + email and org name for the email template.
		inviterName, inviterEmail := ac.UserID, ""
		if u, err := userrepo.New(s.db).GetUser(ctx, ac.UserID); err == nil {
			inviterName = u.DisplayName
			inviterEmail = u.Email
		}
		orgName := ac.ActiveOrganizationID
		if org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID); err == nil {
			orgName = org.Name
		}

		if err := s.email.Send(ctx, row.Email, email.TeamInvite{
			InviteLink:       pwlSess.Link,
			OrganizationName: orgName,
			InviterName:      inviterName,
			InviterEmail:     inviterEmail,
		}); err != nil {
			logger.WarnContext(ctx, "failed to send invite email",
				attr.SlogError(err),
			)
		}
	}

	return dbInvitationToGen(&row, &ac.UserID), nil
}

func (s *Service) RevokeInvite(ctx context.Context, payload *gen.RevokeInvitePayload) error {
	ac, err := s.authContext(ctx)
	if err != nil {
		return err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
		attr.SlogOrganizationInviteID(payload.InvitationID),
	)
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	inviteID, err := uuid.Parse(payload.InvitationID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid invitation id").Log(ctx, logger)
	}

	invite, err := orgrepo.New(s.db).GetInvitationByID(ctx, inviteID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.C(oops.CodeNotFound).Log(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get invitation").Log(ctx, logger)
	}
	if invite.OrganizationID != ac.ActiveOrganizationID {
		return oops.E(oops.CodeForbidden, nil, "invitation does not belong to this organization").Log(ctx, logger)
	}

	if err := orgrepo.New(s.db).RevokeInvitation(ctx, inviteID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "revoke invitation").Log(ctx, logger)
	}

	return nil
}

func (s *Service) ListInvites(ctx context.Context, _ *gen.ListInvitesPayload) (*gen.ListInvitesResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
	)

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	rows, err := orgrepo.New(s.db).ListPendingInvitations(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list invitations").Log(ctx, logger)
	}

	out := make([]*gen.OrganizationInvitation, 0, len(rows))
	for i := range rows {
		inviterID := conv.FromPGText[string](rows[i].InviterUserID)
		out = append(out, dbInvitationToGen(&rows[i], inviterID))
	}
	return &gen.ListInvitesResult{Invitations: out}, nil
}

// ListUsers returns Gram organization members from organization_user_relationships.
// That table is the in-app source of truth for roster and RemoveUser; WorkOS owns
// invite/membership lifecycle but the dashboard "team" list should match what Gram authorizes.
func (s *Service) ListUsers(ctx context.Context, _ *gen.ListUsersPayload) (*gen.ListUsersResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	rows, err := orgrepo.New(s.db).ListOrganizationUsers(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization users").Log(ctx, s.logger)
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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
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
		UserID:         conv.ToPGText(payload.UserID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "user is not a member of this organization").Log(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get organization user relationship").Log(ctx, logger)
	}

	if err := qtx.DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: ac.ActiveOrganizationID,
		UserID:         conv.ToPGText(payload.UserID),
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

// generateInviteToken returns a raw token and its SHA-256 hex hash.
func generateInviteToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate random bytes: %w", err)
	}
	raw = hex.EncodeToString(b)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// dbInvitationToGen maps a DB invitation row into the public API shape.
func dbInvitationToGen(row *orgrepo.OrganizationInvitation, inviterUserID *string) *gen.OrganizationInvitation {
	if row == nil {
		return nil
	}
	expiresAt := ""
	if row.ExpiresAt.Valid {
		expiresAt = row.ExpiresAt.Time.UTC().Format(time.RFC3339)
	}
	acceptedAt := ""
	if row.AcceptedAt.Valid {
		acceptedAt = row.AcceptedAt.Time.UTC().Format(time.RFC3339)
	}
	revokedAt := ""
	if row.RevokedAt.Valid {
		revokedAt = row.RevokedAt.Time.UTC().Format(time.RFC3339)
	}
	createdAt := ""
	if row.CreatedAt.Valid {
		createdAt = row.CreatedAt.Time.UTC().Format(time.RFC3339)
	}
	updatedAt := ""
	if row.UpdatedAt.Valid {
		updatedAt = row.UpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	return &gen.OrganizationInvitation{
		ID:            row.ID.String(),
		Email:         row.Email,
		State:         row.State,
		AcceptedAt:    conv.PtrEmpty(acceptedAt),
		RevokedAt:     conv.PtrEmpty(revokedAt),
		InviterUserID: inviterUserID,
		ExpiresAt:     conv.PtrEmpty(expiresAt),
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}
}

// handleInviteCallback processes the WorkOS passwordless magic-link redirect.
// Flow: invitee clicks magic link → WorkOS authenticates → redirects here with
// code + state. We exchange the code to verify the invitee's email, validate the
// invite token, accept the invite, add the user to the org, then redirect to the
// dashboard where the normal login flow will create a session for the invitee.
func (s *Service) handleInviteCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	redirectError := func(msg string) {
		http.Redirect(w, r, s.siteURL+"?signin_error="+url.QueryEscape(msg), http.StatusTemporaryRedirect)
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.logger.ErrorContext(ctx, "invite callback: missing code parameter")
		redirectError("invite link expired or already used")
		return
	}

	// Exchange the WorkOS SSO code for the authenticated user's profile.
	profile, err := s.orgs.AuthenticateWithInviteLink(ctx, code)
	if err != nil {
		s.logger.ErrorContext(ctx, "invite callback: code exchange failed", attr.SlogError(err))
		redirectError("invite link expired or already used")
		return
	}

	// Extract invite_token from the state parameter.
	stateParam := r.URL.Query().Get("state")
	stateValues, err := url.ParseQuery(stateParam)
	if err != nil || stateValues.Get("invite_token") == "" {
		s.logger.ErrorContext(ctx, "invite callback: missing invite_token in state")
		redirectError("invalid invite link")
		return
	}
	tokenHash := hashToken(stateValues.Get("invite_token"))

	// Look up and validate the invite.
	invite, err := orgrepo.New(s.db).GetInvitationByTokenHash(ctx, tokenHash)
	if err != nil {
		s.logger.ErrorContext(ctx, "invite callback: invite not found", attr.SlogError(err))
		redirectError("invite not found")
		return
	}
	if invite.State != "pending" {
		redirectError("invite already used or revoked")
		return
	}
	if invite.ExpiresAt.Valid && invite.ExpiresAt.Time.Before(time.Now()) {
		redirectError("invite expired")
		return
	}

	// Verify the authenticated email matches the invite.
	inviteeEmail := strings.ToLower(profile.Email)
	if invite.Email != inviteeEmail {
		s.logger.WarnContext(ctx, "invite callback: email mismatch",
			slog.String("invite_email", invite.Email),
			slog.String("authenticated_email", inviteeEmail),
		)
		redirectError("email does not match invitation")
		return
	}

	// Accept the invite and add the user to the org.
	repo := orgrepo.New(s.db)
	if err := repo.AcceptInvitation(ctx, invite.ID); err != nil {
		s.logger.ErrorContext(ctx, "invite callback: failed to accept invitation", attr.SlogError(err))
		redirectError("failed to accept invite")
		return
	}

	// Try to create the org-user relationship if the user already exists in Gram.
	if user, err := userrepo.New(s.db).GetUserByEmail(ctx, inviteeEmail); err == nil {
		if _, err := repo.UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: invite.OrganizationID,
			UserID:         user.ID,
		}); err != nil {
			s.logger.WarnContext(ctx, "invite callback: failed to create org membership", attr.SlogError(err))
		}
	}

	// Redirect through the registry's OIDC login so the invitee gets a Gram
	// session in one step. The return_url tells the registry to redirect back
	// to Gram's auth callback after authentication. If the user already has a
	// WorkOS session (from the magic link click), this completes instantly.
	loginParams := url.Values{}
	loginParams.Set("return_url", strings.TrimRight(s.siteURL, "/")+"/rpc/auth.callback")
	http.Redirect(w, r, s.registryURL+"/v1/speakeasy_provider/login?"+loginParams.Encode(), http.StatusTemporaryRedirect)
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
		UserID:             conv.FromPGTextOrEmpty[string](row.UserID),
		Name:               row.UserDisplayName,
		Email:              row.UserEmail,
		PhotoURL:           conv.FromPGText[string](row.UserPhotoUrl),
		WorkosMembershipID: conv.FromPGText[string](row.WorkosMembershipID),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}
}
