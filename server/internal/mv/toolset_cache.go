package mv

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

// TODO: We should rename this if we start to include other toolset output primitives like resources
// Alternatively we could have a different cache but seems like a bad idea
type ToolsetTools struct {
	DeploymentID    string
	ToolsetID       string
	Version         int64
	Tools           []*types.Tool
	Resources       []*types.Resource
	SecurityVars    []*types.SecurityVariable
	ServerVars      []*types.ServerVariable
	FunctionEnvVars []*types.FunctionEnvironmentVariable
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
