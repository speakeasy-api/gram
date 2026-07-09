package shared

import (
	. "goa.design/goa/v3/dsl"
)

// RiskPolicyActionEnum applies the allowed-values constraint to an action
// attribute. Use it inside an Attribute("action", String, ...) DSL so every
// payload and result that exposes the field keeps the set in sync.
//
// Callers add Default("flag") on payloads that want the default-flag
// semantics; on update payloads (where the field is optional) they leave the
// default off so the generated Go type stays *string.
//
// "redact" is intentionally absent. Genuine in-transit redaction would need
// to rewrite both user prompts and tool inputs before they reach the model.
// Tool-input rewriting is supported by every coding-agent hook protocol we
// target (Claude Code's PreToolUse `hookSpecificOutput.updatedInput`,
// Cursor's `preToolUse.updated_input`, Gemini CLI's `BeforeTool.tool_input`),
// but user-prompt rewriting is NOT — those protocols only let a hook append
// context or block, never replace the prompt verbatim. Shipping redact
// anyway would silently leak secrets from prompts even when policy claims
// otherwise, so we keep the surface to flag/block until we have a story for
// the prompt path. See:
//   - https://docs.claude.com/en/docs/claude-code/hooks
//   - https://cursor.com/docs/agent/hooks
func RiskPolicyActionEnum() {
	Enum("flag", "block")
}

// RiskPolicyTypeEnum applies the allowed-values constraint to a policy_type
// attribute. "standard" is the regex/presidio/custom detection policy;
// "prompt_based" is an LLM-judge policy that evaluates `prompt`
// against in-scope messages. Use it inside an Attribute("policy_type", ...).
func RiskPolicyTypeEnum() {
	Enum("standard", "prompt_based")
}

// RiskPolicyModelConfig is the per-policy LLM-judge model configuration for
// `prompt_based` policies. All fields are optional; unset fields fall back to
// judge defaults (default model, low temperature, fail-open on judge error).
var RiskPolicyModelConfig = Type("RiskPolicyModelConfig", func() {
	Meta("struct:pkg:path", "types")

	Attribute("model", String, "OpenRouter model id the judge should use. Empty selects the default judge model.")
	Attribute("temperature", Float64, "Sampling temperature for the judge. Defaults to a low value for deterministic verdicts.")
	Attribute("fail_open", Boolean, "When the judge errors or times out: true allows the message (fail-open), false blocks it (fail-closed). Defaults to fail-open.")
})

// RiskPolicyAudienceTypeEnum applies the allowed-values constraint to a policy
// audience type. `everyone` means the policy applies to every user in the org;
// `targeted` means the policy applies only to users or roles granted
// risk_policy:evaluate for the policy resource.
func RiskPolicyAudienceTypeEnum() {
	Enum("everyone", "targeted")
}

// RiskPolicyEvalVerdictEnum constrains a policy-eval review verdict. It is the
// reviewer's ground-truth judgment of a chat session under a prompt-based
// policy: `correct` (guardrail agreed), `false_positive` (guardrail flagged a
// session it should not — tighten), `missed` (guardrail missed one it should
// flag — broaden).
func RiskPolicyEvalVerdictEnum() {
	Enum("correct", "false_positive", "missed")
}

// RiskPolicyEvalReview is one reviewer's saved ground-truth verdict on a chat
// session under a prompt-based policy — a row in the policy's durable regression
// set. Kept physically separate from live findings: eval review activity never
// touches risk_results, the outbox, or enforcement.
var RiskPolicyEvalReview = Type("RiskPolicyEvalReview", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The review ID.", func() {
		Format(FormatUUID)
	})
	Attribute("policy_id", String, "The prompt-based policy the verdict belongs to.", func() {
		Format(FormatUUID)
	})
	Attribute("policy_version", Int64, "The policy version in effect when the verdict was recorded (provenance).")
	Attribute("chat_id", String, "The chat session being judged.", func() {
		Format(FormatUUID)
	})
	Attribute("verdict", String, "The reviewer's ground-truth verdict.", func() {
		RiskPolicyEvalVerdictEnum()
	})
	Attribute("reviewed_by", String, "User id of the reviewer who recorded the verdict.")
	Attribute("created_at", String, "When the verdict was first recorded.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the verdict was last updated.", func() {
		Format(FormatDateTime)
	})

	Required("id", "policy_id", "policy_version", "chat_id", "verdict", "reviewed_by", "created_at", "updated_at")
})

