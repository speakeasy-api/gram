# Standalone MCP authentication fixtures

Local OAuth issuers, MCP upstreams, a protocol-driving MCP client, and dashboard-driven Playwright scenarios. This is not a demo seed, migration/backfill runner, or product-management API client.

## Hypothesis-driven path expansion

The supplied gap catalog is a list of hypotheses, not expected failures. New `*-paths.spec.ts` and card-action cases assert working authorization, execution, administration and recovery paths; unsupported or blocked stages are not counted as visited. Every browser case still verifies persisted typed enforcement before UI provisioning.

- `mise run test:mcp-auth:unit`: strict mock/protocol infrastructure checks.
- `mise run test:mcp-auth`: browser runner; accepts Playwright file/grep/list arguments.
- `mise run test:mcp-auth:all`: mock units, then browser files **serially**, with a separate bounded budget and preserved ignored artifacts per file. An invalid mock unit suite prevents browser measurement. No retries or ordinary seed/API/SQL provisioning; separately authorized setup exceptions are recorded below.

[STATE-CONSTRUCTION.md](./STATE-CONSTRUCTION.md) separately records UI-created, historical/hand-built persisted-state, and environment requirements. Hand-built SQL state has not been created. The earlier 44-case measured ledger remains historical evidence; [PATH-EVIDENCE.md](./PATH-EVIDENCE.md) tracks the expanded inventory, latest recorded outcomes, and the actual evidence boundary for each hypothesis. A persisted enforcement flag does not attest that every credential type avoids an internal compatibility hand-back.

## Run

From an initialized worktree:

```sh
mise run test:mcp-auth:unit         # No stack/browser required
mise run wake                      # Start this worktree's existing local stack
mise run test:mcp-auth --list       # List browser scenarios
mise run test:mcp-auth              # Headless system Google Chrome
mise run test:mcp-auth --headed    # Watch administration and authentication
```

Install system Google Chrome if missing. Do not use bundled Chromium or disable its sandbox. The suite uses fresh browser contexts and no automatic retries. The helper reloads the staff page and checks both the enforced status and Stop enforcing control; missing staff access, migration readiness, or persisted enforcement fails setup. Each result is annotated with this prerequisite. There is no legacy/shadow serving mode or header-editor fallback in this suite. The dashboard target must be loopback; production targets are refused. Run one copy at a time against an isolated worktree.

Workload setup now uses `MCP_AUTH_WORKLOAD_HOST` default `127.0.0.1.sslip.io`, with every DNS answer required to be `127.0.0.1`. This introduces an explicit external `sslip.io` DNS dependency. Dedicated certificate/key files (`local/ssl/certs/workload.pem`, `local/ssl/keys/workload-key.pem`) use the existing trusted mkcert CA; optional `MCP_AUTH_WORKLOAD_CERT_FILE` / `MCP_AUTH_WORKLOAD_KEY_FILE` overrides avoid global `GRAM_SSL` changes. No hosts or global DNS changes were made. Native Go system-roots HTTPS and Node trusted-CA probes succeeded: **setup ready, not an auth pass**. Earlier DNS preflight failures remain historical. EMA scenarios now use `emaIdentity` after mandatory enforcement: `ema-bootstrap.ts` reads the canonical signed-in user UUID/email independently through Account → profile and verifies the email domain in rendered DOM, never from an incoming assertion. No manual identity variables are required. The helper enables the organization issuer through its UI if Off and verifies Enabled after reload; cleanup restores Off only if changed, retaining the provisioned issuer and keys. This requires deployment-level issuer provisioning and resolver rollout prerequisites; the current local deployment cannot supply these through rendered UI (see evidence below).

The `enforcement-setup` project turns on upstream auth enforcement and the organization gateway once; both stay on. `MCP_AUTH_WORKERS` sets parallel spec files in `isolated` mode (default 1, `mcp-auth:all` uses 3); `org-wide-scenarios` run alone because they change organization state.

