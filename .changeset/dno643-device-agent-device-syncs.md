---
"server": minor
---

mig: add device_agent_device_syncs for per-device agent heartbeats

Sibling of device_agent_syncs, keyed on (organization_id, serial_number)
instead of email, plus the case-insensitive serial indexes both sides of the
coverage join will need. Schema only — nothing reads or writes the table yet.