var RiskPolicy = Type("RiskPolicy", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The risk policy ID.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The policy name.")
	Attribute("policy_type", String, "Policy type: standard (regex/presidio/custom detection) or prompt_based (LLM-judge).", func() {
		RiskPolicyTypeEnum()
		Default("standard")
	})
	Attribute("sources", ArrayOf(String), "Detection sources enabled for this policy.")
	Attribute("presidio_entities", ArrayOf(String), "Presidio entity types to scan for. When empty, scans all entities.")
	Attribute("presidio_score_threshold", Float64, "Minimum Presidio confidence (0.0-1.0) a PII match must clear to surface. Omit/null applies the default (0.5).", func() {
		Minimum(0)
		Maximum(1)
		Example(0.75)
	})
	Attribute("prompt_injection_rules", ArrayOf(String), "Prompt-injection detection rule ids enabled in addition to the heuristic baseline. When empty, only heuristics run.")
	Attribute("approved_email_domains", ArrayOf(String), "For the account_identity source: corporate email domains considered approved. Sessions whose AI-account email domain is not listed are flagged. Empty means the domain rule is inert.")
	Attribute("disabled_rules", ArrayOf(String), "Canonical rule_ids (e.g. 'secret.aws_access_token', 'pii.credit_card') the policy author has unchecked within an otherwise-enabled category. Empty means every rule in the selected categories runs; matching findings are dropped at scan time.")
	Attribute("custom_rule_ids", ArrayOf(String), "Custom detection rule ids attached as detectors: a match produces a finding. Custom rules are pure detectors.")
	Attribute("message_types", ArrayOf(String), "Message types this policy applies to. When empty or omitted, applies to all types. Valid values: user_message, tool_request, tool_response, assistant_message.")
	Attribute("scope_include", String, "CEL scope predicate: the policy evaluates a message only when this boolean expression is true (in addition to message_types). Null/empty means all messages are in scope.")
	Attribute("scope_exempt", String, "CEL exemption predicate: the policy is skipped for a message when this boolean expression is true. Null/empty means no inline exemption.")
	Attribute("enabled", Boolean, "Whether the policy is active.")
	Attribute("action", String, "Policy action: flag (log only) or block (deny in real-time).", func() {
		RiskPolicyActionEnum()
		Default("flag")
	})
	Attribute("audience_type", String, "Policy audience type: everyone or targeted.", func() {
		RiskPolicyAudienceTypeEnum()
		Default("everyone")
	})
	Attribute("audience_principal_urns", ArrayOf(String), "Principal URNs the policy applies to. Contains user:all when audience_type is everyone.")
	Attribute("auto_name", Boolean, "Whether the policy name is auto-generated. When true, the name is regenerated on each update.")
	Attribute("user_message", String, "Optional message shown to the end user when this policy blocks an action or surfaces a flagged finding. When unset, a default message is rendered.")
	Attribute("prompt", String, "For prompt_based policies: the guardrail prompt the LLM judge evaluates each in-scope message against. Null for standard policies.")
	Attribute("model_config", RiskPolicyModelConfig, "For prompt_based policies: per-policy LLM-judge model configuration. Null for standard policies.")
	Attribute("score", Float64, "CVSS-style severity (0.1-10) the author assigns to findings this policy produces. Descriptive only; changing it does not re-scan messages. Defaults to 5.", func() {
		Minimum(0.1)
		Maximum(10)
		Default(5)
		Example(5)
	})
	Attribute("version", Int64, "Policy version, incremented on each update.")
	Attribute("created_at", String, "When the policy was created.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the policy was last updated.", func() {
		Format(FormatDateTime)
	})
	Attribute("pending_messages", Int64, "Number of messages not yet analyzed at the current policy version.")
	Attribute("total_messages", Int64, "Total number of messages in the project.")

	Required("id", "project_id", "name", "policy_type", "sources", "enabled", "action", "audience_type", "audience_principal_urns", "auto_name", "score", "version", "created_at", "updated_at", "pending_messages", "total_messages")
})

var RiskCustomDetectionRule = Type("RiskCustomDetectionRule", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The custom detection rule ID.", func() {
		Format(FormatUUID)
	})
	Attribute("rule_id", String, "Stable rule identifier, prefixed with `custom.`.")
	Attribute("title", String, "Human-readable title for the rule.")
	Attribute("description", String, "Description of what the rule detects.")
	Attribute("regex", String, "Legacy RE2-compatible regex pattern (read-only). Live for existing rules; evaluated as content.match(regex) when detection_expr is empty. New rules author detection_expr instead.")
	Attribute("detection_expr", String, "CEL detection predicate: a boolean expression over message fields whose true verdict produces a finding. Supersedes regex.")
	Attribute("severity", String, "Severity level for findings produced by this rule.", func() {
		Enum("info", "low", "medium", "high", "critical")
	})
	Attribute("created_at", String, "When the custom detection rule was created.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the custom detection rule was last updated.", func() {
		Format(FormatDateTime)
	})

	Required("id", "rule_id", "title", "description", "regex", "severity", "created_at", "updated_at")
})