`MCP_AUTH_PROJECT_MODE` selects project separation for a whole run: `isolated` (default) gives every scenario its own project, and `shared` puts every scenario in one project, so scenarios can observe what earlier ones left behind. A failure in `shared` needs an `isolated` run of the same scenario before it is attributed to sharing. The `shared-project-cleanup` teardown removes the shared project; `mcp-auth:all` keeps it until after its last spec.

Mock upstreams, EMA upstreams and workload issuers run in one host process (`mock-host.ts`) for the whole run, so a URL stored by an earlier scenario still answers. The process holds no scenario state: each scenario has a URL prefix (`/s/<id>`) and its protocol state, controls and journal are files under the run directory. `.logs/mcp-auth/runs/` holds a standalone run's directory, and `mcp-auth:all` uses its evidence directory. Names of servers and offers carry a random suffix in both modes, because name collisions are not under test.

The separately authorized EMA bootstrap uses `.mise-tasks/mcp-auth/bootstrap.sh` and `e2e/mcp-auth/bootstrap.ts`: it may create the platform credential through the supported API and apply a local organization-targeted rollout CSV entry. Subsequent organization issuer UI enablement and cleanup succeeded; it also applies the EMA-specific sign-in-enforcement flag for that organization. The current EMA boundary is recorded below and in PATH-EVIDENCE.md. Bootstrap never earns auth coverage, relaxes mandatory persisted enforcement, substitutes for real protocol/strict mocks, or establishes that a root cause is fixed.

For manual dashboard exploration, start the upstreams independently:

```sh
mise run mcp-auth:serve             # Random loopback port
mise run mcp-auth:serve 4123        # Stable local port
```

The server prints only newly invented fixture configuration. Its terminal accepts `events`, `expire`, `revoke`, `reset`, and `quit`. Ctrl-C stops it. Fixed secrets are synthetic test inputs, never credentials for a real provider. Alpha and beta are unrelated issuers. Configure the printed URLs through the dashboard; starting the mocks creates no Speakeasy records.

## Evidence boundary

- Default scenario provisioning/administration uses real dashboard controls, with browser assertions against rendered DOM. The user has explicitly authorized prerequisite setup exceptions for EMA and similarly setup-locked paths; keep their bootstrap provenance separate from scenario execution. This is not blanket SQL-write authorization. Outside those recorded exceptions, no direct management administration calls, SDK provisioning, browser API evaluation, route interception, injected auth/storage state, or hidden feature-flag overrides are used. A separate loopback-only, bounded read-only SQL diagnostic checks synthetic fixture topology and scope overrides after setup; it never creates/repairs edges or reads credential values.
- The MCP client calls only the real resource/OAuth protocol endpoints. It performs discovery, registration, PKCE, callback validation, token exchange/refresh/revoke, initialization, tool listing and tool invocation. Agent credentials must originate from a UI issuance flow.
- Mock control methods affect only the fixture's own in-memory state. The protocol unit test can approve its own mock issuer through HTTP; browser scenarios may not use that shortcut to administer or authorize the product.
- Failure to express a model through the dashboard is a product constructibility blocker, not a reason to insert rows. Historical legacy-only states, impossible persisted states and migration-only variants must remain distinguished from models a current customer can create.
- Passing mock/driver unit tests is not evidence that enforced product auth works. Every browser scenario must verify persisted enforcement through the staff UI; readiness or staff-access failures are explicit blockers.

## Components

| File                  | Responsibility                                                                                                                   |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `mock-server.ts`      | Independent issuers, discovery, DCR, consent, JWKS, PKCE, refresh rotation, revocation, introspection, protected MCP resources   |
| `client.ts`           | Real protocol driver; loopback browser callback, exact state validation, JSON/SSE responses, session IDs and redacted transcript |
| `protocol.test.ts`    | Driver-to-upstream interoperability independent of Speakeasy                                                                     |
| `dashboard.ts`        | Dashboard-only setup and consent helpers                                                                                         |
| `ui.spec.ts`          | Full-path browser/protocol scenarios and assertions                                                                              |
| `CAPABILITY-TRACE.md` | Source trace of multi-server organization gateways and multiple auth methods for users/agents                                    |

