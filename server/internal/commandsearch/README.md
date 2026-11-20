# Command resource search

Command K uses `GET /rpc/commandSearch.search?q=...` to search authorization-safe project resources without loading inventories into the browser. Normalized queries must contain 2–128 Unicode characters. The response carries schema version `1`, a generation timestamp, compact deterministic candidates, and partial-timeout metadata; final ranking remains local to the dashboard.

## Included resources

Search covers people, registered agent identities, assistants, environments, named MCP gateways, MCP servers/toolsets, plugins, custom detection rules, and skills. People are active organization members gated by organization or project read access and queried by display-name or email prefix with `LIMIT 21`. Registered agents are available only to ordinary human sessions; live membership and the agent roster's ownership/read-grant rules are applied to prefix matches before caps. Environment, gateway, plugin, rule, and skill authorization is checked before SQL runs. Assistant and MCP grants can name or exclude individual resources, so those prefix matches are filtered through the same row-level authorization checks as their list endpoints before any response cap is applied. Skills are omitted when the skills product feature is disabled, and feature-check errors fail preflight.

Every adapter uses direct tenant/project-scoped SQL with a normalized lowercase prefix interval. Collection-authorized kinds use `LIMIT 21`, overfetching one row to detect truncation. Assistant and MCP queries retain every prefix match until row authorization has run, preventing denied early rows from hiding a later authorized match. Registered agents are different because readability is evaluated in Go: their adapter walks 200-row keyset pages only until it finds 21 readable matches, exhausts the prefix, reaches the shared deadline, or hits the same 20,000-row hard scan ceiling as the canonical agent roster. It fails rather than silently treating that ceiling as exhaustion. Every kind returns at most 20 items to the browser. Name/title predicates use `lower(column) >= lower_bound AND lower(column) < lower_ceiling`, backed by matching lifecycle-partial expression indexes. Canonical lowercase slugs and rule IDs use raw prefix intervals and their existing composite indexes. No adapter uses `LIKE`, `ILIKE`, a leading wildcard, HTTP, or `COUNT`, and no whole inventory crosses the network. Go ranks exact matches before slug/alias prefixes, then name prefixes, with stable kind/name/ID ties. Results are capped at 40 globally. Per-kind or global omissions set `truncated=true` and list the affected resource kinds in `truncated_kinds`; truncation is independent from timeout failure metadata.

Adapters run concurrently under one shared 250 millisecond deadline that starts before authorization preflight. Authorization failures, preflight timeouts or cancellations, and non-context adapter errors fail the request. Only an adapter interrupted by the shared deadline contributes to a partial response. An adapter deadline returns completed results with `partial=true`, `timed_out=true`, and the exact unfinished resource kinds in `failed_kinds`. Logs contain only durations, counts, payload-size estimates, and failure/partial flags—never names, selectors, IDs, or query text.

## Local query-plan evidence

`EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)` was run against transaction-scoped local fixtures: 10,000 rows were inserted into each retained table, 100 rows matched the prefix, tables were analyzed, and the transaction was rolled back. The normal planner was used; `enable_seqscan` was not changed. The four collection-authorized plans below applied the production SQL `LIMIT 21`, returned 21 rows for truncation detection, and avoided sequential scans.

| Kind        | Plan/index evidence                                                                                                           | Shared hits | Execution |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------- | ----------: | --------: |
| Environment | `Bitmap Index Scan` on `environments_project_id_lower_name_idx` plus `environments_project_id_slug_key`                       |          12 |  0.104 ms |
| Gateway     | `Index Scan` on `meta_mcp_servers_organization_project_lower_name_idx`                                                        |           8 |  0.098 ms |
| Plugin      | `Bitmap Index Scan` on `plugins_organization_project_lower_name_idx` plus `plugins_organization_id_project_id_slug_key`       |          10 |  0.096 ms |
| Rule        | `Bitmap Index Scan` on `risk_custom_rules_org_project_lower_title_idx` plus `risk_custom_detection_rules_project_rule_id_key` |          10 |  0.093 ms |

The remaining corpora have matching lifecycle-partial expression indexes: `assistants_project_id_lower_name_idx`, `mcp_servers_project_id_lower_name_listable_idx`, `toolsets_project_id_lower_name_idx`, `skills_project_id_lower_display_name_idx`, `users_display_name_lower_active_idx`, the existing `users_email_lower_idx`, and the existing `agents_organization_name_key`. Existing slug/name indexes cover their raw aliases. MCP name/slug ranges are applied independently inside the direct-server and toolset branches before `UNION ALL`, so only matching rows enter the final deterministic 21-row sort. Toolsets enter that branch only through an inner lateral designated-host lookup: the canonical live wrapper first, otherwise the oldest live endpoint-backed wrapper.

## Production-scale browser evidence

A loopback-only fixture added 50,000 assistants to the local default project and was deleted after the run. The rejected full-index design transferred 9.25 MB in 368 ms and then blocked the browser for more than 30 seconds. With bounded search, a broad prefix returned 20 items in a 3.28 KB body; the browser resource timing was 163 ms and the palette remained responsive. An exact match on the 50,000th assistant returned in 18.6 ms with a 324-byte body. After the page settled, opening an empty palette issued no inventory requests; only the existing `auth.info` request used to scope recents remained.

## Deliberate exclusions

- Deployments are history rather than durable named resources.
- Sources depend on deployment assets and external-MCP redirect rules.
- Guardrail policies require an additional product-feature contract.
- Access requests are workflow state rather than durable resource inventory.
- Catalog entries retain their specialized registry authorization and pagination contract.
- Managed devices, gateway connection actions, static/page/setup actions, recents, channels, chats, transcripts, messages, telemetry, audit/history, and other live or privacy-sensitive data are intentionally excluded.

## Platform MCP and demo data

No Platform MCP tool is added. Command search is a UI optimization rather than an agent outcome, and existing management tools already expose resource discovery.

No demo seed change is required: existing fictional members and registered agents cover the migrated identity kinds and the endpoint persists no data. The search endpoint persists no rows.
