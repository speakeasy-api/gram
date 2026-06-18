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
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/workos/workos-go/v6/pkg/workos_errors"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/organizations/server"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/gen/types"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
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
	pfRepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	telemrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	svix "github.com/svix/svix-webhooks/go"
	"github.com/svix/svix-webhooks/go/models"
)

const (
	defaultInviteExpiryDays = 7
)

// OrganizationProvider is the WorkOS surface the organizations service uses.
// *workos.Client implements it.
type OrganizationProvider interface {
	DeleteOrganizationMembership(ctx context.Context, workosMembershipID string) error
	CreateOrganizationMembership(ctx context.Context, workosUserID, workosOrgID, roleSlug string) (string, error)
	GetOrganizationDomainPolicy(ctx context.Context, workosOrgID string) (*workos.OrganizationDomainPolicy, error)
	ListRoles(ctx context.Context, workosOrgID string) ([]workos.Role, error)
	GenerateAdminPortalLink(ctx context.Context, workosOrgID string, intent workos.PortalIntent, opts workos.GenerateAdminPortalLinkOpts) (string, error)
	ListConnections(ctx context.Context, organizationID string) ([]workos.Connection, error)
	ListDirectories(ctx context.Context, organizationID string) ([]workos.Directory, error)
}

var _ OrganizationProvider = (*workos.Client)(nil)

// InviteIdentityProvider handles invitee identity verification and user upsert
// through the same resolver used by the normal auth flow.
type InviteIdentityProvider interface {
	AuthenticateWithMagicAuth(ctx context.Context, email string) (*identity.IDPUserInfo, error)
	UpsertUserFromIDP(ctx context.Context, idpUser *identity.IDPUserInfo) (string, error)
}

type orgFeatureChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// HookEventReader is the subset of the telemetry repo used by the onboarding
// wizard's verifyOnboardingHooksSetup poll. Decoupled from a concrete repo so
// tests can stub it.
type HookEventReader interface {
	ListRecentHookEventsForOnboarding(ctx context.Context, arg telemrepo.ListRecentHookEventsForOnboardingParams) ([]telemrepo.RecentHookEvent, error)
	CountRecentHookEventsForOnboarding(ctx context.Context, projectIDs []string, sinceUnixNano int64) (uint64, error)
}

type Service struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	db        *pgxpool.Pool
	auth      *auth.Auth
	authz     *authz.Engine
	sessions  *sessions.Manager
	orgs      OrganizationProvider
	invite    InviteIdentityProvider
	features  orgFeatureChecker
	hooks     HookEventReader // optional; nil disables verifyOnboardingHooksSetup
	email     *email.Service
	serverURL string // API server URL; used to build invite links
	siteURL   string // frontend URL; used for post-callback browser redirects
	audit     *audit.Logger
	svix      *svix.Svix
}

var _ gen.Service = (*Service)(nil)

var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionMgr *sessions.Manager, orgs OrganizationProvider, invite InviteIdentityProvider, features orgFeatureChecker, hooks HookEventReader, authzEngine *authz.Engine, emailService *email.Service, serverURL string, siteURL string, auditLogger *audit.Logger, svix *svix.Svix) *Service {
	logger = logger.With(attr.SlogComponent("organizations"))

	return &Service{
		logger:    logger,
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/organizations"),
		db:        db,
		auth:      auth.New(logger, db, sessionMgr, authzEngine),
		authz:     authzEngine,
		sessions:  sessionMgr,
		orgs:      orgs,
		invite:    invite,
		features:  features,
		hooks:     hooks,
		email:     emailService,
		serverURL: serverURL,
		siteURL:   siteURL,
		audit:     auditLogger,
		svix:      svix,
	}
}