Profiles: anonymous, alpha OAuth, beta OAuth, static `X-Fixture-Key`, pass-through `X-Fixture-User`, and a server accepting alpha OAuth or a static credential. Distinct identities are returned by `fixture_echo`, and the upstream journal records successful/denied routing without tokens, authorization codes, client secrets or header values.

## Incident reproduction

[INCIDENT-MATRIX.md](./INCIDENT-MATRIX.md) maps failure-relevant graphs and transitions to concrete tests and remaining gaps. The initial seven cases were smoke coverage, not reproductions. Added shared-registration/missing-bridge and resource-specific-scope scenarios have measured red assertions on unchanged product code. A green smoke count must not be used as evidence that these configurations work. The mock now enforces protected-resource token scopes and journals rejected authorization attempts, including missing resources.

## What must be proved

1. The dashboard can create the intended source, registration, typed offer, attachment, audience, gateway and credential selection; reread through the UI.
2. A regular user and an agent with independent credential state reach only their selected upstream identity.
3. An organization gateway can reach two unrelated providers, not merely servers whose registrations are bound to its organization issuer. Project/meta/agent surfaces are distinct and must not be inferred from direct-server success.
4. One server can offer personal OAuth and shared credentials; an explicit choice wins and credential changes do not switch an existing caller to another provider.
5. Fresh login, existing session, expired access, refresh, revoke/reconnect and changes during the flow are separate assertions. Enforcement stays on throughout every scenario; restoring the original setting afterward is cleanup only, not a legacy-path test.

### Fixture model mapping

The earlier data-model inventory describes target shapes, not executable SQL fixtures. Use visible actions to create each supported shape:

| Model family                                  | Dashboard construction / boundary                                                                                |
| --------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| OAuth dual representation                     | Discover/create a remote connection and configure its OAuth identity                                             |
| Legacy-only / unmapped historical choice      | Out of scope for legacy serving; test refusal/migration only under typed enforcement, without backfill shortcuts |
| Static / pass-through / OAuth plus headers    | Add/edit typed credential offers and their headers through settings                                              |
| Same client on several servers                | Attach an existing credential rather than silently clone it                                                      |
| Multiple clients on one server                | Add distinct offers and choose each as a user and agent                                                          |
| Organization-scoped / sibling-project sharing | Choose ownership and attach via UI; cross-project scenarios are implemented; runtime evidence is separate        |
| Null-organization legacy registration         | Historical storage shape; not directly manufacturable via a database helper                                      |
| No-auth / required-auth without credentials   | Configure available identity controls; missing setup must remain visible                                         |
| Disabled/deleted client or front              | Disable/delete via UI after a successful call, then reuse the issued credential                                  |
| Wrong-provider alternative                    | Attempt the real credential selector; a prevented selection is valid protective behavior                         |
| Missing direct sign-in / typed-only setup     | Verify exact surface creation and login; never add a legacy bridge behind the UI                                 |
| Toolset / unproxied / tunneled                | Native tunnel scenarios are implemented separately; a remote fixture is not tunnel coverage                      |
| EMA / self-owned service account              | Shared self/service-account scenarios are implemented; EMA is not inferred from them                             |
| Corrupt or cross-tenant selection             | Test UI rejection/isolation, not impossible rows inserted into persistence                                       |

This table is a coverage contract, not a claim that every model has a passing browser scenario. **IMPLEMENTED:** the current supported inventory is **77 tests in 22 files**, verified by `.logs/mcp-auth/resumed-scenarios/current-supported-list.log`. The historical 83-case inventory included six unsupported G23 native-tunnel static/pass-through positives: the UI/backend rejects that construction (`server/internal/authclients/create.go:390–403`). Their exclusion is neither a pass nor a product bug. **Both G8 unsigned-sibling cases remain** in `tunnel-header-paths.spec.ts`; supported native OAuth/self and anonymous-isolation contracts were not removed.

### Current expanded-path evidence boundary

