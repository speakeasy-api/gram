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
)
