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
	SessionID            string
	ActiveOrganizationID string
	UserID               string
}

func SessionCacheKey(sessionID string) string {
	return "sessions:" + sessionID
}

func (s Session) CacheKey() string {
	return SessionCacheKey(s.SessionID)
}

func (s Session) AdditionalCacheKeys() []string {
	return []string{}
}

type CachedUserInfo struct {
	UserID          string
	UserWhitelisted bool
	Admin           bool
	Email           string
	Organizations   []auth.OrganizationEntry
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
