package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"go.opentelemetry.io/otel/codes"
)

type speakeasyProviderUser struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	PhotoURL     *string   `json:"photo_url"`
	GithubHandle *string   `json:"github_handle"`
	Admin        bool      `json:"admin"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Whitelisted  bool      `json:"whitelisted"`
}

type speakeasyProviderOrganization struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Slug               string    `json:"slug"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	AccountType        string    `json:"account_type"`
	SSOConnectionID    *string   `json:"sso_connection_id,omitempty"`
	UserWorkspaceSlugs []string  `json:"user_workspaces_slugs"` // speakeasy-registry side is user_workspaces_slugs
}

type validateTokenResponse struct {
	User          speakeasyProviderUser           `json:"user"`
	Organizations []speakeasyProviderOrganization `json:"organizations"`
}

type TokenExchangeRequest struct {
	Code string `json:"code"`
}

type TokenExchangeResponse struct {
	IDToken string `json:"id_token"`
}

func (s *Manager) ExchangeTokenFromSpeakeasy(ctx context.Context, code string) (idtoken string, err error) {
	ctx, span := s.tracer.Start(ctx, "sessions.exchangeTokenFromSpeakeasy")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	// Prepare the request body
	payload := TokenExchangeRequest{Code: code}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token exchange request: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", s.speakeasyServerAddress+"/v1/speakeasy_provider/exchange", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create token exchange request: %w", err)
	}

	req.Header.Set("speakeasy-auth-provider-key", s.speakeasySecretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Send the request
	resp, err := s.speakeasyClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to perform token exchange: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.logger.ErrorContext(context.Background(), "failed to close response body", attr.SlogError(err))
		}
	}()

	// Check for non-200 status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed with status %s", resp.Status)
	}

	// Parse the response
	var exchangeResp TokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&exchangeResp); err != nil {
		return "", fmt.Errorf("failed to decode token exchange response: %w", err)
	}

	return exchangeResp.IDToken, nil
}

func (s *Manager) RevokeTokenFromSpeakeasy(ctx context.Context, idToken string) (err error) {
	ctx, span := s.tracer.Start(ctx, "sessions.revokeTokenFromSpeakeasy")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	// Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", s.speakeasyServerAddress+"/v1/speakeasy_provider/revoke", nil)
	if err != nil {
		return fmt.Errorf("failed to create revoke token request: %w", err)
	}

	req.Header.Set("speakeasy-auth-provider-key", s.speakeasySecretKey)
	req.Header.Set("speakeasy-auth-provider-id-token", idToken)

	// Send the request
	resp, err := s.speakeasyClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform token revocation: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.logger.ErrorContext(context.Background(), "failed to close response body", attr.SlogError(err))
		}
	}()

	// Check for non-200 status
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token revocation failed with status %s", resp.Status)
	}

	return nil
}

