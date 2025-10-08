package mv

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

type ToolsetTools struct {
	DeploymentID string
	ToolsetID    string
	Version      int64
	Tools        []*types.Tool
	SecurityVars []*types.SecurityVariable
	ServerVars   []*types.ServerVariable
}

var _ cache.CacheableObject[ToolsetTools] = (*ToolsetTools)(nil)

func (c ToolsetTools) CacheKey() string {
	return ToolsetCacheKey(c.ToolsetID, c.DeploymentID, c.Version)
}

func ToolsetCacheKey(toolsetID string, deploymentID string, version int64) string {
	return fmt.Sprintf("toolset:%s:%s:%d", deploymentID, toolsetID, version)
}

func (c ToolsetTools) TTL() time.Duration {
	return 1 * time.Hour
}

func (c ToolsetTools) AdditionalCacheKeys() []string {
	return []string{}
}
