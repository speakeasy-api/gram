package sessions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/gen/auth"
)

func (s *Manager) GetUserInfoFromLocalEnvFile(userID string) (*CachedUserInfo, error) {
	userInfo, ok := s.localEnvFile[userID]
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

func (s *Manager) PopulateLocalDevDefaultAuthSession(ctx context.Context) (string, error) {
	var gramSession *Session

	for userID, userInfo := range s.localEnvFile {
		if err := s.InvalidateUserInfoCache(ctx, userID); err != nil {
			s.logger.WarnContext(ctx, "failed to invalidate user info cache", slog.String("error", err.Error()))
		}
		gramSession = &Session{
			SessionID:            uuid.NewString(),
			UserID:               userID,
			ActiveOrganizationID: userInfo.Organizations[0].OrganizationID,
		}
		break
	}

	if gramSession == nil {
		return "", fmt.Errorf("no user found in local env file")
	}

	if err := s.sessionCache.Store(ctx, *gramSession); err != nil {
		return "", fmt.Errorf("failed to store session in cache: %w", err)
	}

	return gramSession.SessionID, nil
}
