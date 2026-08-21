package feature

type Flag string

const (
	FlagSpeakeasyOpenAPIParserV0 Flag = "speakeasy-openapi-parser-v0"
	FlagClickhouseToolMetrics    Flag = "clickhouse-tool-metrics"
	FlagAssistants               Flag = "assistants"
	// FlagPromptPolicies gates the natural-language / LLM-judge ("prompt
	// based") risk policy MVP. While set, only opted-in organizations can
	// create or update nl-type risk policies and have them enforced. The
	// dashboard gates the matching UI behind the same key.
	FlagPromptPolicies Flag = "gram-prompt-policies"
	// FlagBudgets gates the Budgets (spend control) rollout end to end with
	// one key: the dashboard hides the Budgets tab on the Costs page behind
	// it, and the background spend-rule evaluator skips organizations
	// without it — no warning/breach events are recorded and the hooks spend
	// gate snapshot is cleared, so enforcement never blocks an org that
	// cannot see the feature. Targeted by PostHog organization group (org
	// slug), the same way the dashboard evaluates it.
	FlagBudgets Flag = "gram-budgets"
	// FlagRiskRecommendedScopes gates per-project composition of recommended
	// per-category detection scopes. Default off during rollout.
	FlagRiskRecommendedScopes Flag = "risk-recommended-scopes"

	// FlagDeviceLevelCoverage switches device-agent coverage from matching a
	// device's assigned-user email against user-keyed heartbeats to matching
	// its hardware serial against device-keyed ones, falling back to email
	// when no serial match exists. Gated because the flip is visible: an org
	// whose agents predate hardware reporting sees the same numbers, but one
	// mid-upgrade sees devices move between buckets as serials arrive.
	// Targeted by PostHog organization group (org slug), like FlagBudgets.
	// Removed once device-level matching is GA.
	FlagDeviceLevelCoverage Flag = "device-level-coverage"

	FlagRiskFindingAnalytics Flag = "risk-finding-analytics"
	FlagRiskAsyncScanShadow  Flag = "risk-async-scan-shadow"

	// FlagPlatformMCP controls the engineering rollout of Platform MCP. The
	// durable platform_mcp product feature remains the organization-admin opt-in
	// once this release flag permits access.
	FlagPlatformMCP Flag = "platform-mcp"
	// FlagPlatformMCPDashboard controls dashboard discovery and onboarding for
	// Platform MCP. It is presentation-only; runtime authorization requires
	// FlagPlatformMCP and the durable organization product feature.
	FlagPlatformMCPDashboard Flag = "platform-mcp-dashboard"

	// FlagAssistantPlatformMCP grants a project's managed (dashboard)
	// assistant the Platform MCP read toolset — the "platform" platform
	// toolset re-serving the Platform MCP read tools over the assistant
	// runtime channel. Targeted by PostHog organization group (org slug),
	// like FlagBudgets. Evaluated server-side only; removed once the toolset
	// is GA.
	FlagAssistantPlatformMCP Flag = "assistant-platform-mcp"
	// FlagRiskOverviewFromClickHouse serves the risk overview endpoint from
	// ClickHouse risk_findings instead of Postgres risk_results. Per-org
	// rollout gate; removed once the ClickHouse read path is GA.
	FlagRiskOverviewFromClickHouse Flag = "risk-overview-from-clickhouse"
	// FlagRiskListFromClickHouse serves the project-wide risk events listing
	// (ListRiskResults without a chat_id) from ClickHouse risk_findings
	// instead of Postgres risk_results. The chat-scoped listing stays on
	// Postgres, which is the only store holding raw match content. Per-org
	// rollout gate; removed once the ClickHouse read path is GA.
	FlagRiskListFromClickHouse Flag = "risk-list-from-clickhouse"
	// FlagRiskWatchdog gates the Watchdog signals endpoint (risk.getSignals).
	// Key matches the dashboard's page-level flag so a single PostHog flag
	// controls both the UI and the API surface.
	FlagRiskWatchdog Flag = "gram-risk-watchdog"

	// FlagCanonicalIdentityFold serves cost analytics (telemetry.query /
	// telemetry.listSessions) email filters and group-bys through the
	// ClickHouse identity_map fold, so one employee's directory, personal,
	// and case-variant emails read as one identity. Targeted by PostHog
	// organization group (org slug), like FlagBudgets. Removed once the fold
	// is GA (DNO-856).
	FlagCanonicalIdentityFold Flag = "canonical-identity-fold"
	// FlagCanonicalIdentityFoldShadow runs the folded variant of the
	// telemetry.query table read alongside the literal one — serving the
	// literal result — and logs divergence, validating the fold on real
	// traffic before FlagCanonicalIdentityFold enables it anywhere. Ignored
	// when the fold flag is on. Same targeting; removed with the fold flag.
	FlagCanonicalIdentityFoldShadow Flag = "canonical-identity-fold-shadow"

	// FlagPaygSelfServeBilling gates the self-serve Stripe Checkout rollout.
	// Targeted by PostHog organization group (org slug) and removed once PAYG
	// billing is generally available.
	FlagPaygSelfServeBilling Flag = "gram-payg-self-serve-billing"

	// FlagMCPApproval gates the MCP approval workflow end to end: the
	// approval queue, evidence gathering, deciding, and the promotion of
	// blocked-server redemptions into approval requests (orgs off the flag
	// fall back to legacy bypass requests). Targeted by PostHog organization
	// group (org slug), like FlagBudgets. A rollout gate while the workflow
	// is dogfooded; if approval becomes a sold capability the durable
	// entitlement returns through productfeatures alongside this flag.
	FlagMCPApproval Flag = "gram-mcp-approval"

	// FlagMCPResearch gates the MCP research agent within an approval-enabled
	// organization: starting runs and executing queued ones. Targeted by
	// PostHog organization group like FlagMCPApproval, and separate from it
	// so research — the spend-heavy, web-facing piece — rolls out to a
	// narrower set than the approval workflow. Fails closed: a flag-service
	// error reads as off.
	FlagMCPResearch Flag = "gram-mcp-research"

	// FlagMCPResearchKill is the research kill switch: affirmatively on means
	// no research runs anywhere, checked before the rollout flag. It exists
	// apart from FlagMCPResearch so an emergency stop never touches the
	// rollout flag's org targeting — un-killing restores exactly the release
	// state from before. Fails closed: research must not run while the state
	// of its stop control is unknown.
	FlagMCPResearchKill Flag = "gram-mcp-research-kill"

	// FlagHooksRollout gates the phased rollout of new observability (hooks)
	// plugin generator versions. Unlike the other flags it is consulted via its
	// PAYLOAD, not its boolean state: the flag carries a JSON payload
	// {"version": N} naming the highest hooksGeneratorVersion the matched org is
	// cleared to receive. An org gets a new hooks version only when its cleared
	// version reaches it; a code-side version bump never touches the payload, so
	// nothing auto-rolls — promoting a version is the deliberate act of raising
	// the pin in PostHog. The always-immediate canary set lives in code (see
	// plugins.canaryHooksOrgSlugs), independent of this flag, so a PostHog outage
	// can't strand it on stale hooks.
	FlagHooksRollout Flag = "hooks-rollout"
)

