package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/gen/auth"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/o11y"
)

var unsafeSessionData = []byte(`
{
  "1245": {
    "user_email": "user@example.com",
    "admin": false,
    "organizations": [
      {
        "organization_id": "550e8400-e29b-41d4-a716-446655440000",
        "organization_name": "Organization 123",
        "organization_slug": "organization-123",
        "account_type": "business"
      },
      {
        "organization_id": "e0395991-d5c5-4c2f-8c3b-4eae305524ed",
        "organization_name": "Organization 456",
        "organization_slug": "organization-456",
        "account_type": "enterprise"
      }
    ]
  }
}
`)

func NewUnsafeManager(logger *slog.Logger, redisClient *redis.Client, suffix cache.Suffix, localEnvPath string) (*Manager, error) {
	raw := unsafeSessionData
	if localEnvPath != "" {
		file, err := os.Open(filepath.Clean(localEnvPath))
		if err != nil {
			return nil, fmt.Errorf("failed to open local env file: %w", err)
		}
		defer o11y.LogDefer(context.Background(), logger, func() error {
			return file.Close()
		})

		raw, err = io.ReadAll(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read local env file: %w", err)
		}
	}

	var data localEnvFile
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal local env file: %w", err)
	}

	return &Manager{
		logger:                 logger.With(slog.String("component", "sessions")),
		sessionCache:           cache.New[Session](logger.With(slog.String("cache", "session")), redisClient, sessionCacheExpiry, cache.SuffixNone),
		userInfoCache:          cache.New[CachedUserInfo](logger.With(slog.String("cache", "user_info")), redisClient, userInfoCacheExpiry, cache.SuffixNone),
		localEnvFile:           data,
		unsafeLocal:            true,
		speakeasyServerAddress: "",
		speakeasySecretKey:     "",
	}, nil
}

func (s *Manager) GetUserInfoFromLocalEnvFile(userID string) (*CachedUserInfo, error) {
	userInfo, ok := s.localEnvFile[userID]
	if !ok {
		return nil, fmt.Errorf("user with ID %s not found", userID)
	}

	// Convert to CachedUserInfo format
	result := &CachedUserInfo{
		UserID:          userID,
		UserWhitelisted: true,
		Email:           userInfo.UserEmail,
		Admin:           userInfo.Admin,
		Organizations:   make([]auth.OrganizationEntry, len(userInfo.Organizations)),
	}

	// Convert organizations
	for i, org := range userInfo.Organizations {
		result.Organizations[i] = auth.OrganizationEntry{
			ID:          org.OrganizationID,
			Name:        org.OrganizationName,
			Slug:        org.OrganizationSlug,
			AccountType: org.AccountType,
			Projects:    []*auth.ProjectEntry{},
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