const inviteCallbackPath = "/rpc/organizations.inviteCallback"
const setupCallbackPath = "/v1/setup/callback"

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)

	// Raw HTTP handler for Gram invite-token acceptance.
	mux.Handle("GET", inviteCallbackPath, service.handleInviteCallback)

	// Raw HTTP handler for onboarding setup portal callback.
	// WorkOS success_url redirects here; we verify the setup state and redirect
	// to the appropriate wizard step in the dashboard.
	mux.Handle("GET", setupCallbackPath, service.handleSetupCallback)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) Get(ctx context.Context, _ *gen.GetPayload) (res *gen.Organization, err error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read organization details").LogError(ctx, s.logger)
	}

	return &gen.Organization{
		ID:                org.ID,
		Name:              org.Name,
		Slug:              types.Slug(org.Slug),
		AccountType:       org.GramAccountType,
		WebhooksOnboarded: org.SvixAppID.String != "",
		WebhooksEnabled:   org.WebhooksEnabled.Bool && org.SvixAppID.String != "",
		CreatedAt:         org.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:         org.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
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

	normalizedEmail := conv.NormalizeEmail(payload.Email)
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.OrganizationInviteEmail(normalizedEmail),
	)

	emailDomain, ok := inviteEmailDomain(normalizedEmail)
	if !ok {
		return nil, oops.E(oops.CodeBadRequest, nil, "email must be a valid email address").LogError(ctx, logger)
	}

	_, err = userrepo.New(s.db).GetConnectedUserByEmail(ctx, userrepo.GetConnectedUserByEmailParams{
		Email:          normalizedEmail,
		OrganizationID: ac.ActiveOrganizationID,
	})
	switch {
	case err == nil:
		return nil, oops.E(oops.CodeConflict, nil, "user is already a member of this organization").LogError(ctx, logger)
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "check organization membership").LogError(ctx, logger)
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get organization metadata").LogError(ctx, logger)
	}
	if workosOrgID := conv.FromPGTextOrEmpty[string](org.WorkosID); workosOrgID != "" {
		if err := s.ensureInviteEmailDomainAllowed(ctx, logger, workosOrgID, emailDomain); err != nil {
			return nil, err
		}
	}

	rawToken, tokenHash, err := generateInviteToken()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate invite token").LogError(ctx, logger)
	}

	roleSlug := pgtype.Text{String: "", Valid: false}
	if payload.RoleID != nil && *payload.RoleID != "" {
		roleSlug, err = s.resolveInviteRoleSlug(ctx, ac.ActiveOrganizationID, *payload.RoleID, logger)
		if err != nil {
			return nil, err
		}
		span.AddEvent("invite.role_resolved", trace.WithAttributes(attr.OrganizationInviteRoleSlug(roleSlug.String)))
	}

	// Expire stale invites and create the new one in a single transaction so
	// a concurrent request cannot slip in between and claim the unique index.
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback is no-op after commit

	txRepo := orgrepo.New(tx)

	// Transition any expired-but-still-pending invites to 'expired' state so
	// the partial unique index (org_id, email) WHERE state = 'pending' does
	// not block re-inviting after an invite naturally expires.
	if err := txRepo.ExpireStaleInvitations(ctx, orgrepo.ExpireStaleInvitationsParams{
		OrganizationID: ac.ActiveOrganizationID,
		Email:          normalizedEmail,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "expire stale invitations").LogError(ctx, logger)
	}

	row, err := txRepo.CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: ac.ActiveOrganizationID,
		Email:          normalizedEmail,
		TokenHash:      tokenHash,
		InviterUserID:  conv.ToPGText(ac.UserID),
		RoleSlug:       roleSlug,
		ExpiresInDays:  int32(defaultInviteExpiryDays),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "organization_invitations_org_email_pending_key" {
			return nil, oops.E(oops.CodeConflict, nil, "an invitation is already pending for this email").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create invitation").LogError(ctx, logger)
	}
	span.SetAttributes(attr.OrganizationInviteID(row.ID.String()))
	span.AddEvent("invite.created", trace.WithAttributes(
		attr.OrganizationInviteID(row.ID.String()),
		attr.OrganizationInviteEmail(normalizedEmail),
	))

	if err := s.audit.LogOrganizationInviteCreate(ctx, tx, audit.LogOrganizationInviteCreateEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		InvitationURN:    urn.NewOrganizationInvitation(row.ID),
		InviteeEmail:     row.Email,
		RoleSlug:         conv.FromPGText[string](row.RoleSlug),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization invitation creation").LogError(ctx, logger)
	}

	inviteLink := ""
	if s.email != nil {
		inviteURL, err := url.Parse(s.serverURL + inviteCallbackPath)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build invite link").LogError(ctx, logger)
		}
		q := inviteURL.Query()
		q.Set("invite_token", rawToken)
		inviteURL.RawQuery = q.Encode()
		inviteLink = inviteURL.String()
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit invitation").LogError(ctx, logger)
	}

	if s.email != nil {
		// Look up inviter display name + email and org name for the email template.
		inviterName, inviterEmail := ac.UserID, ""
		if u, err := userrepo.New(s.db).GetUser(ctx, ac.UserID); err == nil {
			inviterName = strings.TrimSpace(u.DisplayName)
			inviterEmail = strings.TrimSpace(u.Email)
			if inviterName == "" {
				inviterName = inviterEmail
			}
			if inviterName == "" {
				inviterName = ac.UserID
			}
		}
		orgName := ac.ActiveOrganizationID
		if org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID); err == nil {
			orgName = org.Name
		}

		if err := s.email.Send(ctx, row.Email, email.TeamInvite{
			InviteLink:       inviteLink,
			OrganizationName: orgName,
			InviterName:      inviterName,
			InviterEmail:     inviterEmail,
		}); err != nil {
			span.RecordError(err)
			span.AddEvent("invite.email_failed")
			// Revoke the invite so the user can retry — the invitee never
			// received the invite link so the invite is useless.
			_ = orgrepo.New(s.db).RevokeInvitation(ctx, row.ID)
			return nil, oops.E(oops.CodeUnexpected, err, "failed to send invite email").LogError(ctx, logger)
		}
		span.AddEvent("invite.email_sent")
	}

	return dbInvitationToGen(&row, &ac.UserID), nil
}

func (s *Service) ensureInviteEmailDomainAllowed(ctx context.Context, logger *slog.Logger, workosOrgID string, emailDomain string) error {
	policy, err := s.orgs.GetOrganizationDomainPolicy(ctx, workosOrgID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get organization trusted domains").LogError(ctx, logger)
	}
	if policy == nil {
		return nil
	}

	hasTrustedDomains := false
	trustedDomains := make([]string, 0, len(policy.Domains))
	for _, domain := range policy.Domains {
		trustedDomain := normalizeInviteDomain(domain.Domain)
		if trustedDomain == "" {
			continue
		}
		hasTrustedDomains = true
		trustedDomains = append(trustedDomains, trustedDomain)
		if trustedDomain == emailDomain {
			return nil
		}
	}
	if !hasTrustedDomains {
		return nil
	}

	return oops.E(oops.CodeBadRequest, nil, "invite email must use one of this organization's trusted domains: %s", strings.Join(trustedDomains, ", ")).LogError(ctx, logger)
}

