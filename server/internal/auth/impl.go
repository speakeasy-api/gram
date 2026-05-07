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

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	srv "github.com/speakeasy-api/gram/server/gen/http/auth/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const dispositionAssistants = "assistants"

type authErr string

const (
	authErrCodeLookup authErr = "lookup_error"
	authErrInit       authErr = "init_error"
)

const gramWaitlistTypeForm = "https://speakeasyapi.typeform.com/to/h6WJdwWr"

type AuthConfigurations struct {
	SpeakeasyServerAddress string
	GramServerURL          string
	SignInRedirectURL      string
	Environment            string
}

// Service for gram dashboard authentication endpoints

type AssistantsSubscriptionCancelScheduler interface {
	ScheduleCancelAssistantsSubscription(ctx context.Context, subscriptionID string) error
}

type Service struct {
	tracer              trace.Tracer
	logger              *slog.Logger
	db                  *pgxpool.Pool
	sessions            *sessions.Manager
	cfg                 AuthConfigurations
	authz               *authz.Engine
	billing             billing.Repository
	cancelSubsScheduler AssistantsSubscriptionCancelScheduler
	projectsRepo        *projectsRepo.Queries
	envRepo             *envRepo.Queries
	orgRepo             *orgRepo.Queries
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	cfg AuthConfigurations,
	authzEngine *authz.Engine,
	billingRepo billing.Repository,
	cancelSubsScheduler AssistantsSubscriptionCancelScheduler,
) *Service {
	logger = logger.With(attr.SlogComponent("auth"))

	return &Service{
		tracer:              tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/auth"),
		logger:              logger,
		db:                  db,
		sessions:            sessions,
		cfg:                 cfg,
		authz:               authzEngine,
		billing:             billingRepo,
		cancelSubsScheduler: cancelSubsScheduler,
		projectsRepo:        projectsRepo.New(db),
		envRepo:             envRepo.New(db),
		orgRepo:             orgRepo.New(db),
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
	ctx, err := s.sessions.Authenticate(ctx, key)
	if err != nil {
		return ctx, err
	}
	ctx, err = s.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "load access grants").Log(ctx, s.logger)
	}
	return ctx, nil
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

	idToken, err := s.sessions.ExchangeTokenFromSpeakeasy(ctx, payload.Code)
	if err != nil {
		return redirectWithError(authErrCodeLookup, err)
	}

	userInfo, err := s.sessions.GetUserInfoFromSpeakeasy(ctx, idToken)
	if err != nil {
		return redirectWithError(authErrCodeLookup, err)
	}

	if !userInfo.Admin && !userInfo.UserWhitelisted {
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
		if dispositionFromState(payload) == dispositionAssistants {
			location, err := s.autoProvisionForAssistants(ctx, idToken, userInfo, &session)
			if err != nil {
				return redirectWithError(authErrInit, err)
			}
			return &gen.CallbackResult{
				Location:      location,
				SessionToken:  session.SessionID,
				SessionCookie: session.SessionID,
			}, nil
		}

		if err := s.sessions.StoreSession(ctx, session); err != nil {
			return redirectWithError(authErrInit, err)
		}

		return &gen.CallbackResult{
			Location:      s.callbackRedirectURL(ctx, payload),
			SessionToken:  session.SessionID,
			SessionCookie: session.SessionID,
		}, nil
	}

	activeOrg := userInfo.Organizations[0]
	selectedActiveOrgFromState := false
	if org, ok := activeOrganizationFromState(payload, userInfo.Organizations); ok {
		activeOrg = org
		selectedActiveOrgFromState = true
	}

	// For admins we allow you to override the active organization returned by header if present.
	if !selectedActiveOrgFromState && userInfo.Admin {
		if adminOverride, _ := contextvalues.GetAdminOverrideFromContext(ctx); adminOverride != "" {
			for _, org := range userInfo.Organizations {
				if org.Slug == adminOverride {
					activeOrg = org
					break
				}
			}
		}
	}

	orgMetadata, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          activeOrg.ID,
		Name:        activeOrg.Name,
		Slug:        activeOrg.Slug,
		WorkosID:    conv.PtrToPGText(activeOrg.WorkosID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
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

	return &gen.CallbackResult{
		Location:      s.callbackRedirectURL(ctx, payload),
		SessionToken:  session.SessionID,
		SessionCookie: session.SessionID,
	}, nil
}

