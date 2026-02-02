package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	srv "github.com/speakeasy-api/gram/server/gen/http/auth/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	teamsRepo "github.com/speakeasy-api/gram/server/internal/teams/repo"
)

type authErr string

const (
	authErrCodeLookup      authErr = "lookup_error"
	authErrInit            authErr = "init_error"
	authErrLocalDevStubbed authErr = "local_dev_stubbed"
)

const gramWaitlistTypeForm = "https://speakeasyapi.typeform.com/to/h6WJdwWr"

type AuthConfigurations struct {
	SpeakeasyServerAddress string
	GramServerURL          string
	SignInRedirectURL      string
	Environment            string
	DevMode                bool
}

// Service for gram dashboard authentication endpoints
type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	sessions     *sessions.Manager
	cfg          AuthConfigurations
	projectsRepo *projectsRepo.Queries
	envRepo      *envRepo.Queries
	orgRepo      *orgRepo.Queries
	teamsRepo    *teamsRepo.Queries
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, cfg AuthConfigurations) *Service {
	logger = logger.With(attr.SlogComponent("auth"))

	return &Service{
		tracer:       otel.Tracer("github.com/speakeasy-api/gram/server/internal/auth"),
		logger:       logger,
		db:           db,
		sessions:     sessions,
		cfg:          cfg,
		projectsRepo: projectsRepo.New(db),
		envRepo:      envRepo.New(db),
		orgRepo:      orgRepo.New(db),
		teamsRepo:    teamsRepo.New(db),
	}
}

func FormSignInRedirectURL(siteURL string) string {
	return siteURL
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
	return s.sessions.Authenticate(ctx, key, true) // TODO: canStubAuth is a temporary hack to allow us to limit auth stubbing to rpc/auth endpoints
}

func (s *Service) Callback(ctx context.Context, payload *gen.CallbackPayload) (res *gen.CallbackResult, err error) {
	redirectWithError := func(code authErr, err error) (*gen.CallbackResult, error) {
		s.logger.ErrorContext(ctx, "signin error", attr.SlogError(err), attr.SlogReason(string(code)))
		return &gen.CallbackResult{
			Location:      fmt.Sprintf("%s?signin_error=%s", s.cfg.SignInRedirectURL, err.Error()),
			SessionToken:  "",
			SessionCookie: "",
		}, nil
	}

	if payload.Code == "" {
		return redirectWithError(authErrCodeLookup, errors.New("code is required"))
	}

	state := decodeStateParam(payload)

	idToken, err := s.sessions.ExchangeTokenFromSpeakeasy(ctx, payload.Code)
	if err != nil {
		return redirectWithError(authErrCodeLookup, err)
	}

	userInfo, err := s.sessions.GetUserInfoFromSpeakeasy(ctx, idToken)
	if err != nil {
		return redirectWithError(authErrCodeLookup, err)
	}

	// Users with a valid invite token bypass the whitelist check.
	hasInviteToken := state != nil && state.InviteToken != ""
	if !userInfo.Admin && !userInfo.UserWhitelisted && !hasInviteToken {
		return &gen.CallbackResult{
			Location:      gramWaitlistTypeForm,
			SessionToken:  "",
			SessionCookie: "",
		}, nil
	}

	session := sessions.Session{
		SessionID:            idToken,
		UserID:               userInfo.UserID,
		ActiveOrganizationID: "",
	}

	if len(userInfo.Organizations) == 0 {
		if err := s.sessions.StoreSession(ctx, session); err != nil {
			return redirectWithError(authErrInit, err)
		}

		// Process invite token for users with no existing orgs.
		if hasInviteToken {
			if orgSlug, err := s.processInviteToken(ctx, state.InviteToken, userInfo.UserID, userInfo.Email, &session); err != nil {
				s.logger.ErrorContext(ctx, "failed to process invite token", attr.SlogError(err))
				// Fall through — the user is still signed in, just not added to the org.
			} else {
				return &gen.CallbackResult{
					Location:      "/" + orgSlug,
					SessionToken:  session.SessionID,
					SessionCookie: session.SessionID,
				}, nil
			}
		}

		return &gen.CallbackResult{
			Location:      s.callbackRedirectURL(ctx, payload),
			SessionToken:  session.SessionID,
			SessionCookie: session.SessionID,
		}, nil
	}

	activeOrg := userInfo.Organizations[0]

	// For speakeasy users and admins we default speakeasy-team being the active organization if present
	// For admins we allow you to override the active organization returned by header if present
	if strings.HasSuffix(userInfo.Email, "@speakeasy.com") || strings.HasSuffix(userInfo.Email, "@speakeasyapi.dev") || userInfo.Admin {
		override := "speakeasy-team"
		if userInfo.Admin {
			if adminOverride, _ := contextvalues.GetAdminOverrideFromContext(ctx); adminOverride != "" {
				override = adminOverride
			}
		}
		for _, org := range userInfo.Organizations {
			if org.Slug == override {
				activeOrg = org
				break
			}
		}
	}

	orgMetadata, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              activeOrg.ID,
		Name:            activeOrg.Name,
		Slug:            activeOrg.Slug,
		SsoConnectionID: conv.PtrToPGText(activeOrg.SsoConnectionID),
	})
	if err != nil {
		return redirectWithError(authErrInit, err)
	}

	if orgMetadata.DisabledAt.Valid {
		return redirectWithError(authErrInit, errors.New("this organization is disabled, please reach out to support@speakeasy.com for more information"))
	}

	session.ActiveOrganizationID = activeOrg.ID
	if err := s.sessions.StoreSession(ctx, session); err != nil {
		return redirectWithError(authErrInit, err)
	}

	// Process invite token for users that already have orgs.
	if hasInviteToken {
		if orgSlug, err := s.processInviteToken(ctx, state.InviteToken, userInfo.UserID, userInfo.Email, &session); err != nil {
			s.logger.ErrorContext(ctx, "failed to process invite token", attr.SlogError(err))
			// Fall through — redirect to the normal destination.
		} else {
			return &gen.CallbackResult{
				Location:      "/" + orgSlug,
				SessionToken:  session.SessionID,
				SessionCookie: session.SessionID,
			}, nil
		}
	}

	return &gen.CallbackResult{
		Location:      s.callbackRedirectURL(ctx, payload),
		SessionToken:  session.SessionID,
		SessionCookie: session.SessionID,
	}, nil
}

