---
"server": minor
---

Gram Session OAuth issuers now control which OAuth Client ID Metadata Document clients they accept, decided before any document is fetched. Issuers default to admitting Gram's curated catalog of verified MCP clients (Claude Code, Claude, VS Code, Zed, Goose, ChatGPT, Codex CLI, Notion, MCPJam, Factory Droid, ToolHive) plus any URLs configured on the issuer, and can be switched to admit any spec-valid client or none at all; the new `userSessionIssuersCimdClients` service lists the catalog and manages per-issuer URLs, and the admission mode is readable and writable on the existing `userSessionIssuers` endpoints. Separately, a metadata document that omits `token_endpoint_auth_method` is now accepted as a public client rather than rejected, matching the spec and unblocking clients such as ChatGPT and Codex CLI whose documents omit it.
