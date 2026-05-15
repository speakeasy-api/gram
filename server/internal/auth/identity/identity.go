package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/orgslug"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/users"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// IDPClient is the slim interface for IDP code exchange. Wraps the WorkOS
// user-management SDK so Gram doesn't leak SDK types with irrelevant
// browser-flow fields (CodeVerifier, IPAddress, UserAgent).
type IDPClient interface {
	AuthenticateWithCode(ctx context.Context, clientID, code string) (*AuthenticateResult, error)
}

// AuthenticateResult holds the fields Gram uses from the IDP code exchange.
type AuthenticateResult struct {
	AccessToken    string
	OrganizationID string // WorkOS org ID the user selected during auth (may be empty)
	User           AuthenticatedUser
}

// AuthenticatedUser holds the user fields Gram reads after IDP authentication.
type AuthenticatedUser struct {
	ID                string
	FirstName         string
	LastName          string
	Email             string
	ProfilePictureURL string
	ExternalID        string
}

// WorkOSClient is the subset of workos.Client needed for identity resolution:
// org membership sync, cross-system ID synchronization, and org provisioning.
type WorkOSClient interface {
	ListUserMemberships(ctx context.Context, userID string) ([]workos.Member, error)
	GetOrganization(ctx context.Context, orgID string) (*workos.Organization, error)
	EnsureUserExternalID(ctx context.Context, workosUserID, gramUserID string) error
	EnsureOrgExternalID(ctx context.Context, workosOrgID, gramOrgID string) error
	CreateOrganization(ctx context.Context, name, gramOrgID string) (string, error)
	CreateOrganizationMembership(ctx context.Context, workosUserID, workosOrgID, roleSlug string) (string, error)
}

// IDPUserInfo represents the user identity returned by the IDP after code exchange.
type IDPUserInfo struct {
	Sub             string  `json:"sub"`
	Email           string  `json:"email"`
	Name            string  `json:"name"`
	Picture         *string `json:"picture,omitempty"`
	ExternalID      string  `json:"-"`
	WorkOSSessionID string  `json:"-"`
	OrganizationID  string  `json:"-"` // WorkOS org ID selected during auth
}

// Resolver handles identity concerns: IDP code exchange, user upsert, org
// membership sync, user-info caching, and authorization URL construction.
type Resolver struct {
	logger        *slog.Logger
	tracer        trace.Tracer
	userInfoCache cache.TypedCacheObject[sessions.CachedUserInfo]
	idpBaseURL    string
	idpClientID   string
	idpClient     IDPClient
	workosClient  WorkOSClient
	orgRepo       *orgRepo.Queries
	userRepo      *userRepo.Queries
	pylon         *pylon.Pylon
	posthog       *posthog.Posthog
}

func NewResolver(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	redisClient cache.Cache,
	idpBaseURL string,
	idpClientID string,
	idpClient IDPClient,
	workosClient WorkOSClient,
	orgRepo *orgRepo.Queries,
	userRepo *userRepo.Queries,
	pylon *pylon.Pylon,
	posthog *posthog.Posthog,
) *Resolver {
	logger = logger.With(attr.SlogComponent("identity"))
	return &Resolver{
		logger:        logger,
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/auth/identity"),
		userInfoCache: cache.NewTypedObjectCache[sessions.CachedUserInfo](logger.With(attr.SlogCacheNamespace("user_info")), redisClient, cache.SuffixNone),
		idpBaseURL:    idpBaseURL,
		idpClientID:   idpClientID,
		idpClient:     idpClient,
		workosClient:  workosClient,
		orgRepo:       orgRepo,
		userRepo:      userRepo,
		pylon:         pylon,
		posthog:       posthog,
	}
}

// ExchangeCodeForTokens exchanges an authorization code for user identity
// via the WorkOS user-management SDK.
func (r *Resolver) ExchangeCodeForTokens(ctx context.Context, code string) (_ *IDPUserInfo, err error) {
	ctx, span := r.tracer.Start(ctx, "identity.exchangeCodeForTokens")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	resp, err := r.idpClient.AuthenticateWithCode(ctx, r.idpClientID, code)
	if err != nil {
		return nil, fmt.Errorf("workos authenticate with code: %w", err)
	}

	name := strings.TrimSpace(resp.User.FirstName + " " + resp.User.LastName)
	var picture *string
	if resp.User.ProfilePictureURL != "" {
		picture = &resp.User.ProfilePictureURL
	}

	return &IDPUserInfo{
		Sub:             resp.User.ID,
		Email:           resp.User.Email,
		Name:            name,
		Picture:         picture,
		ExternalID:      resp.User.ExternalID,
		WorkOSSessionID: extractSessionIDFromJWT(resp.AccessToken),
		OrganizationID:  resp.OrganizationID,
	}, nil
}

