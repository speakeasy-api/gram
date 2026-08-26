package sessions

import (
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

const userInfoCacheExpiry = 15 * time.Minute

var _ cache.CacheableObject[Session] = (*Session)(nil)

type Session struct {
	SessionID             string
	ActiveOrganizationID  string
	UserID                string
	WorkOSSessionID       string
	ImpersonatorEmail     string
	SupportOrganizationID string
	SupportExpiresAt      time.Time
}

func SessionCacheKey(sessionID string) string {
	// Version the namespace so sessions created under the previous, longer
	// expiry policy cannot bypass the 72-hour idle timeout after rollout.
	return "sessions:v2:" + sessionID
}

func (s Session) CacheKey() string {
	return SessionCacheKey(s.SessionID)
}

func (s Session) TTL() time.Duration {
	if !s.SupportExpiresAt.IsZero() {
		return time.Until(s.SupportExpiresAt)
	}
	return constants.SessionIdleTimeout
}

var _ cache.CacheableObject[CachedUserInfo] = (*CachedUserInfo)(nil)

// Organization is an internal representation of a user's organization membership,
// populated from the database. This is distinct from the Goa-generated
// auth.OrganizationEntry which is the API response type.
type Organization struct {
	ID                 string
	Name               string
	Slug               string
	WorkosID           *string
	UserWorkspaceSlugs []string
	SSOEnabled         bool
	SCIMEnabled        bool
}

type CachedUserInfo struct {
	UserID             string
	Admin              bool
	Email              string
	DisplayName        *string
	PhotoURL           *string
	UserPylonSignature *string
	Organizations      []Organization
}

func UserInfoCacheKey(userID string) string {
	return "userInfo:" + userID
}

func (c CachedUserInfo) CacheKey() string {
	return UserInfoCacheKey(c.UserID)
}

func (c CachedUserInfo) TTL() time.Duration {
	return userInfoCacheExpiry
}
