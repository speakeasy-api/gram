---
"server": minor
---

Add Temporal scheduling for device integrations: a five-minute coordinator workflow fans out one child workflow per due sync (workflow-id deduped per org and sync), and a sync runner executes inventory pulls and evidence pushes. Inventory syncs upsert the MDM-reported fleet — resolving assigned emails to org members — and mark absent devices missing only in the transaction that records a fully completed snapshot, so a partial pull can never report unvisited devices as missing. Evidence pushes build the org's coverage snapshot and skip delivery when its digest matches the last successful push. Failures back off exponentially (capped at the schedule interval) and repeated credential rejections auto-pause the schedule; successes clear failure state by contract so recovered schedules render as healthy. Workflow and activity payloads carry sync ids only — credentials are decrypted inside the running activity and never enter Temporal history.
