package onboarding

import (
	"fmt"
	"slices"
)

// Destination is the dashboard area where a step's work happens.
type Destination string

const (
	DestinationPlugins      Destination = "plugins"
	DestinationIntegrations Destination = "integrations"
	DestinationPolicies     Destination = "policies"
	DestinationMCP          Destination = "mcp"
	DestinationDevices      Destination = "devices"
	DestinationSettings     Destination = "settings"
)

// EvidenceKind names what a step's verification looks for.
type EvidenceKind string

const (
	// EvidenceHookEvents: hook or session events from the step's sources.
	EvidenceHookEvents EvidenceKind = "hook_events"
	// EvidenceCostRows: telemetry rows carrying token or cost usage.
	EvidenceCostRows EvidenceKind = "cost_rows"
	// EvidenceActivePolicy: at least one enabled risk policy.
	EvidenceActivePolicy EvidenceKind = "active_policy"
	// EvidenceAIDetections: at least one shadow AI detection from a device agent.
	EvidenceAIDetections EvidenceKind = "ai_detections"
	// EvidenceGatewayTraffic: MCP traffic through a toolset or gateway.
	EvidenceGatewayTraffic EvidenceKind = "gateway_traffic"
	// EvidencePluginAssignment: a plugin assigned to at least one audience.
	EvidencePluginAssignment EvidenceKind = "plugin_assignment"
)

// Step is one recommended action. Slug is stable for a given (technique,
// product) pair so verification survives re-planning.
type Step struct {
	Slug        string
	Title       string
	Description string
	Technique   string
	// ProductSlug is empty for steps that cover the whole organization.
	ProductSlug string
	Destination Destination
	Evidence    EvidenceKind
	// Sources are the hook_source ids EvidenceHookEvents filters on.
	Sources []string
	// EvidenceText says, in the admin's words, what verification looks for.
	EvidenceText string
}

// SelectedProduct is one product an admin picked, with its declared plan.
type SelectedProduct struct {
	Product string
	// Plan is empty for products without plans.
	Plan string
}

// Answers is everything the next-step rules look at.
type Answers struct {
	UseCase  UseCase
	MDM      MDMVendor
	Products []SelectedProduct
}

// noStep is the zero Step returned when nothing can be planned.
var noStep Step

// StepsFor returns the ordered steps that cover the answers' use case. The
// rules are deliberately plain if/else over (use case, product, plan, MDM):
// the aim is the quickest aha moment, not the most complete rollout. Steps
// refer to catalog rows by slug only.
func StepsFor(c Catalog, a Answers) []Step {
	var steps []Step
	add := func(s Step, ok bool) {
		if !ok {
			return
		}
		if slices.ContainsFunc(steps, func(existing Step) bool { return existing.Slug == s.Slug }) {
			return
		}
		steps = append(steps, s)
	}

	switch a.UseCase {
	case UseCaseSecurity:
		add(stepCreateRiskPolicy(), true)
		for _, p := range a.Products {
			add(instrumentationStep(c, a, p))
		}
		// Nothing selected can be scanned, or the org manages devices: the
		// device agent is the shortest path to a shadow AI detection.
		if len(steps) == 1 || a.MDM.Managed() {
			add(stepDeviceAgent(), true)
		}
	case UseCaseMCPGateway:
		add(stepCreateGateway(), true)
		add(stepDistributePlugin(a), true)
	case UseCaseCostTracking:
		for _, p := range a.Products {
			add(costStep(c, a, p))
		}
	default:
		for _, p := range a.Products {
			add(instrumentationStep(c, a, p))
		}
	}
	return steps
}

// NextStep returns the first planned step that is not verified. ok is false
// when every planned step is verified or nothing can be planned.
func NextStep(c Catalog, a Answers, verified map[string]bool) (Step, bool) {
	for _, s := range StepsFor(c, a) {
		if !verified[s.Slug] {
			return s, true
		}
	}
	return noStep, false
}

