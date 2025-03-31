package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/gen/auth"
	"github.com/speakeasy-api/gram/internal/log"
	"go.uber.org/zap"
)

type localEnvFile map[string]struct {
	UserEmail     string `json:"user_email"`
	Organizations []struct {
		OrganizationID   string `json:"organization_id"`
		OrganizationName string `json:"organization_name"`
		OrganizationSlug string `json:"organization_slug"`
		AccountType      string `json:"account_type"`
	} `json:"organizations"`
}

func GetUserInfoFromLocalEnvFile(userID string) (*CachedUserInfo, error) {
	file, err := os.Open(os.Getenv("LOCAL_ENV_PATH"))
	if err != nil {
		return nil, fmt.Errorf("failed to open local env file: %w", err)
	}
	defer file.Close()

	byteValue, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read local env file: %w", err)
	}

	var data localEnvFile
	if err := json.Unmarshal(byteValue, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal local env file: %w", err)
	}

	userInfo, ok := data[userID]
	if !ok {
		return nil, fmt.Errorf("user with ID %s not found", userID)
	}

	// Convert to CachedUserInfo format
	result := &CachedUserInfo{
		UserID: userID,
		Email:  userInfo.UserEmail,
	}

	// Convert organizations
	result.Organizations = make([]auth.Organization, len(userInfo.Organizations))
	for i, org := range userInfo.Organizations {
		result.Organizations[i] = auth.Organization{
			OrganizationID:   org.OrganizationID,
			OrganizationName: org.OrganizationName,
			OrganizationSlug: org.OrganizationSlug,
			AccountType:      org.AccountType,
		}
	}

	return result, nil
}

func (s *Sessions) PopulateLocalDevDefaultAuthSession(ctx context.Context) (string, error) {
	file, err := os.Open(os.Getenv("LOCAL_ENV_PATH"))
	if err != nil {
		return "", fmt.Errorf("failed to open local env file: %w", err)
	}
	defer file.Close()

	byteValue, err := ioutil.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("failed to read local env file: %w", err)
	}

	var data map[string]struct {
		UserEmail     string `json:"user_email"`
		Organizations []struct {
			OrgID       string `json:"organization_id"`
			OrgName     string `json:"organization_name"`
			OrgSlug     string `json:"organization_slug"`
			AccountType string `json:"account_type"`
		} `json:"organizations"`
	}
	if err := json.Unmarshal(byteValue, &data); err != nil {
		return "", fmt.Errorf("failed to unmarshal local env file: %w", err)
	}

	var gramSession *GramSession

	for userID, userInfo := range data {
		if err := s.InvalidateUserInfoCache(ctx, userID); err != nil {
			log.From(ctx).Warn("failed to invalidate user info cache", zap.Error(err))
		}
		gramSession = &GramSession{
			ID:                   uuid.NewString(),
			UserID:               userID,
			UserEmail:            userInfo.UserEmail,
			ActiveOrganizationID: userInfo.Organizations[0].OrgID,
			ActiveProjectID:      "",
		}
		break
	}

	if gramSession == nil {
		return "", fmt.Errorf("no user found in local env file")
	}

	err = s.sessionCache.Store(ctx, *gramSession)
	if err != nil {
		return "", fmt.Errorf("failed to store session in cache: %w", err)
	}

	return gramSession.ID, nil
}
