---
"server": patch
---

Support MCP 2026-07-28-only servers when inspecting and registering remote URLs through Platform MCP. Probe with `server/discover` before falling back to legacy initialization, while preserving OAuth discovery, JSON-only admission, and inspection safety limits.
