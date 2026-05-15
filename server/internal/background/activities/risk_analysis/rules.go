package risk_analysis

import (
	"strings"
)

// RuleContext carries optional, source-specific values that catalog entries
// can interpolate into their descriptions. Fields are best-effort: callers
// fill in what they have, the catalog ignores the rest. Never include the
// `match` value here, since descriptions must not echo sensitive data.
type RuleContext struct {
	// ToolName is the recorded MCP / native tool name (e.g. "mcp__github__create_pr"
	// or "Bash"). Used by shadow_mcp, destructive_tool, cli_destructive.
	ToolName string
	// MatchedPattern is the cli_destructive pattern FullName ("shell/rm-rf").
	MatchedPattern string
}

// Rule id conventions
//
// All rule ids are lowercase. They are grouped by **risk category** via a
// short dotted prefix so consumers can pattern-match on category without a
// switch on `source`:
//
//	secret.<gitleaks-rule>           — credentials / API keys / tokens
//	pii.<presidio-entity>            — personal / financial / medical data
//	shadow-mcp                       — unverified MCP tool call (single rule)
//	destructive.tool                 — MCP tool annotated as destructive
//	destructive.cli-<command>        — destructive shell / git / db / cloud command
//	pi                               — ML classifier prompt injection verdict
//	pi.<heuristic>                   — L0 heuristic prompt injection match
//
// The pair (source, rule_id) is the stable composite identity for downstream
// consumers, but the prefix alone is enough to bucket findings into
// dashboard categories.

const (
	prefixSecret      = "secret."
	prefixPII         = "pii."
	prefixDestructive = "destructive."
	prefixPI          = "pi."

	// RuleShadowMCP is the canonical rule id emitted for every shadow_mcp
	// finding. The detection mechanism (missing toolset id, unknown
	// toolset, ...) is implementation detail kept in logs; the rule_id
	// describes the risk itself.
	RuleShadowMCP = "shadow-mcp"

	// RuleDestructiveTool is the canonical rule id emitted for every
	// destructive_tool finding.
	RuleDestructiveTool = prefixDestructive + "tool"

	// RulePromptInjectionClassifier is the canonical rule id emitted when
	// the L1 ML classifier flags a message. The specific classifier
	// (deberta-v3 today) is implementation detail.
	RulePromptInjectionClassifier = "pi"
)

// CanonicalGitleaksRuleID prepends the `secret.` prefix to a gitleaks rule
// id. Gitleaks rule ids are already kebab-case so no other normalization
// is needed.
func CanonicalGitleaksRuleID(raw string) string {
	return prefixSecret + strings.ToLower(raw)
}

// CanonicalPresidioRuleID converts a Presidio entity type (UPPER_SNAKE) to
// the canonical `pii.<kebab>` rule id (`MEDICAL_LICENSE` -> `pii.medical-license`).
func CanonicalPresidioRuleID(raw string) string {
	return prefixPII + strings.ReplaceAll(strings.ToLower(raw), "_", "-")
}

// ruleSpec describes one normalized rule. Describe builds the human-readable
// description for a finding, optionally interpolating fields from
// RuleContext. The function must not include the `match` value.
type ruleSpec struct {
	ruleID      string
	description func(RuleContext) string
}

// Normalize returns the canonical rule id and a sanitized description for a
// finding. Callers pass the already-canonical rule id (see the
// `CanonicalXxxRuleID` helpers and the per-source `Rule*` constants); the
// description either comes from the catalog or, when absent, from
// fallbackDescription, or a per-source default.
func Normalize(source, canonicalRuleID, fallbackDescription string, rctx RuleContext) (string, string) {
	if spec, ok := ruleCatalog[canonicalRuleID]; ok {
		return canonicalRuleID, spec.description(rctx)
	}
	if fallbackDescription != "" {
		return canonicalRuleID, fallbackDescription
	}
	return canonicalRuleID, defaultDescription(source, rctx)
}

// defaultDescription is used when a finding has no catalog entry and no
// upstream description. Per-source one-liners keep the public contract
// uniform without making the catalog exhaustive.
func defaultDescription(source string, rctx RuleContext) string {
	switch source {
	case SourcePresidio:
		return "Identified potentially sensitive personal information."
	case "shadow_mcp":
		if rctx.ToolName != "" {
			return "Detected an unverified MCP tool call to " + quote(rctx.ToolName) + "."
		}
		return "Detected an unverified MCP tool call."
	case "destructive_tool":
		if rctx.ToolName != "" {
			return "Detected a call to " + quote(rctx.ToolName) + ", which its MCP server annotates as destructive."
		}
		return "Detected a call to a tool annotated as destructive by its MCP server."
	case SourceCLIDestructive:
		if rctx.ToolName != "" {
			return "Detected a destructive command pattern in the arguments of tool " + quote(rctx.ToolName) + "."
		}
		return "Detected a destructive command pattern in tool arguments."
	case SourcePromptInjection:
		return "Detected a prompt injection attempt."
	case "gitleaks":
		return "Identified a sensitive credential or secret."
	}
	return "Detected a policy violation."
}

