---
"server": minor
---

feat: let organizations manage the Shadow AI scan targets their device agents probe for

The list of AI tools the device agent probes for is now served per organization on `agent.getPlugins`, as a server-owned `ai_scan` key in the remote-configuration document: the Speakeasy defaults compiled into Gram, overlaid with the targets an organization adds or customizes through the new `agent.listAiScanTargets`, `agent.upsertAiScanTarget`, and `agent.deleteAiScanTarget` endpoints. Adding a target no longer needs an agent release, every change is recorded in the organization's audit log, and the served `list_version` is what agents echo as `target_list_version` on scan receipts. Organization admins cannot set `ai_scan` directly in the settings document.