func extractSessionIDFromJWT(token string) string {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		SID string `json:"sid"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.SID
}

// UpsertUserFromIDP upserts a user record from OIDC identity claims and
// returns the user ID.
func (r *Resolver) UpsertUserFromIDP(ctx context.Context, idpUser *IDPUserInfo) (_ string, err error) {
	ctx, span := r.tracer.Start(ctx, "identity.upsertUserFromIDP")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	gramUserID, admin := r.resolveGramUserID(ctx, idpUser)
	span.SetAttributes(
		attr.AuthUserID(gramUserID),
		attr.WorkOSUserID(idpUser.Sub),
		attr.ExternalUserID(idpUser.ExternalID),
	)

	user, err := r.userRepo.UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          gramUserID,
		Email:       idpUser.Email,
		DisplayName: idpUser.Name,
		PhotoUrl:    conv.PtrToPGText(idpUser.Picture),
		Admin:       admin,
	})
	if err != nil {
		return "", fmt.Errorf("upsert user: %w", err)
	}

	if err := r.userRepo.OverwriteUserWorkosID(ctx, userRepo.OverwriteUserWorkosIDParams{
		ID:       gramUserID,
		WorkosID: pgtype.Text{String: idpUser.Sub, Valid: true},
	}); err != nil {
		r.logger.ErrorContext(ctx, "failed to set workos_id on user", attr.SlogError(err))
	}

	if r.workosClient != nil {
		if err := r.workosClient.EnsureUserExternalID(ctx, idpUser.Sub, gramUserID); err != nil {
			r.logger.ErrorContext(ctx, "failed to sync external_id to workos",
				attr.SlogError(err),
				attr.SlogWorkOSUserID(idpUser.Sub),
				attr.SlogAuthUserID(gramUserID),
			)
		}
	}

	if user.WasCreated {
		if err := r.posthog.CaptureEvent(ctx, "is_first_time_user_signup", user.Email, map[string]any{
			"email":        user.Email,
			"display_name": user.DisplayName,
		}); err != nil {
			r.logger.ErrorContext(ctx, "failed to capture is_first_time_user_signup event", attr.SlogError(err))
		}
	}

	return user.ID, nil
}

func (r *Resolver) resolveGramUserID(ctx context.Context, idpUser *IDPUserInfo) (string, bool) {
	if existing, err := r.userRepo.GetUserByEmail(ctx, idpUser.Email); err == nil {
		return existing.ID, existing.Admin
	}
	if idpUser.ExternalID != "" {
		return idpUser.ExternalID, false
	}
	return users.UserIDFromWorkOSID(idpUser.Sub), false
}

// BuildUserInfoFromDB constructs a CachedUserInfo by querying user data and
// org memberships from the database. Falls back to WorkOS API when the local
// DB has no memberships for the user.
func (r *Resolver) BuildUserInfoFromDB(ctx context.Context, userID string) (*sessions.CachedUserInfo, error) {
	ctx, span := r.tracer.Start(ctx, "identity.buildUserInfoFromDB")
	defer span.End()

	user, err := r.userRepo.GetUser(ctx, userID)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("get user: %w", err)
	}

	orgRows, err := r.orgRepo.ListOrganizationsForUser(ctx, conv.ToPGText(userID))
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("list organizations for user: %w", err)
	}

	if len(orgRows) == 0 && user.WorkosID.Valid && r.workosClient != nil {
		if synced, err := r.syncMembershipsFromWorkOS(ctx, userID, user.WorkosID.String); err != nil {
			r.logger.ErrorContext(ctx, "workos membership fallback failed", attr.SlogError(err))
		} else {
			orgRows = synced
		}
	}

	organizations := make([]sessions.Organization, len(orgRows))
	for i, org := range orgRows {
		var workosID *string
		if org.WorkosID.Valid {
			workosID = &org.WorkosID.String
		}
		organizations[i] = sessions.Organization{
			ID:                 org.ID,
			Name:               org.Name,
			Slug:               org.Slug,
			WorkosID:           workosID,
			UserWorkspaceSlugs: []string{org.Slug},
		}
	}

	var pylonSignature *string
	if sig, err := r.pylon.Sign(user.Email); err != nil {
		r.logger.ErrorContext(ctx, "error signing user email", attr.SlogError(err))
	} else if sig != "" {
		pylonSignature = &sig
	}

	displayName := user.DisplayName
	var photoURL *string
	if user.PhotoUrl.Valid {
		photoURL = &user.PhotoUrl.String
	}

	return &sessions.CachedUserInfo{
		UserID:             user.ID,
		Email:              user.Email,
		Admin:              user.Admin,
		DisplayName:        &displayName,
		PhotoURL:           photoURL,
		UserPylonSignature: pylonSignature,
		Organizations:      organizations,
	}, nil
}

// SyncMembershipsFromWorkOS refreshes local WorkOS organization memberships
// and invalidates cached user info so the next read observes the synced rows.
func (r *Resolver) SyncMembershipsFromWorkOS(ctx context.Context, gramUserID, workosUserID string) error {
	if r.workosClient == nil || workosUserID == "" {
		return nil
	}

	if _, err := r.syncMembershipsFromWorkOS(ctx, gramUserID, workosUserID); err != nil {
		return err
	}

	if err := r.InvalidateUserInfoCache(ctx, gramUserID); err != nil {
		return fmt.Errorf("invalidate user info cache: %w", err)
	}
	return nil
}

func (r *Resolver) syncMembershipsFromWorkOS(ctx context.Context, gramUserID, workosUserID string) ([]orgRepo.ListOrganizationsForUserRow, error) {
	members, err := r.workosClient.ListUserMemberships(ctx, workosUserID)
	if err != nil {
		return nil, fmt.Errorf("list workos memberships: %w", err)
	}
	if len(members) == 0 {
		return nil, nil
	}

	for _, m := range members {
		org, err := r.workosClient.GetOrganization(ctx, m.OrganizationID)
		if err != nil {
			return nil, fmt.Errorf("get workos organization %q: %w", m.OrganizationID, err)
		}

		gramOrgID := r.resolveGramOrgID(ctx, m.OrganizationID, org.ExternalID)

		slug := orgslug.Slugify(org.Name)
		if slug == "" {
			slug = m.OrganizationID
		}
		shouldCreateOrg := false
		existingOrg, err := r.orgRepo.GetOrganizationMetadata(ctx, gramOrgID)
		switch {
		case err == nil:
			if !existingOrg.WorkosID.Valid {
				if _, err := r.orgRepo.SetOrgWorkosID(ctx, orgRepo.SetOrgWorkosIDParams{
					WorkosID:       pgtype.Text{String: m.OrganizationID, Valid: true},
					OrganizationID: gramOrgID,
				}); err != nil {
					return nil, fmt.Errorf("set workos id for organization %q: %w", gramOrgID, err)
				}
			} else if existingOrg.WorkosID.String != m.OrganizationID {
				return nil, fmt.Errorf("workos organization %q resolved to gram organization %q with different workos_id %q", m.OrganizationID, gramOrgID, existingOrg.WorkosID.String)
			}
		case errors.Is(err, pgx.ErrNoRows):
			slug, err = orgslug.FindUnique(ctx, r.orgRepo, slug)
			if err != nil {
				return nil, fmt.Errorf("find unique slug for workos organization %q: %w", m.OrganizationID, err)
			}
			shouldCreateOrg = true
		default:
			return nil, fmt.Errorf("get org metadata for workos organization %q: %w", m.OrganizationID, err)
		}

		if shouldCreateOrg {
			if _, err := r.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
				ID:          gramOrgID,
				Name:        org.Name,
				Slug:        slug,
				WorkosID:    pgtype.Text{String: m.OrganizationID, Valid: true},
				Whitelisted: pgtype.Bool{Bool: false, Valid: false},
			}); err != nil {
				return nil, fmt.Errorf("upsert org metadata from workos %q: %w", m.OrganizationID, err)
			}
		}

		if err := r.workosClient.EnsureOrgExternalID(ctx, m.OrganizationID, gramOrgID); err != nil {
			r.logger.ErrorContext(ctx, "failed to sync org external_id to workos",
				attr.SlogError(err),
				attr.SlogWorkOSOrganizationID(m.OrganizationID),
				attr.SlogOrganizationID(gramOrgID),
			)
		}
	}

	workosOrgIDs := make([]string, len(members))
	membershipIDs := make([]string, len(members))
	for i, m := range members {
		workosOrgIDs[i] = m.OrganizationID
		membershipIDs[i] = m.ID
	}
	if err := r.orgRepo.SetUserWorkOSMemberships(ctx, orgRepo.SetUserWorkOSMembershipsParams{
		UserID:              pgtype.Text{String: gramUserID, Valid: gramUserID != ""},
		WorkosOrgIds:        workosOrgIDs,
		WorkosMembershipIds: membershipIDs,
	}); err != nil {
		return nil, fmt.Errorf("set user workos memberships: %w", err)
	}

	rows, err := r.orgRepo.ListOrganizationsForUser(ctx, conv.ToPGText(gramUserID))
	if err != nil {
		return nil, fmt.Errorf("re-read organizations after workos sync: %w", err)
	}
	return rows, nil
}

func (r *Resolver) resolveGramOrgID(ctx context.Context, workosOrgID, externalID string) string {
	if existing, err := r.orgRepo.GetOrganizationByWorkosID(ctx, pgtype.Text{String: workosOrgID, Valid: true}); err == nil {
		return existing.ID
	}
	if externalID != "" {
		return externalID
	}
	return orgid.FromWorkOSID(workosOrgID)
}

// GetUserInfo returns cached user info, falling back to a DB lookup on cache miss.
func (r *Resolver) GetUserInfo(ctx context.Context, userID string) (*sessions.CachedUserInfo, bool, error) {
	if userInfo, err := r.userInfoCache.Get(ctx, sessions.UserInfoCacheKey(userID)); err == nil {
		return &userInfo, true, nil
	}

	userInfo, err := r.BuildUserInfoFromDB(ctx, userID)
	if err != nil {
		return nil, false, fmt.Errorf("fetch user info: %w", err)
	}

	if err = r.userInfoCache.Store(ctx, *userInfo); err != nil {
		r.logger.ErrorContext(ctx, "failed to store user info in cache", attr.SlogError(err))
	}

	return userInfo, false, nil
}

// HasAccessToOrganization checks whether the user belongs to the given org.
func (r *Resolver) HasAccessToOrganization(ctx context.Context, organizationID, userID string) (*sessions.Organization, string, bool) {
	userInfo, _, err := r.GetUserInfo(ctx, userID)
	if err != nil {
		return nil, "", false
	}

	for _, org := range userInfo.Organizations {
		if org.ID == organizationID {
			return &org, userInfo.Email, true
		}
	}
	return nil, userInfo.Email, false
}

// IsAdmin returns whether the user is an admin. Cache miss returns false (safe default).
func (r *Resolver) IsAdmin(ctx context.Context, userID string) bool {
	if userInfo, err := r.userInfoCache.Get(ctx, sessions.UserInfoCacheKey(userID)); err == nil {
		return userInfo.Admin
	}
	return false
}

// InvalidateUserInfoCache removes cached user info so the next GetUserInfo
// call rebuilds it from the database.
func (r *Resolver) InvalidateUserInfoCache(ctx context.Context, userID string) error {
	err := r.userInfoCache.Delete(ctx, sessions.CachedUserInfo{
		UserID:             userID,
		Admin:              false,
		Email:              "",
		DisplayName:        nil,
		PhotoURL:           nil,
		UserPylonSignature: nil,
		Organizations:      nil,
	})
	if err != nil {
		return fmt.Errorf("cache delete: %w", err)
	}
	return nil
}

const workosAuthorizeEndpoint = "https://api.workos.com/user_management/authorize"

// BuildAuthorizationURL constructs the OIDC authorization URL that the
// browser should be redirected to.
func (r *Resolver) BuildAuthorizationURL(ctx context.Context, params sessions.AuthURLParams) (*url.URL, error) {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", r.idpClientID)
	q.Set("redirect_uri", params.CallbackURL)
	q.Set("state", params.State)
	q.Set("scope", "openid email profile")
	q.Set("provider", "authkit")

	authorizeBase := workosAuthorizeEndpoint
	if !strings.HasPrefix(r.idpClientID, "client_") {
		authorizeBase = r.idpBaseURL + "/authorize"
	}

	authURL, err := url.Parse(fmt.Sprintf("%s?%s", authorizeBase, q.Encode()))
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to parse OIDC authorization URL", attr.SlogError(err))
		return nil, fmt.Errorf("parse OIDC authorization URL: %w", err)
	}

	return authURL, nil
}

// ProvisionOrgInWorkOS creates a WorkOS organization and membership for a
// locally-created org. Used by Register and auto-provisioning flows.
// Non-fatal: logs errors but does not fail the caller.
// ProvisionOrgInWorkOS creates a WorkOS organization with gramOrgID as the
// external_id, then creates a membership linking the user. Returns the WorkOS
// org ID so the caller can store it on the Gram org row. Returns ("", nil)
// when no WorkOS client is configured (e.g. tests, OSS).
func (r *Resolver) ProvisionOrgInWorkOS(ctx context.Context, gramOrgID, orgName, gramUserID string) (string, error) {
	if r.workosClient == nil {
		return "", nil
	}

	// Look up user's WorkOS ID from the database.
	user, err := r.userRepo.GetUser(ctx, gramUserID)
	if err != nil {
		return "", fmt.Errorf("look up user for WorkOS provisioning: %w", err)
	}
	if !user.WorkosID.Valid {
		return "", fmt.Errorf("user %s has no workos_id", gramUserID)
	}

	workosOrgID, err := r.workosClient.CreateOrganization(ctx, orgName, gramOrgID)
	if err != nil {
		return "", fmt.Errorf("create WorkOS organization: %w", err)
	}

	if _, err := r.workosClient.CreateOrganizationMembership(ctx, user.WorkosID.String, workosOrgID, "admin"); err != nil {
		return "", fmt.Errorf("create WorkOS organization membership: %w", err)
	}

	return workosOrgID, nil
}
