package mv

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

type ToolsetBaseContents struct {
	DeploymentID    string
	ToolsetID       string
	Version         int64
	Tools           []*types.Tool
	Resources       []*types.Resource
	SecurityVars    []*types.SecurityVariable
	ServerVars      []*types.ServerVariable
	FunctionEnvVars []*types.FunctionEnvironmentVariable
	ExternalMCPHeaderDefinitions []*types.ExternalMCPHeaderDefinition
}

var _ cache.CacheableObject[ToolsetBaseContents] = (*ToolsetBaseContents)(nil)

func (c ToolsetBaseContents) CacheKey() string {
	return ToolsetCacheKey(c.ToolsetID, c.DeploymentID, c.Version)
}

func ToolsetCacheKey(toolsetID string, deploymentID string, version int64) string {
	return fmt.Sprintf("toolset:%s:%s:%d", deploymentID, toolsetID, version)
}

func (c ToolsetBaseContents) TTL() time.Duration {
	return 1 * time.Hour
}

func (c ToolsetBaseContents) AdditionalCacheKeys() []string {
	return []string{}
}
