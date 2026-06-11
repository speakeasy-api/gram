package feature

type Flag string

const (
	FlagSpeakeasyOpenAPIParserV0 Flag = "speakeasy-openapi-parser-v0"
	FlagClickhouseToolMetrics    Flag = "clickhouse-tool-metrics"
	FlagAssistants               Flag = "assistants"
	// FlagPromptInjectionUseClassifier opts an organization in to the L1
	// deberta ML classifier for prompt-injection detection. When unset
	// (the default), the scanner uses the L0 heuristic regex/keyword
	// engine. The engine choice is an implementation detail kept out of
	// the policy schema; the resulting finding rule_id is always
	// `prompt_injection` regardless of engine.
	FlagPromptInjectionUseClassifier Flag = "prompt-injection-use-classifier"
	// FlagPromptPolicies gates the natural-language / LLM-judge ("prompt
	// based") risk policy MVP. While set, only opted-in organizations can
	// create or update nl-type risk policies and have them enforced. The
	// dashboard gates the matching UI behind the same key.
	FlagPromptPolicies Flag = "gram-prompt-policies"
	// FlagSkillsManagement gates the skills registry rollout. The dashboard
	// hides the skills pages and nav behind this key (in addition to the
	// skills_capture product feature), so the feature stays invisible to
	// users until explicitly released in PostHog.
	FlagSkillsManagement Flag = "gram-skills-management"
)
