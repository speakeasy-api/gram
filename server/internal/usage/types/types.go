package usage_types

import (
	"context"

	polarComponents "github.com/polarsource/polar-go/models/components"
	gen "github.com/speakeasy-api/gram/server/gen/usage"
)

type ToolCallType string

const (
	ToolCallType_HTTP        ToolCallType = "http"
	ToolCallType_HigherOrder ToolCallType = "higher-order"
)

type ToolCallUsageEvent struct {
	OrganizationID   string
	RequestBytes     int64
	OutputBytes      int64
	ToolID           string
	ToolName         string
	ProjectID        string
	ProjectSlug      *string
	OrganizationSlug *string
	ToolsetSlug      *string
	ChatID           *string
	MCPURL           *string
	Type             ToolCallType
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
	ChatID           *string
	MCPURL           *string
}

type PlatformUsageEvent struct {
	OrganizationID    string
	PublicMCPServers  int64
	PrivateMCPServers int64
	TotalToolsets     int64
	TotalTools        int64
}

type Tier string

const (
	Tier_Free     Tier = "free"
	Tier_Business Tier = "business"
)

type CustomerState struct {
	OrganizationID      string
	Tier                Tier
	PeriodUsage         *gen.PeriodUsage
}

// CustomerStateProvider provides customer state information for organization account type determination
type CustomerStateProvider interface {
	GetCustomerState(ctx context.Context, orgID string) (*CustomerState, error)
}

type UsageClient interface {
	TrackToolCallUsage(ctx context.Context, event ToolCallUsageEvent)
	TrackPromptCallUsage(ctx context.Context, event PromptCallUsageEvent)
	TrackPlatformUsage(ctx context.Context, event PlatformUsageEvent)
	GetPeriodUsage(ctx context.Context, orgID string) (*gen.PeriodUsage, error)
	CreateCheckout(ctx context.Context, orgID string, serverURL string) (string, error)
	CreateCustomerSession(ctx context.Context, orgID string) (string, error)
	GetGramFreeTierProduct(ctx context.Context) (*polarComponents.Product, error)
	GetGramProProduct(ctx context.Context) (*polarComponents.Product, error)
	CustomerStateProvider
}