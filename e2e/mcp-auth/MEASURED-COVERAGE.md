# Measured typed-client authentication coverage

> **Historical 44-case baseline.** Additional hypothesis-driven path cases are being measured separately; this table is not the complete expanded inventory. See `STATE-CONSTRUCTION.md` for hand-built-state requirements, and run `mise run test:mcp-auth --list` for current cases.

Latest completed attempt per scenario identity, not a sum of historical reruns. All browser setup uses the real UI; SQL diagnostics are read-only and never provision or repair fixtures. These are synthetic cases, not exported production records.

**44 scenarios attempted: 15 passed, 29 failed, 0 timed out.** Every recorded scenario has the persisted-enforcement prerequisite annotation and a local auth-graph attachment (**44/44** each). No retry or expected-failure inversion is configured. This is not a green suite.

## Measurement boundaries

- Results span serial invocations and are not one atomic environment snapshot. UI driver corrections between runs are documented in `CAPABILITY-TRACE.md`; follow-up correction runs supersede only their selected scenario identities.
- The first invocation reached its 30-minute suite budget after 15 completed cases. Its other 29 cases did not run (one machine `skipped` record and 28 empty results); the remaining invocation subsequently attempted all 29. No unrun/skipped record is counted as coverage.
- Failure labels identify the observed assertion/protocol boundary, not necessarily a proven product root cause. Fixture/UI failures are not token-routing evidence.
- A lifecycle test that fails in its shared setup does not measure the named later mutation. In particular, shared personal/static graph loss blocks disable/detach/rotation, and existing-personal HTTP 401 can block gateway mutations before they start.
- Graph counts are projects / logical servers / stored gateways from the widest-scope attached snapshot; they do not merge unrelated capture aliases. They observe actual UI-created state. Automatic organization/project gateway membership is explicitly `not_captured`; protocol assertions, not these counts, establish that inventory.
- Aliases are local to each capture and must not be compared across snapshots. Graphs exclude raw record IDs, credential values and identities. Production topology/audit data remains separate and private.
- Failed synthetic projects remain for diagnosis; successful fixtures are cleaned through UI controls. Enforcement restoration is cleanup, never legacy-path/rollback coverage.
- The 21 recipe families / 135 model expansions are an independently validated inventory, not 135 executed browser scenarios. EMA, second-human isolation, workload federation and complete recovery/stale-selection transitions remain gaps.

## Latest per-case outcomes

