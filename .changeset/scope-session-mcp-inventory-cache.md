---
"server": patch
---

Hooks session state is now bound to the project that owns it. Session MCP inventory snapshots, their inventory-read status, and the session agent variant are cached per project, so a hooks key from one project can no longer change what the shadow-MCP guard reads for the same session id in another project. An unauthenticated SessionStart no longer seeds a snapshot the guard trusts. Cached session identity is only returned to the project that seeded it, and it can't be replaced by another project, so another project's OTEL export can no longer redirect a session's unauthenticated hooks. Hooks buffered by an authenticated request are only flushed by that project. Sessions already in progress at deploy lose their cached inventory snapshot until their next inventory report.
