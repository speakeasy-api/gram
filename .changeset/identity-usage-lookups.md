---
"server": minor
---

Add the remaining per-user lookups an identity view needs: devices, whole-id risk findings, and the shadow MCP servers one person reached.

- `deviceIntegrations.listManagedDevices` takes `user_ids` and `user_emails`, OR'd. Both legs are needed: a device only carries a resolved user id when the MDM's reported email matched a member, and the MDM's email can be an alias the directory does not know.
- `risk.listResults` takes `external_user_ids`, matched whole rather than as a substring. The existing `user_id` filter is a case-insensitive substring match, so filtering to `dev@acme.co` also returns `dev@acme.com`'s findings. Both the Postgres and ClickHouse paths honour the new filter; `user_id` is unchanged.
- `GET /rpc/access.listShadowMCPInventoryServersForUser` inverts the shadow MCP inventory. The table is URL-keyed with no user column, so the set of servers comes from that person's telemetry and is then enriched with the same policy state the project-wide listing shows.

The shadow MCP filter routes its email leg through the canonical identity fold, so one person's work and personal addresses resolve to the same subject instead of splitting across them.