func (s *Service) resolveInviteRoleSlug(ctx context.Context, organizationID string, roleID string, logger *slog.Logger) (pgtype.Text, error) {
	if strings.TrimSpace(roleID) == "" {
		return pgtype.Text{String: "", Valid: false}, oops.E(oops.CodeBadRequest, nil, "role id is required").LogError(ctx, logger)
	}

	// The dashboard sends a Gram local role UUID (as returned by
	// /rpc/access.listRoles). Resolve it against the local roles table to
	// recover the WorkOS slug stored on the invite for acceptance time.
	roleUUID, err := uuid.Parse(roleID)
	if err != nil {
		return pgtype.Text{String: "", Valid: false}, oops.E(oops.CodeBadRequest, nil, "role not found").LogError(ctx, logger)
	}

	role, err := accessrepo.New(s.db).GetOrganizationRoleByID(ctx, accessrepo.GetOrganizationRoleByIDParams{
		OrganizationID: organizationID,
		ID:             roleUUID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return pgtype.Text{String: "", Valid: false}, oops.E(oops.CodeBadRequest, nil, "role not found").LogError(ctx, logger)
	case err != nil:
		return pgtype.Text{String: "", Valid: false}, oops.E(oops.CodeUnexpected, err, "get role for invite").LogError(ctx, logger)
	}

	return conv.ToPGText(role.WorkosSlug), nil
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

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.OrganizationInviteID(payload.InvitationID),
	)

	inviteID, err := uuid.Parse(payload.InvitationID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid invitation id").LogError(ctx, logger)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := orgrepo.New(tx)
	invite, err := txRepo.GetInvitationByID(ctx, inviteID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.C(oops.CodeNotFound).LogError(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get invitation").LogError(ctx, logger)
	}
	if invite.OrganizationID != ac.ActiveOrganizationID {
		return oops.E(oops.CodeForbidden, nil, "invitation does not belong to this organization").LogError(ctx, logger)
	}

	span.SetAttributes(
		attr.OrganizationInviteEmail(invite.Email),
		attr.OrganizationInviteState(invite.State),
	)

	beforeSnapshot := dbInvitationToGen(&invite, conv.FromPGText[string](invite.InviterUserID))
	row, err := txRepo.RevokeInvitationForOrganization(ctx, orgrepo.RevokeInvitationForOrganizationParams{
		ID:             inviteID,
		OrganizationID: ac.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "revoke invitation").LogError(ctx, logger)
	}

	afterSnapshot := dbInvitationToGen(&row, conv.FromPGText[string](row.InviterUserID))
	if err := s.audit.LogOrganizationInviteRevoke(ctx, tx, audit.LogOrganizationInviteRevokeEvent{
		OrganizationID:           ac.ActiveOrganizationID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:         ac.Email,
		ActorSlug:                nil,
		InvitationURN:            urn.NewOrganizationInvitation(row.ID),
		InviteeEmail:             row.Email,
		InvitationSnapshotBefore: beforeSnapshot,
		InvitationSnapshotAfter:  afterSnapshot,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization invitation revocation").LogError(ctx, logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit invitation revocation").LogError(ctx, logger)
	}

	span.AddEvent("invite.revoked")
	return nil
}

func (s *Service) UpdateInviteRole(ctx context.Context, payload *gen.UpdateInviteRolePayload) (*gen.OrganizationInvitation, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
		attr.SlogOrganizationInviteID(payload.InvitationID),
		attr.SlogAccessRoleID(payload.RoleID),
	)
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.OrganizationInviteID(payload.InvitationID),
		attr.AccessRoleID(payload.RoleID),
	)

	inviteID, err := uuid.Parse(payload.InvitationID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid invitation id").LogError(ctx, logger)
	}

	repo := orgrepo.New(s.db)
	invite, err := repo.GetInvitationByID(ctx, inviteID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound).LogError(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get invitation").LogError(ctx, logger)
	}
	if invite.OrganizationID != ac.ActiveOrganizationID {
		return nil, oops.E(oops.CodeForbidden, nil, "invitation does not belong to this organization").LogError(ctx, logger)
	}
	if invite.State != "pending" {
		return nil, oops.E(oops.CodeBadRequest, nil, "invitation is not pending").LogError(ctx, logger)
	}
	if invite.ExpiresAt.Valid && !invite.ExpiresAt.Time.After(time.Now()) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invitation is expired").LogError(ctx, logger)
	}

	roleSlug, err := s.resolveInviteRoleSlug(ctx, ac.ActiveOrganizationID, payload.RoleID, logger)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := orgrepo.New(tx)
	beforeSnapshot := dbInvitationToGen(&invite, conv.FromPGText[string](invite.InviterUserID))
	row, err := txRepo.UpdateInvitationRole(ctx, orgrepo.UpdateInvitationRoleParams{
		ID:             inviteID,
		OrganizationID: ac.ActiveOrganizationID,
		RoleSlug:       roleSlug,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound).LogError(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update invitation role").LogError(ctx, logger)
	}

	afterSnapshot := dbInvitationToGen(&row, conv.FromPGText[string](row.InviterUserID))
	if err := s.audit.LogOrganizationInviteRoleUpdate(ctx, tx, audit.LogOrganizationInviteRoleUpdateEvent{
		OrganizationID:           ac.ActiveOrganizationID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:         ac.Email,
		ActorSlug:                nil,
		InvitationURN:            urn.NewOrganizationInvitation(row.ID),
		InviteeEmail:             row.Email,
		InvitationSnapshotBefore: beforeSnapshot,
		InvitationSnapshotAfter:  afterSnapshot,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization invitation role update").LogError(ctx, logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit invitation role update").LogError(ctx, logger)
	}

	span.AddEvent("invite.role_updated", trace.WithAttributes(attr.OrganizationInviteRoleSlug(roleSlug.String)))
	return afterSnapshot, nil
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
		return nil, oops.E(oops.CodeUnexpected, err, "list invitations").LogError(ctx, logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list organization users").LogError(ctx, s.logger)
	}

	excludedUserIDs, err := pfRepo.New(s.db).ListSessionCaptureExclusions(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list session capture exclusions").LogError(ctx, s.logger)
	}
	excludedSet := make(map[string]struct{}, len(excludedUserIDs))
	for _, uid := range excludedUserIDs {
		excludedSet[uid] = struct{}{}
	}

	out := make([]*gen.OrganizationUser, 0, len(rows))
	for i := range rows {
		_, loggingExcluded := excludedSet[conv.FromPGTextOrEmpty[string](rows[i].UserID)]
		out = append(out, organizationUserToGen(&rows[i], loggingExcluded))
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
		return oops.E(oops.CodeBadRequest, nil, "cannot remove yourself from the organization").LogError(ctx, logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	qtx := orgrepo.New(tx)

	rel, err := qtx.GetOrganizationUserRelationship(ctx, orgrepo.GetOrganizationUserRelationshipParams{
		OrganizationID: ac.ActiveOrganizationID,
		UserID:         conv.ToPGText(payload.UserID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "user is not a member of this organization").LogError(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get organization user relationship").LogError(ctx, logger)
	}

	if err := qtx.DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: ac.ActiveOrganizationID,
		UserID:         conv.ToPGText(payload.UserID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete organization user relationship").LogError(ctx, logger)
	}

	if rel.WorkosMembershipID.Valid && rel.WorkosMembershipID.String != "" {
		if err := s.orgs.DeleteOrganizationMembership(ctx, rel.WorkosMembershipID.String); err != nil {
			return oops.E(oops.CodeUnexpected, err, "remove user").LogError(ctx, logger)
		}
	} else {
		s.logger.DebugContext(ctx, "skipping WorkOS membership delete: no workos_membership_id on relationship",
			attr.SlogOrganizationID(ac.ActiveOrganizationID),
			attr.SlogUserID(payload.UserID),
			attr.SlogWorkOSOrganizationID(workosOrgID),
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) EnableWebhooks(ctx context.Context, payload *gen.EnableWebhooksPayload) (err error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger
	orgID := ac.ActiveOrganizationID

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to read organization details").LogError(ctx, s.logger)
	}

	appID := conv.FromPGTextOrEmpty[string](org.SvixAppID)

	if appID == "" {
		app, err := s.svix.Application.GetOrCreate(ctx, models.ApplicationIn{
			Name:         ac.OrganizationSlug,
			Uid:          &orgID,
			Metadata:     &map[string]string{},
			RateLimit:    nil,
			ThrottleRate: nil,
		}, nil)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to create or get webhook connection").LogError(ctx, logger)
		}
		appID = app.Id
	}

	if appID == "" {
		return oops.E(oops.CodeUnexpected, nil, "malformed webhook connection details").LogError(ctx, logger)
	}

	logger = logger.With(attr.SlogOrganizationID(orgID), attr.SlogSvixAppID(appID))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to access organization details").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	var updated bool
	row, err := orgrepo.New(s.db).UpsertSvixAppID(ctx, orgrepo.UpsertSvixAppIDParams{
		ID:        orgID,
		SvixAppID: conv.ToPGText(appID),
	})
	switch {
	case err == nil:
		if row.PreviousSvixAppID.String != "" && row.PreviousSvixAppID.String != appID {
			logger.ErrorContext(ctx, "overwriting existing svix application id",
				attr.SlogSvixPreviousAppID(row.PreviousSvixAppID.String),
			)
		}
		updated = true
	case errors.Is(err, pgx.ErrNoRows):
		stored := conv.FromPGTextOrEmpty[string](row.SvixAppID)
		if stored != appID {
			return oops.E(oops.CodeUnexpected, nil, "failed to find organization details to update").LogError(ctx, logger)
		}
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to store webhook connection details").LogError(ctx, logger)
	}

	if updated {
		if err := s.audit.LogOrganizationWebhooksToggled(ctx, dbtx, audit.LogOrganizationWebhooksToggledEvent{
			OrganizationID:   orgID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName: ac.Email,
			ActorSlug:        nil,
			OrganizationName: org.Name,
			OrganizationSlug: org.Slug,
			WebhooksEnabled:  row.WebhooksEnabled.Bool,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to log webhook toggle event").LogError(ctx, logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to save webhook connection details").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) DisableWebhooks(ctx context.Context, payload *gen.DisableWebhooksPayload) (err error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	orgID := ac.ActiveOrganizationID
	logger := s.logger.With(attr.SlogOrganizationID(orgID))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to access organization details").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to read organization details").LogError(ctx, logger)
	}

	var updated bool
	row, err := orgrepo.New(s.db).SetWebhooksEnabled(ctx, orgrepo.SetWebhooksEnabledParams{
		ID:      orgID,
		Enabled: pgtype.Bool{Bool: false, Valid: true},
	})
	switch {
	case err == nil:
		updated = true
	case errors.Is(err, pgx.ErrNoRows):
		// Org didn't have svix app id to begin with, effectively a noop
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to store webhook connection details").LogError(ctx, logger)
	}

	if updated {
		if err := s.audit.LogOrganizationWebhooksToggled(ctx, dbtx, audit.LogOrganizationWebhooksToggledEvent{
			OrganizationID:   orgID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName: ac.Email,
			ActorSlug:        nil,
			OrganizationName: org.Name,
			OrganizationSlug: org.Slug,
			WebhooksEnabled:  row.WebhooksEnabled.Bool,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to log webhook toggle event").LogError(ctx, logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to save webhook connection details").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) CreatePortalSession(ctx context.Context, payload *gen.CreatePortalSessionPayload) (res *gen.CreatePortalSessionResult, err error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}

	// See note below on why we use this pointer to a slice. It's also partly
	// because we want to get around false-positive from ineffassign linter.
	// The empty/zero-value of this slice is harmful because it grants all
	// capabilities in svix.
	var caps *[]models.AppPortalCapability
	readCheckErr := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil})
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err == nil {
		caps = new(fullSvixAppPortalCapabilities())
	} else if readCheckErr == nil {
		caps = new(minimumSvixAppPortalCapabilities())
	} else {
		return nil, readCheckErr
	}

	logger := s.logger

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read organization details").LogError(ctx, logger)
	}

	appID := conv.FromPGTextOrEmpty[string](org.SvixAppID)
	enabled := org.WebhooksEnabled.Bool

	if appID == "" || !enabled {
		return nil, oops.E(oops.CodeBadRequest, nil, "webhooks not enabled for this organization")
	}

	session, err := s.svix.Authentication.AppPortalAccess(ctx, appID, models.AppPortalAccessIn{
		// Safeguard because an empty slice grants all capabilties and so we
		// need to use a pointer to slice to mark the absence of capabilities
		// and then default to minimum capabilities in that case.
		Capabilities: conv.PtrValOr(caps, minimumSvixAppPortalCapabilities()),
		Expiry:       new(uint64(24 * 60 * 60)),
		Application:  nil,
		FeatureFlags: nil,
		ReadOnly:     nil,
		SessionId:    nil,
	}, nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create webhook portal session").LogError(ctx, logger)
	}

	return &gen.CreatePortalSessionResult{
		URL:   session.Url,
		Token: session.Token,
	}, nil
}

func (s *Service) GetOnboardingStatus(ctx context.Context, payload *gen.GetOnboardingStatusPayload) (*gen.OnboardingStatusResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read organization details").LogError(ctx, s.logger)
	}

	workosOrgID := conv.FromPGTextOrEmpty[string](org.WorkosID)
	if workosOrgID == "" {
		return &gen.OnboardingStatusResult{SsoConfigured: false, DsyncConfigured: false}, nil
	}

	connections, err := s.orgs.ListConnections(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to check SSO connections").LogError(ctx, s.logger)
	}

	directories, err := s.orgs.ListDirectories(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to check directory sync").LogError(ctx, s.logger)
	}

	return &gen.OnboardingStatusResult{
		SsoConfigured:   workos.HasActiveConnection(connections),
		DsyncConfigured: workos.HasActiveDirectory(directories),
	}, nil
}

const verifyOnboardingHooksLimit = 50

func (s *Service) VerifyOnboardingHooksSetup(ctx context.Context, payload *gen.VerifyOnboardingHooksSetupPayload) (*gen.VerifyOnboardingHooksSetupResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if s.hooks == nil {
		// Telemetry/ClickHouse is not wired in this binary. Return an empty result
		// so the wizard can render gracefully on local OSS setups.
		return &gen.VerifyOnboardingHooksSetupResult{Events: []*gen.OnboardingHookEvent{}, LatestUnixNano: "0", TotalCount: 0}, nil
	}

	var sinceUnixNano int64
	if payload.SinceUnixNano != nil && *payload.SinceUnixNano != "" {
		parsed, parseErr := strconv.ParseInt(*payload.SinceUnixNano, 10, 64)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid since_unix_nano cursor").LogError(ctx, s.logger)
		}
		if parsed > 0 {
			sinceUnixNano = parsed
		}
	}

	projects, err := projectsrepo.New(s.db).ListProjectsByOrganization(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list projects for organization").LogError(ctx, s.logger)
	}

	if len(projects) == 0 {
		return &gen.VerifyOnboardingHooksSetupResult{Events: []*gen.OnboardingHookEvent{}, LatestUnixNano: strconv.FormatInt(sinceUnixNano, 10), TotalCount: 0}, nil
	}

	projectIDs := make([]string, 0, len(projects))
	slugByID := make(map[string]string, len(projects))
	for _, p := range projects {
		id := p.ID.String()
		projectIDs = append(projectIDs, id)
		slugByID[id] = p.Slug
	}

	rows, err := s.hooks.ListRecentHookEventsForOnboarding(ctx, telemrepo.ListRecentHookEventsForOnboardingParams{
		ProjectIDs:    projectIDs,
		SinceUnixNano: sinceUnixNano,
		Limit:         verifyOnboardingHooksLimit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read recent hook events").LogError(ctx, s.logger)
	}

	total, err := s.hooks.CountRecentHookEventsForOnboarding(ctx, projectIDs, sinceUnixNano)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to count recent hook events").LogError(ctx, s.logger)
	}

	events := make([]*gen.OnboardingHookEvent, 0, len(rows))
	latest := sinceUnixNano
	for _, r := range rows {
		if r.TimeUnixNano > latest {
			latest = r.TimeUnixNano
		}
		ev := &gen.OnboardingHookEvent{
			TimeUnixNano: strconv.FormatInt(r.TimeUnixNano, 10),
			Source:       r.HookSource,
			ProjectSlug:  slugByID[r.GramProjectID],
			ToolName:     r.ToolName,
			EventName:    r.EventName,
			UserEmail:    r.UserEmail,
			ChatID:       r.GramChatID,
			Status:       conv.PtrEmpty(r.Status),
		}
		events = append(events, ev)
	}

	return &gen.VerifyOnboardingHooksSetupResult{
		Events:         events,
		LatestUnixNano: strconv.FormatInt(latest, 10),
		TotalCount:     int(total), //nolint:gosec // count fits int
	}, nil
}

