# Command resource search

Command K uses `GET /rpc/commandSearch.search?q=...` to search authorization-safe project resources without loading inventories into the browser. Normalized queries must contain 2–128 Unicode characters. The response carries schema version `1`, a generation timestamp, compact deterministic candidates, and partial-timeout metadata; final ranking remains local to the dashboard.

## Included resources

Search covers environments, named MCP gateways, plugins, and custom detection rules. These kinds have project or organization collection-level authorization that can be checked before their SQL runs. Assistants, MCP servers/toolsets, and skills are intentionally excluded because they support resource-specific grants that the current search SQL cannot express exactly; post-fetch authorization filtering is not used.

Every adapter uses one direct tenant/project-scoped SQL query with a normalized lowercase prefix interval and `LIMIT 20`. Name/title predicates use `lower(column) >= lower_bound AND lower(column) < lower_ceiling`, backed by matching lifecycle-partial expression indexes. Canonical lowercase slugs and rule IDs use raw prefix intervals and their existing composite indexes. No adapter uses `LIKE`, `ILIKE`, a leading wildcard, HTTP, `COUNT`, pagination, or a whole-inventory transfer. Go ranks exact matches before slug/alias prefixes, then name prefixes, with stable kind/name/ID ties. Results are capped at 40 globally.

Adapters run concurrently under one shared 250 millisecond deadline that starts before authorization preflight. Authorization and non-timeout adapter errors fail the request. A timeout returns completed results with `partial=true`, `timed_out=true`, and the exact unfinished resource kinds in `failed_kinds`. Logs contain only durations, counts, payload-size estimates, and failure/partial flags—never names, selectors, IDs, or query text.

## Local query-plan evidence

`EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)` was run against transaction-scoped local fixtures: 10,000 rows were inserted into each retained table, 100 rows matched the prefix, tables were analyzed, and the transaction was rolled back. The normal planner was used; `enable_seqscan` was not changed. Every plan applied the SQL `LIMIT 20`, returned 20 rows, and avoided sequential scans.

| Kind        | Plan/index evidence                                                                                                           | Shared hits | Execution |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------- | ----------: | --------: |
| Environment | `Bitmap Index Scan` on `environments_project_id_lower_name_idx` plus `environments_project_id_slug_key`                       |           9 |  0.067 ms |
| Gateway     | `Index Scan` on `meta_mcp_servers_organization_project_lower_name_idx`                                                        |           8 |  0.055 ms |
| Plugin      | `Bitmap Index Scan` on `plugins_organization_project_lower_name_idx` plus `plugins_organization_id_project_id_slug_key`       |          11 |  0.059 ms |
| Rule        | `Bitmap Index Scan` on `risk_custom_rules_org_project_lower_title_idx` plus `risk_custom_detection_rules_project_rule_id_key` |          10 |  0.056 ms |

To reproduce without durable data, pipe a psql script through `docker exec -i gram-gram-db-1 psql -U gram -d gram`: start `BEGIN`, select one local live project and organization into psql variables with `\gset`, insert 10,000 synthetic rows per table from `generate_series(1, 10000)`, run `ANALYZE` on the four tables, execute the four production selects from `queries.sql` under `EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)` with a synthetic prefix interval, and finish with `ROLLBACK`. Do not substitute production or customer identifiers.

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
