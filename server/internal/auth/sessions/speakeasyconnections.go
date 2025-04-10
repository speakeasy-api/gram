package sessions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/gen/auth"
)

func (s *Manager) GetUserInfoFromSpeakeasy() (*CachedUserInfo, error) {
	// This function is currently empty and needs to be implemented
	// TODO: This will call GET api.speakeasy.com/v1/gram/info/{userID}
	return nil, fmt.Errorf("not implemented")
}

func (s *Manager) InvalidateUserInfoCache(ctx context.Context, userID string) error {
	return s.userInfoCache.Delete(ctx, CachedUserInfo{UserID: userID, Organizations: []auth.Organization{}, Email: ""})
}

func (s *Manager) GetUserInfo(ctx context.Context, userID string) (*CachedUserInfo, error) {
	if userInfo, err := s.userInfoCache.Get(ctx, UserInfoCacheKey(userID)); err == nil {
		return &userInfo, nil
	}

	var userInfo *CachedUserInfo
	var err error

	if s.unsafeLocal {
		userInfo, err = s.GetUserInfoFromLocalEnvFile(userID)
	} else {
		userInfo, err = s.GetUserInfoFromSpeakeasy()
	}
	if err != nil {
		return nil, err
	}

	if err = s.userInfoCache.Store(ctx, *userInfo); err != nil {
		s.logger.ErrorContext(ctx, "failed to store user info in cache", slog.String("error", err.Error()))
	}

	return userInfo, err
}

func (s *Manager) HasAccessToOrganization(ctx context.Context, userID, organizationID string) (*auth.Organization, bool) {
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
