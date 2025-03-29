package sessions

import (
	"context"
	"fmt"
	"os"

	"github.com/speakeasy-api/gram/gen/auth"
	"github.com/speakeasy-api/gram/internal/log"
)

func (s *Sessions) GetUserInfoFromSpeakeasy() (*CachedUserInfo, error) {
	// This function is currently empty and needs to be implemented
	// TODO: This will call GET api.speakeasy.com/v1/gram/info/{userID}
	return nil, fmt.Errorf("not implemented")
}

func (s *Sessions) InvalidateUserInfoCache(ctx context.Context, userID string) error {
	return s.userInfoCache.Delete(ctx, CachedUserInfo{UserID: userID})
}

func (s *Sessions) GetUserInfo(ctx context.Context, userID string) (*CachedUserInfo, error) {
	if userInfo, err := s.userInfoCache.Get(ctx, UserInfoCacheKey(userID)); err == nil {
		return &userInfo, nil
	}

	var userInfo *CachedUserInfo
	var err error

	if os.Getenv("GRAM_ENVIRONMENT") == "local" {
		userInfo, err = GetUserInfoFromLocalEnvFile(userID)
	} else {
		userInfo, err = s.GetUserInfoFromSpeakeasy()
	}
	if err != nil {
		return nil, err
	}

	if err = s.userInfoCache.Store(ctx, *userInfo); err != nil {
		log.From(ctx).Error("failed to store user info in cache", "error", err)
	}

	return userInfo, err
}

func (s *Sessions) HasAccessToOrganization(ctx context.Context, userID, organizationID string) (*auth.Organization, bool) {
	userInfo, err := s.GetUserInfo(ctx, userID)
	if err != nil {
		return nil, false
	}

	for _, org := range userInfo.Organizations {
		if org.OrganizationID == organizationID {
			return &org, true
		}
	}
	return nil, false
}