func (s *Service) Login(ctx context.Context, payload *gen.LoginPayload) (res *gen.LoginResult, err error) {
	if s.sessions.IsUnsafeLocalDevelopment() {
		err = errors.New("calling rpc.login for local development stubbed auth is not supported because stubbed auth implies always being logged in. Reaching this point suggests a problem with dashboard authentication")
		s.logger.ErrorContext(ctx, "signin error", attr.SlogError(err), attr.SlogReason(string(authErrLocalDevStubbed)))
		return &gen.LoginResult{
			Location: fmt.Sprintf("%s?signin_error=%s", s.cfg.SignInRedirectURL, authErrLocalDevStubbed),
		}, nil
	}

	returnAddress := strings.TrimRight(s.cfg.GramServerURL, "/")

	// Get the request context to access the Host
	requestCtx, ok := contextvalues.GetRequestContext(ctx)
	if ok && requestCtx != nil && strings.Contains(requestCtx.Host, "speakeasyapi.vercel.app") && s.cfg.Environment == "dev" {
		// For preview builds, use the request host with https protocol
		returnAddress = "https://" + requestCtx.Host
	}

	values := url.Values{}
	values.Add("return_url", returnAddress+"/rpc/auth.callback")
	values.Add("state", encodeStateParam(payload))

	location := s.cfg.SpeakeasyServerAddress + "/v1/speakeasy_provider/login?" + values.Encode()

	return &gen.LoginResult{
		Location: location,
	}, nil
}

