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

	FlagRiskFindingAnalytics Flag = "risk-finding-analytics"
	FlagRiskAsyncScanShadow  Flag = "risk-async-scan-shadow"

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
