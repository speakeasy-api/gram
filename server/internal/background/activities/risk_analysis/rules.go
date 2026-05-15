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

// ruleSpec describes one normalized rule. Describe builds the human-readable
// description for a finding, optionally interpolating fields from
// RuleContext. The function must not include the `match` value.
type ruleSpec struct {
	source      string
	ruleID      string
	description func(RuleContext) string
}

// CanonicalRuleID returns the kebab-case canonical form of a raw rule id
// emitted by one of the scanners. The transformation is:
//  1. lowercase
//  2. strip a leading "<source>." prefix when present (legacy writers used
//     dotted prefixes that are now redundant with the `source` column)
//  3. replace any remaining ".", "_", or "/" with "-"
//
// The function is deterministic and idempotent so the same input always
// produces the same key in the catalog and in the database.
func CanonicalRuleID(source, raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if id == "" {
		return ""
	}

	// Legacy prefixes that some scanners stamped onto rule ids before the
	// `source` column carried the disambiguation.
	for _, prefix := range []string{
		strings.ToLower(source) + ".",
		"pi.", // prompt_injection wrote rule ids as `pi.<rule>`.
	} {
		if strings.HasPrefix(id, prefix) {
			id = id[len(prefix):]
			break
		}
	}

	id = strings.NewReplacer(".", "-", "_", "-", "/", "-").Replace(id)
	return id
}

// Normalize returns the canonical rule id and a sanitized description for a
// finding emitted by one of the scanners. The description never echoes
// `match` and never leaks internal validator strings; when the catalog has
// no entry, fallbackDescription is used (set this to the upstream library's
// description for gitleaks; pass "" elsewhere to get a per-source default).
func Normalize(source, rawRuleID, fallbackDescription string, rctx RuleContext) (string, string) {
	ruleID := CanonicalRuleID(source, rawRuleID)
	if spec, ok := ruleCatalog[catalogKey(source, ruleID)]; ok {
		return ruleID, spec.description(rctx)
	}

	if fallbackDescription != "" {
		return ruleID, fallbackDescription
	}

	return ruleID, defaultDescription(source, rctx)
}

