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
)

type localEnvFile map[string]struct {
	UserEmail     string `json:"user_email"`
	Organizations []struct {
		OrgID       string `json:"org_id"`
		OrgName     string `json:"org_name"`
		OrgSlug     string `json:"org_slug"`
		AccountType string `json:"account_type"`
		Projects    []struct {
			ProjectID string `json:"project_id"`
		} `json:"projects"`
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
			OrgID:       org.OrgID,
			OrgName:     org.OrgName,
			OrgSlug:     org.OrgSlug,
			AccountType: org.AccountType,
			Projects:    make([]*auth.Project, len(org.Projects)),
		}
		// Convert projects
		for j, proj := range org.Projects {
			result.Organizations[i].Projects[j] = &auth.Project{
				ProjectID: proj.ProjectID,
			}
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
			OrgID       string `json:"org_id"`
			OrgName     string `json:"org_name"`
			OrgSlug     string `json:"org_slug"`
			AccountType string `json:"account_type"`
			Projects    []struct {
				ProjectID string `json:"project_id"`
			} `json:"projects"`
		} `json:"organizations"`
	}
	if err := json.Unmarshal(byteValue, &data); err != nil {
		return "", fmt.Errorf("failed to unmarshal local env file: %w", err)
	}

	var gramSession *GramSession

	for userID, userInfo := range data {
		gramSession = &GramSession{
			ID:                   uuid.NewString(),
			UserID:               userID,
			UserEmail:            userInfo.UserEmail,
			ActiveOrganizationID: userInfo.Organizations[0].OrgID,
			ActiveProjectID:      userInfo.Organizations[0].Projects[0].ProjectID,
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
