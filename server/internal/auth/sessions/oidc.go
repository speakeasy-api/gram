package sessions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/workos/workos-go/v6/pkg/usermanagement"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/users"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"go.opentelemetry.io/otel/codes"
)

// IDPUserInfo represents the user identity returned by the IDP after code exchange.
type IDPUserInfo struct {
	Sub             string  `json:"sub"`
	Email           string  `json:"email"`
	Name            string  `json:"name"`
	Picture         *string `json:"picture,omitempty"`
	ExternalID      string  `json:"-"`
	WorkOSSessionID string  `json:"-"`
}

// ExchangeCodeForTokens exchanges an authorization code for user identity
// via the WorkOS user-management SDK. Both production (api.workos.com) and
// local dev (dev-idp mock-workos) implement the same authenticate endpoint,
// so there is a single code path.
func (s *Manager) ExchangeCodeForTokens(ctx context.Context, code string) (_ *IDPUserInfo, err error) {
	ctx, span := s.tracer.Start(ctx, "sessions.exchangeCodeForTokens")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	resp, err := s.umClient.AuthenticateWithCode(ctx, usermanagement.AuthenticateWithCodeOpts{
		ClientID:     s.idpClientID,
		Code:         code,
		CodeVerifier: "",
		IPAddress:    "",
		UserAgent:    "",
	})
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
	}, nil
}

// extractSessionIDFromJWT decodes the JWT payload (without verification) to
// extract the "sid" claim. The access token signature is already validated by
// WorkOS — we only need the session ID for logout/revocation.
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
// returns the user ID. Captures a PostHog event for first-time signups.
//
// User ID resolution follows a three-step priority:
//  1. If WorkOS already has an external_id for this user (set by Registry or a
//     previous login), use that — it's the canonical cross-system ID.
//  2. If a Gram user with this email already exists, reuse their existing ID
//     so we don't create duplicates.
//  3. Otherwise derive a deterministic UUIDv5 from the WorkOS user ID using
//     the same namespace as the Speakeasy Registry so both systems arrive at
//     the same ID independently.
//
// After resolving the ID, we sync bidirectionally: Gram stores the WorkOS user
// ID, and WorkOS stores the Gram user ID as external_id.
func (s *Manager) UpsertUserFromIDP(ctx context.Context, idpUser *IDPUserInfo) (_ string, err error) {
	ctx, span := s.tracer.Start(ctx, "sessions.upsertUserFromIDP")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	gramUserID, admin := s.resolveGramUserID(ctx, idpUser)
	span.SetAttributes(
		attr.AuthUserID(gramUserID),
		attr.WorkOSUserID(idpUser.Sub),
		attr.ExternalUserID(idpUser.ExternalID),
	)

	user, err := s.userRepo.UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          gramUserID,
		Email:       idpUser.Email,
		DisplayName: idpUser.Name,
		PhotoUrl:    conv.PtrToPGText(idpUser.Picture),
		Admin:       admin,
	})
	if err != nil {
		return "", fmt.Errorf("upsert user: %w", err)
	}

	// Always update the WorkOS user ID so downstream code (e.g. webhook sync)
	// can correlate Gram users with WorkOS identities. We use Overwrite (not
	// Set) because the WorkOS user may have been deleted and recreated with
	// the same email, giving it a new ID that must replace the stale one.
	if err := s.userRepo.OverwriteUserWorkosID(ctx, userRepo.OverwriteUserWorkosIDParams{
		ID:       gramUserID,
		WorkosID: pgtype.Text{String: idpUser.Sub, Valid: true},
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to set workos_id on user", attr.SlogError(err))
	}

	// Write the Gram user ID back to WorkOS as external_id so it becomes the
	// stable cross-system identifier. Non-fatal on failure.
	if s.workosClient != nil {
		if err := s.workosClient.EnsureUserExternalID(ctx, idpUser.Sub, gramUserID); err != nil {
			s.logger.ErrorContext(ctx, "failed to sync external_id to workos",
				attr.SlogError(err),
				attr.SlogWorkOSUserID(idpUser.Sub),
				attr.SlogAuthUserID(gramUserID),
			)
		}
	}

	if user.WasCreated {
		if err := s.posthog.CaptureEvent(ctx, "is_first_time_user_signup", user.Email, map[string]any{
			"email":        user.Email,
			"display_name": user.DisplayName,
		}); err != nil {
			s.logger.ErrorContext(ctx, "failed to capture is_first_time_user_signup event", attr.SlogError(err))
		}
	}

	return user.ID, nil
}

// resolveGramUserID determines the Gram user ID to use for an IDP login.
func (s *Manager) resolveGramUserID(ctx context.Context, idpUser *IDPUserInfo) (string, bool) {
	// Priority 1: Existing Gram user by email — always reuse their ID.
	// User IDs are immutable once created; this preserves legacy Speakeasy
	// IDP UUIDs and any other previously-assigned IDs.
	if existing, err := s.userRepo.GetUserByEmail(ctx, idpUser.Email); err == nil {
		return existing.ID, existing.Admin
	}

	// Priority 2: WorkOS already has an external_id (set by Registry backfill
	// or a previous login in another system). Use it for the new Gram user so
	// the cross-system ID stays consistent.
	if idpUser.ExternalID != "" {
		return idpUser.ExternalID, false
	}

	// Priority 3: Brand-new user with no external_id — derive a deterministic
	// UUIDv5 from the WorkOS user ID. Uses the same namespace as the Speakeasy
	// Registry so both systems arrive at the same ID independently.
	return users.UserIDFromWorkOSID(idpUser.Sub), false
}