// instrumentationStep is the one action that gets a product's sessions
// flowing, or false when nothing in the code can observe it on that plan.
func instrumentationStep(c Catalog, a Answers, sel SelectedProduct) (Step, bool) {
	product, ok := c.Product(sel.Product)
	if !ok {
		return noStep, false
	}

	switch product.Vendor {
	case VendorAnthropic:
		if product.Slug == ProductClaudeTag {
			return stepClaudeTagHooks(product), true
		}
		if sel.Plan == PlanAnthropicEnterprise {
			return stepAnthropicInferenceHooks(c, a), true
		}
		switch product.Slug {
		case ProductClaudeCodeCLI, ProductClaudeCodeDesktop:
			if a.MDM.Managed() {
				return stepMDMManagedSettings(product, a.MDM), true
			}
			return stepObservabilityPlugin(product, TechniqueClaudeHooks, "Claude Code"), true
		case ProductCowork:
			return stepObservabilityPlugin(product, TechniqueClaudeHooks, "Cowork"), true
		default:
			// Claude Chat and Claude Code Web are only observable through
			// inference hooks, which need an Enterprise plan.
			return noStep, false
		}
	case VendorOpenAI:
		switch product.Slug {
		case ProductCodex:
			return stepObservabilityPlugin(product, TechniqueCodexHooks, "Codex"), true
		default:
			if slices.Contains(openAIOrgPlans, sel.Plan) {
				return stepOpenAIComplianceImport(c, a), true
			}
			return noStep, false
		}
	case VendorCursor:
		return stepObservabilityPlugin(product, TechniqueCursorHooks, "Cursor"), true
	case VendorLiteLLM:
		return stepLiteLLM(product), true
	default:
		return stepObservabilityPlugin(product, TechniqueUnifiedIngest, product.Name), true
	}
}

// costStep is the one action that gets a product's usage and cost flowing.
// Vendor admin APIs win where the plan allows them: one credential covers
// every seat with no device rollout.
func costStep(c Catalog, a Answers, sel SelectedProduct) (Step, bool) {
	product, ok := c.Product(sel.Product)
	if !ok {
		return noStep, false
	}

	switch product.Vendor {
	case VendorAnthropic:
		if sel.Plan == PlanAnthropicEnterprise && product.Slug != ProductClaudeTag {
			return stepAnthropicAdminAnalytics(), true
		}
		return instrumentationStep(c, a, sel)
	case VendorOpenAI:
		if slices.Contains(openAIOrgPlans, sel.Plan) {
			return stepOpenAIComplianceCosts(), true
		}
		return instrumentationStep(c, a, sel)
	case VendorCursor:
		if slices.Contains(cursorOrgPlans, sel.Plan) {
			return stepCursorAdminAPI(), true
		}
		return instrumentationStep(c, a, sel)
	default:
		return instrumentationStep(c, a, sel)
	}
}

// selectedSources returns the source ids of every selected product that the
// filter accepts.
func selectedSources(c Catalog, a Answers, keep func(ProductSpec) bool) []string {
	var sources []string
	for _, sel := range a.Products {
		product, ok := c.Product(sel.Product)
		if !ok || !keep(product) {
			continue
		}
		for _, id := range product.SourceIDs {
			if !slices.Contains(sources, id) {
				sources = append(sources, id)
			}
		}
	}
	return sources
}

func stepObservabilityPlugin(product ProductSpec, technique, label string) Step {
	return Step{
		Slug:         TechniquePluginDistribution + ":" + product.Slug,
		Title:        fmt.Sprintf("Install the %s observability plugin", label),
		Description:  fmt.Sprintf("Download the observability plugin for %s and install it on one machine. The first session it reports is your proof that coverage works; rolling it out to everyone can come after.", label),
		Technique:    technique,
		ProductSlug:  product.Slug,
		Destination:  DestinationPlugins,
		Evidence:     EvidenceHookEvents,
		Sources:      product.SourceIDs,
		EvidenceText: fmt.Sprintf("a session or tool call from %s in the last 30 days", label),
	}
}