func quote(s string) string {
	return "\"" + s + "\""
}

func presidioRule(ruleID, desc string) ruleSpec {
	return ruleSpec{
		ruleID:      ruleID,
		description: func(RuleContext) string { return desc },
	}
}

func shadowMCPRule(ruleID, withTool, withoutTool string) ruleSpec {
	return ruleSpec{
		ruleID: ruleID,
		description: func(rctx RuleContext) string {
			if rctx.ToolName == "" {
				return withoutTool
			}
			return strings.ReplaceAll(withTool, "%s", quote(rctx.ToolName))
		},
	}
}

func destructiveToolRule(ruleID, withTool, withoutTool string) ruleSpec {
	return ruleSpec{
		ruleID: ruleID,
		description: func(rctx RuleContext) string {
			if rctx.ToolName == "" {
				return withoutTool
			}
			return strings.ReplaceAll(withTool, "%s", quote(rctx.ToolName))
		},
	}
}

// cliDestructiveRule names a curated command pattern. `command` is the
// human-readable form of the matched command (e.g. `rm -rf`), used inside
// the description sentence.
func cliDestructiveRule(ruleID, command string) ruleSpec {
	return ruleSpec{
		ruleID: ruleID,
		description: func(rctx RuleContext) string {
			if rctx.ToolName == "" {
				return "Detected a " + quote(command) + " invocation in tool arguments."
			}
			return "Detected a " + quote(command) + " invocation in the arguments of tool " + quote(rctx.ToolName) + "."
		},
	}
}

func promptInjectionRule(ruleID, desc string) ruleSpec {
	return ruleSpec{
		ruleID:      ruleID,
		description: func(RuleContext) string { return desc },
	}
}