func (s *Service) Login(ctx context.Context, payload *gen.LoginPayload) (res *gen.LoginResult, err error) {
	// In local dev, use the site URL so the mock IDP redirects back through
	// the Vite proxy (which forwards /rpc to the server). In production, use
	// the server URL directly since the site may not proxy /rpc paths.
	returnAddress := strings.TrimRight(s.cfg.GramServerURL, "/")
	if s.cfg.Environment == "local" {
		returnAddress = strings.TrimRight(s.cfg.SignInRedirectURL, "/")
	}

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

func activeOrganizationFromState(payload *gen.CallbackPayload, organizations []sessions.Organization) (sessions.Organization, bool) {
	var empty sessions.Organization

	state := decodeStateParam(payload)
	if state == nil {
		return empty, false
	}

	orgSlug := organizationSlugFromDestinationURL(state.FinalDestinationURL)
	if orgSlug == "" {
		return empty, false
	}

	for _, org := range organizations {
		if org.Slug == orgSlug {
			return org, true
		}
	}

	return empty, false
}

func organizationSlugFromDestinationURL(destinationURL string) string {
	location := relativeURL(destinationURL)
	if location == "" {
		return ""
	}

	parsed, err := url.Parse(location)
	if err != nil {
		return ""
	}

	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return ""
	}

	orgSlug, _, _ := strings.Cut(path, "/")
	return orgSlug
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

	var selected sessions.Organization
	orgFound := false
	for _, org := range userInfo.Organizations {
		if org.ID == selectedOrg {
			selected = org
			orgFound = true
			break
		}
	}
	if !orgFound {
		return nil, oops.E(oops.CodeInvalid, nil, "organization not found in user info")
	}
	authCtx.ActiveOrganizationID = selectedOrg

	if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          selected.ID,
		Name:        selected.Name,
		Slug:        selected.Slug,
		WorkosID:    conv.PtrToPGText(selected.WorkosID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error upserting organization metadata").Log(ctx, s.logger)
	}

	if err := s.sessions.StoreSession(ctx, sessions.Session{
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

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting user info").Log(ctx, s.logger)
	}

	// For admins we only return the active organization to avoid overloaded returns
	if userInfo.Admin {
		for _, org := range userInfo.Organizations {
			if org.ID == authCtx.ActiveOrganizationID {
				userInfo.Organizations = []sessions.Organization{org}
			}
		}
	}

	// Write through the user-org relationship as a backfill mechanism.
	// For admins, only upsert when the active org is one they actually belong to.
	// Admins can override their active org to visit customer orgs via the admin
	// override feature, and we must not insert a relationship row in those cases.
	belongsToActiveOrg := !userInfo.Admin
	for _, org := range userInfo.Organizations {
		if org.ID == authCtx.ActiveOrganizationID {
			belongsToActiveOrg = true
			break
		}
	}

	if belongsToActiveOrg {
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
		// Build the full list of project IDs, then filter to only those the
		// user is allowed to see (mirrors the projects.List endpoint).
		projectIDs := make([]string, 0, len(projectRows))
		for _, p := range projectRows {
			projectIDs = append(projectIDs, p.ID.String())
		}

		allowedIDs := projectIDs
		if len(projectIDs) > 0 && org.ID == authCtx.ActiveOrganizationID {
			checks := make([]authz.Check, len(projectIDs))
			for i, id := range projectIDs {
				checks[i] = authz.Check{Scope: authz.ScopeProjectRead, ResourceID: id, ResourceKind: "", Dimensions: nil}
			}
			allowedIDs, err = s.authz.Filter(ctx, checks)
			if err != nil {
				return nil, err
			}
		}

		allowed := make(map[string]struct{}, len(allowedIDs))
		for _, id := range allowedIDs {
			allowed[id] = struct{}{}
		}

		orgProjects := make([]*gen.ProjectEntry, 0, len(allowedIDs))
		for _, project := range projectRows {
			if _, ok := allowed[project.ID.String()]; !ok {
				continue
			}
			orgProjects = append(orgProjects, &gen.ProjectEntry{
				ID:   project.ID.String(),
				Name: project.Name,
				Slug: types.Slug(project.Slug),
			})
		}

		organizations = append(organizations, &gen.OrganizationEntry{
			ID:                 org.ID,
			Name:               org.Name,
			Slug:               org.Slug,
			UserWorkspaceSlugs: org.UserWorkspaceSlugs,
			Projects:           orgProjects,
		})
	}

	return &gen.InfoResult{
		SessionToken:          *authCtx.SessionID,
		SessionCookie:         *authCtx.SessionID,
		ActiveOrganizationID:  authCtx.ActiveOrganizationID,
		GramAccountType:       authCtx.AccountType,
		HasActiveSubscription: authCtx.HasActiveSubscription,
		Whitelisted:           authCtx.Whitelisted,
		UserID:                userInfo.UserID,
		UserEmail:             userInfo.Email,
		UserSignature:         userInfo.UserPylonSignature,
		UserDisplayName:       userInfo.DisplayName,
		UserPhotoURL:          userInfo.PhotoURL,
		IsAdmin:               userInfo.Admin,
		Organizations:         organizations,
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

	whitelisted := false
	if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          info.Organizations[0].ID,
		Name:        info.Organizations[0].Name,
		Slug:        info.Organizations[0].Slug,
		WorkosID:    conv.PtrToPGText(info.Organizations[0].WorkosID),
		Whitelisted: conv.PtrToPGBool(&whitelisted),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error upserting organization metadata").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) autoProvisionForAssistants(ctx context.Context, idToken string, userInfo *sessions.CachedUserInfo, session *sessions.Session) (string, error) {
	info, err := s.sessions.CreateOrgFromSpeakeasy(ctx, idToken, generateLegibleOrgName())
	if err != nil {
		return "", fmt.Errorf("create org via speakeasy: %w", err)
	}
	if invalidationErr := s.sessions.InvalidateUserInfoCache(ctx, userInfo.UserID); invalidationErr != nil {
		return "", fmt.Errorf("invalidate user info cache: %w", invalidationErr)
	}
	if len(info.Organizations) == 0 {
		return "", errors.New("speakeasy returned no organizations after register")
	}

	org := info.Organizations[0]

	whitelisted := true
	if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          org.ID,
		Name:        org.Name,
		Slug:        org.Slug,
		WorkosID:    conv.PtrToPGText(org.WorkosID),
		Whitelisted: conv.PtrToPGBool(&whitelisted),
	}); err != nil {
		return "", fmt.Errorf("upsert organization metadata: %w", err)
	}

	projects, err := s.getProjectsOrSetupDefaults(ctx, org.ID)
	if err != nil {
		return "", fmt.Errorf("setup default project: %w", err)
	}
	if len(projects) == 0 {
		return "", errors.New("default project missing after setup")
	}

	subID, benefitErr := s.billing.AttachAssistantsBenefit(ctx, org.ID, userInfo.Email)
	if benefitErr != nil {
		s.logger.ErrorContext(ctx, "failed to attach assistants benefit", attr.SlogError(benefitErr), attr.SlogOrganizationID(org.ID))
	} else if subID != "" {
		if err := s.cancelSubsScheduler.ScheduleCancelAssistantsSubscription(ctx, subID); err != nil {
			s.logger.ErrorContext(ctx, "failed to schedule assistants subscription cancel-at-period-end", attr.SlogError(err), attr.SlogOrganizationID(org.ID))
		}
	}

	session.ActiveOrganizationID = org.ID
	if err := s.sessions.StoreSession(ctx, *session); err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}

	return fmt.Sprintf("/%s/projects/%s/assistants/new?disposition=%s", org.Slug, projects[0].Slug, dispositionAssistants), nil
}

func dispositionFromState(payload *gen.CallbackPayload) string {
	state := decodeStateParam(payload)
	if state == nil {
		return ""
	}
	parsed, err := url.Parse(relativeURL(state.FinalDestinationURL))
	if err != nil {
		return ""
	}
	return parsed.Query().Get("disposition")
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
	var empty projectsRepo.Project
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return empty, oops.E(oops.CodeConflict, nil, "project already exists")
		}
		return empty, oops.E(oops.CodeUnexpected, err, "error creating default project").Log(ctx, s.logger)
	}

	return project, nil
}

type loginState struct {
	FinalDestinationURL string `json:"final_destination_url"`
}

func encodeStateParam(payload *gen.LoginPayload) string {
	state := loginState{
		FinalDestinationURL: conv.PtrValOr(payload.Redirect, ""),
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
