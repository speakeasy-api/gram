package sessions

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/workos/workos-go/v6/pkg/usermanagement"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"go.opentelemetry.io/otel/codes"
)

// IDPUserInfo represents the response from the OIDC /userinfo endpoint.
type IDPUserInfo struct {
	Sub     string  `json:"sub"`
	Email   string  `json:"email"`
	Name    string  `json:"name"`
	Picture *string `json:"picture,omitempty"`
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
		Sub:     resp.User.ID,
		Email:   resp.User.Email,
		Name:    name,
		Picture: picture,
	}, nil
}

// UpsertUserFromIDP upserts a user record from OIDC identity claims and
// returns the user ID. Captures a PostHog event for first-time signups.
func (s *Manager) UpsertUserFromIDP(ctx context.Context, idpUser *IDPUserInfo) (string, error) {
	// Preserve admin status for existing users — admin is now managed in Gram's
	// DB rather than being sourced from the IDP on every login.
	admin := false
	if existing, err := s.userRepo.GetUser(ctx, idpUser.Sub); err == nil {
		admin = existing.Admin
	}

	user, err := s.userRepo.UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          idpUser.Sub,
		Email:       idpUser.Email,
		DisplayName: idpUser.Name,
		PhotoUrl:    conv.PtrToPGText(idpUser.Picture),
		Admin:       admin,
	})
	if err != nil {
		return "", fmt.Errorf("upsert user: %w", err)
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

// BuildUserInfoFromDB constructs a CachedUserInfo by querying user data and
// org memberships from the database. This replaces the old Speakeasy IDP
// /validate call.
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
