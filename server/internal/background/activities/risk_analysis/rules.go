package risk_analysis

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

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
//	destructive.<cat>.<name>         — destructive shell / git / db / cloud command
//	prompt-injection                 — prompt injection (engine selected per-org)
//
// The pair (source, rule_id) is the stable composite identity for downstream
// consumers, but the prefix alone is enough to bucket findings into
// dashboard categories.

const (
	prefixSecret      = "secret."
	prefixPII         = "pii."
	prefixDestructive = "destructive."

	// RuleShadowMCP is the canonical rule id emitted for every shadow_mcp
	// finding. The detection mechanism (missing toolset id, unknown
	// toolset, ...) is implementation detail kept in logs; the rule_id
	// describes the risk itself.
	RuleShadowMCP = "shadow-mcp"

	// RuleDestructiveTool is the canonical rule id emitted for every
	// destructive_tool finding.
	RuleDestructiveTool = prefixDestructive + "tool"

	// RulePromptInjection is the canonical rule id emitted for every
	// prompt-injection finding. There is exactly one rule: whether the
	// match came from the L1 deberta classifier or an L0 heuristic regex
	// is an implementation detail not part of the public contract.
	RulePromptInjection = "prompt-injection"

	// DeadLetterRuleID is the rule id emitted for Presidio dead-letter
	// sentinel rows when a message could not be analyzed.
	DeadLetterRuleID = prefixPII + "dead-letter"
)

// Describe* functions are the single entry point per scanner for emitting
// a canonical (rule_id, description) pair on a Finding. Each takes only
// the context that source needs — no shared "context bag" — so a new
// scanner adds a new function rather than extending a shared type.
//
// In dev/test, every returned rule_id is validated against the canonical
// grammar so writer drift fails CI loudly.

// DescribeShadowMCP returns the canonical (rule_id, description) for an
// unverified MCP tool call.
func DescribeShadowMCP(toolName string) (string, string) {
	if toolName == "" {
		return guard(RuleShadowMCP), "Detected an unverified MCP tool call."
	}
	return guard(RuleShadowMCP), fmt.Sprintf("Detected an unverified MCP tool call to %q.", toolName)
}

// DescribeDestructiveTool returns the canonical (rule_id, description)
// for an MCP tool call whose tool definition carries a destructive
// annotation.
func DescribeDestructiveTool(toolName string) (string, string) {
	if toolName == "" {
		return guard(RuleDestructiveTool), "Detected a call to a tool annotated as destructive by its MCP server."
	}
	return guard(RuleDestructiveTool), fmt.Sprintf("Detected a call to %q, which its MCP server annotates as destructive.", toolName)
}

// DescribeCLIDestructive returns the canonical (rule_id, description) for
// a cli_destructive pattern match. The pattern's FullName() is the
// canonical rule id directly.
func DescribeCLIDestructive(pattern cliDestructivePattern, toolName string) (string, string) {
	ruleID := pattern.FullName()
	cmd := cliCommandHumanForm[ruleID]
	if cmd == "" {
		if toolName == "" {
			return guard(ruleID), "Detected a destructive command pattern in tool arguments."
		}
		return guard(ruleID), fmt.Sprintf("Detected a destructive command pattern in the arguments of tool %q.", toolName)
	}
	if toolName == "" {
		return guard(ruleID), fmt.Sprintf("Detected a %q invocation in tool arguments.", cmd)
	}
	return guard(ruleID), fmt.Sprintf("Detected a %q invocation in the arguments of tool %q.", cmd, toolName)
}

// DescribePresidioEntity returns the canonical (rule_id, description) for
// a Presidio finding. rawEntityType is Presidio's UPPER_SNAKE entity name.
func DescribePresidioEntity(rawEntityType string) (string, string) {
	ruleID := CanonicalPresidioRuleID(rawEntityType)
	desc, ok := presidioEntityDescriptions[ruleID]
	if !ok {
		desc = "Identified potentially sensitive personal information."
	}
	return guard(ruleID), desc
}

// DescribePresidioDeadLetter returns the canonical (rule_id, description)
// for a Presidio dead-letter sentinel row.
func DescribePresidioDeadLetter() (string, string) {
	return guard(DeadLetterRuleID), "Presidio could not analyze this message after exhausting its retry budget."
}

// DescribeGitleaks returns the canonical (rule_id, description) for a
// gitleaks finding. Gitleaks ships a human-readable description per rule
// that never echoes the matched secret, so it passes through unchanged.
func DescribeGitleaks(rawRuleID, upstreamDescription string) (string, string) {
	return guard(CanonicalGitleaksRuleID(rawRuleID)), upstreamDescription
}

// DescribePromptInjection returns the canonical (rule_id, description) for
// any prompt-injection finding. The same rule id is emitted regardless of
// whether the match came from the L1 classifier or an L0 heuristic.
func DescribePromptInjection() (string, string) {
	return guard(RulePromptInjection), "Detected a prompt injection attempt."
}

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

// guard validates the rule id against the canonical grammar and panics in
// dev/test if it doesn't match. Returns the id unchanged so callers can
// compose it inline.
func guard(ruleID string) string {
	if !enforceRuleIDFormat {
		return ruleID
	}
	if err := ValidateRuleID(ruleID); err != nil {
		panic(fmt.Sprintf("risk_analysis: invalid canonical rule id: %v", err))
	}
	return ruleID
}