func stepMDMManagedSettings(product ProductSpec, mdm MDMVendor) Step {
	return Step{
		Slug:         TechniqueManagedSettings + ":" + product.Slug,
		Title:        fmt.Sprintf("Push Claude Code managed settings through %s", mdm.Name()),
		Description:  fmt.Sprintf("Generate the managed settings file, which carries the OpenTelemetry export and the observability plugin, and push it to one test device with %s. Every device it reaches reports sessions without anyone installing anything.", mdm.Name()),
		Technique:    TechniqueManagedSettings,
		ProductSlug:  product.Slug,
		Destination:  DestinationDevices,
		Evidence:     EvidenceHookEvents,
		Sources:      product.SourceIDs,
		EvidenceText: "a Claude Code session from a managed device in the last 30 days",
	}
}

func stepAnthropicInferenceHooks(c Catalog, a Answers) Step {
	sources := selectedSources(c, a, func(p ProductSpec) bool { return p.Vendor == VendorAnthropic && p.Slug != ProductClaudeTag })
	return Step{
		Slug:         TechniqueAnthropicInferenceHooks,
		Title:        "Turn on Anthropic inference hooks",
		Description:  "In the Claude.ai admin console, point inference hooks at the platform. Every Claude conversation in your organization, on the web, in Claude Code and in Cowork, starts arriving with no device rollout.",
		Technique:    TechniqueAnthropicInferenceHooks,
		ProductSlug:  "",
		Destination:  DestinationIntegrations,
		Evidence:     EvidenceHookEvents,
		Sources:      sources,
		EvidenceText: "a Claude conversation in the last 30 days",
	}
}

func stepClaudeTagHooks(product ProductSpec) Step {
	return Step{
		Slug:         TechniqueClaudeTagHooks,
		Title:        "Connect Claude Tag",
		Description:  "Add the platform's hook endpoint to Claude Tag so Slack conversations with Claude are reported.",
		Technique:    TechniqueClaudeTagHooks,
		ProductSlug:  product.Slug,
		Destination:  DestinationIntegrations,
		Evidence:     EvidenceHookEvents,
		Sources:      product.SourceIDs,
		EvidenceText: "a Claude Tag conversation in the last 30 days",
	}
}

func stepOpenAIComplianceImport(c Catalog, a Answers) Step {
	sources := selectedSources(c, a, func(p ProductSpec) bool { return p.Vendor == VendorOpenAI && p.Slug != ProductCodex })
	return Step{
		Slug:         TechniqueOpenAIComplianceAPI + ":conversations",
		Title:        "Connect the OpenAI Compliance API",
		Description:  "Add an OpenAI Compliance API key. ChatGPT conversations and Codex Web tasks are imported for the whole workspace, with nothing to install.",
		Technique:    TechniqueOpenAIComplianceAPI,
		ProductSlug:  "",
		Destination:  DestinationIntegrations,
		Evidence:     EvidenceHookEvents,
		Sources:      sources,
		EvidenceText: "an imported ChatGPT or Codex Web conversation in the last 30 days",
	}
}

func stepLiteLLM(product ProductSpec) Step {
	return Step{
		Slug:         TechniqueLiteLLMGuardrail,
		Title:        "Point LiteLLM at the platform",
		Description:  "Add the guardrail callback and the OTLP exporter to your LiteLLM proxy config. Every request through the proxy is scanned and lands in observability.",
		Technique:    TechniqueLiteLLMGuardrail,
		ProductSlug:  product.Slug,
		Destination:  DestinationIntegrations,
		Evidence:     EvidenceHookEvents,
		Sources:      product.SourceIDs,
		EvidenceText: "a request through LiteLLM in the last 30 days",
	}
}

