package billing

import (
	"context"
	"slices"
)

type ToolCallType string

const (
	ToolCallTypeHTTP        ToolCallType = "http"
	ToolCallTypeFunction    ToolCallType = "function"
	ToolCallTypePlatform    ToolCallType = "platform"
	ToolCallTypeHigherOrder ToolCallType = "higher-order"
	ToolCallTypeExternalMCP ToolCallType = "external-mcp"
)

type ModelUsageSource string

// modelUsageSources accumulates every source registered below, so the full
// set can be iterated without a second hand-maintained list.
var modelUsageSources []ModelUsageSource

func registerModelUsageSource(s ModelUsageSource) ModelUsageSource {
	modelUsageSources = append(modelUsageSources, s)
	return s
}

// The surfaces whose LLM completions run through Gram's server — the
// population billed as tokens under management. Registering a source here is
// its single point of declaration: it names the identifier AND adds it to
// ModelUsageSources, which the billing page's telemetry reads iterate to
// scope analytics to the billed population. Completion telemetry is tagged
// with these values (gram.hook.source); agent-fleet telemetry observed via
// OTEL (claude-code, cursor, codex, …) is not billed and is never registered
// here.
var (
	ModelUsageSourcePlayground = registerModelUsageSource("playground")
	ModelUsageSourceElements   = registerModelUsageSource("elements")
	ModelUsageSourceGram       = registerModelUsageSource("gram")
	ModelUsageSourceSlack      = registerModelUsageSource("slack")

	// ModelUsageSourceRiskAnalysis tags the platform's own risk-policy
	// analysis inference (risk judge, prompt-injection scanner). Scanning is
	// the metered unit of the enterprise TUM contracts — the act of securing
	// observed agent traffic — so this source is registered (billed) even
	// though no end user initiates the completions. The billing page reports
	// it as its own "Risk policy analysis model" section, separate from
	// user-facing completion surfaces.
	//
	// Callers tagging gram or risk-analysis (platform-initiated inference)
	// must also set openrouter.KeyTypeInternal on the completion request so
	// the usage bills against the org's internal OpenRouter key, not the
	// customer-facing chat key's monthly cap. For risk-analysis the unified
	// client rejects a chat-key pairing at request initialization; gram stays
	// convention-only because the completions proxy legitimately accepts a
	// client-supplied gram source on the chat key (Elements).
	ModelUsageSourceRiskAnalysis = registerModelUsageSource("risk-analysis")

	// ModelUsageSourceAssistants tags assistant-runtime completions (the
	// runner sends X-Gram-Source: assistant on every /chat/completions call).
	// Registered per AGE-2850: assistant inference runs on the org's
	// OpenRouter chat key, so leaving it out of the billed population let
	// orgs draw down their monthly key cap with usage TUM never captured.
	// Polar ingestion and the completions credit gate always covered this
	// source — registration is what scopes it into the TUM cycle snapshots
	// and the billing page reads. If the Polar credits meter filters events
	// by source metadata, "assistants" must be included there as well (Polar
	// dashboard config, not in this repo).
	ModelUsageSourceAssistants = registerModelUsageSource("assistants")
)

// ModelUsageSources lists every registered completion surface.
func ModelUsageSources() []ModelUsageSource {
	return slices.Clone(modelUsageSources)
}

// ModelUsageSourceStrings lists every registered completion surface as plain
// strings, the shape the telemetry hook_source filters take.
func ModelUsageSourceStrings() []string {
	out := make([]string, len(modelUsageSources))
	for i, s := range modelUsageSources {
		out[i] = string(s)
	}
	return out
}

type ModelUsageEvent struct {
	OrganizationSlug      string
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
