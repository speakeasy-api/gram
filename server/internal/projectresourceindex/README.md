# Project resource index

`GET /rpc/projectResourceIndex.get` returns one complete snapshot of the current project's authorized, openable resources for Command K local matching. It has no query parameter, pagination, per-kind cap, global cap, prefix filtering, or server ranking.

The response contract is versioned (`schema_version: "1"`) and timestamped (`generated_at`). `kinds` always contains assistant, environment, gateway, mcp, plugin, rule, and skill metadata. Every successful response reports `complete: true`, `overflow: false`, and the exact returned `item_count` for each kind. There is currently no count safety boundary. Any set-query error fails the whole request; the service never presents a partial snapshot as complete.

The seven direct set queries run concurrently. They retain project and organization tenancy, lifecycle/listability predicates, the existing collection-level authorization checks, plugin read/write fallback, MCP designated-host routing, and the `FeatureSkills` gate. Unauthorized or feature-disabled kinds are represented as complete empty sets.

## Indexed resources

- assistants: live assistants, UUID route selector
- environments: live environments, slug route selector
- gateways: live project gateways, UUID route selector
- MCP servers: live direct servers plus live toolsets with a designated openable host; reserved routes fall back to the host UUID
- plugins: live plugins, UUID route selector and slug keyword
- custom rules: live rules, rule ID route selector
- skills: unarchived skills when `FeatureSkills` is enabled

The former eight lowercase-name prefix indexes were removed because complete-set queries do not use them. Existing tenant, lifecycle, slug, and uniqueness indexes remain. No new index is justified without production-like cardinality evidence and `EXPLAIN (ANALYZE, BUFFERS)` showing a material improvement.

No Platform MCP tool is added: this endpoint is an internal dashboard inventory transport, not a new management outcome, and existing Platform MCP tools remain the agent-facing resource-management surface. No demo seed change is needed because the endpoint persists no data and existing fictional resources exercise every retained kind.

## Performance measurements

These are **observations, not latency or memory targets**. No product SLO or payload target has been assigned to this endpoint. The 50,000-item row is a synthetic safety-boundary stress case; the endpoint does not currently impose a 50,000-item boundary. Re-run on production-like cardinalities before using these numbers for capacity decisions.

Environment for the observed run: Apple M5 Pro, Darwin arm64, Go 1.27.4, Node 24.22.2. The database measurement used this worktree's local seeded PostgreSQL instance over loopback. Runs were taken from an otherwise lightly loaded development machine on 2026-11-13.

Repeat the measurements from the repository root:

```sh
# Synthetic backend: seven concurrent store calls, production adapters,
# sorting, compact JSON serialization, and gzip (five samples; median times).
cd server && mise exec -- go test ./internal/projectresourceindex \
  -run TestSyntheticIndexPerformanceEvidence -count=1 -v

# Synthetic local DB cardinalities: writes uniquely marked assistant fixtures,
# runs seven production SQL queries concurrently, then hard-deletes fixtures.
# The test refuses non-loopback database hosts.
cd server && PROJECT_RESOURCE_INDEX_DB_BENCH=1 mise exec -- go test \
  -tags projectresourceindexdbbench ./internal/projectresourceindex \
  -run TestSyntheticDatabaseQueryEvidence -count=1 -v

# Synthetic browser-side payload: parse, candidate mapping, and local rank
# (seven samples; median times).
mise exec -- aube run -F dashboard test \
  src/components/command-palette/project-resource-index.bench.test.ts \
  --reporter=verbose
```

### Synthetic complete-index results

The backend fixture distributes deterministic resources evenly across all seven kinds. Its `service index` column uses an in-memory store, so it measures the full production service's authorization checks, seven concurrent adapters, mapping, metadata, and sorting, and **is not a database timing**. `construct + JSON` adds conversion to the actual compact snake-case wire shape and serialization. Backend allocation is Go `TotalAlloc` delta (the largest of five runs), not retained heap. Client heap delta is a noisy, approximate median `heapUsed` delta without forced GC. Compact and gzip byte counts are exact for each benchmark's deterministic fixture; the client fixture differs slightly because it models the decoded mapping input independently.

