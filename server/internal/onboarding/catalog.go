package onboarding

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/onboarding/repo"
)

// UseCase is the single outcome an admin picks after describing their stack.
// Onboarding is done once the use case has evidence behind it.
type UseCase string

const (
	UseCaseObservability UseCase = "observability"
	UseCaseCostTracking  UseCase = "cost-tracking"
	UseCaseSecurity      UseCase = "security"
	UseCaseMCPGateway    UseCase = "mcp-gateway"
)

// UseCases lists every use case in the order the wizard offers them.
var UseCases = []UseCase{UseCaseObservability, UseCaseCostTracking, UseCaseSecurity, UseCaseMCPGateway}

// Name is the label the dashboard shows for the use case.
func (u UseCase) Name() string {
	switch u {
	case UseCaseObservability:
		return "Observability"
	case UseCaseCostTracking:
		return "Spend Controls"
	case UseCaseSecurity:
		return "Security & Policies"
	case UseCaseMCPGateway:
		return "Distribution"
	default:
		return string(u)
	}
}

// MDMVendor is the device-management vendor an organization declares. The
// vendor alone is captured: the existing device integrations already say what
// each one can push.
type MDMVendor string

const (
	MDMJamf   MDMVendor = "jamf"
	MDMIntune MDMVendor = "intune"
	MDMIru    MDMVendor = "iru"
	MDMNone   MDMVendor = "none"
)

// MDMVendors lists every vendor in the order the wizard offers them.
var MDMVendors = []MDMVendor{MDMJamf, MDMIntune, MDMIru, MDMNone}

// Name is the label the dashboard shows for the vendor.
func (m MDMVendor) Name() string {
	switch m {
	case MDMJamf:
		return "Jamf"
	case MDMIntune:
		return "Microsoft Intune"
	case MDMIru:
		return "Iru (formerly Kandji)"
	case MDMNone:
		return "No MDM"
	default:
		return string(m)
	}
}

// Managed reports whether the vendor can push configuration to devices.
func (m MDMVendor) Managed() bool {
	return m == MDMJamf || m == MDMIntune || m == MDMIru
}

// Provider slugs. A provider sells plans and makes products; the plan an
// organization declares for a provider applies to every product of it.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
	ProviderCursor    = "cursor"
	ProviderGitHub    = "github"
	ProviderOpenCode  = "opencode"
	ProviderOpenClaw  = "openclaw"
	ProviderPi        = "pi"
	ProviderLiteLLM   = "litellm"
)

// Product slugs the next-step rules refer to.
const (
	ProductClaudeCodeCLI     = "claude-code-cli"
	ProductClaudeCodeDesktop = "claude-code-desktop"
	ProductClaudeCodeWeb     = "claude-code-web"
	ProductCowork            = "cowork"
	ProductClaudeTag         = "claude-tag"
	ProductClaudeChat        = "claude-chat"
	ProductCodex             = "codex"
	ProductCodexWeb          = "codex-web"
	ProductChatGPT           = "chatgpt"
	ProductChatGPTWork       = "chatgpt-work"
	ProductCursor            = "cursor"
	ProductOpenCode          = "opencode"
	ProductCopilotCLI        = "copilot-cli"
	ProductOpenClaw          = "openclaw"
	ProductPi                = "pi"
	ProductLiteLLM           = "litellm"
)

// Plan slugs the next-step rules refer to.
const (
	PlanAnthropicPro        = "anthropic-pro"
	PlanAnthropicMax        = "anthropic-max"
	PlanAnthropicTeam       = "anthropic-team"
	PlanAnthropicEnterprise = "anthropic-enterprise"
	PlanOpenAIPlus          = "openai-plus"
	PlanOpenAIPro           = "openai-pro"
	PlanOpenAIBusiness      = "openai-business"
	PlanOpenAIEnterprise    = "openai-enterprise"
	PlanCursorPro           = "cursor-pro"
	PlanCursorTeams         = "cursor-teams"
	PlanCursorEnterprise    = "cursor-enterprise"
	PlanCopilotBusiness     = "copilot-business"
	PlanCopilotEnterprise   = "copilot-enterprise"
)

