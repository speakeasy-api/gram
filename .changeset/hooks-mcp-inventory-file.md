---
"server": patch
---

Make the Claude hook shadow-MCP guard resilient to a missing SessionStart MCP inventory snapshot (DNO-286). The MCP inventory captured at SessionStart is now persisted to a per-session file, and the blocking PreToolUse hook replays it in its own payload so enforcement no longer depends on the server having cached the async SessionStart snapshot in time. The server prefers a payload-supplied inventory, writes it back to the cache so the telemetry path self-heals, and falls back to the cached snapshot (still failing closed) when neither is available.