// RiskExclusionMatchTypeEnum constrains the match_type field to the supported
// strategies. Kept here so payloads and the result type stay in sync.
func RiskExclusionMatchTypeEnum() {
	Enum("exact", "regex", "rule_id", "source", "entity_type")
}

var RiskExclusion = Type("RiskExclusion", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The exclusion ID.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID.", func() {
		Format(FormatUUID)
	})
	Attribute("risk_policy_id", String, "The policy this exclusion is bound to. Null/omitted means global: the exclusion applies to every policy in the project.", func() {
		Format(FormatUUID)
	})
	Attribute("match_type", String, "How match_value is interpreted: exact (finding text), regex (RE2 pattern over finding text), rule_id, source, or entity_type (presidio entity, matched as rule_id 'pii.<entity>').", func() {
		RiskExclusionMatchTypeEnum()
	})
	Attribute("match_value", String, "The value matched against findings, interpreted per match_type.")
	Attribute("rule_id_filter", String, "Optional narrowing: an exact/regex/source exclusion only applies to findings with this rule_id. Empty means any.")
	Attribute("source_filter", String, "Optional narrowing: an exact/regex/rule_id exclusion only applies to findings from this source. Empty means any.")
	Attribute("enabled", Boolean, "Whether the exclusion is active.")
	Attribute("created_at", String, "When the exclusion was created.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the exclusion was last updated.", func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "match_type", "match_value", "rule_id_filter", "source_filter", "enabled", "created_at", "updated_at")
})

var RiskResult = Type("RiskResult", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The result ID.", func() {
		Format(FormatUUID)
	})
	Attribute("policy_id", String, "The risk policy ID.", func() {
		Format(FormatUUID)
	})
	Attribute("policy_version", Int64, "Policy version when this result was produced.")
	Attribute("block_id", String, "ID of the durable tool call block recorded for this finding's message, when one exists. Links to the block page at /blocks/:id.", func() {
		Format(FormatUUID)
	})
	Attribute("chat_message_id", String, "The chat message that was scanned.", func() {
		Format(FormatUUID)
	})
	Attribute("chat_id", String, "The chat session containing the message.", func() {
		Format(FormatUUID)
	})
	Attribute("chat_title", String, "Title of the chat session.")
	Attribute("user_id", String, "The user who owns the chat session.")
	Attribute("source", String, "Detection source (e.g. gitleaks).")
	Attribute("rule_id", String, "The matched rule identifier.")
	Attribute("description", String, "Human-readable description of the finding.")
	Attribute("match", String, "The matched secret or sensitive data. Null when the caller isn't authorized to see raw match content for this result's chat (see match_redacted).")
	Attribute("start_pos", Int, "Start byte position within the message content.")
	Attribute("end_pos", Int, "End byte position within the message content.")
	Attribute("confidence", Float64, "Confidence score for this finding.")
	Attribute("tags", ArrayOf(String), "Tags from the detection rule.")
	Attribute("spans", ArrayOf(RiskSpan), "All matched spans attributed to this finding. A finding may carry several correlated spans (e.g. a custom rule matching a tool's function name and its arguments on the same call). The top-level match/start_pos/end_pos mirror the primary (first) span. Null alongside match when the result is redacted.")
	Attribute("match_redacted", String, "Opaque fingerprint of match, in the same `<redacted len=N sha=XXXXXXXX>` form as RiskResultRedacted.match_redacted. Populated whenever match is null so callers without raw access still get a stable, non-reversible correlation token. For shadow_mcp findings this is the original match value passed through verbatim (a non-sensitive server URL or command identifier).")
	Attribute("created_at", String, "When this result was created.", func() {
		Format(FormatDateTime)
	})

	Required("id", "policy_id", "policy_version", "chat_message_id", "source", "created_at")
})

// RiskSpan is one matched span attributed to a finding.
var RiskSpan = Type("RiskSpan", func() {
	Meta("struct:pkg:path", "types")

	Attribute("match", String, "The matched secret or sensitive data for this span.")
	Attribute("field", String, "The message field this span matched, in author-facing form (content, prompt, assistant, tool_result, or tool.name/tool.server/tool.function/tool.args). Empty for detectors that don't attribute a field (e.g. gitleaks, presidio).")
	Attribute("path", String, "The JSON sub-path within the field for a `.get(...)` match (e.g. 'command', 'payload.sql'). Empty when the whole field value matched.")
	Attribute("start_pos", Int, "Start byte position within the message content.")
	Attribute("end_pos", Int, "End byte position within the message content.")

	Required("match")
})