func catalogKey(source, ruleID string) string {
	return source + "/" + ruleID
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

// presidioCatalog returns the canonical description for each Presidio
// entity type the dashboard offers in DETECTION_RULES. The wording mirrors
// gitleaks: a short, declarative sentence that names the category without
// echoing the matched value.
func presidioRule(ruleID, desc string) ruleSpec {
	return ruleSpec{
		source:      SourcePresidio,
		ruleID:      ruleID,
		description: func(RuleContext) string { return desc },
	}
}

// shadowMCPDescribe wraps a fmt.Sprintf-style template that takes the tool
// name. The template MUST include exactly one `%s` placeholder.
func shadowMCPRule(ruleID, withTool, withoutTool string) ruleSpec {
	return ruleSpec{
		source: "shadow_mcp",
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
		source: "destructive_tool",
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
		source: SourceCLIDestructive,
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
		source:      SourcePromptInjection,
		ruleID:      ruleID,
		description: func(RuleContext) string { return desc },
	}
}

// ruleCatalog is the single source of truth for canonical descriptions of
// every rule_id Gram writes to risk_results. The map is keyed by
// "<source>/<canonical-rule-id>". Sources that already emit safe upstream
// descriptions (gitleaks) are intentionally absent — they fall through the
// `fallbackDescription` path in Normalize.
var ruleCatalog = func() map[string]ruleSpec {
	specs := []ruleSpec{
		// Presidio: financial.
		presidioRule("credit-card", "Identified a credit card number, which may expose cardholder data."),
		presidioRule("iban-code", "Identified an International Bank Account Number, which may expose financial account data."),
		presidioRule("us-bank-number", "Identified a US bank account number, which may expose financial account data."),
		presidioRule("crypto", "Identified a cryptocurrency wallet address."),

		// Presidio: PII.
		presidioRule("email-address", "Identified an email address."),
		presidioRule("phone-number", "Identified a telephone number."),
		presidioRule("ip-address", "Identified an IP address."),
		presidioRule("mac-address", "Identified a network interface (MAC) address."),
		presidioRule("person", "Identified a person name."),
		presidioRule("location", "Identified a location reference."),
		presidioRule("date-time", "Identified a date or time reference that may correlate with a person."),
		presidioRule("nrp", "Identified a nationality, religious, or political reference."),
		presidioRule("url", "Identified a URL that may carry sensitive context."),

		// Presidio: government identifiers.
		presidioRule("us-ssn", "Identified a US Social Security Number."),
		presidioRule("us-passport", "Identified a US passport number."),
		presidioRule("us-driver-license", "Identified a US driver license number."),
		presidioRule("us-itin", "Identified a US Individual Taxpayer Identification Number."),
		presidioRule("uk-nhs", "Identified a UK National Health Service number."),
		presidioRule("uk-nino", "Identified a UK National Insurance Number."),
		presidioRule("uk-passport", "Identified a UK passport number."),
		presidioRule("es-nif", "Identified a Spanish personal tax identifier (NIF)."),
		presidioRule("it-fiscal-code", "Identified an Italian personal fiscal code."),
		presidioRule("au-tfn", "Identified an Australian Tax File Number."),
		presidioRule("in-pan", "Identified an Indian Permanent Account Number."),
		presidioRule("in-aadhaar", "Identified an Indian Aadhaar identifier."),
		presidioRule("sg-nric-fin", "Identified a Singapore NRIC or FIN identifier."),

		// Presidio: healthcare.
		presidioRule("medical-license", "Identified a medical license number, which may expose protected health information."),
		presidioRule("us-mbi", "Identified a US Medicare Beneficiary Identifier."),
		presidioRule("us-npi", "Identified a US National Provider Identifier."),
		presidioRule("medical-disease-disorder", "Identified a disease or disorder reference that may expose protected health information."),
		presidioRule("medical-medication", "Identified a medication or drug reference that may expose protected health information."),
		presidioRule("medical-therapeutic-procedure", "Identified a treatment or diagnostic procedure that may expose protected health information."),
		presidioRule("medical-clinical-event", "Identified a clinical event that may expose protected health information."),
		presidioRule("medical-biological-attribute", "Identified a biological attribute that may expose protected health information."),
		presidioRule("medical-family-history", "Identified a family medical history reference that may expose protected health information."),

		// Presidio: dead-letter sentinel for messages the scanner could not analyze.
		{
			source: SourcePresidio,
			ruleID: "dead-letter",
			description: func(RuleContext) string {
				return "Presidio could not analyze this message after exhausting its retry budget."
			},
		},

		// shadow_mcp: a single risk — an unverified MCP tool call. Which
		// validator path rejected it (missing toolset id, unknown toolset,
		// tool not in toolset, ...) is implementation detail kept in logs;
		// the public rule_id describes the risk itself.
		shadowMCPRule(
			"shadow-mcp",
			"Detected an unverified MCP tool call to %s.",
			"Detected an unverified MCP tool call.",
		),

		// destructive_tool.
		destructiveToolRule(
			"annotated-destructive",
			"Detected a call to %s, which its MCP server annotates as destructive.",
			"Detected a call to a tool annotated as destructive by its MCP server.",
		),

		// cli_destructive: one entry per curated pattern in cli_destructive.go.
		cliDestructiveRule("shell-rm-rf", "rm -rf"),
		cliDestructiveRule("shell-dd", "dd"),
		cliDestructiveRule("shell-mkfs", "mkfs"),
		cliDestructiveRule("shell-fork-bomb", "fork bomb"),
		cliDestructiveRule("shell-chmod-recursive", "chmod -R"),
		cliDestructiveRule("shell-chown-recursive", "chown -R"),
		cliDestructiveRule("shell-sudo", "sudo"),
		cliDestructiveRule("git-push-force", "git push --force"),
		cliDestructiveRule("git-reset-hard", "git reset --hard"),
		cliDestructiveRule("git-clean-force", "git clean -f"),
		cliDestructiveRule("git-branch-delete-force", "git branch -D"),
		cliDestructiveRule("database-drop", "DROP TABLE"),
		cliDestructiveRule("database-truncate", "TRUNCATE"),
		cliDestructiveRule("database-delete-without-where", "DELETE without WHERE"),
		cliDestructiveRule("database-dropdb", "dropdb"),
		cliDestructiveRule("cloud-aws-ec2-terminate", "aws ec2 terminate-instances"),
		cliDestructiveRule("cloud-aws-s3-rb", "aws s3 rb"),
		cliDestructiveRule("cloud-gcloud-projects-delete", "gcloud projects delete"),
		cliDestructiveRule("cloud-kubectl-delete-namespace", "kubectl delete namespace"),
		cliDestructiveRule("cloud-kubectl-delete-workload", "kubectl delete workload"),

		// prompt_injection rules. Descriptions stay generic so they never echo
		// the matched phrase, which is preserved in the `match` column.
		promptInjectionRule("instruction-override", "Detected an instruction override phrase that attempts to bypass prior instructions."),
		promptInjectionRule("role-hijack-you-are-now", "Detected a role hijack attempt that asserts a new role."),
		promptInjectionRule("role-hijack-act-as-privileged", "Detected a role hijack attempt that asks the model to act as a privileged role."),
		promptInjectionRule("system-prompt-leak", "Detected an attempt to elicit the system prompt or initial instructions."),
		promptInjectionRule("delimiter-injection", "Detected a forged role or instruction delimiter."),
		promptInjectionRule("encoded-payload", "Detected an encoded blob with an explicit decode or execute instruction."),
		promptInjectionRule("deberta-v3-classifier", "An ML classifier flagged this message as a prompt injection attempt."),
	}

	out := make(map[string]ruleSpec, len(specs))
	for _, s := range specs {
		out[catalogKey(s.source, s.ruleID)] = s
	}
	return out
}()
