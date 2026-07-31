---
"server": minor
---

Auto-provision the Drata Custom Connection on connect. When an evidence-sink provider implements the new optional `Provisioner` capability, the connect flow creates its vendor-side object and stores the resulting ids, so the customer no longer hand-crafts it against the vendor API. Drata implements it: it find-or-creates the dedicated Custom Connection with the exact record schema and `required` list (omitting `agentLastSeenAt` so never-seen-agent records are never rejected), keyed on a deterministic name so a re-save reuses the connection instead of duplicating it. A new optional `workspace_id` field defaults to 1, and `connection_id` becomes optional — filled in automatically.
