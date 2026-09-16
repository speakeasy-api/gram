// Package llmanalyzer declares the contracts of the fine-tuned risk model
// analyzer: its finding source, its rule ids and the mapping between policy
// sources and the risks the model scores.
//
// For organizations with feature.FlagRiskLLMAnalyzer enabled the analyzer
// replaces the gitleaks, Presidio, prompt-injection and destructive-tool
// engines on both the realtime enforcement lane and the batch flag lane.
// Policies keep their configured sources: each covered source maps to one of
// the model's risk keys, a single model call scores every key, and a positive
// score becomes a finding carrying the rule id for that key. Categories are
// resolved from the rule id, so analyzer findings never leak the analyzer name
// into the dashboard.
package llmanalyzer

// Source is the finding source label written on every finding the analyzer
// emits.
const Source = "llm_analyzer"

// Rule ids emitted by the analyzer. Each one is prefix-classifiable by the
// categories package: secret. and pii. match their existing prefixes and the
// other two are listed explicitly on their category definitions.
const (
	// RuleSecret flags leaked credentials, API keys or private keys.
	RuleSecret = "secret.llm"
	// RulePII flags personal data about an identifiable person.
	RulePII = "pii.llm"
	// RulePromptInjection flags instructions that try to hijack the agent.
	RulePromptInjection = "prompt_injection.llm"
	// RuleDestructiveTool flags tool calls with destructive or irreversible
	// effects. Both destructive_tool and cli_destructive policy sources map to
	// it; the category is resolved from the policy source at fan-out.
	RuleDestructiveTool = "destructive_tool.llm"
	// RuleDeadLetter marks the sentinel finding emitted when the model could
	// not be consulted. Enforcing policies treat it as a deny.
	RuleDeadLetter = "llm_analyzer.dead_letter"
)

// Risk keys the model scores. They are the exact keys of the JSON object the
// fine-tuned model returns, one score per key.
const (
	KeySecretsLeak         = "secrets_leak"
	KeyPersonalDataLeak    = "personal_data_leak"
	KeyPromptInjection     = "prompt_injection"
	KeyDestructiveToolCall = "destructive_tool_call"
)

// Policy source names are literals because the packages that own them
// (gitleaks, the Presidio activity, promptinjection, shadowmcp and
// clidestructive) must stay importable by this one without cycles. Canonical
// definitions:
//
//	gitleaks          server/internal/scanners/gitleaks.Source
//	presidio          server/internal/background/activities/risk_analysis.SourcePresidio
//	prompt_injection  server/internal/scanners/promptinjection.Source
//	destructive_tool  server/internal/shadowmcp.SourceDestructiveTool
//	cli_destructive   server/internal/scanners/clidestructive.Source
var riskKeysBySource = map[string]string{
	"gitleaks":         KeySecretsLeak,
	"presidio":         KeyPersonalDataLeak,
	"prompt_injection": KeyPromptInjection,
	"destructive_tool": KeyDestructiveToolCall,
	"cli_destructive":  KeyDestructiveToolCall,
}

var ruleIDsByKey = map[string]string{
	KeySecretsLeak:         RuleSecret,
	KeyPersonalDataLeak:    RulePII,
	KeyPromptInjection:     RulePromptInjection,
	KeyDestructiveToolCall: RuleDestructiveTool,
}

// CoveredSources lists, in a stable order, the policy sources the analyzer
// stands in for when the organization is on the flag.
var CoveredSources = []string{
	"gitleaks",
	"presidio",
	"prompt_injection",
	"destructive_tool",
	"cli_destructive",
}

// RiskKeyForSource maps a policy source to the model risk key that covers it.
// ok is false for sources the analyzer does not replace (custom, shadow_mcp,
// account_identity, ...).
func RiskKeyForSource(source string) (key string, ok bool) {
	key, ok = riskKeysBySource[source]
	return key, ok
}

// RuleIDForKey returns the rule id emitted for a positive score on the given
// model risk key, or an empty string for an unknown key.
func RuleIDForKey(key string) string {
	return ruleIDsByKey[key]
}

// CoversAnySource reports whether at least one of the policy sources is
// replaced by the analyzer.
func CoversAnySource(sources []string) bool {
	for _, source := range sources {
		if _, ok := riskKeysBySource[source]; ok {
			return true
		}
	}
	return false
}
