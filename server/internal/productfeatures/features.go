package productfeatures

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

type Feature string

const (
	FeatureLogs       Feature = "logs"
	FeatureToolIOLogs Feature = "tool_io_logs"
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
