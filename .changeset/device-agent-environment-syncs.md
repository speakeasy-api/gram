---
"server": patch
---

Internal: adds the `device_agent_environment_syncs` table, which records device-agent heartbeats from machines that are not an individual's endpoint — cloud coding sandboxes, containers, and shared self-hosted servers. Nothing reads or writes it in this release; the agent poll starts populating it in a follow-up.
