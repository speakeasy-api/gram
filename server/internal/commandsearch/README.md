# Command resource search

Command K uses `GET /rpc/commandSearch.search?q=...` to search authorization-safe project resources without loading inventories into the browser. Normalized queries must contain 2–128 Unicode characters. The response carries schema version `1`, a generation timestamp, compact deterministic candidates, and partial-timeout metadata; final ranking remains local to the dashboard.

## Included resources

Search covers environments, named MCP gateways, plugins, and custom detection rules. These kinds have project or organization collection-level authorization that can be checked before their SQL runs. Assistants, MCP servers/toolsets, and skills are intentionally excluded because they support resource-specific grants that the current search SQL cannot express exactly; post-fetch authorization filtering is not used.

Every adapter uses one direct tenant/project-scoped SQL query with a normalized lowercase prefix interval and `LIMIT 21`, overfetching one row to detect truncation before returning at most 20 items per kind. Name/title predicates use `lower(column) >= lower_bound AND lower(column) < lower_ceiling`, backed by matching lifecycle-partial expression indexes. Canonical lowercase slugs and rule IDs use raw prefix intervals and their existing composite indexes. No adapter uses `LIKE`, `ILIKE`, a leading wildcard, HTTP, `COUNT`, pagination, or a whole-inventory transfer. Go ranks exact matches before slug/alias prefixes, then name prefixes, with stable kind/name/ID ties. Results are capped at 40 globally. Per-kind or global omissions set `truncated=true` and list the affected resource kinds in `truncated_kinds`; truncation is independent from timeout failure metadata.

Adapters run concurrently under one shared 250 millisecond deadline that starts before authorization preflight. Authorization failures, preflight timeouts or cancellations, and non-context adapter errors fail the request. Only an adapter interrupted by the shared deadline contributes to a partial response. An adapter deadline returns completed results with `partial=true`, `timed_out=true`, and the exact unfinished resource kinds in `failed_kinds`. Logs contain only durations, counts, payload-size estimates, and failure/partial flags—never names, selectors, IDs, or query text.

## Local query-plan evidence

`EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)` was run against transaction-scoped local fixtures: 10,000 rows were inserted into each retained table, 100 rows matched the prefix, tables were analyzed, and the transaction was rolled back. The normal planner was used; `enable_seqscan` was not changed. Every plan applied the production SQL `LIMIT 21`, returned 21 rows for truncation detection, and avoided sequential scans.

| Kind        | Plan/index evidence                                                                                                           | Shared hits | Execution |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------- | ----------: | --------: |
| Environment | `Bitmap Index Scan` on `environments_project_id_lower_name_idx` plus `environments_project_id_slug_key`                       |          12 |  0.104 ms |
| Gateway     | `Index Scan` on `meta_mcp_servers_organization_project_lower_name_idx`                                                        |           8 |  0.098 ms |
| Plugin      | `Bitmap Index Scan` on `plugins_organization_project_lower_name_idx` plus `plugins_organization_id_project_id_slug_key`       |          10 |  0.096 ms |
| Rule        | `Bitmap Index Scan` on `risk_custom_rules_org_project_lower_title_idx` plus `risk_custom_detection_rules_project_rule_id_key` |          10 |  0.093 ms |

## Deliberate exclusions

- Assistants, MCP servers/toolsets, and skills require exact resource-specific authorization that is unsafe to reproduce with post-fetch filtering.
- Deployments are history rather than durable named resources.
- Sources depend on deployment assets and external-MCP redirect rules.
- Guardrail policies require an additional product-feature contract.
- Access requests are workflow state rather than durable resource inventory.
- Agent identities and catalog entries have specialized organization/registry authorization and pagination contracts.
- People, managed devices, gateway connection actions, static/page/setup actions, recents, channels, chats, transcripts, messages, telemetry, audit/history, and other live or privacy-sensitive data are intentionally excluded.

## Platform MCP and demo data

No Platform MCP tool is added. Command search is a UI optimization rather than an agent outcome, and existing management tools already expose resource discovery.

No demo seed change is required: the existing fictional environments, gateways, plugins, and custom detection rules already cover every retained kind. The search endpoint persists no rows.
