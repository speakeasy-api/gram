package sessions

import "time"

const (
	sessionCacheExpiry = 15 * 24 * time.Hour
)

type GramSession struct {
	ID, ActiveOrganizationID, ActiveProjectID, UserID, UserEmail string
}

func DocumentCacheKey(ID string) string {
	return "gramSession:" + ID
}

func (s GramSession) CacheKey() string {
	return DocumentCacheKey(s.ID)
}

func (s GramSession) AdditionalCacheKeys() []string {
	return []string{}
}
