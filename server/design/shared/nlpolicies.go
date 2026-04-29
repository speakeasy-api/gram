package shared

import (
	. "goa.design/goa/v3/dsl"
)

var NLPolicy = Type("NLPolicy", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The NL policy ID.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The project ID. Empty when org-wide.", func() { Format(FormatUUID) })
	Attribute("name", String, "Policy name.")
	Attribute("description", String, "Author-facing summary.")
	Attribute("nl_prompt", String, "The natural-language judge prompt.")
	Attribute("scope_per_call", Boolean, "Run inline on each tool call.")
	Attribute("scope_session", Boolean, "Run async over the rolling chat-session window.")
	Attribute("mode", String, "audit | enforce | disabled.", func() {
		Enum("audit", "enforce", "disabled")
	})
	Attribute("fail_mode", String, "fail_open | fail_closed — judge error/timeout behavior in enforce mode.", func() {
		Enum("fail_open", "fail_closed")
	})
	Attribute("static_rules", String, "JSON-encoded static rule list (see spec §6 grammar).")
	Attribute("version", Int64, "Incremented on each update.")
	Attribute("created_at", String, "RFC3339 timestamp.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "RFC3339 timestamp.", func() {
		Format(FormatDateTime)
	})

	Required("id", "name", "nl_prompt", "scope_per_call", "scope_session", "mode", "fail_mode", "static_rules", "version", "created_at", "updated_at")
})

var NLPolicyDecision = Type("NLPolicyDecision", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "Decision row ID.", func() { Format(FormatUUID) })
	Attribute("nl_policy_id", String, "Policy that produced this decision.", func() { Format(FormatUUID) })
	Attribute("nl_policy_version", Int64, "Policy version snapshot at decision time.")
	Attribute("chat_id", String, "Source chat (optional).", func() { Format(FormatUUID) })
	Attribute("session_id", String, "Source MCP session ID.")
	Attribute("tool_urn", String, "Tool that was being called.")
	Attribute("decision", String, "ALLOW | BLOCK | JUDGE_ERROR.", func() {
		Enum("ALLOW", "BLOCK", "JUDGE_ERROR")
	})
	Attribute("decided_by", String, "static_rule | llm_judge | fail_mode | session_quarantine.", func() {
		Enum("static_rule", "llm_judge", "fail_mode", "session_quarantine")
	})
	Attribute("reason", String, "Short human-readable reason.")
	Attribute("mode", String, "Snapshot of policy mode at decision time.", func() {
		Enum("audit", "enforce", "disabled")
	})
	Attribute("enforced", Boolean, "True when mode=enforce AND decision=BLOCK.")
	Attribute("judge_latency_ms", Int, "Round-trip latency of the LLM call (when applicable).")
	Attribute("created_at", String, "RFC3339 timestamp.", func() {
		Format(FormatDateTime)
	})

	Required("id", "nl_policy_id", "nl_policy_version", "tool_urn", "decision", "decided_by", "mode", "enforced", "created_at")
})

var NLPolicySessionVerdict = Type("NLPolicySessionVerdict", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "Verdict row ID.", func() { Format(FormatUUID) })
	Attribute("session_id", String, "Quarantined session.")
	Attribute("chat_id", String, "Source chat.", func() { Format(FormatUUID) })
	Attribute("nl_policy_id", String, "Policy that produced the verdict.", func() { Format(FormatUUID) })
	Attribute("nl_policy_version", Int64, "Policy version snapshot.")
	Attribute("verdict", String, "OK | QUARANTINED.", func() {
		Enum("OK", "QUARANTINED")
	})
	Attribute("reason", String, "Why.")
	Attribute("quarantined_at", String, "RFC3339 — null when verdict=OK.", func() {
		Format(FormatDateTime)
	})
	Attribute("cleared_at", String, "RFC3339 — non-null when cleared.", func() {
		Format(FormatDateTime)
	})
	Attribute("cleared_by", String, "Clearing user ID.", func() { Format(FormatUUID) })
	Attribute("created_at", String, "RFC3339.", func() {
		Format(FormatDateTime)
	})

	Required("id", "session_id", "nl_policy_id", "nl_policy_version", "verdict", "created_at")
})

var NLPolicyReplayRun = Type("NLPolicyReplayRun", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "Run ID.", func() { Format(FormatUUID) })
	Attribute("nl_policy_id", String, "Policy under test.", func() { Format(FormatUUID) })
	Attribute("nl_policy_version", Int64, "Policy version snapshot.")
	Attribute("status", String, "pending | running | completed | failed.", func() {
		Enum("pending", "running", "completed", "failed")
	})
	Attribute("counts", String, "JSON-encoded counts: {would_block, would_allow, judge_error}.")
	Attribute("sample_filter", String, "JSON-encoded filter envelope.")
	Attribute("started_at", String, "RFC3339.", func() {
		Format(FormatDateTime)
	})
	Attribute("completed_at", String, "RFC3339 — null until completed.", func() {
		Format(FormatDateTime)
	})

	Required("id", "nl_policy_id", "nl_policy_version", "status", "sample_filter", "started_at")
})

var NLPolicyReplayResult = Type("NLPolicyReplayResult", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "Result row ID.", func() { Format(FormatUUID) })
	Attribute("replay_run_id", String, "Parent run.", func() { Format(FormatUUID) })
	Attribute("chat_message_id", String, "Source chat message replayed.", func() { Format(FormatUUID) })
	Attribute("tool_urn", String, "Tool that was called originally.")
	Attribute("decision", String, "ALLOW | BLOCK | JUDGE_ERROR.", func() {
		Enum("ALLOW", "BLOCK", "JUDGE_ERROR")
	})
	Attribute("reason", String, "Judge reason.")
	Attribute("judge_latency_ms", Int, "")
	Attribute("created_at", String, "RFC3339.", func() {
		Format(FormatDateTime)
	})

	Required("id", "replay_run_id", "decision", "created_at")
})
