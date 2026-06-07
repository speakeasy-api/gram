---
"server": patch
---

Drop the unused `assistant_dashboard_messages` table (contract migration). It was the dashboard-assistant conversation log written via the `platform_dashboard_send_message` egress tool and read via `assistants.listMessages`; the #3204 rearchitecture replaced that path with rendering the real conversation through `chat.load`, leaving the table fully orphaned (no reads, writes, queries, or code references). Completes the expand-contract from that change.