func stepAnthropicAdminAnalytics() Step {
	return Step{
		Slug:         TechniqueAnthropicAdminAnalytics,
		Title:        "Connect the Anthropic Admin Analytics API",
		Description:  "Add an Anthropic admin key. Token usage and cost for every seat in your Claude organization are imported daily.",
		Technique:    TechniqueAnthropicAdminAnalytics,
		ProductSlug:  "",
		Destination:  DestinationIntegrations,
		Evidence:     EvidenceCostRows,
		Sources:      nil,
		EvidenceText: "token usage or cost rows in the last 30 days",
	}
}

func stepOpenAIComplianceCosts() Step {
	return Step{
		Slug:         TechniqueOpenAIComplianceAPI + ":costs",
		Title:        "Connect the OpenAI Compliance API",
		Description:  "Add an OpenAI Compliance API key. Usage and cost for ChatGPT and Codex are imported for the whole workspace.",
		Technique:    TechniqueOpenAIComplianceAPI,
		ProductSlug:  "",
		Destination:  DestinationIntegrations,
		Evidence:     EvidenceCostRows,
		Sources:      nil,
		EvidenceText: "token usage or cost rows in the last 30 days",
	}
}

func stepCursorAdminAPI() Step {
	return Step{
		Slug:         TechniqueCursorAdminAPI,
		Title:        "Connect the Cursor Admin API",
		Description:  "Add a Cursor Admin API key. Seats, usage and spend are imported for the whole team.",
		Technique:    TechniqueCursorAdminAPI,
		ProductSlug:  "",
		Destination:  DestinationIntegrations,
		Evidence:     EvidenceCostRows,
		Sources:      nil,
		EvidenceText: "token usage or cost rows in the last 30 days",
	}
}

func stepCreateRiskPolicy() Step {
	return Step{
		Slug:         TechniqueRiskPolicies,
		Title:        "Turn on a risk policy",
		Description:  "Enable one of the starter policies, such as secrets or PII. Nothing is scanned until a policy says what to look for.",
		Technique:    TechniqueRiskPolicies,
		ProductSlug:  "",
		Destination:  DestinationPolicies,
		Evidence:     EvidenceActivePolicy,
		Sources:      nil,
		EvidenceText: "at least one enabled risk policy",
	}
}

func stepDeviceAgent() Step {
	return Step{
		Slug:         TechniqueDeviceAgent,
		Title:        "Install the device agent on one machine",
		Description:  "The device agent reports which AI tools are installed and running. One enrolled machine is enough to see your first shadow AI detection.",
		Technique:    TechniqueDeviceAgent,
		ProductSlug:  "",
		Destination:  DestinationDevices,
		Evidence:     EvidenceAIDetections,
		Sources:      nil,
		EvidenceText: "a shadow AI detection from a device agent in the last 30 days",
	}
}

func stepCreateGateway() Step {
	return Step{
		Slug:         TechniqueMCPGateway,
		Title:        "Create an MCP gateway and route one server through it",
		Description:  "Add a gateway, attach one MCP server your team already uses and connect a client to the gateway URL. The first tool call through it is your proof.",
		Technique:    TechniqueMCPGateway,
		ProductSlug:  "",
		Destination:  DestinationMCP,
		Evidence:     EvidenceGatewayTraffic,
		Sources:      nil,
		EvidenceText: "a tool call through a toolset or gateway in the last 30 days",
	}
}

func stepDistributePlugin(a Answers) Step {
	description := "Publish the plugin marketplace and assign the gateway plugin to a group. Members get the gateway in their coding agents without copying a URL."
	if a.MDM.Managed() {
		description = fmt.Sprintf("Publish the plugin marketplace and assign the gateway plugin to a group, then push the managed settings through %s so every device picks it up.", a.MDM.Name())
	}
	return Step{
		Slug:         TechniquePluginDistribution,
		Title:        "Distribute the gateway as a plugin",
		Description:  description,
		Technique:    TechniquePluginDistribution,
		ProductSlug:  "",
		Destination:  DestinationPlugins,
		Evidence:     EvidencePluginAssignment,
		Sources:      nil,
		EvidenceText: "a plugin assigned to at least one audience",
	}
}
