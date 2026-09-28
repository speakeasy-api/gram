---
"server": patch
---

Make MCP method availability version-aware across hosted, platform toolset, and meta servers. Keep `initialize` and `ping` for revisions before 2026-07-28, and prepare 2026-07-28 `server/discover` with shared server descriptions, the correct `supportedVersions` field, and privacy-aware cache hints. Acknowledge discovery notifications without a JSON-RPC response body.
