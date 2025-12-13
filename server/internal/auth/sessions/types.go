package sessions

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

const (
	sessionCacheExpiry  = 15 * 24 * time.Hour
	userInfoCacheExpiry = 15 * time.Minute
)

var _ cache.CacheableObject[Session] = (*Session)(nil)

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

func (s Session) TTL() time.Duration {
	return sessionCacheExpiry
}

var _ cache.CacheableObject[CachedUserInfo] = (*CachedUserInfo)(nil)

type CachedUserInfo struct {
	UserID             string
	UserWhitelisted    bool
	Admin              bool
	Email              string
	DisplayName        *string
	PhotoURL           *string
	UserPylonSignature *string
	Organizations      []auth.OrganizationEntry
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

func (c CachedUserInfo) TTL() time.Duration {
	return userInfoCacheExpiry
}

type AuthURLParams struct {
	CallbackURL     string
	Scope           string
	State           string
	ClientID        string
	ScopesSupported []string
}
