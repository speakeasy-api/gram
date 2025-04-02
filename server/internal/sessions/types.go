package sessions

import (
	"time"

	"github.com/speakeasy-api/gram/gen/auth"
)

const (
	sessionCacheExpiry  = 15 * 24 * time.Hour
	userInfoCacheExpiry = 15 * time.Minute
)

type Session struct {
	ID                   string
	ActiveOrganizationID string
	UserID               string
	UserEmail            string
}

func SessionCacheKey(ID string) string {
	return "sessions:" + ID
}

func (s Session) CacheKey() string {
	return SessionCacheKey(s.ID)
}

func (s Session) AdditionalCacheKeys() []string {
	return []string{}
}

type CachedUserInfo struct {
	UserID        string
	Email         string
	Organizations []auth.Organization
}

func UserInfoCacheKey(userID string) string {
	return "speakeasyUserInfo:" + userID
}

func (c CachedUserInfo) CacheKey() string {
	return UserInfoCacheKey(c.UserID)
}

func (c CachedUserInfo) AdditionalCacheKeys() []string {
	return []string{}
}
