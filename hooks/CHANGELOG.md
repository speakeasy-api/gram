# hooks

## 0.1.1

### Patch Changes

- 63008ae: Restore Claude MCP inventory capture in the Go hooks relay. Session start and configuration-change hooks now send a locally redacted inventory snapshot through canonical ingest so external MCP URLs appear in Shadow MCP inventory before a tool is called.

## 0.1.0

### Minor Changes

- 22fb780: Introduce the speakeasy-hooks binary: a single Go binary that receives coding-agent hook events (Claude Code, Cursor, Codex), relays them to the Speakeasy platform, enforces server policy decisions such as shadow MCP blocking, and performs browser sign-in on its own so it can recover authentication mid-session.
