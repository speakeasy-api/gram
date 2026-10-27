# Hosted MCP wrappers backfill

Gives every live hosted toolset (non-empty `mcp_slug`) that still has no canonical wrapper one: an `mcp_servers` row with `id = toolset.id` and `toolset_id = toolset.id`, plus exactly one live `mcp_endpoints` row at the toolset's address when its custom domain (if any) is live. A toolset bound to a soft-deleted custom domain gets no live endpoint (reported as `dead_custom_domain`). Each row is written by `hostedmcp.BackfillSync` from the toolset's frozen hosting columns. A toolset that already has a canonical wrapper is left untouched: the wrapper owns its hosting state. Postgres only; connects with `$GRAM_DATABASE_URL`.

```
go run ./server/cmd/tools/migrations hosted-mcp-wrappers -h
```

Flags: `-apply` (default is a dry run), `-project <PROJECT_ID>` or `-all-projects` (one is required), `-limit N`, `-cursor <TOOLSET_ID>`, `-report <path>`.

## Precondition

#7294 and #7298 must be deployed before `-apply`: serving resolves the canonical wrapper first and denies one that drifts from its toolset, and every toolset write keeps the wrapper in sync. Without them a backfilled wrapper would go stale on the next toolset edit.

## Behaviour

Candidates are walked in toolset id order. Each toolset runs in its own transaction: a per-toolset advisory lock, the toolset row `FOR UPDATE` (BackfillSync then locks custom domains, endpoints, and the server in that order), the guards re-checked, then BackfillSync. A dry run executes the same transaction and always rolls it back, so it briefly holds the same row locks but commits nothing.

Writes are audited as the system principal `system:hosted-mcp-wrapper-backfill`. Audit rows are recorded without publishing audit webhook events to the outbox. A transient database error (deadlock, lock or statement timeout, cancellation, lost connection) stops the run instead of being reported as a BackfillSync refusal; resume with `-cursor`. The toolset's `mcp_slug` is used verbatim, including platform slugs that predate the org-prefix rule; neither BackfillSync nor the address check rewrites or rejects them.

Toolset-backed `mcp_servers` rows with a fresh id (gateway members, create-from-source, Platform MCP) are separate servers. They are never adopted, modified, or deleted; they are only counted.

## Outcomes

| Outcome                      | Meaning                                                                                                                                                            |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `would_create` / `created`   | No canonical wrapper existed; wrapper and endpoint created.                                                                                                        |
| `already_complete`           | A canonical wrapper already exists. It owns its hosting state, so the tool never changes it; no writes.                                                            |
| `dead_custom_domain`         | Toolset is bound to a soft-deleted custom domain: wrapper synced, no live endpoint (the domain took its endpoints with it). `wrote` says whether anything changed. |
| `blocked_slug_collision`     | Another server holds the endpoint address or the project-scoped server slug.                                                                                       |
| `blocked_canonical_conflict` | The toolset's id is held by a tombstoned or foreign `mcp_servers` row.                                                                                             |
| `blocked_sync_rejected`      | BackfillSync refused the toolset (for example private network access without an online ingress); the reason is its message.                                        |
| `skipped`                    | The toolset was deleted, lost its slug, or changed concurrently since listing; rerun.                                                                              |

Every row also carries `fresh_id_servers` (other live toolset-backed servers for the toolset), and the summary counts `fresh_id_servers_present`. Rows list `domains_to_reconcile` when BackfillSync cleared a domain root; start a custom domain reconcile for each.

Stdout carries the mode, phase, outcome counts, rows scanned, toolsets walked, writes, and the last toolset id processed. The `-report` file holds ids, slugs, outcomes, reasons, and counts; it never holds names or emails.

## Run book

1. Dev: dry run with `-report`; review counts.
2. Dev: `-apply`; rerun `-apply` and confirm `writes` is 0 and every row is `already_complete` (or a repeated `dead_custom_domain` / `blocked_*`).
3. Prod: dry run with `-report`. Review every `blocked_*` row and the `fresh_id_servers_present` count before going on.
4. Prod canary: `-apply -project <PROJECT_ID>`; confirm the project's hosted servers still serve through their endpoints.
5. Prod: `-apply -all-projects -limit N` in batches, resuming with `-cursor` from the summary's `last_cursor`. Give each batch its own `-report` path: an `-apply` refuses an existing file.
6. Prod: a final `-apply -all-projects` without `-limit` must report `writes: 0`.

Failed runs exit nonzero; rows commit one toolset at a time, and an `-apply` run prints the resume cursor (a dry run commits nothing, so its cursor is never reused).

## Post-run checks

- The `mcp.toolset_slug_fallback` counter drops for traffic to the covered toolsets.
- The AIM-7 removal gate requires zero new increments to `mcp.toolset_slug_fallback` summed over every `gram.mcp.entry_point` value, and to `mcp.legacy_audience_accepted`, for at least the longest access-token lifetime (720h): entry points such as `shared_resource` and `org_gateway_probe` were split out of `oauth`, so no single tag covers all traffic.
- Plugin publication logs no `mcp_server_ambiguous` or "ambiguous toolset MCP serving wrapper".