[PATH-EVIDENCE.md](./PATH-EVIDENCE.md) is authoritative for latest per-case outcomes and historical attempts. The earlier 44-case ledger in [MEASURED-COVERAGE.md](./MEASURED-COVERAGE.md) and the source/history discussion in [CAPABILITY-TRACE.md](./CAPABILITY-TRACE.md) are separate evidence, not current aggregate results. Historical smoke **6 passed / 1 failed**, nine-case incident **5 passed / 4 failed**, and later red attempts remain historical; subsequent successes do not rewrite those runs.

- **Measured passes:** G4 supported Refresh and G5 supported reconnect with exact scopes/identity passed on their supported issuer-bridge paths; typed-only G5 is a separate contract. G14 passed in **29.479s** (`registration-identity-and-expiry-repair/results.json`): the source-valid zero advertised TTL case completed startup/repeated calls without a refresh loop, actual revocation, explicit reconnect with a fresh grant, and a recovered call. G17's **39.052s** pass remains preserved (`g17-repaired-driver-evidence/results.json`), proving assigned access allowed, sibling denied, then assigned access allowed again. Both paths are under `.logs/mcp-auth/resumed-scenarios/`; earlier failures remain historical.
- **Typed-only G5:** repaired setup now reaches initial authorization, which sends an empty resource and receives strict `invalid_target`. Rendered metadata reports that the identity provider rejected the requested resource, correlated with `challenge.go:2171`. This is not the former selector blocker, or evidence of an admission/missing-bridge/reconnect failure.
- **G10:** owner reconnect used the exact grant and produced two authorized calls, but exchange for the same workload still returned `setup_required` HTTP 400. Source inspection excludes Workload callers from personal-offer candidates before owner-aware readiness; this is distinct from the graph-binding-loss observations.
- **G27:** the first turn explicitly selected the second static offer (`static-alt-user`), then hit selected-connection access denial with zero execution. It was not a personal/default first-turn test. This boundary alone does not establish a product root cause.
- **EMA:** `mcp-auth:bootstrap` now applies the org-scoped `sign-in-enforcement` and `internal-session-issuer` rows, and both scenarios reach a live EMA exchange. Both now stop at consent: the EMA choice is selected, the direct tool inventory returns 409, and **Give access** stays disabled because a page with other offers does not defer inventory. See `.logs/mcp-auth/resumed-scenarios/ema-consent-snapshot/`. The identity tool remains unmeasured.
- **Code Mode G9:** latest `safe-prerequisite-and-recovery-diagnostics` evidence confirms initial anonymous execution and stale-call refusal with no stale dispatch or forwarding. `tools.connect` returned `execution_failed`; this does not prove runtime outage or the historical reason mismatch. Full recovery of the frozen catalog after adding sign-in remains unproved.

All five focused scenarios were measured with fresh persisted upstream-auth enforcement; five failures are not five product bugs. Latest per-case statuses, non-atomic aggregate counts and unmeasured contracts belong in the authoritative path ledger, not a duplicated total here.

The expanded suite is not green. Classify HTTP/refresh failures, missing choices/members, graph-confirmed attachment loss, invalid assumptions, and setup/driver failures separately; a failed base configuration does not measure its later disable/detach/rotation or recovery assertions. No successful runtime tunnel invocation is inferred from connection alone. Second-human-subject isolation, complete recovery sequences and historical/adversarial variants remain separate gaps. Unreached stages are not passes; legacy/shadow attempts earn no enforced-path coverage, and enforcement restoration is cleanup, not rollback coverage. Production/source audits remain separate from local runtime evidence.

### Focused reconciliation