// Technique slugs: the ways the platform can cover a product.
const (
	TechniqueClaudeHooks             = "claude-hooks"
	TechniqueCursorHooks             = "cursor-hooks"
	TechniqueCodexHooks              = "codex-hooks"
	TechniqueUnifiedIngest           = "unified-ingest"
	TechniqueOTELExport              = "otel-export"
	TechniquePluginDistribution      = "plugin-distribution"
	TechniqueManagedSettings         = "managed-settings"
	TechniqueDeviceAgent             = "device-agent"
	TechniqueAnthropicInferenceHooks = "anthropic-inference-hooks"
	TechniqueAnthropicComplianceAPI  = "anthropic-compliance-api"
	TechniqueAnthropicAdminAnalytics = "anthropic-admin-analytics"
	TechniqueOpenAIComplianceAPI     = "openai-compliance-api"
	TechniqueCodexCloudLogs          = "codex-cloud-logs"
	TechniqueChatGPTConversations    = "chatgpt-conversations"
	TechniqueCursorAdminAPI          = "cursor-admin-api"
	TechniqueLiteLLMGuardrail        = "litellm-guardrail-otlp"
	TechniqueMCPGateway              = "mcp-gateway"
	TechniqueMDMIntegration          = "mdm-integration"
	TechniqueSpendRules              = "spend-rules"
	TechniqueClaudeTagHooks          = "claude-tag-hooks"
	TechniqueRiskPolicies            = "risk-policies"
)

// Capability slugs: what a technique delivers, grouped by use case.
const (
	CapabilitySessions          = "sessions"
	CapabilityToolCalls         = "tool-calls"
	CapabilityTranscripts       = "transcripts"
	CapabilityMCPInventory      = "mcp-inventory"
	CapabilityTokens            = "tokens"
	CapabilityCost              = "cost"
	CapabilityBudgets           = "budgets"
	CapabilityPolicyEnforcement = "policy-enforcement"
	CapabilityFindings          = "findings"
	CapabilityShadowAI          = "shadow-ai"
	CapabilityShadowMCP         = "shadow-mcp"
	CapabilityGatewayTraffic    = "gateway-traffic"
	CapabilityPluginInstall     = "plugin-install"
)

// ProviderSpec is one vendor in the catalog.
type ProviderSpec struct {
	Slug string
	Name string
}

// PlanSpec is one plan a provider sells.
type PlanSpec struct {
	Slug     string
	Provider string
	Name     string
}

// TechniqueSupport says a product supports a technique, optionally only on
// some of its provider's plans. An empty Plans list means every plan.
type TechniqueSupport struct {
	Technique string
	Plans     []string
}

// ProductSpec is one product surface in the catalog.
type ProductSpec struct {
	Slug     string
	Name     string
	Provider string

	// SourceIDs are the hook_source / ingest adapter ids whose events belong
	// to this product. Evidence checks filter on them.
	SourceIDs []string

	Techniques []TechniqueSupport
}

// TechniqueSpec is one coverage technique in the catalog.
type TechniqueSpec struct {
	Slug         string
	Name         string
	Description  string
	Capabilities []string
}

// CapabilitySpec is one capability in the catalog.
type CapabilitySpec struct {
	Slug    string
	Name    string
	UseCase UseCase
}

// Catalog is the coverage model: providers, their plans, products, techniques
// and capabilities, and how they relate. It is the source the reference
// tables are seeded from, and changing it means opening a pull request.
type Catalog struct {
	Providers    []ProviderSpec
	Plans        []PlanSpec
	Products     []ProductSpec
	Techniques   []TechniqueSpec
	Capabilities []CapabilitySpec
}

// Provider looks a provider up by slug.
func (c Catalog) Provider(slug string) (ProviderSpec, bool) {
	for _, p := range c.Providers {
		if p.Slug == slug {
			return p, true
		}
	}
	var none ProviderSpec
	return none, false
}

// Product looks a product up by slug.
func (c Catalog) Product(slug string) (ProductSpec, bool) {
	for _, p := range c.Products {
		if p.Slug == slug {
			return p, true
		}
	}
	var none ProductSpec
	return none, false
}

