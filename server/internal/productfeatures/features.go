package productfeatures

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

type Feature string

const (
	FeatureLogs                  Feature = "logs"
	FeatureToolIOLogs            Feature = "tool_io_logs"
	FeatureRBAC                  Feature = "rbac"
	FeatureSessionCapture        Feature = "session_capture"
	FeatureAuthzChallengeLogging Feature = "authz_challenge_logging"
	FeatureAssistantMemory       Feature = "assistant_memory"
)

type FeatureCache struct {
	OrganizationID string
	Feature        Feature
	Enabled        bool
}

var _ cache.CacheableObject[FeatureCache] = (*FeatureCache)(nil)

func (f FeatureCache) CacheKey() string {
	return FeatureCacheKey(f.OrganizationID, f.Feature)
}

func FeatureCacheKey(organizationID string, feature Feature) string {
	return fmt.Sprintf("feature:%s:%s", organizationID, string(feature))
}

func (f FeatureCache) TTL() time.Duration {
	return 15 * time.Minute
}

func (c FeatureCache) AdditionalCacheKeys() []string {
	return []string{}
}

// SessionCaptureExclusionsCache is the per-organization set of Gram user IDs
// excluded from session capture. Cached as a single blob so hook-path lookups
// avoid hitting Postgres on every event.
type SessionCaptureExclusionsCache struct {
	OrganizationID string
	UserIDs        []string
}

var _ cache.CacheableObject[SessionCaptureExclusionsCache] = (*SessionCaptureExclusionsCache)(nil)

func (s SessionCaptureExclusionsCache) CacheKey() string {
	return SessionCaptureExclusionsCacheKey(s.OrganizationID)
}

func SessionCaptureExclusionsCacheKey(organizationID string) string {
	return fmt.Sprintf("session_capture_exclusions:%s", organizationID)
}

func (s SessionCaptureExclusionsCache) TTL() time.Duration {
	return 15 * time.Minute
}

func (s SessionCaptureExclusionsCache) AdditionalCacheKeys() []string {
	return []string{}
}
