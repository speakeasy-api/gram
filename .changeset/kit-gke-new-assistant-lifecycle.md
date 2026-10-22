---
"server": patch
---

Add opt-in GKE provisioning for new Kit assistants with issuer-bound workload MCP bootstrap, canonical transcript reuse, and UID-fenced runtime authority. Existing conversations are not migrated. Rootfs-only checkpoints restore into fresh processes with renewed credentials and preserve canonical session bindings. Unsupported snapshot scopes and missing restore evidence fail closed.