func (s *Service) SwitchScopes(ctx context.Context, payload *gen.SwitchScopesPayload) (res *gen.SwitchScopesResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting user info").Log(ctx, s.logger)
	}

	selectedOrg := authCtx.ActiveOrganizationID
	if payload.OrganizationID != nil {
		selectedOrg = *payload.OrganizationID
	}

	orgFound := false
	for _, org := range userInfo.Organizations {
		if org.ID == selectedOrg {
			orgFound = true
			if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
				ID:              org.ID,
				Name:            org.Name,
				Slug:            org.Slug,
				SsoConnectionID: conv.PtrToPGText(org.SsoConnectionID),
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error upserting organization metadata").Log(ctx, s.logger)
			}
			break
		}
	}
	if !orgFound {
		return nil, oops.E(oops.CodeInvalid, nil, "organization not found in user info")
	}
	authCtx.ActiveOrganizationID = selectedOrg

	if err := s.sessions.UpdateSession(ctx, sessions.Session{
		SessionID:            *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               authCtx.UserID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating auth session").Log(ctx, s.logger)
	}

	return &gen.SwitchScopesResult{
		SessionToken:  *authCtx.SessionID,
		SessionCookie: *authCtx.SessionID,
	}, nil
}

func (s *Service) Logout(ctx context.Context, payload *gen.LogoutPayload) (res *gen.LogoutResult, err error) {
	// Clears cookie and invalidates session
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, authCtx.UserID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error invalidating user").Log(ctx, s.logger)
	}

	if err := s.sessions.RevokeTokenFromSpeakeasy(ctx, *authCtx.SessionID); err != nil {
		s.logger.ErrorContext(ctx, "error revoking token", attr.SlogError(err))
	}

	if err := s.sessions.ClearSession(ctx, sessions.Session{
		SessionID:            *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               authCtx.UserID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error clearing session").Log(ctx, s.logger)
	}
	return &gen.LogoutResult{SessionCookie: ""}, nil
}

func (s *Service) Info(ctx context.Context, payload *gen.InfoPayload) (res *gen.InfoResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, fromCache, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting user info").Log(ctx, s.logger)
	}

	// For admins we only return the active organization to avoid overloaded returns
	if userInfo.Admin {
		for _, org := range userInfo.Organizations {
			if org.ID == authCtx.ActiveOrganizationID {
				userInfo.Organizations = []gen.OrganizationEntry{org}
			}
		}
	}

	// on auth info write through data for user/org relationship as a backfill mechanism
	// user and org both will have been created by now
	// admin is only exception where there is not a single user-org relationship written
	if !userInfo.Admin {
		if _, err := s.orgRepo.UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			UserID:         authCtx.UserID,
		}); err != nil {
			s.logger.ErrorContext(ctx, "error upserting organization user relationship", attr.SlogError(err))
		}
	}

	// Fully unpack the userInfo object
	organizations := make([]*gen.OrganizationEntry, 0, len(userInfo.Organizations))
	for _, org := range userInfo.Organizations {
		// TODO: Not the cleanest but a temporary measue while in POC phase.
		// This may actually be bettter executed from elsewhere
		projectRows, err := s.getProjectsOrSetupDefaults(ctx, org.ID)
		if err != nil {
			return nil, err
		}
		var orgProjects []*gen.ProjectEntry
		for _, project := range projectRows {
			orgProjects = append(orgProjects, &gen.ProjectEntry{
				ID:   project.ID.String(),
				Name: project.Name,
				Slug: types.Slug(project.Slug),
			})
		}

		// write through organization metadata when not from cache to keep entries updated
		// TODO: there may be a better place to do this
		if !fromCache {
			if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
				ID:              org.ID,
				Name:            org.Name,
				Slug:            org.Slug,
				SsoConnectionID: conv.PtrToPGText(org.SsoConnectionID),
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error upserting organization metadata").Log(ctx, s.logger)
			}
		}

		organizations = append(organizations, &gen.OrganizationEntry{
			ID:                 org.ID,
			Name:               org.Name,
			Slug:               org.Slug,
			SsoConnectionID:    org.SsoConnectionID,
			UserWorkspaceSlugs: org.UserWorkspaceSlugs,
			Projects:           orgProjects,
		})
	}

	return &gen.InfoResult{
		SessionToken:         *authCtx.SessionID,
		SessionCookie:        *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		GramAccountType:      authCtx.AccountType,
		UserID:               userInfo.UserID,
		UserEmail:            userInfo.Email,
		UserSignature:        userInfo.UserPylonSignature,
		UserDisplayName:      userInfo.DisplayName,
		UserPhotoURL:         userInfo.PhotoURL,
		IsAdmin:              userInfo.Admin,
		Organizations:        organizations,
	}, nil
}

