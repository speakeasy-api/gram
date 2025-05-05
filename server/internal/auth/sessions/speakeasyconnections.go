package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/gen/auth"
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
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	AccountType string    `json:"account_type"`
}

type validateTokenResponse struct {
	User          speakeasyProviderUser           `json:"user"`
	Organizations []speakeasyProviderOrganization `json:"organizations"`
}

func (s *Manager) GetUserInfoFromSpeakeasy(idToken string) (*CachedUserInfo, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", s.speakeasyServerAddress+"/v1/speakeasy_provider/validate", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("speakeasy-auth-provider-id-token", idToken)
	req.Header.Set("speakeasy-auth-provider-key", s.speakeasySecretKey)

	resp, err := client.Do(req)
	if err != nil {
		s.logger.ErrorContext(context.Background(), "failed to make request", slog.String("error", err.Error()))
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.logger.ErrorContext(context.Background(), "failed to close response body", slog.String("error", err.Error()))
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
	var nonFreeOrganizations []auth.OrganizationEntry
	for i, org := range validateResp.Organizations {
		authOrg := auth.OrganizationEntry{
			ID:          org.ID,
			Name:        org.Name,
			Slug:        org.Slug,
			AccountType: org.AccountType,
			Projects:    []*auth.ProjectEntry{}, // filled in from gram server
		}

		organizations[i] = authOrg

		if org.AccountType != "free" {
			nonFreeOrganizations = append(nonFreeOrganizations, authOrg)
		}
	}

	// If applicable we will only utilize non-free organizations
	if len(nonFreeOrganizations) > 0 {
		organizations = nonFreeOrganizations
	}

	return &CachedUserInfo{
		UserID:          validateResp.User.ID,
		UserWhitelisted: validateResp.User.Whitelisted,
		Email:           validateResp.User.Email,
		Admin:           validateResp.User.Admin,
		Organizations:   organizations,
	}, nil
}

func (s *Manager) InvalidateUserInfoCache(ctx context.Context, userID string) error {
	return s.userInfoCache.Delete(ctx, CachedUserInfo{UserID: userID, UserWhitelisted: true, Organizations: []auth.OrganizationEntry{}, Email: "", Admin: false})
}

func (s *Manager) GetUserInfo(ctx context.Context, userID, sessionID string) (*CachedUserInfo, error) {
	if userInfo, err := s.userInfoCache.Get(ctx, UserInfoCacheKey(userID)); err == nil {
		return &userInfo, nil
	}

	var userInfo *CachedUserInfo
	var err error

	if s.unsafeLocal {
		userInfo, err = s.GetUserInfoFromLocalEnvFile(userID)
	} else {
		userInfo, err = s.GetUserInfoFromSpeakeasy(sessionID)
	}
	if err != nil {
		return nil, err
	}

	if err = s.userInfoCache.Store(ctx, *userInfo); err != nil {
		s.logger.ErrorContext(ctx, "failed to store user info in cache", slog.String("error", err.Error()))
	}

	return userInfo, err
}

func (s *Manager) HasAccessToOrganization(ctx context.Context, organizationID, userID, sessionID string) (*auth.OrganizationEntry, bool) {
	userInfo, err := s.GetUserInfo(ctx, userID, sessionID)
	if err != nil {
		return nil, false
	}

	for _, org := range userInfo.Organizations {
		if org.ID == organizationID {
			return &org, true
		}
	}
	return nil, false
}