// RiskSpanRedacted mirrors RiskSpan with the raw match replaced by an opaque
// fingerprint, for agent / MCP consumption. The field/path attribution is
// structural (not secret content) so it passes through unredacted.
var RiskSpanRedacted = Type("RiskSpanRedacted", func() {
	Meta("struct:pkg:path", "types")

	Attribute("match_redacted", String, "Opaque fingerprint of this span's match, in the same form as RiskResultRedacted.match_redacted.")
	Attribute("field", String, "The message field this span matched (see RiskSpan.field).")
	Attribute("path", String, "The JSON sub-path within the field for a `.get(...)` match (see RiskSpan.path).")
	Attribute("position_known", Boolean, "Whether this span carried byte-position information.")

	Required("match_redacted", "position_known")
})

// RiskResultRedacted mirrors RiskResult but replaces the raw `match` content
// with an opaque length+SHA256-prefix fingerprint. Designed for agent / MCP
// consumption so secret content from gitleaks-, presidio-, or
// prompt-injection-class findings never reaches the model context.
//
// For shadow_mcp findings the original match value is a server URL / stdio
// command identifier that the dashboard already renders unmasked — that
// passthrough is preserved here so agents can correlate findings to servers
// without losing signal.
var RiskResultRedacted = Type("RiskResultRedacted", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The result ID.", func() {
		Format(FormatUUID)
	})
	Attribute("policy_id", String, "The risk policy ID.", func() {
		Format(FormatUUID)
	})
	Attribute("policy_version", Int64, "Policy version when this result was produced.")
	Attribute("chat_message_id", String, "The chat message that was scanned.", func() {
		Format(FormatUUID)
	})
	Attribute("chat_id", String, "The chat session containing the message.", func() {
		Format(FormatUUID)
	})
	Attribute("chat_title", String, "Title of the chat session.")
	Attribute("user_id", String, "The user who owns the chat session.")
	Attribute("source", String, "Detection source (e.g. gitleaks, presidio, shadow_mcp).")
	Attribute("rule_id", String, "The matched rule identifier.")
	Attribute("description", String, "Human-readable description of the finding.")
	Attribute("match_redacted", String, "Opaque fingerprint of the original match in the form `<redacted len=N sha=XXXXXXXX>` where N is the byte length of the original match and XXXXXXXX is the first 8 hex characters of sha256(match). For shadow_mcp findings the original match value (a non-sensitive server URL or command identifier) is passed through verbatim.")
	Attribute("position_known", Boolean, "Whether the original finding carried byte-position information within the source message. Exact positions are intentionally not exposed to avoid reconstruction attacks.")
	Attribute("confidence", Float64, "Confidence score for this finding.")
	Attribute("tags", ArrayOf(String), "Tags from the detection rule.")
	Attribute("spans_redacted", ArrayOf(RiskSpanRedacted), "All matched spans attributed to this finding, each with its match replaced by an opaque fingerprint.")
	Attribute("created_at", String, "When this result was created.", func() {
		Format(FormatDateTime)
	})

	Required("id", "policy_id", "policy_version", "chat_message_id", "source", "created_at", "match_redacted", "position_known")
})

var RiskChatSummary = Type("RiskChatSummary", func() {
	Meta("struct:pkg:path", "types")

	Attribute("chat_id", String, "The chat session ID.", func() {
		Format(FormatUUID)
	})
	Attribute("chat_title", String, "Title of the chat session.")
	Attribute("user_id", String, "The user who owns the chat session.")
	Attribute("findings_count", Int64, "Number of findings in this chat.")
	Attribute("latest_detected", String, "When the most recent finding was detected.", func() {
		Format(FormatDateTime)
	})

	Required("chat_id", "findings_count", "latest_detected")
})

var RiskPolicyStatus = Type("RiskPolicyStatus", func() {
	Meta("struct:pkg:path", "types")

	Attribute("policy_id", String, "The risk policy ID.", func() {
		Format(FormatUUID)
	})
	Attribute("policy_version", Int64, "Current policy version.")
	Attribute("total_messages", Int64, "Total messages in the project.")
	Attribute("analyzed_messages", Int64, "Messages analyzed at the current policy version.")
	Attribute("pending_messages", Int64, "Messages not yet analyzed.")
	Attribute("findings_count", Int64, "Number of findings at the current policy version.")
	Attribute("workflow_status", String, "Workflow state: running, sleeping, or not_started.", func() {
		Enum("running", "sleeping", "not_started")
	})

	Required("policy_id", "policy_version", "total_messages", "analyzed_messages", "pending_messages", "findings_count", "workflow_status")
})
