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

	// FlagUserSessionCIMD gates inbound OAuth Client ID Metadata Document
	// (CIMD) support on the user-session authorization server: URL-shaped
	// client_id values on /mcp/{slug}/authorize are resolved by fetching the
	// metadata document instead of requiring RFC 7591 DCR. Evaluated
	// server-side per organization with distinctID = the issuer's org ID and
	// no groups.
	FlagUserSessionCIMD Flag = "gram-user-session-cimd"
	// FlagConsentToolFiltering gates the consent-screen tool picker island
	// and its consent-scoped MCP transport. The enforcement path — persisting
	// user_sessions.tool_selection and filtering tools/list and tools/call by
	// it — is always on; the flag only controls whether new approvals can
	// author a restrictive selection. Staged this way so every runtime pod
	// enforces selections before any pod's consent screen can create one: a
	// one-step activation would let a restrictive token minted mid-rollout
	// reach an old pod that ignores the policy and serves every tool.
	// Evaluated server-side per organization with distinctID = the issuer's
	// org ID and no groups, like FlagUserSessionCIMD. Removed once the picker
	// is GA.
	FlagConsentToolFiltering Flag = "gram-consent-tool-filtering"
	// FlagPlatformMCPRollout gates the organization-targeted Platform MCP rollout.
	// It is evaluated in addition to the durable Platform MCP product capability.
	FlagPlatformMCPRollout Flag = "platform-mcp-rollout"
	// FlagPlatformMCPCatalogRegistration independently gates Platform MCP catalog
	// registration and provider-setup handoffs. It is evaluated after the main
	// Platform MCP gate and is default-off during the mutation rollout.
	FlagPlatformMCPCatalogRegistration Flag = "platform-mcp-catalog-registration"
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
