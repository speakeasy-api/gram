package sessions

import (
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

const userInfoCacheExpiry = 15 * time.Minute

var _ cache.CacheableObject[Session] = (*Session)(nil)

const AccessLifetime = 10 * time.Minute
const RefreshIdleLifetime = 72 * time.Hour

type Session struct {
	// RefreshHash links the access record to refresh state, never its bearer secret.
	RefreshHash           string    `json:"RefreshHash"`
	ExpiresAt             time.Time `json:"ExpiresAt"`
	SessionID             string    `json:"SessionID"`
	ActiveOrganizationID  string    `json:"ActiveOrganizationID"`
	UserID                string    `json:"UserID"`
	WorkOSSessionID       string    `json:"WorkOSSessionID"`
	ImpersonatorEmail     string    `json:"ImpersonatorEmail"`
	SupportOrganizationID string    `json:"SupportOrganizationID"`
	SupportExpiresAt      time.Time `json:"SupportExpiresAt"`
}

func SessionCacheKey(sessionID string) string {
	// Cut over from legacy long-lived access credentials.
	return "sessions:v3:" + sessionID
}

func (s Session) CacheKey() string {
	return SessionCacheKey(s.SessionID)
}

func (s Session) TTL() time.Duration {
	ttl := AccessLifetime
	if !s.ExpiresAt.IsZero() {
		ttl = min(ttl, time.Until(s.ExpiresAt))
	}
	if !s.SupportExpiresAt.IsZero() {
		ttl = min(ttl, time.Until(s.SupportExpiresAt))
	}
	return ttl
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