// ruleIDFormat is the canonical rule id grammar: lowercase ASCII letters
// and digits, hyphens within a segment, dots between segments.
var ruleIDFormat = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*(\.[a-z0-9]+(-[a-z0-9]+)*)*$`)

// ValidateRuleID returns an error when id does not conform to the canonical
// rule id grammar.
func ValidateRuleID(id string) error {
	if id == "" {
		return fmt.Errorf("rule id is empty")
	}
	if !ruleIDFormat.MatchString(id) {
		return fmt.Errorf("rule id %q is not in canonical form (lowercase kebab segments joined by dots)", id)
	}
	return nil
}

// enforceRuleIDFormat is true when the binary is running under `go test`
// (covers unit + integration suites) or after cmd/ wiring opts in via
// EnableRuleIDFormatEnforcement (used in local-dev). In those modes the
// Describe* builders panic on a malformed canonical rule id so writer
// drift is caught immediately. Production runs leave the value through.
var enforceRuleIDFormat = testing.Testing()

// EnableRuleIDFormatEnforcement opts the process in to strict canonical
// rule_id validation: any Describe* builder returning a malformed id will
// panic. Intended for local development; cmd/ wires this on when
// `--environment=local`. Test binaries get the same behavior automatically
// via testing.Testing().
func EnableRuleIDFormatEnforcement() {
	enforceRuleIDFormat = true
}

// presidioEntityDescriptions maps canonical Presidio rule ids to their
// human-readable, source-agnostic description. Lookup miss falls through
// to a generic PII string in DescribePresidioEntity.
var presidioEntityDescriptions = map[string]string{
	// Financial.
	prefixPII + "credit-card":    "Identified a credit card number, which may expose cardholder data.",
	prefixPII + "iban-code":      "Identified an International Bank Account Number, which may expose financial account data.",
	prefixPII + "us-bank-number": "Identified a US bank account number, which may expose financial account data.",
	prefixPII + "crypto":         "Identified a cryptocurrency wallet address.",

	// PII.
	prefixPII + "email-address": "Identified an email address.",
	prefixPII + "phone-number":  "Identified a telephone number.",
	prefixPII + "ip-address":    "Identified an IP address.",
	prefixPII + "mac-address":   "Identified a network interface (MAC) address.",
	prefixPII + "person":        "Identified a person name.",
	prefixPII + "location":      "Identified a location reference.",
	prefixPII + "date-time":     "Identified a date or time reference that may correlate with a person.",
	prefixPII + "nrp":           "Identified a nationality, religious, or political reference.",
	prefixPII + "url":           "Identified a URL that may carry sensitive context.",

	// Government identifiers.
	prefixPII + "us-ssn":            "Identified a US Social Security Number.",
	prefixPII + "us-passport":       "Identified a US passport number.",
	prefixPII + "us-driver-license": "Identified a US driver license number.",
	prefixPII + "us-itin":           "Identified a US Individual Taxpayer Identification Number.",
	prefixPII + "uk-nhs":            "Identified a UK National Health Service number.",
	prefixPII + "uk-nino":           "Identified a UK National Insurance Number.",
	prefixPII + "uk-passport":       "Identified a UK passport number.",
	prefixPII + "es-nif":            "Identified a Spanish personal tax identifier (NIF).",
	prefixPII + "it-fiscal-code":    "Identified an Italian personal fiscal code.",
	prefixPII + "au-tfn":            "Identified an Australian Tax File Number.",
	prefixPII + "in-pan":            "Identified an Indian Permanent Account Number.",
	prefixPII + "in-aadhaar":        "Identified an Indian Aadhaar identifier.",
	prefixPII + "sg-nric-fin":       "Identified a Singapore NRIC or FIN identifier.",

	// Healthcare.
	prefixPII + "medical-license":               "Identified a medical license number, which may expose protected health information.",
	prefixPII + "us-mbi":                        "Identified a US Medicare Beneficiary Identifier.",
	prefixPII + "us-npi":                        "Identified a US National Provider Identifier.",
	prefixPII + "medical-disease-disorder":      "Identified a disease or disorder reference that may expose protected health information.",
	prefixPII + "medical-medication":            "Identified a medication or drug reference that may expose protected health information.",
	prefixPII + "medical-therapeutic-procedure": "Identified a treatment or diagnostic procedure that may expose protected health information.",
	prefixPII + "medical-clinical-event":        "Identified a clinical event that may expose protected health information.",
	prefixPII + "medical-biological-attribute":  "Identified a biological attribute that may expose protected health information.",
	prefixPII + "medical-family-history":        "Identified a family medical history reference that may expose protected health information.",
}

// cliCommandHumanForm maps a cli_destructive canonical rule id to the
// human form of the matched command, embedded in the description sentence.
var cliCommandHumanForm = map[string]string{
	"destructive.shell.rm-rf":                    "rm -rf",
	"destructive.shell.dd":                       "dd",
	"destructive.shell.mkfs":                     "mkfs",
	"destructive.shell.fork-bomb":                "fork bomb",
	"destructive.shell.chmod-recursive":          "chmod -R",
	"destructive.shell.chown-recursive":          "chown -R",
	"destructive.shell.sudo":                     "sudo",
	"destructive.git.push-force":                 "git push --force",
	"destructive.git.reset-hard":                 "git reset --hard",
	"destructive.git.clean-force":                "git clean -f",
	"destructive.git.branch-delete-force":        "git branch -D",
	"destructive.database.drop":                  "DROP TABLE",
	"destructive.database.truncate":              "TRUNCATE",
	"destructive.database.delete-without-where":  "DELETE without WHERE",
	"destructive.database.dropdb":                "dropdb",
	"destructive.cloud.aws-ec2-terminate":        "aws ec2 terminate-instances",
	"destructive.cloud.aws-s3-rb":                "aws s3 rb",
	"destructive.cloud.gcloud-projects-delete":   "gcloud projects delete",
	"destructive.cloud.kubectl-delete-namespace": "kubectl delete namespace",
	"destructive.cloud.kubectl-delete-workload":  "kubectl delete workload",
}
