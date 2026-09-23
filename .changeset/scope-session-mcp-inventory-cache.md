---
"server": patch
---

Session MCP inventory snapshots, their inventory-read status, and the session agent variant are now cached per project. A hooks key from one project can no longer change what the shadow-MCP guard reads for the same session id in another project, and an unauthenticated SessionStart no longer seeds a snapshot the guard trusts. Sessions already in progress at deploy lose their cached snapshot until their next inventory report.
