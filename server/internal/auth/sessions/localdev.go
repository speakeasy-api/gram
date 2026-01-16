package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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
        "account_type": "enterprise"
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

func NewUnsafeManager(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client, suffix cache.Suffix, localEnvPath string, billingRepo billing.Repository) (*Manager, error) {
	logger = logger.With(attr.SlogComponent("sessions"))

	raw := unsafeSessionData
	if localEnvPath != "" {
		file, err := os.Open(filepath.Clean(localEnvPath))
		if err != nil {
			logger.WarnContext(context.Background(), "failed to open local env file, defaulting to inlined data (localdev.go)", attr.SlogError(err))
		} else if file != nil {
			defer o11y.LogDefer(context.Background(), logger, func() error {
				return file.Close()
			})

			raw, err = io.ReadAll(file)
			if err != nil {
				return nil, fmt.Errorf("failed to read local env file: %w", err)
			}
		}
	}

	var data localEnvFile
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal local env file: %w", err)
	}

	fakePylon, err := pylon.NewPylon(logger, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create fake pylon: %w", err)
	}

	fakePosthog := posthog.New(context.Background(), logger, "test-posthog-key", "test-posthog-host", "")

	return &Manager{
		logger:                 logger.With(attr.SlogComponent("sessions")),
		sessionCache:           cache.NewTypedObjectCache[Session](logger.With(attr.SlogCacheNamespace("session")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		userInfoCache:          cache.NewTypedObjectCache[CachedUserInfo](logger.With(attr.SlogCacheNamespace("user_info")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		localEnvFile:           data,
		unsafeLocal:            true,
		speakeasyServerAddress: "",
		speakeasySecretKey:     "",
		orgRepo:                orgRepo.New(db),
		userRepo:               userRepo.New(db),
		pylon:                  fakePylon,
		posthog:                fakePosthog,
		billingRepo:            billingRepo,
	}, nil
}

func (s *Manager) GetUserInfoFromLocalEnvFile(userID string) (*CachedUserInfo, error) {
	userInfo, ok := s.localEnvFile[userID]
	if !ok {
		return nil, fmt.Errorf("user with ID %s not found", userID)
	}

	// Convert to CachedUserInfo format
	result := &CachedUserInfo{
		UserID:             userID,
		UserWhitelisted:    true,
		Email:              userInfo.UserEmail,
		Admin:              userInfo.Admin,
		DisplayName:        nil,
		PhotoURL:           nil,
		UserPylonSignature: nil,
		Organizations:      make([]auth.OrganizationEntry, len(userInfo.Organizations)),
	}

	// Convert organizations
	for i, org := range userInfo.Organizations {
		result.Organizations[i] = auth.OrganizationEntry{
			ID:                 org.OrganizationID,
			Name:               org.OrganizationName,
			Slug:               org.OrganizationSlug,
			SsoConnectionID:    nil,
			UserWorkspaceSlugs: []string{},
			Projects:           []*auth.ProjectEntry{},
		}
	}

	return result, nil
}

func (s *Manager) PopulateLocalDevDefaultAuthSession(ctx context.Context) (string, error) {
	if !s.unsafeLocal {
		return "", fmt.Errorf("failed to populate local session in non-local environment")
	}

	var gramSession *Session

	for userID, userInfo := range s.localEnvFile {
		if err := s.InvalidateUserInfoCache(ctx, userID); err != nil {
			s.logger.WarnContext(ctx, "failed to invalidate user info cache", attr.SlogError(err))
		}

		if _, err := s.userRepo.UpsertUser(ctx, userRepo.UpsertUserParams{
			ID:          userID,
			Email:       userInfo.UserEmail,
			DisplayName: "stubbed user",
			PhotoUrl:    conv.PtrToPGText(nil),
			Admin:       userInfo.Admin,
		}); err != nil {
			return "", fmt.Errorf("failed to upsert user: %w", err)
		}

		_, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
			ID:              userInfo.Organizations[0].OrganizationID,
			Name:            userInfo.Organizations[0].OrganizationName,
			Slug:            userInfo.Organizations[0].OrganizationSlug,
			SsoConnectionID: conv.PtrToPGText(nil),
		})
		if err != nil {
			return "", fmt.Errorf("failed to upsert organization metadata: %w", err)
		}

		if !userInfo.Admin {
			if _, err := s.orgRepo.UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
				OrganizationID: userInfo.Organizations[0].OrganizationID,
				UserID:         userID,
			}); err != nil {
				return "", fmt.Errorf("failed to upsert organization user relationship: %w", err)
			}
		}

		accountType := userInfo.Organizations[0].AccountType
		if !slices.Contains([]string{"free", "pro", "enterprise"}, accountType) {
			accountType = "free"
		}
		err = s.orgRepo.SetAccountType(ctx, orgRepo.SetAccountTypeParams{
			ID:              userInfo.Organizations[0].OrganizationID,
			GramAccountType: accountType,
		})
		if err != nil {
			return "", fmt.Errorf("failed to set account type: %w", err)
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
