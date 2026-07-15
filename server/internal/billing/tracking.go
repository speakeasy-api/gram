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

// The surfaces whose LLM completions run through Gram's server — inference
// GRAM spends (from the org's OpenRouter keys), as opposed to the agent
// traffic the platform merely observes. Registering a source here is its
// single point of declaration: it names the identifier AND adds it to
// ModelUsageSources. Tokens-under-management billing counts the OBSERVED
// agent traffic (claude-code, cursor, codex sessions seen via OTEL/hooks)
// and uses this registry as its EXCLUSION list (see
// GramHostedHookSourceStrings): Gram-spent inference — whether reactive
// (risk-policy judges) or user-initiated (playground, elements) — is never
// tokens under management.
var (
	ModelUsageSourcePlayground = registerModelUsageSource("playground")
	ModelUsageSourceElements   = registerModelUsageSource("elements")
	ModelUsageSourceGram       = registerModelUsageSource("gram")
	ModelUsageSourceSlack      = registerModelUsageSource("slack")

	// ModelUsageSourceRiskAnalysis tags the platform's own risk-policy
	// analysis inference (risk judge, prompt-injection scanner) — the
	// textbook case of tokens Gram spends REACTING to observed traffic, so
	// it must never count as tokens under management.
	//
	// Callers tagging gram or risk-analysis (platform-initiated inference)
	// must also set openrouter.KeyTypeInternal on the completion request so
	// the usage bills against the org's internal OpenRouter key, not the
	// customer-facing chat key's monthly cap. For risk-analysis the unified
	// client rejects a chat-key pairing at request initialization; gram stays
	// convention-only because the completions proxy legitimately accepts a
	// client-supplied gram source on the chat key (Elements).
	ModelUsageSourceRiskAnalysis = registerModelUsageSource("risk-analysis")
)

// ModelUsageSourceAssistants tags assistants completions in telemetry but is
// deliberately NOT registered above: registration drives OpenRouter key
// conventions and Polar metering exemptions for Gram-run surfaces, and
// assistants inference is Speakeasy-covered today. It is still Gram-spent
// inference, so GramHostedHookSourceStrings appends it to the TUM exclusion
// list explicitly.
const ModelUsageSourceAssistants ModelUsageSource = "assistants"

// The platform-initiated risk-analysis judges are likewise unregistered:
// their completions are tagged and billed under ModelUsageSourceRiskAnalysis.
// These values exist only as BYOK key slots, so a project can override the
// key paying for the prompt-based risk-policy judge and the prompt-injection
// classifier independently of each other and of the assistant.
const (
	ModelUsageSourceRiskPolicy      ModelUsageSource = "risk-policy"
	ModelUsageSourcePromptInjection ModelUsageSource = "prompt-injection"
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

// GramHostedHookSourceStrings lists every hook_source value Gram-server-run
// completions are tagged with: the registered surfaces plus the unregistered
// assistants tag, plus the empty string for rows recorded before Gram
// completions were tagged (observed agent traffic is always tagged at ingest — claude-code,
// cursor, codex — so an untagged row can only be Gram-era history). This is
// the tokens-under-management EXCLUSION list — billing counts observed agent
// traffic, and everything Gram itself spends (reactive scanning inference
// and user-initiated hosted chat alike) is out of scope.
func GramHostedHookSourceStrings() []string {
	return append(ModelUsageSourceStrings(), string(ModelUsageSourceAssistants), "")
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
