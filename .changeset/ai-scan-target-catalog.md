---
"server": minor
---

feat: serve the Shadow AI scan target catalog to device agents from a platform-managed database catalog

The list of AI tools the device agent probes for now lives in Postgres and is injected into the remote-configuration document agents receive on `agent.getPlugins` as a server-owned `ai_scan` key, so adding a target no longer needs an agent release or a server deploy. Platform administrators manage the catalog through the new `platformAiScanTargets` service; every change is recorded as a catalog revision, and the revision number is what agents echo as `target_list_version` on scan receipts. Organization admins cannot set `ai_scan`, so an organization can never steer what the scanner probes for on employee devices.