// ruleCatalog is the single source of truth for canonical descriptions of
// every rule_id Gram writes to risk_results. Keyed by the canonical rule id.
// Sources that already emit safe upstream descriptions (gitleaks) are
// intentionally absent — they fall through the `fallbackDescription` path
// in Normalize.
var ruleCatalog = func() map[string]ruleSpec {
	specs := []ruleSpec{
		// Presidio: financial.
		presidioRule(prefixPII+"credit-card", "Identified a credit card number, which may expose cardholder data."),
		presidioRule(prefixPII+"iban-code", "Identified an International Bank Account Number, which may expose financial account data."),
		presidioRule(prefixPII+"us-bank-number", "Identified a US bank account number, which may expose financial account data."),
		presidioRule(prefixPII+"crypto", "Identified a cryptocurrency wallet address."),

		// Presidio: PII.
		presidioRule(prefixPII+"email-address", "Identified an email address."),
		presidioRule(prefixPII+"phone-number", "Identified a telephone number."),
		presidioRule(prefixPII+"ip-address", "Identified an IP address."),
		presidioRule(prefixPII+"mac-address", "Identified a network interface (MAC) address."),
		presidioRule(prefixPII+"person", "Identified a person name."),
		presidioRule(prefixPII+"location", "Identified a location reference."),
		presidioRule(prefixPII+"date-time", "Identified a date or time reference that may correlate with a person."),
		presidioRule(prefixPII+"nrp", "Identified a nationality, religious, or political reference."),
		presidioRule(prefixPII+"url", "Identified a URL that may carry sensitive context."),

		// Presidio: government identifiers.
		presidioRule(prefixPII+"us-ssn", "Identified a US Social Security Number."),
		presidioRule(prefixPII+"us-passport", "Identified a US passport number."),
		presidioRule(prefixPII+"us-driver-license", "Identified a US driver license number."),
		presidioRule(prefixPII+"us-itin", "Identified a US Individual Taxpayer Identification Number."),
		presidioRule(prefixPII+"uk-nhs", "Identified a UK National Health Service number."),
		presidioRule(prefixPII+"uk-nino", "Identified a UK National Insurance Number."),
		presidioRule(prefixPII+"uk-passport", "Identified a UK passport number."),
		presidioRule(prefixPII+"es-nif", "Identified a Spanish personal tax identifier (NIF)."),
		presidioRule(prefixPII+"it-fiscal-code", "Identified an Italian personal fiscal code."),
		presidioRule(prefixPII+"au-tfn", "Identified an Australian Tax File Number."),
		presidioRule(prefixPII+"in-pan", "Identified an Indian Permanent Account Number."),
		presidioRule(prefixPII+"in-aadhaar", "Identified an Indian Aadhaar identifier."),
		presidioRule(prefixPII+"sg-nric-fin", "Identified a Singapore NRIC or FIN identifier."),

		// Presidio: healthcare.
		presidioRule(prefixPII+"medical-license", "Identified a medical license number, which may expose protected health information."),
		presidioRule(prefixPII+"us-mbi", "Identified a US Medicare Beneficiary Identifier."),
		presidioRule(prefixPII+"us-npi", "Identified a US National Provider Identifier."),
		presidioRule(prefixPII+"medical-disease-disorder", "Identified a disease or disorder reference that may expose protected health information."),
		presidioRule(prefixPII+"medical-medication", "Identified a medication or drug reference that may expose protected health information."),
		presidioRule(prefixPII+"medical-therapeutic-procedure", "Identified a treatment or diagnostic procedure that may expose protected health information."),
		presidioRule(prefixPII+"medical-clinical-event", "Identified a clinical event that may expose protected health information."),
		presidioRule(prefixPII+"medical-biological-attribute", "Identified a biological attribute that may expose protected health information."),
		presidioRule(prefixPII+"medical-family-history", "Identified a family medical history reference that may expose protected health information."),

		// Presidio: dead-letter sentinel for messages the scanner could not analyze.
		{
			ruleID: prefixPII + "dead-letter",
			description: func(RuleContext) string {
				return "Presidio could not analyze this message after exhausting its retry budget."
			},
		},

		// shadow_mcp: single rule. The detection mechanism stays in logs.
		shadowMCPRule(
			RuleShadowMCP,
			"Detected an unverified MCP tool call to %s.",
			"Detected an unverified MCP tool call.",
		),

		// destructive_tool.
		destructiveToolRule(
			RuleDestructiveTool,
			"Detected a call to %s, which its MCP server annotates as destructive.",
			"Detected a call to a tool annotated as destructive by its MCP server.",
		),

		// cli_destructive: one entry per curated pattern in cli_destructive.go.
		// Rule ids are produced directly by cliDestructivePattern.FullName()
		// in `destructive.<category>.<name>` form.
		cliDestructiveRule("destructive.shell.rm-rf", "rm -rf"),
		cliDestructiveRule("destructive.shell.dd", "dd"),
		cliDestructiveRule("destructive.shell.mkfs", "mkfs"),
		cliDestructiveRule("destructive.shell.fork-bomb", "fork bomb"),
		cliDestructiveRule("destructive.shell.chmod-recursive", "chmod -R"),
		cliDestructiveRule("destructive.shell.chown-recursive", "chown -R"),
		cliDestructiveRule("destructive.shell.sudo", "sudo"),
		cliDestructiveRule("destructive.git.push-force", "git push --force"),
		cliDestructiveRule("destructive.git.reset-hard", "git reset --hard"),
		cliDestructiveRule("destructive.git.clean-force", "git clean -f"),
		cliDestructiveRule("destructive.git.branch-delete-force", "git branch -D"),
		cliDestructiveRule("destructive.database.drop", "DROP TABLE"),
		cliDestructiveRule("destructive.database.truncate", "TRUNCATE"),
		cliDestructiveRule("destructive.database.delete-without-where", "DELETE without WHERE"),
		cliDestructiveRule("destructive.database.dropdb", "dropdb"),
		cliDestructiveRule("destructive.cloud.aws-ec2-terminate", "aws ec2 terminate-instances"),
		cliDestructiveRule("destructive.cloud.aws-s3-rb", "aws s3 rb"),
		cliDestructiveRule("destructive.cloud.gcloud-projects-delete", "gcloud projects delete"),
		cliDestructiveRule("destructive.cloud.kubectl-delete-namespace", "kubectl delete namespace"),
		cliDestructiveRule("destructive.cloud.kubectl-delete-workload", "kubectl delete workload"),

		// prompt_injection. The L1 classifier rule_id is just `pi` — the
		// model (deberta-v3) is implementation detail. L0 heuristic
		// matches carry a `pi.<heuristic>` sub-rule.
		promptInjectionRule(RulePromptInjectionClassifier, "An ML classifier flagged this message as a prompt injection attempt."),
		promptInjectionRule(prefixPI+"instruction-override", "Detected an instruction override phrase that attempts to bypass prior instructions."),
		promptInjectionRule(prefixPI+"role-hijack", "Detected a role hijack attempt."),
		promptInjectionRule(prefixPI+"system-prompt-leak", "Detected an attempt to elicit the system prompt or initial instructions."),
		promptInjectionRule(prefixPI+"delimiter-injection", "Detected a forged role or instruction delimiter."),
		promptInjectionRule(prefixPI+"encoded-payload", "Detected an encoded blob with an explicit decode or execute instruction."),
	}

	out := make(map[string]ruleSpec, len(specs))
	for _, s := range specs {
		out[s.ruleID] = s
	}
	return out
}()
