package sessions

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/speakeasy-api/gram/server/internal/users/stub"
)

func NewUnsafeManager(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client, suffix cache.Suffix, localEnvPath string, billingRepo billing.Repository) (*Manager, error) {
	logger = logger.With(attr.SlogComponent("sessions"))
	data, err := stub.UnmarshalLocalUser(localEnvPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load stubbed local user: %w", err)
	}

	fakePylon, err := pylon.NewPylon(logger, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create fake pylon: %w", err)
	}

	fakePosthog := posthog.New(context.Background(), logger, "test-posthog-key", "test-posthog-host")

	return &Manager{
		logger:                 logger.With(attr.SlogComponent("sessions")),
		sessionCache:           cache.NewTypedObjectCache[Session](logger.With(attr.SlogCacheNamespace("session")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		userInfoCache:          cache.NewTypedObjectCache[CachedUserInfo](logger.With(attr.SlogCacheNamespace("user_info")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		stubbedUser:            *data,
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
	userInfo, ok := s.stubbedUser[userID]
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

	for userID, userInfo := range s.stubbedUser {
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
			ID:   userInfo.Organizations[0].OrganizationID,
			Name: userInfo.Organizations[0].OrganizationName,
			Slug: userInfo.Organizations[0].OrganizationSlug,
		})
		if err != nil {
			return "", fmt.Errorf("failed to upsert organization metadata: %w", err)
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
