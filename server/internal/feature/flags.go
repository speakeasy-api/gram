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

	// FlagEnterpriseTrials gates atomic enterprise-trial provisioning on the
	// public self-signup path. While set for the new org, Register creates a
	// ready-to-use enterprise trial (gram_account_type=enterprise,
	// whitelisted=true, the default 14-day trial window, and the enterprise
	// feature bundle minus sso/scim) in one Postgres transaction instead of a
	// bare free org. Targeted by PostHog organization group (org slug), the same
	// way FlagBudgets is evaluated, with distinctID = the new org's Gram ID. It
	// fails closed to false: on a PostHog error or unconfigured client Register
	// keeps its current free, non-whitelisted behavior. v1 gates the Register
	// path only.
	FlagEnterpriseTrials Flag = "enterprise-trials"
)
