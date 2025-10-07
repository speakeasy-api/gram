package mv

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

type CachedToolset struct {
	DeploymentID string
	ToolsetID    string
	Version   int64
	*types.Toolset
}

var _ cache.CacheableObject[CachedToolset] = (*CachedToolset)(nil)

func (c CachedToolset) CacheKey() string {
	return ToolsetCacheKey(c.ToolsetID, c.DeploymentID, c.Version)
}

func ToolsetCacheKey(toolsetID string, deploymentID string, version int64) string {
	return fmt.Sprintf("toolset:%s:%s:%d", deploymentID, toolsetID, version)
}

func (c CachedToolset) TTL() time.Duration {
	return 1 * time.Hour
}

func (c CachedToolset) AdditionalCacheKeys() []string {
	return []string{}
}