// handleSetupCallback is the backend handler that WorkOS's success_url redirects to
// after portal completion. It authenticates the session, verifies the setup
// state with WorkOS, and 302-redirects to the appropriate wizard step.
//
// Query params:
//   - intent: "sso" or "dsync"
func (s *Service) handleSetupCallback(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "organizations.handleSetupCallback")
	defer span.End()

	intent := r.URL.Query().Get("intent")
	if intent == "" {
		span.SetStatus(codes.Error, "missing intent")
		http.Error(w, "missing intent parameter", http.StatusBadRequest)
		return
	}

	// Authenticate the user's session cookie (set by SessionMiddleware).
	sessionToken, ok := contextvalues.GetSessionTokenFromContext(ctx)
	if !ok {
		span.SetStatus(codes.Error, "unauthenticated")
		http.Redirect(w, r, s.siteURL+"/login", http.StatusTemporaryRedirect)
		return
	}

	ctx, err := s.auth.Authorize(ctx, sessionToken, &security.APIKeyScheme{
		Name:           "session",
		Scopes:         []string{},
		RequiredScopes: []string{},
	})
	if err != nil {
		span.SetStatus(codes.Error, "auth failed")
		http.Redirect(w, r, s.siteURL+"/login", http.StatusTemporaryRedirect)
		return
	}

	ac, err := s.authContext(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "missing auth context")
		http.Error(w, "missing auth context", http.StatusUnauthorized)
		return
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "setup callback: read org", attr.SlogError(err))
		span.SetStatus(codes.Error, "read org failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	workosOrgID := conv.FromPGTextOrEmpty[string](org.WorkosID)
	orgSlug := org.Slug

	// Determine the next step based on what was just completed and what's verified.
	var nextStepSlug string
	switch intent {
	case "sso":
		if workosOrgID != "" {
			connections, err := s.orgs.ListConnections(ctx, workosOrgID)
			if err != nil {
				s.logger.ErrorContext(ctx, "setup callback: list connections", attr.SlogError(err))
			}
			if workos.HasActiveConnection(connections) {
				nextStepSlug = "directory-sync"
			}
		}
	case "dsync":
		// Directory sync may take time to become "linked" after portal setup.
		// Completing the portal is sufficient to advance — DSYNC is also skippable.
		nextStepSlug = "create-marketplace"
	}

	redirectURL := fmt.Sprintf("%s/%s/setup", s.siteURL, orgSlug)
	if nextStepSlug != "" {
		redirectURL += fmt.Sprintf("?step=%s", nextStepSlug)
	}

	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

func (s *Service) SendEnterpriseAdminOnboardingEmail(ctx context.Context, payload *gen.SendEnterpriseAdminOnboardingEmailPayload) (*gen.SendEnterpriseAdminOnboardingEmailResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if s.email == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "email service not configured").LogError(ctx, s.logger)
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read organization details").LogError(ctx, s.logger)
	}

	setupLink := fmt.Sprintf("%s/%s/setup", strings.TrimRight(s.siteURL, "/"), org.Slug)

	tmpl := email.EnterpriseAdminOnboarding{SetupLink: setupLink}

	sent := 0
	for _, recipient := range payload.Recipients {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" {
			continue
		}
		if err := s.email.Send(ctx, recipient, tmpl); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to send onboarding email to %s", recipient).LogError(ctx, s.logger)
		}
		sent++
	}

	return &gen.SendEnterpriseAdminOnboardingEmailResult{
		SentCount: sent,
		SetupLink: setupLink,
	}, nil
}

