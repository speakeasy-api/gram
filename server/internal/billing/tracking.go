package billing

import (
	"context"
)

type ToolCallType string

const (
	ToolCallTypeHTTP        ToolCallType = "http"
	ToolCallTypeFunction    ToolCallType = "function"
	ToolCallTypeHigherOrder ToolCallType = "higher-order"
	ToolCallTypeExternalMCP ToolCallType = "external-mcp"
)

type ModelUsageSource string

const (
	ModelUsageSourceChat   ModelUsageSource = "playground"
	ModelUsageSourceAgents ModelUsageSource = "agents"
)

type ModelUsageEvent struct {
	OrganizationID        string
	ProjectID             string
	Source                ModelUsageSource
	ChatID                string
	Model                 string
	InputTokens           int64
	OutputTokens          int64
	TotalTokens           int64
	Cost                  *float64 // Cost in dollars, nil if pricing unavailable
	NativeTokensCached    int64
	NativeTokensReasoning int64
	CacheDiscount         float64
	UpstreamInferenceCost float64
}

type ToolCallUsageEvent struct {
	OrganizationID        string
	RequestBytes          int64
	OutputBytes           int64
	ToolURN               string
	ToolName              string
	ResourceURI           string
	ProjectID             string
	ProjectSlug           *string
	OrganizationSlug      *string
	ToolsetSlug           *string
	ChatID                *string
	MCPURL                *string
	Type                  ToolCallType
	ResponseStatusCode    int
	ToolsetID             *string
	MCPSessionID          *string
	FunctionCPUUsage      *float64
	FunctionMemUsage      *float64
	FunctionExecutionTime *float64
}

type PromptCallUsageEvent struct {
	OrganizationID   string
	RequestBytes     int64
	OutputBytes      int64
	PromptID         *string
	PromptName       string
	ProjectID        string
	ProjectSlug      *string
	OrganizationSlug *string
	ToolsetSlug      *string
	ToolsetID        *string
	ChatID           *string
	MCPURL           *string
	MCPSessionID     *string
}

type PlatformUsageEvent struct {
	OrganizationID      string
	PublicMCPServers    int64
	PrivateMCPServers   int64
	TotalEnabledServers int64
	TotalToolsets       int64
	TotalTools          int64
}

type Tracker interface {
	TrackToolCallUsage(ctx context.Context, event ToolCallUsageEvent)
	TrackPromptCallUsage(ctx context.Context, event PromptCallUsageEvent)
	TrackModelUsage(ctx context.Context, event ModelUsageEvent)
	TrackPlatformUsage(ctx context.Context, events []PlatformUsageEvent)
}