// BuildUserInfoFromDB constructs a CachedUserInfo by querying user data and
// org memberships from the database. When the local DB has no org memberships
// for the user (e.g. first login before the WorkOS sync job runs), it falls
// back to the WorkOS API to fetch memberships, upserts them into the DB, and
// returns the freshly-created records.
func (s *Manager) BuildUserInfoFromDB(ctx context.Context, userID string) (*CachedUserInfo, error) {
	ctx, span := s.tracer.Start(ctx, "sessions.buildUserInfoFromDB")
	defer span.End()

	user, err := s.userRepo.GetUser(ctx, userID)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("get user: %w", err)
	}

	orgRows, err := s.orgRepo.ListOrganizationsForUser(ctx, userID)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("list organizations for user: %w", err)
	}

	// Fallback: when the DB has no memberships and the user has a WorkOS
	// identity, fetch memberships directly from the WorkOS API so the first
	// login doesn't require the sync job to have run already.
	if len(orgRows) == 0 && user.WorkosID.Valid && s.workosClient != nil {
		if synced, err := s.syncMembershipsFromWorkOS(ctx, userID, user.WorkosID.String); err != nil {
			s.logger.ErrorContext(ctx, "workos membership fallback failed", attr.SlogError(err))
		} else {
			orgRows = synced
		}
	}

	organizations := make([]Organization, len(orgRows))
	for i, org := range orgRows {
		var workosID *string
		if org.WorkosID.Valid {
			workosID = &org.WorkosID.String
		}
		organizations[i] = Organization{
			ID:                 org.ID,
			Name:               org.Name,
			Slug:               org.Slug,
			WorkosID:           workosID,
			UserWorkspaceSlugs: []string{org.Slug},
		}
	}

	var pylonSignature *string
	if sig, err := s.pylon.Sign(user.Email); err != nil {
		s.logger.ErrorContext(ctx, "error signing user email", attr.SlogError(err))
	} else if sig != "" {
		pylonSignature = &sig
	}

	displayName := user.DisplayName
	var photoURL *string
	if user.PhotoUrl.Valid {
		photoURL = &user.PhotoUrl.String
	}

	return &CachedUserInfo{
		UserID:             user.ID,
		Email:              user.Email,
		Admin:              user.Admin,
		DisplayName:        &displayName,
		PhotoURL:           photoURL,
		UserPylonSignature: pylonSignature,
		Organizations:      organizations,
	}, nil
}

var slugifyRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugifyRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// syncMembershipsFromWorkOS fetches the user's org memberships from WorkOS,
// upserts the organizations and relationships into the local DB, then returns
// the freshly-created org rows so BuildUserInfoFromDB can use them immediately.
//
// Organization ID resolution follows the same three-step priority as user IDs:
//  1. If a Gram org with this WorkOS ID already exists in the DB, keep its ID.
//  2. If WorkOS has an external_id for this org (set by Registry or a previous
//     login), use that as the Gram org ID.
//  3. Otherwise derive a deterministic UUIDv5 from the WorkOS org ID.
//
// After resolving the ID, we write it back to WorkOS as external_id.
func (s *Manager) syncMembershipsFromWorkOS(ctx context.Context, gramUserID, workosUserID string) ([]orgRepo.ListOrganizationsForUserRow, error) {
	members, err := s.workosClient.ListUserMemberships(ctx, workosUserID)
	if err != nil {
		return nil, fmt.Errorf("list workos memberships: %w", err)
	}
	if len(members) == 0 {
		return nil, nil
	}

	for _, m := range members {
		org, err := s.workosClient.GetOrganization(ctx, m.OrganizationID)
		if err != nil {
			s.logger.ErrorContext(ctx, "workos get organization failed", attr.SlogError(err))
			continue
		}

		gramOrgID := s.resolveGramOrgID(ctx, m.OrganizationID, org.ExternalID)

		slug := slugify(org.Name)
		if slug == "" {
			slug = m.OrganizationID
		}

		if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
			ID:          gramOrgID,
			Name:        org.Name,
			Slug:        slug,
			WorkosID:    pgtype.Text{String: m.OrganizationID, Valid: true},
			Whitelisted: pgtype.Bool{Bool: false, Valid: false},
		}); err != nil {
			s.logger.ErrorContext(ctx, "upsert org metadata from workos failed", attr.SlogError(err))
			continue
		}

		// Write Gram org ID back to WorkOS as external_id. Non-fatal on failure.
		if err := s.workosClient.EnsureOrgExternalID(ctx, m.OrganizationID, gramOrgID); err != nil {
			s.logger.ErrorContext(ctx, "failed to sync org external_id to workos",
				attr.SlogError(err),
				attr.SlogWorkOSOrganizationID(m.OrganizationID),
				attr.SlogOrganizationID(gramOrgID),
			)
		}

		if err := s.orgRepo.UpsertOrganizationUserRelationshipFromWorkOS(ctx, orgRepo.UpsertOrganizationUserRelationshipFromWorkOSParams{
			OrganizationID:     gramOrgID,
			UserID:             gramUserID,
			WorkosMembershipID: pgtype.Text{String: m.ID, Valid: m.ID != ""},
			WorkosUpdatedAt:    pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			WorkosLastEventID:  pgtype.Text{String: "", Valid: false},
		}); err != nil {
			s.logger.ErrorContext(ctx, "upsert org user relationship from workos failed", attr.SlogError(err))
		}
	}

	// Re-read from DB to get the canonical rows with all columns populated.
	rows, err := s.orgRepo.ListOrganizationsForUser(ctx, gramUserID)
	if err != nil {
		return nil, fmt.Errorf("re-read organizations after workos sync: %w", err)
	}
	return rows, nil
}

