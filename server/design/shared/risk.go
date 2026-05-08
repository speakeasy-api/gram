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

var RiskPolicy = Type("RiskPolicy", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The risk policy ID.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The policy name.")
	Attribute("sources", ArrayOf(String), "Detection sources enabled for this policy.")
	Attribute("presidio_entities", ArrayOf(String), "Presidio entity types to scan for. When empty, scans all entities.")
	Attribute("enabled", Boolean, "Whether the policy is active.")
	Attribute("action", String, "Policy action: flag (log only) or block (deny in real-time).", func() {
		RiskPolicyActionEnum()
		Default("flag")
	})
	Attribute("auto_name", Boolean, "Whether the policy name is auto-generated. When true, the name is regenerated on each update.")
	Attribute("user_message", String, "Optional message shown to the end user when this policy blocks an action or surfaces a flagged finding. When unset, a default message is rendered.")
	Attribute("version", Int64, "Policy version, incremented on each update.")
	Attribute("created_at", String, "When the policy was created.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the policy was last updated.", func() {
		Format(FormatDateTime)
	})
	Attribute("pending_messages", Int64, "Number of messages not yet analyzed at the current policy version.")
	Attribute("total_messages", Int64, "Total number of messages in the project.")

	Required("id", "project_id", "name", "sources", "enabled", "action", "auto_name", "version", "created_at", "updated_at", "pending_messages", "total_messages")
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
	Attribute("match", String, "The matched secret or sensitive data.")
	Attribute("start_pos", Int, "Start byte position within the message content.")
	Attribute("end_pos", Int, "End byte position within the message content.")
	Attribute("confidence", Float64, "Confidence score for this finding.")
	Attribute("tags", ArrayOf(String), "Tags from the detection rule.")
	Attribute("created_at", String, "When this result was created.", func() {
		Format(FormatDateTime)
	})

	Required("id", "policy_id", "policy_version", "chat_message_id", "source", "created_at")
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
