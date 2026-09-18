// Package llmanalyzer declares the contracts of the fine-tuned risk model
// analyzer: its finding source, its rule ids and the mapping between policy
// sources and the risks the model scores.
//
// For organizations with feature.FlagRiskLLMAnalyzer enabled the analyzer
// replaces the gitleaks, Presidio, prompt-injection, destructive-tool and
// CLI-destructive engines on both the realtime enforcement lane and the batch
// flag lane. Policies keep their configured sources: each covered source maps
// to one of the model's risk keys, a single model call scores every key, and a
// positive score becomes a finding carrying the rule id of the policy source
// it was scored for. Two sources may share a risk key (destructive_tool and
// cli_destructive both score destructive_tool_call) but every source has its
// own rule id, so the finding always lands in that source's category.
// Categories are resolved from the rule id, so analyzer findings never leak
// the analyzer name into the dashboard.
package llmanalyzer

// Source is the finding source label written on every finding the analyzer
// emits.
const Source = "llm_analyzer"

// Rule ids emitted by the analyzer. Each one is prefix-classifiable by the
// categories package: secret. and pii. match their existing prefixes and the
// other three are listed explicitly on their category definitions.
const (
	// RuleSecret flags leaked credentials, API keys or private keys. Emitted
	// for gitleaks policies.
	RuleSecret = "secret.llm"
	// RulePII flags personal data about an identifiable person. Emitted for
	// presidio policies.
	RulePII = "pii.llm"
	// RulePromptInjection flags instructions that try to hijack the agent.
	// Emitted for prompt_injection policies.
	RulePromptInjection = "prompt_injection.llm"
	// RuleDestructiveTool flags MCP tool calls with destructive or
	// irreversible effects. Emitted for destructive_tool policies.
	RuleDestructiveTool = "destructive_tool.llm"
	// RuleCLIDestructive flags destructive shell or CLI commands carried in
	// tool arguments (rm -rf, git push --force, DROP TABLE, ...). Emitted for
	// cli_destructive policies.
	RuleCLIDestructive = "cli_destructive.llm"
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

var ruleIDsBySource = map[string]string{
	"gitleaks":         RuleSecret,
	"presidio":         RulePII,
	"prompt_injection": RulePromptInjection,
	"destructive_tool": RuleDestructiveTool,
	"cli_destructive":  RuleCLIDestructive,
}

// ruleIDsByKey resolves a rule id from a risk key alone, for callers that
// have a model score but no policy source. Keys shared by several sources
// resolve to the first source's rule id.
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

// RuleIDForSource maps a policy source to the rule id written on findings the
// analyzer emits for that source's policies. ok is false for sources the
// analyzer does not replace. Prefer it over RuleIDForKey whenever the policy
// source is known: destructive_tool and cli_destructive share a risk key but
// carry distinct rule ids.
func RuleIDForSource(source string) (ruleID string, ok bool) {
	ruleID, ok = ruleIDsBySource[source]
	return ruleID, ok
}

// RuleIDForKey returns the rule id emitted for a positive score on the given
// model risk key, or an empty string for an unknown key. KeyDestructiveToolCall
// resolves to RuleDestructiveTool; use RuleIDForSource to obtain
// RuleCLIDestructive for cli_destructive policies.
func RuleIDForKey(key string) string {
	return ruleIDsByKey[key]
}

// AllRuleIDs returns, in a stable order, every rule id the analyzer emits: one
// per entry of CoveredSources followed by RuleDeadLetter.
func AllRuleIDs() []string {
	out := make([]string, 0, len(CoveredSources)+1)
	for _, source := range CoveredSources {
		out = append(out, ruleIDsBySource[source])
	}
	return append(out, RuleDeadLetter)
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