| Scenario                                                                                                                              | Result | Graph P/S/G | Observed boundary                     | Evidence run    |
| ------------------------------------------------------------------------------------------------------------------------------------- | ------ | ----------- | ------------------------------------- | --------------- |
| enforced agents: two independent keys select distinct static offers and detach affects only the selected offer                        | passed | 1/1/0       | Assertions passed                     | corrections     |
| enforced agents: two independent keys select distinct static offers and disable affects only the selected offer                       | passed | 1/1/0       | Assertions passed                     | corrections     |
| enforced agents: two independent keys select distinct static offers and rotate affects only the selected offer                        | passed | 1/1/0       | Assertions passed                     | unique-fixtures |
| enforced cross-project: organization-scoped static client shares rotation across distinct issuers without merging personal identities | passed | 2/2/0       | Assertions passed                     | timeout-fix     |
| enforced cross-project: project-scoped static is absent from other project picker and its issued agent cannot access that project     | passed | 2/3/0       | Assertions passed                     | timeout-fix     |
| enforced gateway methods: detach chosen shared offer respects scope across overlapping stored gateways                                | failed | 1/2/2       | Protocol HTTP 401                     | full            |
| enforced gateway methods: disable chosen shared offer respects scope across overlapping stored gateways                               | failed | 1/2/2       | Protocol HTTP 401                     | full            |
| enforced gateway methods: one shared credential on two sources through project gateway                                                | failed | 1/2/0       | GATEWAY_FILTER_REGRESSION             | full            |
| enforced gateway methods: one shared credential on two sources through stored gateway                                                 | failed | 1/2/1       | Protocol HTTP 400                     | full            |
| enforced gateway methods: overlapping stored gateways retain distinct named offers and direct personal grants through refresh         | failed | 1/2/2       | Protocol HTTP 401                     | timeout-fix     |
| enforced gateway methods: rotate chosen shared offer respects scope across overlapping stored gateways                                | failed | 1/2/2       | Protocol HTTP 401                     | full            |
| enforced methods: adding same-provider different-resource server preserves the original personal offer                                | failed | 1/2/0       | SIBLING_BINDING_LOSS_REGRESSION       | remaining       |
| enforced methods: adding same-provider same-resource server preserves the original personal offer                                     | failed | 1/2/0       | SIBLING_BINDING_LOSS_REGRESSION       | remaining       |
| enforced methods: caller method shared across two sources never borrows another caller's header or personal login                     | failed | 1/2/0       | TYPED_OFFER_DISCOVERY_REGRESSION      | full            |
| enforced methods: detaching shared client from A denies A but leaves B and personal choices usable                                    | failed | 1/2/0       | PERSONAL_BINDING_LOSS_REGRESSION      | remaining       |
| enforced methods: disabling shared client denies both existing users while personal sessions continue                                 | failed | 1/2/0       | PERSONAL_BINDING_LOSS_REGRESSION      | remaining       |
| enforced methods: personal plus two static offers select each explicitly and retain all sessions                                      | passed | 1/1/0       | Assertions passed                     | remaining       |
| enforced methods: personal static and caller offers explicitly select one channel without credential leakage                          | passed | 1/1/0       | Assertions passed                     | full            |
| enforced methods: rotating shared stored value updates both servers without changing other selections                                 | failed | 1/2/0       | PERSONAL_BINDING_LOSS_REGRESSION      | remaining       |
| enforced methods: same static client on two servers keeps personal alternatives isolated                                              | failed | 1/2/0       | PERSONAL_BINDING_LOSS_REGRESSION      | remaining       |
| enforced multiple OAuth: alpha personal beta personal and static choices preserve old fresh and refreshed sessions                    | failed | 1/2/0       | Protocol HTTP 401                     | remaining       |
| enforced multiple OAuth: disabling selected beta offer denies its session while alpha static and beta source B remain valid           | failed | 1/2/0       | Protocol HTTP 401                     | remaining       |
| enforced multiple OAuth: same administrator beta personal offer attaches to two dual-provider sources without replacing alpha or B    | failed | 1/3/0       | Protocol HTTP 401                     | remaining       |
| enforced: G1={A,B}, G2={A,C} isolate catalogs and membership changes from direct callers                                              | failed | 1/3/2       | MCP JSON-RPC error; reason unresolved | remaining       |
| enforced: no-auth upstream still requires a real downstream user session                                                              | passed | 1/1/0       | Assertions passed                     | remaining       |
| enforced: one server offers personal OAuth and a shared credential                                                                    | passed | 1/1/0       | Assertions passed                     | remaining       |
| enforced: organization gateway retains two unrelated OAuth providers                                                                  | failed | 1/2/0       | GATEWAY_FILTER_REGRESSION             | remaining       |
| enforced: organization, automatic project, and stored gateways overlap on the same source                                             | failed | 1/2/1       | GATEWAY_FILTER_REGRESSION             | remaining       |
| enforced: pass-through header is configured in dashboard and supplied only by the MCP caller                                          | passed | 1/1/0       | Assertions passed                     | remaining       |
| enforced: personal OAuth and an auxiliary static header coexist                                                                       | passed | 1/1/0       | Assertions passed                     | remaining       |
| enforced: regular user OAuth, fresh upstream expiry, refresh and revocation                                                           | passed | 1/1/0       | Assertions passed                     | remaining       |
| enforced: reused registration requests target resource scopes instead of issuer defaults                                              | failed | 1/2/0       | TYPED_SCOPE_REGRESSION                | remaining       |
| enforced: same direct server through project gateway preserves both provider grants                                                   | failed | 1/2/0       | GATEWAY_FILTER_REGRESSION             | full            |
| enforced: same direct server through stored gateway preserves both provider grants                                                    | passed | 1/2/1       | Assertions passed                     | full            |
| enforced: shared registration keeps distinct resources without an issuer bridge                                                       | failed | 1/2/0       | TYPED_RESOURCE_REGRESSION             | remaining       |
| enforced: static header configured in dashboard reaches upstream                                                                      | passed | 1/1/0       | Assertions passed                     | remaining       |
| enforced: typed multi-offer source keeps direct OAuth while stored gateways select static credentials                                 | passed | 1/1/2       | Assertions passed                     | remaining       |
| native tunnel with explicit resource and scoped shared self credential, direct and project gateway                                    | failed | 1/2/0       | Protocol HTTP 412                     | corrections     |
| native tunnel with explicit resource and scoped shared self credential, direct and stored gateway                                     | failed | 1/2/0       | Protocol HTTP 412                     | corrections     |
| native tunnel without an upstream resource stays anonymous beside a signed-in sibling, direct and stored gateway                      | failed | 1/2/0       | UI_CONSENT_BLOCKER                    | corrections     |
| organization-scoped self credential is reused across two UI-created projects                                                          | failed | 2/2/0       | SELF_RESOURCE_REGRESSION              | remaining       |
| shared service account on the same direct sources and a stored gateway                                                                | failed | 1/2/0       | SELF_RESOURCE_REGRESSION              | remaining       |
| shared service account on two direct servers: existing and fresh clients                                                              | failed | 1/2/0       | SELF_RESOURCE_REGRESSION              | remaining       |
| shared service account on two direct servers: existing and fresh clients after nominal expiry                                         | failed | 1/2/0       | SELF_RESOURCE_REGRESSION              | remaining       |

## Local evidence provenance

Artifacts below are ignored and may contain local session or one-time credentials. Do not publish traces, screenshots or raw reports without review. The table above contains only synthetic titles, counts and failure categories.

- **full** — start `2026-11-04T14:38:43.719Z`; machine result statuses `{'passed': 2, 'failed': 12, 'timedOut': 1, 'skipped': 1}`. Preserved report: `.logs/mcp-auth/full-enforced-graph-evidence/results.json`.
- **remaining** — start `2026-11-04T15:14:51.257Z`; machine result statuses `{'failed': 21, 'passed': 8}`. Preserved report: `.logs/mcp-auth/remaining-enforced-graph-evidence/results.json`.
- **corrections** — start `2026-11-04T15:42:21.140Z`; machine result statuses `{'passed': 2, 'failed': 6}`. Preserved report: `.logs/mcp-auth/fixture-corrections-evidence/results.json`.
- **unique-fixtures** — start `2026-11-04T15:53:04.323Z`; machine result statuses `{'passed': 1, 'failed': 2}`. Preserved report: `.logs/mcp-auth/unique-fixture-evidence/results.json`.
- **timeout-fix** — start `2026-11-04T15:58:14.422Z`; machine result statuses `{'passed': 2, 'failed': 1}`. Preserved report: `.logs/mcp-auth/gateway-timeout-fix-evidence/results.json`.

Isolated checks: **25 unit tests passed** (`mise run test:mcp-auth:unit`), and E2E type-check passed (`aube run -F @gram/e2e type-check`). These checks validate infrastructure, not successful product authentication.
