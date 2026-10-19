---
"server": patch
---

Add opt-in GKE provisioning for new Kit assistants with issuer-bound workload MCP bootstrap, canonical transcript reuse, and UID-fenced runtime authority. Existing conversations are not migrated. Snapshot checkpoint and cold-restore lifecycle requires a deployment-verified snapshot driver and fails closed when that contract is unavailable.