func (s *Service) Register(ctx context.Context, payload *gen.RegisterPayload) (err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ActiveOrganizationID != "" {
		return oops.E(oops.CodeInvalid, errors.New("user already has an active organization"), "user already has an active organization")
	}

	if payload.OrgName == "" {
		return oops.E(oops.CodeInvalid, errors.New("org name is required"), "org name is required")
	}

	// Only allow alphanumeric characters, spaces, hyphens, and underscores
	validOrgNameRegex := regexp.MustCompile(`^[a-zA-Z0-9\s-_]+$`)
	if !validOrgNameRegex.MatchString(payload.OrgName) {
		return oops.E(oops.CodeInvalid, errors.New("organization name contains invalid characters"), "organization name contains invalid characters")
	}

	info, err := s.sessions.CreateOrgFromSpeakeasy(ctx, *authCtx.SessionID, payload.OrgName)
	// invalid to insure we pull in the new org info on the next auth.info call
	if invalidationErr := s.sessions.InvalidateUserInfoCache(ctx, authCtx.UserID); invalidationErr != nil {
		return oops.E(oops.CodeUnexpected, invalidationErr, "error invalidating user").Log(ctx, s.logger)
	}

	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error creating org").Log(ctx, s.logger)
	}

	if len(info.Organizations) == 0 {
		return oops.E(oops.CodeUnexpected, errors.New("no organizations returned from speakeasy"), "no organizations returned from speakeasy")
	}

	session := sessions.Session{
		SessionID:            *authCtx.SessionID,
		UserID:               authCtx.UserID,
		ActiveOrganizationID: info.Organizations[0].ID,
	}

	if err := s.sessions.StoreSession(ctx, session); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error storing session").Log(ctx, s.logger)
	}

	if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              info.Organizations[0].ID,
		Name:            info.Organizations[0].Name,
		Slug:            info.Organizations[0].Slug,
		SsoConnectionID: conv.PtrToPGText(nil),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error upserting organization metadata").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) getProjectsOrSetupDefaults(ctx context.Context, organizationID string) ([]projectsRepo.Project, error) {
	projects, err := s.projectsRepo.ListProjectsByOrganization(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing projects").Log(ctx, s.logger)
	}

	if len(projects) == 0 {
		project, err := s.createDefaultProject(ctx, organizationID)
		if err != nil {
			return nil, err
		}

		_, err = s.envRepo.CreateEnvironment(ctx, envRepo.CreateEnvironmentParams{
			OrganizationID: organizationID,
			ProjectID:      project.ID,
			Name:           "Default",
			Slug:           "default",
			Description:    conv.ToPGText("Default project for organization"),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error creating default environment").Log(ctx, s.logger)
		}

		projects = append(projects, project)
	}

	return projects, nil
}

func (s *Service) createDefaultProject(ctx context.Context, organizationID string) (projectsRepo.Project, error) {
	project, err := s.projectsRepo.CreateProject(ctx, projectsRepo.CreateProjectParams{
		OrganizationID: organizationID,
		Name:           "Default",
		Slug:           "default",
	})
	var pgErr *pgconn.PgError
	var empty projectsRepo.Project
	if err != nil {
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return empty, oops.E(oops.CodeConflict, nil, "project already exists")
		}
		return empty, oops.E(oops.CodeUnexpected, err, "error creating default project").Log(ctx, s.logger)
	}

	return project, nil
}

type loginState struct {
	FinalDestinationURL string `json:"final_destination_url"`
	InviteToken         string `json:"invite_token,omitempty"`
}

func encodeStateParam(payload *gen.LoginPayload) string {
	state := loginState{
		FinalDestinationURL: conv.PtrValOr(payload.Redirect, ""),
		InviteToken:         conv.PtrValOr(payload.InviteToken, ""),
	}

	jsonBytes, err := json.Marshal(state)
	if err != nil {
		return ""
	}

	return base64.RawURLEncoding.EncodeToString(jsonBytes)
}

func decodeStateParam(payload *gen.CallbackPayload) *loginState {
	if payload == nil {
		return nil
	}

	rawB64 := conv.PtrValOr(payload.State, "")
	if rawB64 == "" {
		return nil
	}

	rawJSON, err := base64.RawURLEncoding.DecodeString(rawB64)
	if err != nil {
		return nil
	}

	var state *loginState
	err = json.Unmarshal(rawJSON, &state)
	if err != nil {
		return nil
	}

	return state
}

