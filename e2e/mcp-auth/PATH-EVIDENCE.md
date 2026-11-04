# Expanded auth-path evidence

The supplied gap catalog is treated as hypotheses, not expected failures. This ledger records latest completed attempts per scenario, not one atomic full-suite result. Failed setup is not execution evidence for later steps.

**Inventory: 81 browser cases. Recorded statuses: 29 failed, 37 not measured, 15 passed.**
Persisted enforcement verified in 44/44 recorded attempts; auth-graph attachment in 44/44. This does not independently attest every request avoided compatibility hand-back.

## Fixture construction

Browser fixtures are created through actual dashboard/staff UI. Mock controls only affect local protocol fixtures. No manual SQL state was created. [STATE-CONSTRUCTION.md](./STATE-CONSTRUCTION.md) separately lists historical/aged/adversarial persisted state, in-memory contracts, and environment prerequisites.

## Product-boundary evidence (separate from browser counts)

`mise run test:mcp-auth:contracts` executed actual production functions with synthetic structs, HTTP responses and the delegation memory store. The measured combined run recorded **49 test entries, 11 failing entries**. This includes failed parent tests: the distinct failing leaf contracts are six stricter typed-serving guards (G22/G23/G24) and three configuration-error grant-preservation cases (G11). Parent-package operational/parser/resource tests passed.

The stricter guards reject current explicitly permission-gated compatibility hand-backs; they do not demonstrate an authorization bypass or establish that the agent report's proposed root cause is correct. The G11 classifier correctly identifies admin-remediation errors; preservation is tested separately by executing the real memory-state service. Shadow characterization is not parity proof. No SQL selection or persistence is claimed for in-memory tests.

## Gap hypothesis / path mapping

Browser counts below are references, not disjoint totals; one case can examine multiple hypotheses. A failed case can stop before the named target. Consult named test steps and preserved journals.

| Hypothesis | Browser attempts / inventory | Evidence boundary                                                                                                                                                                |
| ---------- | ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| G1         | 1 failed, 1 not measured     | Direct exact-resource red exists; gateway typed-sharing variant is independent. Card/reconnect stages count only if reached.                                                     |
| G2         | 1 not measured               | Typed shared-registration browser variant; ambiguous legacy association variant remains unconstructed.                                                                           |
| G3         | 3 failed, 1 passed           | Missing-member contracts include successful direct controls. Filtering cause is not inferred from missing inventory.                                                             |
| G4         | 9 not measured               | New/existing typed-only card actions plus independent bridge-backed supported baselines; real buttons, not protocol refresh substitutes.                                         |
| G5         | 1 failed, 2 not measured     | Login-scope and card disclosure assertions are separate; card success would not erase a login-resource/scope failure.                                                            |
| G6         | No browser variant           | Production resource-selection helpers only; real idle keepalive needs aged state/controllable clock and scheduling. Not worker E2E.                                              |
| G7         | 1 failed, 1 not measured     | A-survival/refresh requires B authorization to complete. Earlier resource failure blocks overwrite measurement.                                                                  |
| G8         | 2 failed, 2 not measured     | Sibling issuer/client identities use stable per-fixture aliases; unsigned remote/native variants assert absence of borrowed headers.                                             |
| G9         | 1 not measured               | Gateway auth-policy transition/re-consent; Code Mode variant is not established.                                                                                                 |
| G10        | 1 not measured               | Headless workload personal-connection recovery is distinct from missing-connection-at-creation and issuance-profile UI administration.                                           |
| G11        | No browser variant           | Real delegation memory Resolve: transient recovery/fatal negative control work; three configuration-error preservation variants fail. HTTP classification itself passes.         |
| G12        | 1 not measured               | Reversible issuer outage and same-grant recovery; UI status presentation is credited only if asserted.                                                                           |
| G13        | 1 not measured               | Actual HTTP caller cancellation at controlled rotation boundary, not mock-only token rotation.                                                                                   |
| G14        | 1 not measured               | Actual token parser handles zero as unreported; browser test checks usability and reconnect after real revocation.                                                               |
| G15        | 1 not measured               | Public native isolation beside live/dead personal grant. Not proof of an anonymous-owned hosted-public grant scenario.                                                           |
| G16        | 1 not measured               | Actual assistant runtime turn and upstream tool identity; model/runtime failures are environment blockers, not OAuth evidence.                                                   |
| G17        | 1 not measured               | Real HTTPS workload assertions/JWKS and shared-AS server membership; not static API-key or upstream client-credentials substitution.                                             |
| G18        | 1 not measured               | Typed picker eligibility/labels and registration creation journey; ordinary bare-ID selection is not sufficient.                                                                 |
| G19        | 1 not measured               | Current UI OAuth-client rotation plus parser boundaries; historical copied-scope provenance is separately unconstructed.                                                         |
| G20        | No browser variant           | In-memory actual unmapped-choice enforcement boundary. Intentional reconnect, not completed browser recovery.                                                                    |
| G21        | No browser variant           | In-memory incomplete-backfill/setup/stale-choice transitions. No persisted backfill or complete reconnect journey constructed.                                                   |
| G22        | 2 not measured               | EMA/chained protocol cases plus permission/decision contracts. Current mapped compatibility hand-back fails the stricter typed-serving guard; not evidence of permission bypass. |
| G23        | 6 not measured               | Native static/pass-through browser cases and mapped-client permission boundary. Forwarding success alone cannot prove no compatibility hand-back.                                |
| G24        | No browser variant           | Mirrored-copy compatibility/permission boundary and stricter typed-serving guard. Historical drift provenance not constructed.                                                   |
| G25        | 1 not measured               | Exact gateway member auth classification plus execution; frozen/live inventory semantics remain distinct.                                                                        |
| G26        | No browser variant           | Backend shadow characterization only, not typed browser serving or proof of full shadow parity.                                                                                  |
| G27        | 2 not measured               | Issuance-profile administration and actual assistant execution-profile cases are independently labeled.                                                                          |
| G28        | 1 not measured               | Real stale-tab mutation rejects with conflict/precondition and rendered review; no requirement that the report's exact 409 number is the only valid status.                      |