func (s *Service) GenerateWorkOSAdminPortalLink(ctx context.Context, payload *gen.GenerateWorkOSAdminPortalLinkPayload) (res *gen.GenerateWorkOSAdminPortalLinkResult, err error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read organization details").LogError(ctx, s.logger)
	}

	workosOrgID := conv.FromPGTextOrEmpty[string](org.WorkosID)
	if workosOrgID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS")
	}

	var iopts *workos.IntentOptions
	if payload.IntentOptions != nil {
		var sso *workos.SSOIntentOptions
		if payload.IntentOptions.Sso != nil {
			sso = &workos.SSOIntentOptions{
				BookmarkSlug: conv.PtrValOr(payload.IntentOptions.Sso.BookmarkSlug, ""),
				ProviderType: conv.PtrValOr(payload.IntentOptions.Sso.ProviderType, ""),
			}
		}
		var dv *workos.DomainVerificationIntentOptions
		if payload.IntentOptions.DomainVerification != nil {
			dv = &workos.DomainVerificationIntentOptions{
				DomainName: conv.PtrValOr(payload.IntentOptions.DomainVerification.DomainName, ""),
			}
		}
		iopts = &workos.IntentOptions{
			SSO:                sso,
			DomainVerification: dv,
		}
	}
	opts := workos.GenerateAdminPortalLinkOpts{
		ReturnURL:       conv.PtrValOr(payload.ReturnURL, ""),
		SuccessURL:      conv.PtrValOr(payload.SuccessURL, ""),
		ITContactEmails: payload.ItContactEmails,
		IntentOptions:   iopts,
	}

	link, err := s.orgs.GenerateAdminPortalLink(ctx, workosOrgID, workos.PortalIntent(payload.Intent), opts)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate WorkOS admin portal link").LogError(ctx, s.logger)
	}

	return &gen.GenerateWorkOSAdminPortalLinkResult{
		URL: link,
	}, nil
}