// callbackRedirectURL determines the redirect location after authentication. It
// only allows relative URLs to prevent open redirect attacks (see relativeURL).
// If no redirect is found, fall back to SignInRedirectURL.
func (s *Service) callbackRedirectURL(
	ctx context.Context,
	payload *gen.CallbackPayload,
) string {
	var location string

	if state := decodeStateParam(payload); state != nil {
		location = relativeURL(state.FinalDestinationURL)
	}

	if location != "" {
		msg := fmt.Sprintf("Found destination URL in state: '%s'", location)
		s.logger.InfoContext(ctx, msg)

		return location
	}

	return s.cfg.SignInRedirectURL
}

// processInviteToken validates the invite token, adds the user to the
// organisation, marks the invite as accepted, and returns the org slug for
// redirect. The session's active org is updated to the invited org.
func (s *Service) processInviteToken(ctx context.Context, token, userID, userEmail string, session *sessions.Session) (string, error) {
	invite, err := s.teamsRepo.GetTeamInviteByToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("invite not found or already used")
		}
		return "", fmt.Errorf("looking up invite: %w", err)
	}

	if invite.Status != "pending" {
		return "", fmt.Errorf("invite is no longer pending")
	}

	if invite.ExpiresAt.Valid && time.Now().After(invite.ExpiresAt.Time) {
		return "", fmt.Errorf("invite has expired")
	}

	if !strings.EqualFold(invite.Email, userEmail) {
		if !s.cfg.DevMode {
			return "", fmt.Errorf("invite was sent to a different email address")
		}
		s.logger.WarnContext(ctx, "dev mode: skipping invite email match check",
			slog.String("invite_email", invite.Email),
			slog.String("user_email", userEmail),
		)
	}

	// Use a transaction to atomically add the member and accept the invite.
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.teamsRepo.WithTx(dbtx)

	if err := tx.AddOrganizationMember(ctx, teamsRepo.AddOrganizationMemberParams{
		OrganizationID: invite.OrganizationID,
		UserID:         userID,
	}); err != nil {
		return "", fmt.Errorf("adding member to organization: %w", err)
	}

	if _, err := tx.AcceptTeamInvite(ctx, invite.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("invite is no longer pending (concurrent accept)")
		}
		return "", fmt.Errorf("accepting invite: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", fmt.Errorf("committing transaction: %w", err)
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, userID); err != nil {
		s.logger.ErrorContext(ctx, "failed to invalidate user info cache after invite accept",
			attr.SlogError(err),
		)
	}

	orgSlug, err := s.teamsRepo.GetOrganizationSlug(ctx, invite.OrganizationID)
	if err != nil {
		return "", fmt.Errorf("getting organization slug: %w", err)
	}

	// Update session to point at the invited org.
	session.ActiveOrganizationID = invite.OrganizationID
	if err := s.sessions.UpdateSession(ctx, *session); err != nil {
		s.logger.ErrorContext(ctx, "failed to update session after invite accept",
			attr.SlogError(err),
		)
	}

	s.logger.InfoContext(ctx, "team invite accepted via oauth callback",
		slog.String("organization_id", invite.OrganizationID),
		slog.String("user_id", userID),
		slog.String("invite_id", invite.ID.String()),
	)

	return orgSlug, nil
}

// relativeURL converts any URL to a safe relative URL by extracting only the
// path, query, and fragment components.
//
// Examples:
//   - "/dashboard" → "/dashboard"
//   - "/projects?id=123#section" → "/projects?id=123#section"
//   - "http://localhost:3000/dashboard" → "/dashboard"
//   - "https://evil-site.com/phishing" → "/phishing"
//   - "//evil.com/phish" → ""
//   - "invalid:///" → ""
func relativeURL(urlStr string) string {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	isRelative := parsed.Host == "" && parsed.Scheme == ""
	if isRelative {
		return urlStr
	}

	rel := parsed.Path
	if parsed.RawQuery != "" {
		rel += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		rel += "#" + parsed.Fragment
	}

	return rel
}