// Plan looks a plan up by slug.
func (c Catalog) Plan(slug string) (PlanSpec, bool) {
	for _, p := range c.Plans {
		if p.Slug == slug {
			return p, true
		}
	}
	var none PlanSpec
	return none, false
}

// PlansFor returns the plans a provider sells, in catalog order.
func (c Catalog) PlansFor(provider string) []PlanSpec {
	var plans []PlanSpec
	for _, p := range c.Plans {
		if p.Provider == provider {
			plans = append(plans, p)
		}
	}
	return plans
}

var anthropicOrgPlans = []string{PlanAnthropicTeam, PlanAnthropicEnterprise}
var anthropicEnterprise = []string{PlanAnthropicEnterprise}
var openAIOrgPlans = []string{PlanOpenAIBusiness, PlanOpenAIEnterprise}
var cursorOrgPlans = []string{PlanCursorTeams, PlanCursorEnterprise}

func all(technique string) TechniqueSupport {
	return TechniqueSupport{Technique: technique, Plans: nil}
}

func on(technique string, plans []string) TechniqueSupport {
	return TechniqueSupport{Technique: technique, Plans: plans}
}

// Default is the catalog the server ships. Every product here has ingest code
// behind it; Microsoft Foundry, Bedrock and the detection-only shadow AI
// targets are deliberately absent.
var Default = Catalog{
	Providers: []ProviderSpec{
		{Slug: ProviderAnthropic, Name: "Anthropic"},
		{Slug: ProviderOpenAI, Name: "OpenAI"},
		{Slug: ProviderCursor, Name: "Cursor"},
		{Slug: ProviderGitHub, Name: "GitHub"},
		{Slug: ProviderOpenCode, Name: "opencode"},
		{Slug: ProviderOpenClaw, Name: "openclaw"},
		{Slug: ProviderPi, Name: "pi"},
		{Slug: ProviderLiteLLM, Name: "LiteLLM"},
	},
	Plans: []PlanSpec{
		{Slug: PlanAnthropicPro, Provider: ProviderAnthropic, Name: "Pro"},
		{Slug: PlanAnthropicMax, Provider: ProviderAnthropic, Name: "Max"},
		{Slug: PlanAnthropicTeam, Provider: ProviderAnthropic, Name: "Team"},
		{Slug: PlanAnthropicEnterprise, Provider: ProviderAnthropic, Name: "Enterprise"},
		{Slug: PlanOpenAIPlus, Provider: ProviderOpenAI, Name: "Plus"},
		{Slug: PlanOpenAIPro, Provider: ProviderOpenAI, Name: "Pro"},
		{Slug: PlanOpenAIBusiness, Provider: ProviderOpenAI, Name: "Business"},
		{Slug: PlanOpenAIEnterprise, Provider: ProviderOpenAI, Name: "Enterprise"},
		{Slug: PlanCursorPro, Provider: ProviderCursor, Name: "Pro"},
		{Slug: PlanCursorTeams, Provider: ProviderCursor, Name: "Teams"},
		{Slug: PlanCursorEnterprise, Provider: ProviderCursor, Name: "Enterprise"},
		{Slug: PlanCopilotBusiness, Provider: ProviderGitHub, Name: "Copilot Business"},
		{Slug: PlanCopilotEnterprise, Provider: ProviderGitHub, Name: "Copilot Enterprise"},
	},
	Products: []ProductSpec{
		{
			Slug: ProductClaudeCodeCLI, Name: "Claude Code (CLI)", Provider: ProviderAnthropic,
			SourceIDs: []string{"claude-code"},
			Techniques: []TechniqueSupport{
				all(TechniqueClaudeHooks), all(TechniqueOTELExport), all(TechniquePluginDistribution),
				on(TechniqueManagedSettings, anthropicOrgPlans), on(TechniqueAnthropicComplianceAPI, anthropicEnterprise),
				on(TechniqueAnthropicAdminAnalytics, anthropicEnterprise), all(TechniqueMDMIntegration), all(TechniqueSpendRules),
			},
		},
		{
			Slug: ProductClaudeCodeDesktop, Name: "Claude Code Desktop", Provider: ProviderAnthropic,
			SourceIDs: []string{"claude-code-desktop"},
			Techniques: []TechniqueSupport{
				all(TechniqueClaudeHooks), all(TechniqueOTELExport), all(TechniquePluginDistribution),
				on(TechniqueManagedSettings, anthropicOrgPlans), on(TechniqueAnthropicComplianceAPI, anthropicEnterprise),
				on(TechniqueAnthropicAdminAnalytics, anthropicEnterprise), all(TechniqueMDMIntegration), all(TechniqueSpendRules),
			},
		},
		{
			Slug: ProductClaudeCodeWeb, Name: "Claude Code Web", Provider: ProviderAnthropic,
			SourceIDs: []string{"claude-code-web"},
			Techniques: []TechniqueSupport{
				on(TechniqueAnthropicInferenceHooks, anthropicEnterprise), on(TechniqueAnthropicComplianceAPI, anthropicEnterprise),
				on(TechniqueAnthropicAdminAnalytics, anthropicEnterprise),
			},
		},
		{
			Slug: ProductCowork, Name: "Cowork", Provider: ProviderAnthropic,
			SourceIDs: []string{"cowork"},
			Techniques: []TechniqueSupport{
				all(TechniqueClaudeHooks), all(TechniqueOTELExport), all(TechniquePluginDistribution),
				on(TechniqueAnthropicInferenceHooks, anthropicEnterprise), on(TechniqueAnthropicComplianceAPI, anthropicEnterprise),
			},
		},
		{
			Slug: ProductClaudeTag, Name: "Claude Tag", Provider: ProviderAnthropic,
			SourceIDs:  []string{"claude-tag"},
			Techniques: []TechniqueSupport{all(TechniqueClaudeTagHooks)},
		},
		{
			Slug: ProductClaudeChat, Name: "Claude Chat", Provider: ProviderAnthropic,
			SourceIDs: []string{"claude-chat-web", "claude", "claude-chat"},
			Techniques: []TechniqueSupport{
				on(TechniqueAnthropicInferenceHooks, anthropicEnterprise), on(TechniqueAnthropicComplianceAPI, anthropicEnterprise),
				on(TechniqueAnthropicAdminAnalytics, anthropicEnterprise),
			},
		},
		{
			Slug: ProductCodex, Name: "Codex", Provider: ProviderOpenAI,
			SourceIDs: []string{"codex"},
			Techniques: []TechniqueSupport{
				all(TechniqueCodexHooks), all(TechniqueOTELExport), all(TechniquePluginDistribution),
				on(TechniqueOpenAIComplianceAPI, openAIOrgPlans), all(TechniqueSpendRules),
			},
		},
		{
			Slug: ProductCodexWeb, Name: "Codex Web", Provider: ProviderOpenAI,
			SourceIDs:  []string{"codex-web"},
			Techniques: []TechniqueSupport{on(TechniqueCodexCloudLogs, openAIOrgPlans), on(TechniqueOpenAIComplianceAPI, openAIOrgPlans)},
		},
		{
			Slug: ProductChatGPT, Name: "ChatGPT", Provider: ProviderOpenAI,
			SourceIDs:  []string{"chatgpt"},
			Techniques: []TechniqueSupport{on(TechniqueChatGPTConversations, openAIOrgPlans), on(TechniqueOpenAIComplianceAPI, openAIOrgPlans)},
		},
		{
			Slug: ProductChatGPTWork, Name: "ChatGPT Work", Provider: ProviderOpenAI,
			SourceIDs:  []string{"chatgpt-work"},
			Techniques: []TechniqueSupport{on(TechniqueChatGPTConversations, openAIOrgPlans), on(TechniqueOpenAIComplianceAPI, openAIOrgPlans)},
		},
		{
			Slug: ProductCursor, Name: "Cursor", Provider: ProviderCursor,
			SourceIDs: []string{"cursor"},
			Techniques: []TechniqueSupport{
				all(TechniqueCursorHooks), all(TechniquePluginDistribution), on(TechniqueCursorAdminAPI, cursorOrgPlans), all(TechniqueSpendRules),
			},
		},
		{
			Slug: ProductCopilotCLI, Name: "Copilot CLI", Provider: ProviderGitHub,
			SourceIDs:  []string{"copilot"},
			Techniques: []TechniqueSupport{all(TechniqueUnifiedIngest), all(TechniquePluginDistribution), all(TechniqueSpendRules)},
		},
		{
			Slug: ProductOpenCode, Name: "opencode", Provider: ProviderOpenCode,
			SourceIDs:  []string{"opencode"},
			Techniques: []TechniqueSupport{all(TechniqueUnifiedIngest), all(TechniquePluginDistribution), all(TechniqueSpendRules)},
		},
		{
			Slug: ProductOpenClaw, Name: "openclaw", Provider: ProviderOpenClaw,
			SourceIDs:  []string{"openclaw"},
			Techniques: []TechniqueSupport{all(TechniqueUnifiedIngest), all(TechniquePluginDistribution), all(TechniqueSpendRules)},
		},
		{
			Slug: ProductPi, Name: "pi", Provider: ProviderPi,
			SourceIDs:  []string{"pi"},
			Techniques: []TechniqueSupport{all(TechniqueUnifiedIngest), all(TechniquePluginDistribution)},
		},
		{
			Slug: ProductLiteLLM, Name: "LiteLLM", Provider: ProviderLiteLLM,
			SourceIDs:  []string{"litellm"},
			Techniques: []TechniqueSupport{all(TechniqueLiteLLMGuardrail), all(TechniqueSpendRules)},
		},
	},
	Techniques: []TechniqueSpec{
		{Slug: TechniqueClaudeHooks, Name: "Claude Code hooks", Description: "The observability plugin registers Claude Code hooks that report sessions, prompts and tool calls.", Capabilities: []string{CapabilitySessions, CapabilityToolCalls, CapabilityTranscripts, CapabilityMCPInventory, CapabilityTokens, CapabilityCost, CapabilityPolicyEnforcement, CapabilityFindings, CapabilityShadowMCP}},
		{Slug: TechniqueCursorHooks, Name: "Cursor hooks", Description: "The Cursor observability plugin reports sessions and tool calls.", Capabilities: []string{CapabilitySessions, CapabilityToolCalls, CapabilityMCPInventory, CapabilityPolicyEnforcement, CapabilityFindings, CapabilityShadowMCP}},
		{Slug: TechniqueCodexHooks, Name: "Codex hooks", Description: "The Codex observability plugin reports sessions and tool calls.", Capabilities: []string{CapabilitySessions, CapabilityToolCalls, CapabilityMCPInventory, CapabilityPolicyEnforcement, CapabilityFindings, CapabilityShadowMCP}},
		{Slug: TechniqueUnifiedIngest, Name: "Unified ingest plugin", Description: "A generated plugin for opencode, Copilot CLI, openclaw and pi reports sessions, tool calls and usage.", Capabilities: []string{CapabilitySessions, CapabilityToolCalls, CapabilityTokens, CapabilityCost, CapabilityPolicyEnforcement, CapabilityFindings}},
		{Slug: TechniqueOTELExport, Name: "OpenTelemetry export", Description: "Claude Code, Cowork and Codex export OTEL metrics and logs with token usage.", Capabilities: []string{CapabilityTokens, CapabilityCost}},
		{Slug: TechniquePluginDistribution, Name: "Plugin distribution", Description: "A published marketplace installs plugins on every device.", Capabilities: []string{CapabilityPluginInstall}},
		{Slug: TechniqueManagedSettings, Name: "Managed settings", Description: "A managed settings file carries the OTEL export and the plugin, pushed by an MDM.", Capabilities: []string{CapabilityTokens, CapabilityCost, CapabilityPluginInstall}},
		{Slug: TechniqueDeviceAgent, Name: "Device agent", Description: "An agent on each device reports installed and running AI tools.", Capabilities: []string{CapabilityShadowAI}},
		{Slug: TechniqueAnthropicInferenceHooks, Name: "Anthropic inference hooks", Description: "Claude.ai sends every conversation in the organization to the platform.", Capabilities: []string{CapabilitySessions, CapabilityTranscripts, CapabilityPolicyEnforcement, CapabilityFindings}},
		{Slug: TechniqueAnthropicComplianceAPI, Name: "Anthropic Compliance API", Description: "Conversations are imported from the Compliance API.", Capabilities: []string{CapabilitySessions, CapabilityTranscripts, CapabilityFindings}},
		{Slug: TechniqueAnthropicAdminAnalytics, Name: "Anthropic Admin Analytics API", Description: "Usage and cost are imported from the Admin Analytics API.", Capabilities: []string{CapabilityTokens, CapabilityCost}},
		{Slug: TechniqueOpenAIComplianceAPI, Name: "OpenAI Compliance API", Description: "Usage and cost are imported from the OpenAI Compliance API.", Capabilities: []string{CapabilityTokens, CapabilityCost}},
		{Slug: TechniqueCodexCloudLogs, Name: "Codex cloud logs", Description: "Codex Web transcripts are imported from the compliance feed.", Capabilities: []string{CapabilitySessions, CapabilityTranscripts, CapabilityFindings}},
		{Slug: TechniqueChatGPTConversations, Name: "ChatGPT conversations", Description: "ChatGPT conversations are imported from the compliance feed.", Capabilities: []string{CapabilitySessions, CapabilityTranscripts, CapabilityFindings}},
		{Slug: TechniqueCursorAdminAPI, Name: "Cursor Admin API", Description: "Seats, usage and spend are imported from the Cursor Admin API.", Capabilities: []string{CapabilityTokens, CapabilityCost}},
		{Slug: TechniqueLiteLLMGuardrail, Name: "LiteLLM guardrail and OTLP", Description: "A LiteLLM proxy calls the guardrail and exports OTLP traces.", Capabilities: []string{CapabilitySessions, CapabilityTokens, CapabilityCost, CapabilityPolicyEnforcement, CapabilityFindings}},
		{Slug: TechniqueMCPGateway, Name: "MCP gateway", Description: "MCP traffic flows through a gateway the platform hosts.", Capabilities: []string{CapabilityGatewayTraffic, CapabilityMCPInventory, CapabilityShadowMCP}},
		{Slug: TechniqueMDMIntegration, Name: "MDM integration", Description: "Jamf, Intune or Iru report device inventory.", Capabilities: []string{CapabilityShadowAI}},
		{Slug: TechniqueSpendRules, Name: "Spend rules", Description: "Budgets and kill switches act on reported usage.", Capabilities: []string{CapabilityBudgets}},
		{Slug: TechniqueClaudeTagHooks, Name: "Claude Tag hooks", Description: "Claude Tag reports Slack conversations through its own hook endpoint.", Capabilities: []string{CapabilitySessions, CapabilityTranscripts, CapabilityPolicyEnforcement, CapabilityFindings}},
		{Slug: TechniqueRiskPolicies, Name: "Risk policies", Description: "Policies decide what is scanned and what is blocked.", Capabilities: []string{CapabilityPolicyEnforcement, CapabilityFindings}},
	},
	Capabilities: []CapabilitySpec{
		{Slug: CapabilitySessions, Name: "Sessions", UseCase: UseCaseObservability},
		{Slug: CapabilityToolCalls, Name: "Tool calls", UseCase: UseCaseObservability},
		{Slug: CapabilityTranscripts, Name: "Transcripts", UseCase: UseCaseObservability},
		{Slug: CapabilityMCPInventory, Name: "MCP inventory", UseCase: UseCaseObservability},
		{Slug: CapabilityTokens, Name: "Tokens", UseCase: UseCaseCostTracking},
		{Slug: CapabilityCost, Name: "Cost", UseCase: UseCaseCostTracking},
		{Slug: CapabilityBudgets, Name: "Budgets", UseCase: UseCaseCostTracking},
		{Slug: CapabilityPolicyEnforcement, Name: "Policy enforcement", UseCase: UseCaseSecurity},
		{Slug: CapabilityFindings, Name: "Findings", UseCase: UseCaseSecurity},
		{Slug: CapabilityShadowAI, Name: "Shadow AI", UseCase: UseCaseSecurity},
		{Slug: CapabilityShadowMCP, Name: "Shadow MCP", UseCase: UseCaseSecurity},
		{Slug: CapabilityGatewayTraffic, Name: "Gateway traffic", UseCase: UseCaseMCPGateway},
		{Slug: CapabilityPluginInstall, Name: "Plugin install", UseCase: UseCaseMCPGateway},
	},
}