func (s *Manager) GetUserInfoFromSpeakeasy(ctx context.Context, idToken string) (userInfo *CachedUserInfo, err error) {
	ctx, span := s.tracer.Start(ctx, "sessions.getUserInfoFromSpeakeasy")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	req, err := http.NewRequestWithContext(ctx, "GET", s.speakeasyServerAddress+"/v1/speakeasy_provider/validate", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("speakeasy-auth-provider-id-token", idToken)
	req.Header.Set("speakeasy-auth-provider-key", s.speakeasySecretKey)

	resp, err := s.speakeasyClient.Do(req)
	if err != nil {
		s.logger.ErrorContext(context.Background(), "failed to make request", attr.SlogError(err))
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.logger.ErrorContext(context.Background(), "failed to close response body", attr.SlogError(err))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var validateResp validateTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&validateResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	user, err := s.userRepo.UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          validateResp.User.ID,
		Email:       validateResp.User.Email,
		DisplayName: validateResp.User.DisplayName,
		PhotoUrl:    conv.PtrToPGText(validateResp.User.PhotoURL),
		Admin:       validateResp.User.Admin,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upsert user: %w", err)
	}

	// Check if user was created in this upsert
	if user.WasCreated {
		if err := s.posthog.CaptureEvent(ctx, "is_first_time_user_signup", user.Email, map[string]any{
			"email":        user.Email,
			"display_name": user.DisplayName,
		}); err != nil {
			s.logger.ErrorContext(ctx, "failed to capture is_first_time_user_signup event", attr.SlogError(err))
		}
	}

	// Update user and user-org relationships with WorkOS IDs, if applicable
	s.syncWorkOSIDs(ctx, user, validateResp)

	var adminOverride string
	if override, _ := contextvalues.GetAdminOverrideFromContext(ctx); override != "" && validateResp.User.Admin {
		adminOverride = override
	}

	organizations := make([]auth.OrganizationEntry, len(validateResp.Organizations))
	var nonFreeOrganizations []auth.OrganizationEntry
	for i, org := range validateResp.Organizations {
		authOrg := auth.OrganizationEntry{
			ID:                 org.ID,
			Name:               org.Name,
			Slug:               org.Slug,
			SsoConnectionID:    org.SSOConnectionID,
			UserWorkspaceSlugs: org.UserWorkspaceSlugs,
			Projects:           []*auth.ProjectEntry{}, // filled in from gram server
		}

		organizations[i] = authOrg

		if (org.AccountType != "" && org.AccountType != "free") || adminOverride == org.Slug {
			nonFreeOrganizations = append(nonFreeOrganizations, authOrg)
		}
	}

	whitelisted := validateResp.User.Whitelisted

	// If applicable we will only utilize non-free organizations, plus an applied admin override
	if len(nonFreeOrganizations) > 0 {
		organizations = nonFreeOrganizations
		// At this point if a user has paid organizations we consider them whitelisted
		whitelisted = true
	}

	var userPylonSignature *string
	if pylonSignature, err := s.pylon.Sign(validateResp.User.Email); err != nil {
		s.logger.ErrorContext(ctx, "error signing user email", attr.SlogError(err))
	} else if pylonSignature != "" {
		userPylonSignature = &pylonSignature
	}

	return &CachedUserInfo{
		UserID:             validateResp.User.ID,
		UserWhitelisted:    whitelisted,
		Email:              validateResp.User.Email,
		Admin:              validateResp.User.Admin,
		DisplayName:        &validateResp.User.DisplayName,
		PhotoURL:           validateResp.User.PhotoURL,
		UserPylonSignature: userPylonSignature,
		Organizations:      organizations,
	}, nil
}

type createOrgRequest struct {
	OrganizationName string `json:"organization_name"`
	AccountType      string `json:"account_type"`
}

func (s *Manager) CreateOrgFromSpeakeasy(ctx context.Context, idToken string, orgName string) (userInfo *CachedUserInfo, err error) {
	ctx, span := s.tracer.Start(ctx, "sessions.createOrgFromSpeakeasy")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	orgReq := createOrgRequest{
		OrganizationName: orgName,
		AccountType:      "free",
	}

	// Marshal the request body to JSON
	jsonBody, err := json.Marshal(orgReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.speakeasyServerAddress+"/v1/speakeasy_provider/register", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("speakeasy-auth-provider-id-token", idToken)
	req.Header.Set("speakeasy-auth-provider-key", s.speakeasySecretKey)

	resp, err := s.speakeasyClient.Do(req)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to make request", attr.SlogError(err))
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(err))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var validateResp validateTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&validateResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	organizations := make([]auth.OrganizationEntry, len(validateResp.Organizations))
	for i, org := range validateResp.Organizations {
		authOrg := auth.OrganizationEntry{
			ID:                 org.ID,
			Name:               org.Name,
			Slug:               org.Slug,
			SsoConnectionID:    org.SSOConnectionID,
			UserWorkspaceSlugs: org.UserWorkspaceSlugs,
			Projects:           []*auth.ProjectEntry{},
		}

		organizations[i] = authOrg
	}

	return &CachedUserInfo{
		UserID:             validateResp.User.ID,
		UserWhitelisted:    validateResp.User.Whitelisted,
		Email:              validateResp.User.Email,
		Admin:              validateResp.User.Admin,
		DisplayName:        &validateResp.User.DisplayName,
		PhotoURL:           validateResp.User.PhotoURL,
		UserPylonSignature: nil,
		Organizations:      organizations,
	}, nil
}

func (s *Manager) InvalidateUserInfoCache(ctx context.Context, userID string) error {
	err := s.userInfoCache.Delete(ctx, CachedUserInfo{UserID: userID, UserWhitelisted: true, Organizations: []auth.OrganizationEntry{}, Email: "", Admin: false, DisplayName: nil, PhotoURL: nil, UserPylonSignature: nil})
	if err != nil {
		return fmt.Errorf("cache delete: %w", err)
	}

	return nil
}

func (s *Manager) GetUserInfo(ctx context.Context, userID, sessionID string) (*CachedUserInfo, bool, error) {
	if userInfo, err := s.userInfoCache.Get(ctx, UserInfoCacheKey(userID)); err == nil {
		return &userInfo, true, nil
	}

	var userInfo *CachedUserInfo
	var err error

	userInfo, err = s.GetUserInfoFromSpeakeasy(ctx, sessionID)
	if err != nil {
		return nil, false, fmt.Errorf("fetch user info: %w", err)
	}

	if err = s.userInfoCache.Store(ctx, *userInfo); err != nil {
		s.logger.ErrorContext(ctx, "failed to store user info in cache", attr.SlogError(err))
		return userInfo, false, fmt.Errorf("cache user info: %w", err)
	}

	return userInfo, false, nil
}

func (s *Manager) HasAccessToOrganization(ctx context.Context, organizationID, userID, sessionID string) (*auth.OrganizationEntry, string, bool) {
	userInfo, _, err := s.GetUserInfo(ctx, userID, sessionID)
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

// BuildAuthorizationURL builds the authorization URL for Gram OAuth
func (p *Manager) BuildAuthorizationURL(ctx context.Context, params AuthURLParams) (*url.URL, error) {
	urlParams := url.Values{}
	urlParams.Add("return_url", params.CallbackURL)
	urlParams.Add("state", params.State)

	speakeasyAuthURL := fmt.Sprintf("%s/v1/speakeasy_provider/login?%s",
		p.speakeasyServerAddress,
		urlParams.Encode())

	authURL, err := url.Parse(speakeasyAuthURL)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to parse gram OAuth URL", attr.SlogError(err))
		return nil, fmt.Errorf("failed to parse gram OAuth URL: %w", err)
	}

	return authURL, nil
}

func (s *Manager) syncWorkOSIDs(ctx context.Context, user userRepo.UpsertUserRow, validateResp validateTokenResponse) {
	// skip if workos client is not configured
	if s.workos == nil {
		return
	}

	var workosUserID string

	if user.WorkosID.Valid && user.WorkosID.String != "" {
		// Already have the user's WorkOS ID — skip the API lookup
		workosUserID = user.WorkosID.String
	} else {
		workosUser, err := s.workos.GetUserByEmail(ctx, user.Email)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to get workos user by email", attr.SlogError(err))
			return
		}
		if workosUser == nil {
			return
		}

		workosUserID = workosUser.ID

		if err := s.userRepo.SetUserWorkosID(ctx, userRepo.SetUserWorkosIDParams{
			ID:       user.ID,
			WorkosID: conv.ToPGText(workosUserID),
		}); err != nil {
			s.logger.ErrorContext(ctx, "failed to set user workos ID", attr.SlogError(err))
		}
	}

	// Fetch all org memberships for this user in one API call instead of one per org.
	memberships, err := s.workos.ListUserMemberships(ctx, workosUserID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list workos user memberships", attr.SlogError(err))
		return
	}

	membershipByOrgID := make(map[string]int, len(memberships))
	for i, m := range memberships {
		membershipByOrgID[m.OrganizationID] = i
	}

	for _, org := range validateResp.Organizations {
		idx, ok := membershipByOrgID[org.ID]
		if !ok {
			continue
		}
		orgMembership := memberships[idx]

		// Link the organization to its WorkOS org ID if not already linked.
		if _, err := s.orgRepo.SetOrgWorkosID(ctx, orgRepo.SetOrgWorkosIDParams{
			WorkosID:       conv.ToPGText(orgMembership.OrganizationID),
			OrganizationID: org.ID,
		}); err != nil {
			// SetOrgWorkosID only updates when workos_id IS NULL, so
			// pgx.ErrNoRows means it was already linked — not an error.
			if !errors.Is(err, pgx.ErrNoRows) {
				s.logger.ErrorContext(ctx, "failed to set org workos ID", attr.SlogError(err))
			}
		}

		if err := s.orgRepo.AttachWorkOSUserToOrg(
			ctx,
			orgRepo.AttachWorkOSUserToOrgParams{
				OrganizationID:     org.ID,
				UserID:             validateResp.User.ID,
				WorkosMembershipID: conv.ToPGText(orgMembership.ID),
			},
		); err != nil {
			s.logger.ErrorContext(ctx, "failed to attach workos user to org", attr.SlogError(err))
			continue
		}
	}
}
