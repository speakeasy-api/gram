# hooks

## 0.2.0

### Minor Changes

- 85f1496: Cut a speakeasy-hooks release containing the DNO-497/DNO-498 client work that merged after hooks@0.1.1 was tagged and therefore never shipped: the org-settings fail-open cache (the binary mirrors the server-confirmed `fail_open` posture next to its credential cache and consults it when no verdict is obtainable, so fail-open orgs actually fail open during control-plane outages), the offline payload spool + drain (unsent events buffer locally and replay on recovery), and the typed replay marker header. Without this release the pinned 0.1.1 binary fails closed for every org during an outage and spools nothing. After the release is published, `hooksBinaryVersion` and the embedded archive checksums in `server/internal/plugins/hooks_bootstrap.go` must be re-pinned in a follow-up.

## 0.1.1

### Patch Changes

- 63008ae: Restore Claude MCP inventory capture in the Go hooks relay. Session start and configuration-change hooks now send a locally redacted inventory snapshot through canonical ingest so external MCP URLs appear in Shadow MCP inventory before a tool is called.

## 0.1.0

### Minor Changes

- 22fb780: Introduce the speakeasy-hooks binary: a single Go binary that receives coding-agent hook events (Claude Code, Cursor, Codex), relays them to the Speakeasy platform, enforces server policy decisions such as shadow MCP blocking, and performs browser sign-in on its own so it can recover authentication mid-session.