// Validate checks the catalog's internal references: unique slugs, plans and
// products that belong to a provider that exists, plan gates that name plans
// of the product's own provider, techniques and capabilities that exist.
func (c Catalog) Validate() error {
	providers := map[string]struct{}{}
	for _, p := range c.Providers {
		if _, dup := providers[p.Slug]; dup {
			return fmt.Errorf("duplicate provider slug %q", p.Slug)
		}
		providers[p.Slug] = struct{}{}
	}
	plans := map[string]PlanSpec{}
	for _, p := range c.Plans {
		if _, dup := plans[p.Slug]; dup {
			return fmt.Errorf("duplicate plan slug %q", p.Slug)
		}
		if _, ok := providers[p.Provider]; !ok {
			return fmt.Errorf("plan %q refers to unknown provider %q", p.Slug, p.Provider)
		}
		plans[p.Slug] = p
	}
	capabilities := map[string]struct{}{}
	for _, cap := range c.Capabilities {
		if _, dup := capabilities[cap.Slug]; dup {
			return fmt.Errorf("duplicate capability slug %q", cap.Slug)
		}
		if !slices.Contains(UseCases, cap.UseCase) {
			return fmt.Errorf("capability %q has unknown use case %q", cap.Slug, cap.UseCase)
		}
		capabilities[cap.Slug] = struct{}{}
	}
	techniques := map[string]struct{}{}
	for _, t := range c.Techniques {
		if _, dup := techniques[t.Slug]; dup {
			return fmt.Errorf("duplicate technique slug %q", t.Slug)
		}
		techniques[t.Slug] = struct{}{}
		for _, cap := range t.Capabilities {
			if _, ok := capabilities[cap]; !ok {
				return fmt.Errorf("technique %q refers to unknown capability %q", t.Slug, cap)
			}
		}
	}
	products := map[string]struct{}{}
	for _, p := range c.Products {
		if _, dup := products[p.Slug]; dup {
			return fmt.Errorf("duplicate product slug %q", p.Slug)
		}
		products[p.Slug] = struct{}{}
		if _, ok := providers[p.Provider]; !ok {
			return fmt.Errorf("product %q refers to unknown provider %q", p.Slug, p.Provider)
		}
		if len(p.SourceIDs) == 0 {
			return fmt.Errorf("product %q has no source ids", p.Slug)
		}
		for _, support := range p.Techniques {
			if _, ok := techniques[support.Technique]; !ok {
				return fmt.Errorf("product %q refers to unknown technique %q", p.Slug, support.Technique)
			}
			for _, planSlug := range support.Plans {
				plan, ok := plans[planSlug]
				if !ok {
					return fmt.Errorf("product %q gates technique %q on unknown plan %q", p.Slug, support.Technique, planSlug)
				}
				if plan.Provider != p.Provider {
					return fmt.Errorf("product %q (%s) gates technique %q on plan %q sold by %s", p.Slug, p.Provider, support.Technique, planSlug, plan.Provider)
				}
			}
		}
	}
	return nil
}

