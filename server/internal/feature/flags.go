package feature

type Flag string

const (
	FlagSpeakeasyOpenAPIParserV0 Flag = "speakeasy-openapi-parser-v0"
	FlagClickhouseToolMetrics    Flag = "clickhouse-tool-metrics"
	FlagAssistants               Flag = "assistants"
	// FlagPromptInjectionUseClassifier opts an organization in to the L1
	// LLM-judge engine (server/internal/pijudge) for prompt-injection
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
)