// resolveGramOrgID determines the Gram organization ID for a WorkOS org.
func (s *Manager) resolveGramOrgID(ctx context.Context, workosOrgID, externalID string) string {
	// Priority 1: Existing Gram org by WorkOS ID — always reuse its ID.
	if existing, err := s.orgRepo.GetOrganizationByWorkosID(ctx, pgtype.Text{String: workosOrgID, Valid: true}); err == nil {
		return existing.ID
	}

	// Priority 2: WorkOS already has an external_id (set by Registry or a
	// previous sync). Use it for the new Gram org.
	if externalID != "" {
		return externalID
	}

	// Priority 3: Derive a deterministic UUIDv5 from the WorkOS org ID.
	return orgid.FromWorkOSID(workosOrgID)
}

// GetUserInfo returns cached user info, falling back to a DB lookup on cache
// miss. The bool return indicates whether the result came from cache.
func (s *Manager) GetUserInfo(ctx context.Context, userID string) (*CachedUserInfo, bool, error) {
	if userInfo, err := s.userInfoCache.Get(ctx, UserInfoCacheKey(userID)); err == nil {
		return &userInfo, true, nil
	}

	userInfo, err := s.BuildUserInfoFromDB(ctx, userID)
	if err != nil {
		return nil, false, fmt.Errorf("fetch user info: %w", err)
	}

	if err = s.userInfoCache.Store(ctx, *userInfo); err != nil {
		// Cache store failure is non-fatal — data was fetched successfully from DB.
		s.logger.ErrorContext(ctx, "failed to store user info in cache", attr.SlogError(err))
	}

	return userInfo, false, nil
}

// HasAccessToOrganization checks whether the user belongs to the given org.
func (s *Manager) HasAccessToOrganization(ctx context.Context, organizationID, userID string) (*Organization, string, bool) {
	userInfo, _, err := s.GetUserInfo(ctx, userID)
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

// InvalidateUserInfoCache removes cached user info so the next GetUserInfo
// call rebuilds it from the database.
func (s *Manager) InvalidateUserInfoCache(ctx context.Context, userID string) error {
	err := s.userInfoCache.Delete(ctx, CachedUserInfo{
		UserID:             userID,
		Admin:              false,
		Email:              "",
		DisplayName:        nil,
		PhotoURL:           nil,
		UserPylonSignature: nil,
		Organizations:      []Organization{},
	})
	if err != nil {
		return fmt.Errorf("cache delete: %w", err)
	}
	return nil
}

// workosAuthorizeEndpoint is the WorkOS API authorize endpoint. AuthKit hosted
// domains expose OIDC discovery, token, and userinfo endpoints but the authorize
// flow must go through the WorkOS API. The API redirects to the correct AuthKit
// hosted UI automatically.
const workosAuthorizeEndpoint = "https://api.workos.com/user_management/authorize"

// BuildAuthorizationURL constructs the OIDC authorization URL that the
// browser should be redirected to.
func (s *Manager) BuildAuthorizationURL(ctx context.Context, params AuthURLParams) (*url.URL, error) {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", s.idpClientID)
	q.Set("redirect_uri", params.CallbackURL)
	q.Set("state", params.State)
	q.Set("scope", "openid email profile")
	q.Set("provider", "authkit")

	// Authorize goes through the WorkOS API by default (AuthKit hosted domains
	// don't serve /authorize). Dev-idp handles /authorize directly.
	authorizeBase := workosAuthorizeEndpoint
	if !strings.HasPrefix(s.idpClientID, "client_") {
		authorizeBase = s.idpBaseURL + "/authorize"
	}

	authURL, err := url.Parse(fmt.Sprintf("%s?%s", authorizeBase, q.Encode()))
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to parse OIDC authorization URL", attr.SlogError(err))
		return nil, fmt.Errorf("parse OIDC authorization URL: %w", err)
	}

	return authURL, nil
}