// catalogSyncLockKey serialises concurrent syncs, which happen when several
// server replicas start at once.
const catalogSyncLockKey = 0x6f6e626f61726431 // "onboard1"

// SyncCatalog upserts the catalog into the reference tables. Rows are keyed by
// slug; relation rows are replaced so a removed relation disappears. Entity
// rows that left the catalog are kept: answers may still refer to them.
func SyncCatalog(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, c Catalog) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("validate onboarding catalog: %w", err)
	}

	dbtx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin onboarding catalog sync: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if _, err := dbtx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(catalogSyncLockKey)); err != nil {
		return fmt.Errorf("lock onboarding catalog: %w", err)
	}
	queries := repo.New(dbtx)

	providerIDs := map[string]repo.OnboardingProvider{}
	for i, p := range c.Providers {
		row, err := queries.UpsertOnboardingProvider(ctx, repo.UpsertOnboardingProviderParams{Slug: p.Slug, Name: p.Name, SortOrder: int32(i)})
		if err != nil {
			return fmt.Errorf("upsert onboarding provider %q: %w", p.Slug, err)
		}
		providerIDs[p.Slug] = row
	}

	planIDs := map[string]repo.OnboardingPlan{}
	for i, p := range c.Plans {
		row, err := queries.UpsertOnboardingPlan(ctx, repo.UpsertOnboardingPlanParams{ProviderID: providerIDs[p.Provider].ID, Slug: p.Slug, Name: p.Name, SortOrder: int32(i)})
		if err != nil {
			return fmt.Errorf("upsert onboarding plan %q: %w", p.Slug, err)
		}
		planIDs[p.Slug] = row
	}

	capabilityIDs := map[string]repo.OnboardingCapability{}
	for i, cap := range c.Capabilities {
		row, err := queries.UpsertOnboardingCapability(ctx, repo.UpsertOnboardingCapabilityParams{Slug: cap.Slug, Name: cap.Name, UseCase: string(cap.UseCase), SortOrder: int32(i)})
		if err != nil {
			return fmt.Errorf("upsert onboarding capability %q: %w", cap.Slug, err)
		}
		capabilityIDs[cap.Slug] = row
	}

	techniqueIDs := map[string]repo.OnboardingTechnique{}
	for i, t := range c.Techniques {
		row, err := queries.UpsertOnboardingTechnique(ctx, repo.UpsertOnboardingTechniqueParams{Slug: t.Slug, Name: t.Name, Description: t.Description, SortOrder: int32(i)})
		if err != nil {
			return fmt.Errorf("upsert onboarding technique %q: %w", t.Slug, err)
		}
		techniqueIDs[t.Slug] = row
		if err := queries.DeleteOnboardingTechniqueCapabilities(ctx, row.ID); err != nil {
			return fmt.Errorf("reset capabilities of technique %q: %w", t.Slug, err)
		}
		for _, cap := range t.Capabilities {
			if err := queries.InsertOnboardingTechniqueCapability(ctx, repo.InsertOnboardingTechniqueCapabilityParams{TechniqueID: row.ID, CapabilityID: capabilityIDs[cap].ID}); err != nil {
				return fmt.Errorf("link technique %q to capability %q: %w", t.Slug, cap, err)
			}
		}
	}

	for i, p := range c.Products {
		row, err := queries.UpsertOnboardingProduct(ctx, repo.UpsertOnboardingProductParams{ProviderID: providerIDs[p.Provider].ID, Slug: p.Slug, Name: p.Name, SourceIds: p.SourceIDs, SortOrder: int32(i)})
		if err != nil {
			return fmt.Errorf("upsert onboarding product %q: %w", p.Slug, err)
		}
		if err := queries.DeleteOnboardingProductTechniques(ctx, row.ID); err != nil {
			return fmt.Errorf("reset techniques of product %q: %w", p.Slug, err)
		}
		for _, support := range p.Techniques {
			techniqueID := techniqueIDs[support.Technique].ID
			if err := queries.InsertOnboardingProductTechnique(ctx, repo.InsertOnboardingProductTechniqueParams{ProductID: row.ID, TechniqueID: techniqueID}); err != nil {
				return fmt.Errorf("link product %q to technique %q: %w", p.Slug, support.Technique, err)
			}
			for _, planSlug := range support.Plans {
				if err := queries.InsertOnboardingProductTechniquePlan(ctx, repo.InsertOnboardingProductTechniquePlanParams{ProductID: row.ID, TechniqueID: techniqueID, PlanID: planIDs[planSlug].ID}); err != nil {
					return fmt.Errorf("gate technique %q of product %q on plan %q: %w", support.Technique, p.Slug, planSlug, err)
				}
			}
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit onboarding catalog sync: %w", err)
	}
	logger.InfoContext(ctx, "onboarding catalog synced", attr.SlogComponent("onboarding"))
	return nil
}
