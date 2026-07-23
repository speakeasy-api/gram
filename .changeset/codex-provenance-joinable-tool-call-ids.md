---
"server": patch
---

Make Codex MCP tool calls joinable to their recorded provenance (DNO-604). Codex hook payloads carry no per-call tool-call id, so the recorded chat tool-call id (previously the tool name) and the telemetry trace id (previously derived from the session id) could never satisfy the shadow-MCP provenance join `trace_id = sha256(tool_call_id)[:16]` — every Codex MCP call fell back to `x-gram-toolset-id` signature validation. Both sides now derive from a shared `sessionID + "|" + toolName` key, which also moves Codex trace grouping from one-trace-per-session to one-trace-per-(session, tool): Tool Logs rows now carry the actual tool name instead of an arbitrary one per session. The canonical ingest path applies the same shared-key fallback for any sender that omits per-call tool ids.