|           Items | Backend query count | Backend service index (ms) | Backend construct + JSON (ms) | Backend compact JSON (bytes) | Backend gzip (bytes) | Backend allocated delta (bytes) | Client parse + map (ms) | Client heap delta, approx. (bytes) | Fuzzy prefilter + rank (ms) | Client compact JSON (bytes) | Client gzip (bytes) |
| --------------: | ------------------: | -------------------------: | ----------------------------: | ---------------------------: | -------------------: | ------------------------------: | ----------------------: | ---------------------------------: | --------------------------: | --------------------------: | ------------------: |
|           1,000 |                   7 |                      0.130 |                         0.911 |                      158,401 |               12,436 |                       2,512,992 |                   0.466 |                          1,323,216 |                       1.641 |                     166,984 |              14,609 |
|          10,000 |                   7 |                      0.815 |                         8.468 |                    1,579,104 |              118,775 |                      17,007,176 |                   4.332 |                         11,693,752 |                      13.753 |                   1,664,820 |             142,845 |
| 50,000 (stress) |                   7 |                      6.504 |                        48.800 |                    7,893,408 |              590,013 |                      85,168,256 |                  20.919 |                         21,740,120 |                      60.589 |                   8,321,991 |             712,527 |

The fuzzy measurement uses the production `prefilter` and `rank` functions with query `resource 049`; it scans the complete candidate set locally. The mapping benchmark reproduces the production candidate fields but substitutes no-op navigation callbacks, so it does not measure React rendering or route-object creation.

### Synthetic high-cardinality PostgreSQL query evidence

This measurement is distinct from the in-memory service and client measurements above. The opt-in test selects the richest local seeded project without printing its identifiers, then adds uniquely named and uniquely marked assistant fixtures until the **total returned index** reaches 1,000, 10,000, and 50,000 items. This intentionally concentrates cardinality in one kind: the unchanged seed contributes 34 non-assistant resources and 3 assistants, while synthetic assistants bring the assistant result to 966, 9,966, and 49,966 rows. At each size the same seven generated production repository queries start together. Insert setup is timed separately and excluded from query time.

Ordinary test runs do not connect to or write a database because `PROJECT_RESOURCE_INDEX_DB_BENCH=1` is required. Even when enabled, the benchmark refuses non-loopback hosts. A UUID marker is stored in `created_by_user_id` and included in every fixture name. A `t.Cleanup` registered before the first insert hard-deletes exactly that marker's rows, including when an assertion or query fails; the pool closes only after cleanup. Transaction rollback is not used because seven concurrent pgx queries cannot share one transaction.

Observed setup and concurrent wall times:

| Total indexed items | Assistants inserted this step | Insert setup (ms) | Seven-query concurrent wall (ms) |
| ------------------: | ----------------------------: | ----------------: | -------------------------------: |
|               1,000 |                           963 |            23.442 |                            2.104 |
|              10,000 |                         9,000 |           146.579 |                           10.184 |
|     50,000 (stress) |                        40,000 |           441.888 |                           55.950 |

Observed per-kind production query times:

| Total indexed items |          Assistant |  Environment |      Gateway |           MCP |        Plugin |         Rule |        Skill |
| ------------------: | -----------------: | -----------: | -----------: | ------------: | ------------: | -----------: | -----------: |
|               1,000 |     1.757 ms (966) | 0.450 ms (1) | 0.892 ms (2) | 2.080 ms (12) | 0.503 ms (13) | 0.696 ms (3) | 0.886 ms (3) |
|              10,000 |  10.158 ms (9,966) | 0.805 ms (1) | 0.736 ms (2) | 1.372 ms (12) | 0.784 ms (13) | 0.682 ms (3) | 0.788 ms (3) |
|     50,000 (stress) | 55.923 ms (49,966) | 1.017 ms (1) | 0.675 ms (2) | 2.324 ms (12) | 1.060 ms (13) | 0.876 ms (3) | 0.610 ms (3) |

Values in parentheses are returned item counts. The pre-insert seeded baseline was 37 items and 6.353 ms concurrent wall time. Database timings naturally vary with PostgreSQL cache state, connection scheduling, and machine load; these are observations rather than thresholds.