[CAPABILITY-TRACE.md](./CAPABILITY-TRACE.md#focused-reconciliation) records the latest focused interpretations; [PATH-EVIDENCE.md](./PATH-EVIDENCE.md) remains authoritative for non-atomic outcomes, including the pending C02 measurement. Healthy card journeys use opt-in HTTPS mocks with the existing trusted workload certificate/FQDN and DNS answers exclusively `127.0.0.1`. C15 strict revoke/303 reconnect and original agent mutations C03/C04/C05 now pass; the earlier HTTP `jwks_uri` violated strict HTTPS validation and does not establish a healthy-provider revocation bug. G12 honors the actual 30-second Retry-After once, then makes one recovery invoke with two upstream refreshes: no retry-to-green or manual bypass.

EMA additionally requires backend `sign-in-enforcement`, default false and distinct from persisted upstream-auth enforcement. The local provider can be in-memory at startup, not necessarily PostHog; no approved dashboard/staff UI control was found. Authorization for local uncommitted configuration plus restart/restore was requested but not received or performed. Do not bypass this prerequisite.

### Historical infrastructure checks

**Historical checks recorded 42 passing unit tests plus repository type-check, formatter and whitespace checks.** No units or browser tests were rerun for this documentation reconciliation. These infrastructure checks do not attest product/browser success. Earlier 36-test checks and 83-case listings remain historical in the path ledger. Unsupported G23 graphs must not be manufactured through persistence bypasses, an invented OAuth bridge or substitution of a remote server for a tunnel. Historical/adversarial/aged-keepalive variants remain unexecuted; no default SQL writes are authorized.

### Read-only local graph capture

`fixtures.ts` calls `auth.graph()` at teardown whenever fixture project creation succeeded, before cleanup; original smoke hooks capture graphs as well. Scenarios can call it explicitly for topology assertions; sibling-project specs pass both project URLs before their own teardown. Each capture attaches `fixture-auth-graph`. `graph.ts` accepts only local `auth-e2e-*` project URLs and a loopback database, uses a read-only repeatable-read transaction, and emits aliases instead of raw record IDs, without credential values. It observes UI-created state; it cannot provision or repair it. Capture failures remain explicit rather than silently supplying a graph.

Snapshots include persisted server visibility (and derived disabled state), typed offers/attachments, registration and legacy-bridge presence, stored gateways and their persisted members, and persisted upstream selections (including missing-reference flags). Computed organization/project gateways are explicitly `not_captured`: their inventory and runtime routing need separate protocol evidence. The service-account spec asserts two servers share exactly one enabled typed `service_account` client and registration, with missing issuer bridges and direct legacy associations. This is an implemented graph assertion, not a claim that its token exchange or tool calls passed. Production audits remain separate; private extracts and real customer identifiers are not fixture inputs.

## Artifacts and cleanup

Results, screenshots, traces and protocol evidence go under ignored `e2e/.playwright/mcp-auth/`. Browser traces can contain local session and one-time agent credentials; do not publish them unreviewed. Redacted journals omit secrets, but remain local test artifacts. The suite targets synthetic resources only. Issued OAuth grants are revoked through the real OAuth protocol. Teardown captures graphs and journals before closing fixtures, restoring policy, and UI-deleting only owned `createdProjects` in reverse order, including failed scenarios. Preserved private artifacts retain failed attempts; live failed projects are not the retention mechanism. Cleanup failures remain separate from runtime failures. Three older owned synthetic projects were UI-deleted only after report-window and unique-name verification with read-only diagnostics; no quota override or SQL write was used. Historical leftovers can point to dead loopback mocks. The organization gateway includes servers outside the current disposable project; retained fixtures affect discovery, and malformed pre-existing credentials can block inventory capture. An unreachable mock is not by itself proof of the fatal capture cause; member discovery failures may instead be excluded. A fresh browser/project alone is not organization isolation. Inspect the real consent error before attributing a gateway failure to OAuth filtering; do not delete unrelated projects or silently omit gateway members to make this test pass. Staff enforcement and gateway settings changed by a scenario are restored through their UI controls; a setting already enabled before the scenario remains enabled. Never wipe the worktree database as test cleanup.

A failing scenario should identify its layer: environment prerequisite, dashboard constructibility, staff enforcement readiness, OAuth/consent, gateway membership, credential selection, or upstream routing. Do not mark a known auth bug as expected-pass or retry it until it disappears.