// Variants of FlagAssistantPlatformMCP. Anything else — no variant, an
// unrecognized key, an unavailable provider, or an evaluation error — resolves
// to VariantAssistantToolsLegacy, which is the pre-rollout behaviour, so a
// PostHog outage can never strip the managed assistant's tools.
const (
	// VariantAssistantToolsLegacy serves the managed assistant the
	// "managed-assistant" platform toolset (logs, chats, users, risk,
	// deployments, skills, plugins, docs, changelog).
	VariantAssistantToolsLegacy Variant = "legacy"
	// VariantAssistantToolsPlatformMCP serves the managed assistant the
	// "platform" toolset — the Platform MCP read tools — INSTEAD of the
	// legacy toolset, not in addition to it.
	VariantAssistantToolsPlatformMCP Variant = "platformmcp"
)

// AssistantToolsVariant normalizes a resolved variant to one of the two known
// keys, collapsing everything unrecognized onto the legacy default. Both the
// attach path (assistants service) and the serve path (mcp service) must agree
// on this mapping or a toolset would be attached and then 404 at request time.
func AssistantToolsVariant(variant Variant) Variant {
	if variant == VariantAssistantToolsPlatformMCP {
		return VariantAssistantToolsPlatformMCP
	}
	return VariantAssistantToolsLegacy
}
