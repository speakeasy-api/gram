package feature

type Flag string

const (
	FlagSpeakeasyOpenAPIParserV0 Flag = "speakeasy-openapi-parser-v0"
	FlagClickhouseToolMetrics    Flag = "clickhouse-tool-metrics"
	FlagAssistants               Flag = "assistants"
	// FlagPromptInjectionUseRegex selects the L0 heuristic (regex/keyword)
	// engine for prompt-injection detection. When unset (the default), the
	// scanner uses the L1 deberta classifier. The engine choice is an
	// implementation detail kept out of the policy schema; the resulting
	// finding rule_id is always `prompt-injection` regardless of engine.
	FlagPromptInjectionUseRegex Flag = "prompt-injection-use-regex"
)
