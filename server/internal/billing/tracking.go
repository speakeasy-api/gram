package billing

import (
	"context"
)

type ToolCallType string

const (
	ToolCallTypeHTTP        ToolCallType = "http"
	ToolCallTypeFunction    ToolCallType = "function"
	ToolCallTypeHigherOrder ToolCallType = "higher-order"
)

type ToolCallUsageEvent struct {
	OrganizationID   string
	RequestBytes     int64
	OutputBytes      int64
	ToolID           string
	ToolName         string
	ResourceURI      string
	ProjectID        string
	ProjectSlug      *string
	OrganizationSlug *string
	ToolsetSlug      *string
	ChatID           *string
	MCPURL           *string
	Type             ToolCallType
	ToolsetID        *string
	MCPSessionID     *string
	FunctionCPUUsage *int64
	FunctionMemUsage *int64
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
	TrackPlatformUsage(ctx context.Context, events []PlatformUsageEvent)
}