## Latest per-browser-case records

| Scenario                                                                                                                              | Status       | Enforcement / graph            | Observed boundary                | Evidence run    |
| ------------------------------------------------------------------------------------------------------------------------------------- | ------------ | ------------------------------ | -------------------------------- | --------------- |
| G1 G2 UI-created typed shared OAuth registration preserves distinct gateway resource handbacks                                        | not measured | not established / not attached | Implemented; no completed result | —               |
| G10 headless: personal-only offer requires setup; UI reconnects it and the same workload succeeds without a choice                    | not measured | not established / not attached | Implemented; no completed result | —               |
| G12: reversible issuer outage is unavailable, never anonymous, then recovers                                                          | not measured | not established / not attached | Implemented; no completed result | —               |
| G13: disconnect after refresh rotation does not strand the surviving caller                                                           | not measured | not established / not attached | Implemented; no completed result | —               |
| G14: expires_in zero is unreported lifetime, not immediate expiration                                                                 | not measured | not established / not attached | Implemented; no completed result | —               |
| G15: public native tunnel never borrows a live or revoked personal upstream grant                                                     | not measured | not established / not attached | Implemented; no completed result | —               |
| G16 assistant execution: personal OAuth choice binds a profile and later tool turns do not sign in again                              | not measured | not established / not attached | Implemented; no completed result | —               |
| G17 workload: one shared-AS token admits its assigned server and rejects a sibling                                                    | not measured | not established / not attached | Implemented; no completed result | —               |
| G18 enforced: typed registration picker labels, ownership eligibility, and creation-to-connection journey                             | not measured | not established / not attached | Implemented; no completed result | —               |
| G19 enforced: OAuth registration rotation preserves scopes and attachments with a new client identity                                 | not measured | not established / not attached | Implemented; no completed result | —               |
| G22 EMA identity chain: UI setup redeems a verified assertion and runs an identity-bound tool                                         | not measured | not established / not attached | Implemented; no completed result | —               |
| G22 personal OAuth offer with EMA fallback: UI setup redeems a verified assertion and runs an identity-bound tool                     | not measured | not established / not attached | Implemented; no completed result | —               |
| G23: native tunnel caller header reaches only its selected upstream through direct                                                    | not measured | not established / not attached | Implemented; no completed result | —               |
| G23: native tunnel caller header reaches only its selected upstream through project                                                   | not measured | not established / not attached | Implemented; no completed result | —               |
| G23: native tunnel caller header reaches only its selected upstream through stored                                                    | not measured | not established / not attached | Implemented; no completed result | —               |
| G23: native tunnel static header reaches only its selected upstream through direct                                                    | not measured | not established / not attached | Implemented; no completed result | —               |
| G23: native tunnel static header reaches only its selected upstream through project                                                   | not measured | not established / not attached | Implemented; no completed result | —               |
| G23: native tunnel static header reaches only its selected upstream through stored                                                    | not measured | not established / not attached | Implemented; no completed result | —               |
| G25 connection_status classifies each member by its own authentication                                                                | not measured | not established / not attached | Implemented; no completed result | —               |
| G27 administration: ambiguous offers require an explicit app choice; removal and recovery persist                                     | not measured | not established / not attached | Implemented; no completed result | —               |
| G27 assistant execution: ambiguous execution profile requires administrator choice and tool uses that identity                        | not measured | not established / not attached | Implemented; no completed result | —               |
| G28 stale captured consent requests explicit re-review across real tabs then permits recovery                                         | not measured | not established / not attached | Implemented; no completed result | —               |
| G4 enforced: existing consent card Disconnect preserves exact OAuth identity and reconnects after revocation                          | not measured | not established / not attached | Implemented; no completed result | —               |
| G4 enforced: existing consent card Refresh preserves exact OAuth identity                                                             | not measured | not established / not attached | Implemented; no completed result | —               |
| G4 enforced: existing consent card Verify preserves exact OAuth identity                                                              | not measured | not established / not attached | Implemented; no completed result | —               |
| G4 enforced: new consent card Disconnect preserves exact OAuth identity and reconnects after revocation                               | not measured | not established / not attached | Implemented; no completed result | —               |
| G4 enforced: new consent card Refresh preserves exact OAuth identity                                                                  | not measured | not established / not attached | Implemented; no completed result | —               |
| G4 enforced: new consent card Verify preserves exact OAuth identity                                                                   | not measured | not established / not attached | Implemented; no completed result | —               |
| G4 supported typed card Disconnect and reconnect with persisted enforcement and issuer bridge                                         | not measured | not established / not attached | Implemented; no completed result | —               |
| G4 supported typed card Refresh with persisted enforcement and issuer bridge                                                          | not measured | not established / not attached | Implemented; no completed result | —               |
| G4 supported typed card Verify with persisted enforcement and issuer bridge                                                           | not measured | not established / not attached | Implemented; no completed result | —               |
| G5 enforced: connected consent card discloses the exact granted scopes                                                                | not measured | not established / not attached | Implemented; no completed result | —               |
| G5 supported typed card discloses exact granted scopes with persisted enforcement and issuer bridge                                   | not measured | not established / not attached | Implemented; no completed result | —               |
| G7 B login must not overwrite A resource for existing fresh and refreshed callers                                                     | not measured | not established / not attached | Implemented; no completed result | —               |
| G8: configuring personal OAuth leaves an existing unsigned remote sibling anonymous                                                   | not measured | not established / not attached | Implemented; no completed result | —               |
| G8: configuring personal OAuth leaves an existing unsigned tunnel sibling anonymous                                                   | not measured | not established / not attached | Implemented; no completed result | —               |
| G9 approved no-auth member requires reviewed consent after administrator adds authentication                                          | not measured | not established / not attached | Implemented; no completed result | —               |
| enforced agents: two independent keys select distinct static offers and detach affects only the selected offer                        | passed       | verified / attached            | Assertions passed                | corrections     |
| enforced agents: two independent keys select distinct static offers and disable affects only the selected offer                       | passed       | verified / attached            | Assertions passed                | corrections     |
| enforced agents: two independent keys select distinct static offers and rotate affects only the selected offer                        | passed       | verified / attached            | Assertions passed                | unique-fixtures |
| enforced cross-project: organization-scoped static client shares rotation across distinct issuers without merging personal identities | passed       | verified / attached            | Assertions passed                | timeout-fix     |
| enforced cross-project: project-scoped static is absent from other project picker and its issued agent cannot access that project     | passed       | verified / attached            | Assertions passed                | timeout-fix     |
| enforced gateway methods: detach chosen shared offer respects scope across overlapping stored gateways                                | failed       | verified / attached            | Protocol HTTP 401                | full            |
| enforced gateway methods: disable chosen shared offer respects scope across overlapping stored gateways                               | failed       | verified / attached            | Protocol HTTP 401                | full            |
| enforced gateway methods: one shared credential on two sources through project gateway                                                | failed       | verified / attached            | GATEWAY_FILTER_REGRESSION        | full            |
| enforced gateway methods: one shared credential on two sources through stored gateway                                                 | failed       | verified / attached            | Protocol HTTP 400                | full            |
| enforced gateway methods: overlapping stored gateways retain distinct named offers and direct personal grants through refresh         | failed       | verified / attached            | Protocol HTTP 401                | timeout-fix     |
| enforced gateway methods: rotate chosen shared offer respects scope across overlapping stored gateways                                | failed       | verified / attached            | Protocol HTTP 401                | full            |
| enforced methods: adding same-provider different-resource server preserves the original personal offer                                | failed       | verified / attached            | SIBLING_BINDING_LOSS_REGRESSION  | remaining       |
| enforced methods: adding same-provider same-resource server preserves the original personal offer                                     | failed       | verified / attached            | SIBLING_BINDING_LOSS_REGRESSION  | remaining       |
| enforced methods: caller method shared across two sources never borrows another caller's header or personal login                     | failed       | verified / attached            | TYPED_OFFER_DISCOVERY_REGRESSION | full            |
| enforced methods: detaching shared client from A denies A but leaves B and personal choices usable                                    | failed       | verified / attached            | PERSONAL_BINDING_LOSS_REGRESSION | remaining       |
| enforced methods: disabling shared client denies both existing users while personal sessions continue                                 | failed       | verified / attached            | PERSONAL_BINDING_LOSS_REGRESSION | remaining       |
| enforced methods: personal plus two static offers select each explicitly and retain all sessions                                      | passed       | verified / attached            | Assertions passed                | remaining       |
| enforced methods: personal static and caller offers explicitly select one channel without credential leakage                          | passed       | verified / attached            | Assertions passed                | full            |
| enforced methods: rotating shared stored value updates both servers without changing other selections                                 | failed       | verified / attached            | PERSONAL_BINDING_LOSS_REGRESSION | remaining       |
| enforced methods: same static client on two servers keeps personal alternatives isolated                                              | failed       | verified / attached            | PERSONAL_BINDING_LOSS_REGRESSION | remaining       |
| enforced multiple OAuth: alpha personal beta personal and static choices preserve old fresh and refreshed sessions                    | failed       | verified / attached            | Protocol HTTP 401                | remaining       |
| enforced multiple OAuth: disabling selected beta offer denies its session while alpha static and beta source B remain valid           | failed       | verified / attached            | Protocol HTTP 401                | remaining       |
| enforced multiple OAuth: same administrator beta personal offer attaches to two dual-provider sources without replacing alpha or B    | failed       | verified / attached            | Protocol HTTP 401                | remaining       |
| enforced: G1={A,B}, G2={A,C} isolate catalogs and membership changes from direct callers                                              | failed       | verified / attached            | MCP JSON-RPC error               | remaining       |
| enforced: no-auth upstream still requires a real downstream user session                                                              | passed       | verified / attached            | Assertions passed                | remaining       |
| enforced: one server offers personal OAuth and a shared credential                                                                    | passed       | verified / attached            | Assertions passed                | remaining       |
| enforced: organization gateway retains two unrelated OAuth providers                                                                  | failed       | verified / attached            | GATEWAY_FILTER_REGRESSION        | remaining       |
| enforced: organization, automatic project, and stored gateways overlap on the same source                                             | failed       | verified / attached            | GATEWAY_FILTER_REGRESSION        | remaining       |
| enforced: pass-through header is configured in dashboard and supplied only by the MCP caller                                          | passed       | verified / attached            | Assertions passed                | remaining       |
| enforced: personal OAuth and an auxiliary static header coexist                                                                       | passed       | verified / attached            | Assertions passed                | remaining       |
| enforced: regular user OAuth, fresh upstream expiry, refresh and revocation                                                           | passed       | verified / attached            | Assertions passed                | remaining       |
| enforced: reused registration requests target resource scopes instead of issuer defaults                                              | failed       | verified / attached            | TYPED_SCOPE_REGRESSION           | remaining       |
| enforced: same direct server through project gateway preserves both provider grants                                                   | failed       | verified / attached            | GATEWAY_FILTER_REGRESSION        | full            |
| enforced: same direct server through stored gateway preserves both provider grants                                                    | passed       | verified / attached            | Assertions passed                | full            |
| enforced: shared registration keeps distinct resources without an issuer bridge                                                       | failed       | verified / attached            | TYPED_RESOURCE_REGRESSION        | remaining       |
| enforced: static header configured in dashboard reaches upstream                                                                      | passed       | verified / attached            | Assertions passed                | remaining       |
| enforced: typed multi-offer source keeps direct OAuth while stored gateways select static credentials                                 | passed       | verified / attached            | Assertions passed                | remaining       |
| native tunnel with explicit resource and scoped shared self credential, direct and project gateway                                    | failed       | verified / attached            | Protocol HTTP 412                | corrections     |
| native tunnel with explicit resource and scoped shared self credential, direct and stored gateway                                     | failed       | verified / attached            | Protocol HTTP 412                | corrections     |
| native tunnel without an upstream resource stays anonymous beside a signed-in sibling, direct and stored gateway                      | failed       | verified / attached            | UI_CONSENT_BLOCKER               | corrections     |
| organization-scoped self credential is reused across two UI-created projects                                                          | failed       | verified / attached            | SELF_RESOURCE_REGRESSION         | remaining       |
| shared service account on the same direct sources and a stored gateway                                                                | failed       | verified / attached            | SELF_RESOURCE_REGRESSION         | remaining       |
| shared service account on two direct servers: existing and fresh clients                                                              | failed       | verified / attached            | SELF_RESOURCE_REGRESSION         | remaining       |
| shared service account on two direct servers: existing and fresh clients after nominal expiry                                         | failed       | verified / attached            | SELF_RESOURCE_REGRESSION         | remaining       |

## Local provenance

Raw reports/traces are ignored, may include local credentials, and must not be published unreviewed. Public ledger categories exclude tokens, human identifiers and raw database IDs.

- `full`: `2026-11-04T14:38:43.719Z`, `.logs/mcp-auth/full-enforced-graph-evidence/results.json`.
- `remaining`: `2026-11-04T15:14:51.257Z`, `.logs/mcp-auth/remaining-enforced-graph-evidence/results.json`.
- `corrections`: `2026-11-04T15:42:21.140Z`, `.logs/mcp-auth/fixture-corrections-evidence/results.json`.
- `unique-fixtures`: `2026-11-04T15:53:04.323Z`, `.logs/mcp-auth/unique-fixture-evidence/results.json`.
- `timeout-fix`: `2026-11-04T15:58:14.422Z`, `.logs/mcp-auth/gateway-timeout-fix-evidence/results.json`.
- Product boundary log: `.logs/mcp-auth/gap-product-contracts.log`.
- Current inventory: `.logs/mcp-auth/expanded-inventory.log`.
- Fixture unit and type checks: `.logs/mcp-auth/expanded-unit.log`, `.logs/mcp-auth/expanded-typecheck.log`.
