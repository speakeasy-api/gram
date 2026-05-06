package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
	WorkOSID           *string   `json:"workos_id,omitempty"`   //nolint:tagliatelle // workos_id is correct snake_case
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
	resp, err := s.httpClient.Do(req)
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
	resp, err := s.httpClient.Do(req)
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

	// Validate the IDP id token + run the shared post-IDP user bootstrap
	// (UpsertUser, posthog signup event, WorkOS membership sync) via the
	// shared client. Side effects identical to the prior inline implementation;
	// the user-session AS path runs the same pair of calls.
	validated, err := s.speakeasyClient.ValidateIDToken(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("validate id token: %w", err)
	}

	user, err := s.speakeasyClient.BootstrapUser(ctx, validated)
	if err != nil {
		return nil, fmt.Errorf("bootstrap user: %w", err)
	}

	// Chat-session-specific shaping: admin override, non-free org filtering,
	// pylon signing, then assemble CachedUserInfo. None of this belongs in the
	// shared client — it's all chat-session-flavored.
	var adminOverride string
	if override, _ := contextvalues.GetAdminOverrideFromContext(ctx); override != "" && validated.Admin {
		adminOverride = override
	}

	organizations := make([]Organization, len(validated.Organizations))
	var nonFreeOrganizations []Organization
	for i, org := range validated.Organizations {
		o := Organization{
			ID:                 org.ID,
			Name:               org.Name,
			Slug:               org.Slug,
			WorkosID:           org.WorkOSID,
			UserWorkspaceSlugs: org.UserWorkspaceSlugs,
		}

		organizations[i] = o

		if (org.AccountType != "" && org.AccountType != "free") || adminOverride == org.Slug {
			nonFreeOrganizations = append(nonFreeOrganizations, o)
		}
	}

	whitelisted := validated.Whitelisted

	// If applicable we will only utilize non-free organizations, plus an applied admin override
	if len(nonFreeOrganizations) > 0 {
		if !user.Admin { // admins can access any org
			organizations = nonFreeOrganizations
		}
		// At this point if a user has paid organizations we consider them whitelisted
		whitelisted = true
	}

	var userPylonSignature *string
	if pylonSignature, err := s.pylon.Sign(validated.Email); err != nil {
		s.logger.ErrorContext(ctx, "error signing user email", attr.SlogError(err))
	} else if pylonSignature != "" {
		userPylonSignature = &pylonSignature
	}

	return &CachedUserInfo{
		UserID:             validated.UserID,
		UserWhitelisted:    whitelisted,
		Email:              validated.Email,
		Admin:              validated.Admin,
		DisplayName:        &validated.DisplayName,
		PhotoURL:           validated.PhotoURL,
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

	resp, err := s.httpClient.Do(req)
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

	organizations := make([]Organization, len(validateResp.Organizations))
	for i, org := range validateResp.Organizations {
		organizations[i] = Organization{
			ID:                 org.ID,
			Name:               org.Name,
			Slug:               org.Slug,
			WorkosID:           org.WorkOSID,
			UserWorkspaceSlugs: org.UserWorkspaceSlugs,
		}
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
	err := s.userInfoCache.Delete(ctx, CachedUserInfo{UserID: userID, UserWhitelisted: true, Organizations: []Organization{}, Email: "", Admin: false, DisplayName: nil, PhotoURL: nil, UserPylonSignature: nil})
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

func (s *Manager) HasAccessToOrganization(ctx context.Context, organizationID, userID, sessionID string) (*Organization, string, bool) {
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
