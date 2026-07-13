package feature

type Flag string

const (
	FlagSpeakeasyOpenAPIParserV0 Flag = "speakeasy-openapi-parser-v0"
	FlagClickhouseToolMetrics    Flag = "clickhouse-tool-metrics"
	FlagAssistants               Flag = "assistants"
	// FlagPromptInjectionUseClassifier opts an organization in to the L1
	// LLM-judge engine for prompt-injection
	// detection. When unset (the default), the scanner runs the L0 heuristic
	// regex/keyword engine only. The engine choice is an implementation detail
	// kept out of the policy schema; the resulting finding rule_id is always
	// `prompt_injection` regardless of engine. (The PostHog key keeps its
	// historical "use-classifier" name; it now gates the judge.)
	FlagPromptInjectionUseClassifier Flag = "prompt-injection-use-classifier"
	// FlagPromptPolicies gates the natural-language / LLM-judge ("prompt
	// based") risk policy MVP. While set, only opted-in organizations can
	// create or update nl-type risk policies and have them enforced. The
	// dashboard gates the matching UI behind the same key.
	FlagPromptPolicies Flag = "gram-prompt-policies"

	FlagRiskFindingAnalytics Flag = "risk-finding-analytics"

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