func (s *Service) authContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil {
		return nil, oops.E(oops.CodeUnauthorized, errors.New("missing auth context"), "missing auth context").LogError(ctx, s.logger)
	}

	return ac, nil
}

func (s *Service) orgContext(ctx context.Context) (*contextvalues.AuthContext, string, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, "", oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, "", oops.E(oops.CodeUnexpected, err, "get organization metadata").LogError(ctx, s.logger)
	}
	if !org.WorkosID.Valid || org.WorkosID.String == "" {
		return nil, "", oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS").LogError(ctx, s.logger)
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

func inviteEmailDomain(email string) (string, bool) {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || strings.TrimSpace(local) == "" {
		return "", false
	}
	domain = normalizeInviteDomain(domain)
	if domain == "" || strings.Contains(domain, "@") {
		return "", false
	}

	return domain, true
}

func normalizeInviteDomain(domain string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
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
		RoleSlug:      conv.FromPGText[string](row.RoleSlug),
		ExpiresAt:     conv.PtrEmpty(expiresAt),
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}
}

// handleInviteCallback processes Gram invite-token links. Flow: invitee clicks
// the Gram invite link, we validate the invite token, authenticate the invitee
// with a server-created WorkOS Magic Auth code, verify the email, accept the
// invite, add the user to the org, then create a Gram session.
func (s *Service) handleInviteCallback(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "organizations.handleInviteCallback")
	defer span.End()

	redirectError := func(msg string) {
		span.SetStatus(codes.Error, msg)
		http.Redirect(w, r, s.siteURL+"?signin_error="+url.QueryEscape(msg), http.StatusTemporaryRedirect)
	}

	repo := orgrepo.New(s.db)
	loadInvite := func(rawToken string) (orgrepo.OrganizationInvitation, bool) {
		var empty orgrepo.OrganizationInvitation
		if rawToken == "" {
			s.logger.ErrorContext(ctx, "invite callback: missing invite_token")
			redirectError("invalid invite link")
			return empty, false
		}

		invite, err := repo.GetInvitationByTokenHash(ctx, hashToken(rawToken))
		if err != nil {
			s.logger.ErrorContext(ctx, "invite callback: invite not found", attr.SlogError(err))
			redirectError("invite not found")
			return empty, false
		}
		span.SetAttributes(
			attr.OrganizationInviteID(invite.ID.String()),
			attr.OrganizationInviteEmail(invite.Email),
			attr.OrganizationInviteState(invite.State),
			attr.OrganizationID(invite.OrganizationID),
		)
		if invite.State != "pending" {
			span.AddEvent("invite.callback.rejected", trace.WithAttributes(attr.OrganizationInviteState(invite.State)))
			redirectError("invite already used or revoked")
			return empty, false
		}
		if invite.ExpiresAt.Valid && invite.ExpiresAt.Time.Before(time.Now()) {
			span.AddEvent("invite.callback.rejected", trace.WithAttributes(attr.OrganizationInviteState("expired")))
			redirectError("invite expired")
			return empty, false
		}
		span.AddEvent("invite.callback.token_validated")

		return invite, true
	}

	invite, ok := loadInvite(r.URL.Query().Get("invite_token"))
	if !ok {
		return
	}

	idpUser, err := s.invite.AuthenticateWithMagicAuth(ctx, invite.Email)
	if err != nil {
		if inviteRequiresNormalLogin(err) {
			span.AddEvent("invite.callback.normal_login_redirect")
			http.Redirect(w, r, "/rpc/auth.login", http.StatusTemporaryRedirect)
			return
		}
		s.logger.ErrorContext(ctx, "invite callback: magic auth failed", attr.SlogError(err))
		span.RecordError(err)
		redirectError("invite authentication failed")
		return
	}
	if idpUser == nil {
		s.logger.ErrorContext(ctx, "invite callback: empty identity from magic auth")
		redirectError("invite authentication failed")
		return
	}
	span.SetAttributes(
		attr.WorkOSUserID(idpUser.Sub),
		attr.AuthUserEmail(idpUser.Email),
	)
	span.AddEvent("invite.callback.magic_auth_succeeded", trace.WithAttributes(
		attr.WorkOSUserID(idpUser.Sub),
		attr.AuthUserEmail(idpUser.Email),
	))

	// Verify the authenticated email matches the invite.
	inviteeEmail := conv.NormalizeEmail(idpUser.Email)
	if invite.Email != inviteeEmail {
		s.logger.WarnContext(ctx, fmt.Sprintf("invite callback: email mismatch (invite=%s, authenticated=%s)", invite.Email, inviteeEmail))
		span.AddEvent("invite.callback.email_mismatch", trace.WithAttributes(
			attr.OrganizationInviteEmail(invite.Email),
			attr.AuthUserEmail(inviteeEmail),
		))
		redirectError("email does not match invitation")
		return
	}
	idpUser.Email = inviteeEmail
	if strings.TrimSpace(idpUser.Name) == "" {
		idpUser.Name = inviteeEmail
	}

	// Provision the user via the shared identity upsert path. This handles
	// user creation, workos_id stamping, external_id sync, and PostHog events
	// using the same logic the normal auth callback uses.
	gramUserID, err := s.invite.UpsertUserFromIDP(ctx, idpUser)
	if err != nil {
		s.logger.ErrorContext(ctx, "invite callback: failed to provision user", attr.SlogError(err))
		span.RecordError(err)
		redirectError("failed to create user account")
		return
	}
	span.SetAttributes(
		attr.UserID(gramUserID),
		attr.OrganizationID(invite.OrganizationID),
	)
	span.AddEvent("invite.callback.user_provisioned", trace.WithAttributes(attr.UserID(gramUserID)))

	if _, err := repo.UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: invite.OrganizationID,
		UserID:         conv.ToPGText(gramUserID),
	}); err != nil {
		s.logger.ErrorContext(ctx, "invite callback: failed to create org membership", attr.SlogError(err))
		span.RecordError(err)
		redirectError("failed to join organization")
		return
	}
	span.AddEvent("invite.callback.org_membership_created")

	// Create the WorkOS organization membership so ListMembers returns the
	// invited user with the correct role.
	var workosMembershipID string
	if invite.RoleSlug.Valid {
		org, err := repo.GetOrganizationMetadata(ctx, invite.OrganizationID)
		if err != nil {
			s.logger.WarnContext(ctx, "invite callback: failed to get org metadata for WorkOS membership", attr.SlogError(err))
		} else if org.WorkosID.Valid && org.WorkosID.String != "" {
			mid, err := s.orgs.CreateOrganizationMembership(ctx, idpUser.Sub, org.WorkosID.String, invite.RoleSlug.String)
			if err != nil {
				s.logger.WarnContext(ctx, "invite callback: failed to create WorkOS membership", attr.SlogError(err))
				span.RecordError(err)
			} else {
				workosMembershipID = mid
				span.AddEvent("invite.callback.workos_membership_created")
			}
		}
	}

	// Eagerly sync role assignments so the invited user gets RBAC grants
	// immediately. The background sync job will eventually do this too, but
	// it isn't running in all environments yet.
	if invite.RoleSlug.Valid {
		if err := repo.SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
			OrganizationID:     invite.OrganizationID,
			WorkosUserID:       idpUser.Sub,
			WorkosRoleSlugs:    []string{invite.RoleSlug.String},
			UserID:             conv.ToPGText(gramUserID),
			WorkosMembershipID: conv.ToPGText(workosMembershipID),
			WorkosUpdatedAt:    pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
			WorkosLastEventID:  pgtype.Text{String: "", Valid: false},
		}); err != nil {
			s.logger.WarnContext(ctx, "invite callback: failed to sync role assignments", attr.SlogError(err))
			span.RecordError(err)
		} else {
			span.AddEvent("invite.callback.rbac_synced", trace.WithAttributes(
				attr.OrganizationInviteRoleSlug(invite.RoleSlug.String),
			))
		}
	}

	// Accept the invite AFTER all provisioning succeeds. This ensures a
	// transient failure in user creation or org membership doesn't burn the
	// invite — the invitee can retry. The WHERE clause guards against a
	// concurrent revoke or expiry winning the race.
	rowsAffected, err := repo.AcceptInvitation(ctx, invite.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invite callback: failed to accept invitation", attr.SlogError(err))
		redirectError("failed to accept invite")
		return
	}
	if rowsAffected == 0 {
		s.logger.WarnContext(ctx, "invite callback: invite was revoked or expired before acceptance")
		span.AddEvent("invite.callback.acceptance_race_lost")
		redirectError("invite already used, revoked, or expired")
		return
	}
	span.AddEvent("invite.callback.invitation_accepted")

	// Create a Gram session directly. The invitee is already authenticated
	// by Magic Auth, and the WorkOS session ID is stored for logout revocation.
	sessionID := uuid.New().String()
	session := sessions.Session{
		SessionID:            sessionID,
		UserID:               gramUserID,
		ActiveOrganizationID: invite.OrganizationID,
		WorkOSSessionID:      idpUser.WorkOSSessionID,
	}
	if err := s.sessions.StoreSession(ctx, session); err != nil {
		s.logger.ErrorContext(ctx, "invite callback: failed to store session", attr.SlogError(err))
		span.RecordError(err)
		redirectError("failed to create session")
		return
	}
	span.AddEvent("invite.callback.session_created", trace.WithAttributes(
		attr.UserID(gramUserID),
		attr.OrganizationID(invite.OrganizationID),
	))

	// Invalidate cached user info so the session picks up fresh org memberships.
	if err := s.sessions.InvalidateUserInfoCache(ctx, gramUserID); err != nil {
		s.logger.WarnContext(ctx, "invite callback: failed to invalidate user info cache", attr.SlogError(err))
	}

	//nolint:exhaustruct // only desired fields — avoid unexpected zero-value behavior
	http.SetCookie(w, &http.Cookie{
		Name:     "gram_session",
		Value:    sessionID,
		MaxAge:   2592000,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	span.AddEvent("invite.callback.cookie_set")
	s.logger.InfoContext(ctx, "invite callback: complete",
		attr.SlogUserID(gramUserID),
		attr.SlogOrganizationID(invite.OrganizationID),
		attr.SlogOrganizationInviteID(invite.ID.String()),
		attr.SlogAuthUserEmail(inviteeEmail),
	)
	http.Redirect(w, r, s.siteURL, http.StatusTemporaryRedirect)
}

func inviteRequiresNormalLogin(err error) bool {
	var ssoRequired *workos_errors.SSORequiredError
	if errors.As(err, &ssoRequired) {
		return true
	}

	var orgAuthRequired *workos_errors.OrganizationAuthenticationMethodsRequiredError
	if errors.As(err, &orgAuthRequired) {
		return !magicAuthAllowed(orgAuthRequired.AuthMethods)
	}

	return false
}

func magicAuthAllowed(authMethods map[string]bool) bool {
	if authMethods == nil {
		return true
	}

	for method, allowed := range authMethods {
		normalized := strings.ReplaceAll(strings.ToLower(method), "_", "")
		if normalized == "magicauth" {
			return allowed
		}
	}

	return true
}

func organizationUserToGen(row *orgrepo.ListOrganizationUsersRow, loggingExcluded bool) *gen.OrganizationUser {
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
	var lastLogin *string
	if row.UserLastLogin.Valid {
		s := row.UserLastLogin.Time.UTC().Format(time.RFC3339)
		lastLogin = &s
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
		LastLogin:          lastLogin,
		LoggingExcluded:    loggingExcluded,
	}
}

func fullSvixAppPortalCapabilities() []models.AppPortalCapability {
	return []models.AppPortalCapability{
		models.APPPORTALCAPABILITY_VIEW_BASE,
		models.APPPORTALCAPABILITY_VIEW_ENDPOINT_SECRET,
		models.APPPORTALCAPABILITY_MANAGE_ENDPOINT_SECRET,
		models.APPPORTALCAPABILITY_MANAGE_TRANSFORMATIONS,
		models.APPPORTALCAPABILITY_CREATE_ATTEMPTS,
		models.APPPORTALCAPABILITY_MANAGE_ENDPOINT,
	}
}
func minimumSvixAppPortalCapabilities() []models.AppPortalCapability {
	return []models.AppPortalCapability{models.APPPORTALCAPABILITY_VIEW_BASE}
}
