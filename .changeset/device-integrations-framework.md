---
"server": minor
"dashboard": patch
---

Add the device integrations framework: a capability-based provider registry (`InventorySource` for MDM fleet pulls, `EvidenceSink` for compliance evidence pushes) and a new `deviceIntegrations` management service. Organizations can connect a provider with secret credentials stored as an encrypted write-only document and non-secret settings kept readable, validated against the provider's declared field spec; credential rotation updates the config in place so synced device inventory is never orphaned. The service exposes provider discovery (credential specs drive dashboard form rendering), config CRUD with audit logging, a bounded test-connection probe through the SSRF-hardened guardian client, per-schedule state with distinct user-disable and system-auto-pause semantics, and agent-coverage reads: a bucketed summary (active / stale / no agent / no email / unresolved / missing, plus unmanaged agent users) and a paginated device listing, both computed as read-time joins between MDM inventory and the per-user agent heartbeat.

Settings updates merge per key with the stored document (omitted keys keep their values), credential rotation resets the schedules' sync execution state and pushed-snapshot digest, and audit before-snapshots are read inside the upsert transaction. The dashboard's OTel forwarding section is updated for a generated SDK type rename.
