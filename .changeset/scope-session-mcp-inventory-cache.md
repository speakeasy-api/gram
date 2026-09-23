---
"server": patch
---

Hooks session state is now bound to the project that owns it. Session MCP inventory snapshots, their inventory-read status, and the session agent variant are cached per project, and only a sender authenticated to that project can write them. A hooks key from one project can no longer change what the shadow-MCP guard reads for the same session id in another project. MCP inventory reported by unauthenticated Claude hooks is no longer recorded. The first project to attribute a session claims its cached identity atomically; that identity is only returned to its own project and can't be replaced by another, so another project's OTEL export can no longer redirect a session's unauthenticated hooks. Hooks buffered by an authenticated request are only flushed by that project. Sessions already in progress at deploy lose their cached inventory snapshot until their next inventory report.
