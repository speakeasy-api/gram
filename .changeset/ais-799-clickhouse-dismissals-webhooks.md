---
"server": patch
---

Risk finding dismissals now use ClickHouse as their source of truth while preserving Postgres compatibility and audit records. Finding-created webhooks can cut over per organization to the ClickHouse writer without duplicate customer deliveries.
