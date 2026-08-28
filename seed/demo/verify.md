# Demo org verification playbook

Agent-driven page verification for the demo seed. Run after
`mise run seed:demo` with the `gram-playwright-cli` skill
(`mise run playwright …`). This is the executable counterpart of `PAGES.md`:
each check below maps to a `[~]`/`[x]` row there — flip a row to `[x]` only
when its check passes.

## Setup

1. Dev stack running (`pitchfork status` — server, dashboard healthy).
2. Log in to the local dashboard as the dev user (mock IdP, credential-less;
   `mise run seed` has made this user a platform admin).
3. Enter the demo org via the platform-admin impersonation panel, org slug
   `acme-demo` (sets the `gram_admin_override` cookie). Until the dedicated
   demo-impersonation path lands (README "Server changes"), this is the only
   way in. Chat transcripts ARE readable here — the demo org is exempt from
   the impersonation transcript block (chat.load carve-out).

Most checks below can also be run WITHOUT impersonation, straight against your
own org after `mise run seed` — it seeds the same data. Use that for quick
iteration; use the demo org itself before ticking a row, since only it exercises
the demo grant set and the impersonation carve-outs.

## Checks

A check FAILS when the page shows an empty state, an error boundary, or zero
where a value is expected.

1. **Agent sessions list** — sessions list shows ~180 sessions with varied
   titles ("Incident triage… #10xx"), spread over the last ~2 weeks, owners
   `*@demo.getgram.ai`.
2. **Risk events** — findings list non-empty and spans several rules, not one
   repeated row; a `stripe-access-token` finding is badged **High** (policy
   "Acme secrets & PII policy", score 8.0) and opening it shows the
   `sk_live_DEMO…` match. The Dismissed tab is non-empty and its reason column
   shows both a reviewer dismissal and an automated sweep.
3. **Cost dashboard** — non-zero total cost; breakdown by user shows the six
   demo users; by model shows claude-sonnet-4-6 / claude-opus-4-5 / gpt-5.6;
   by agent shows claude-code / cursor.
4. **Project overview** (the `default` project) — metric
   cards non-zero (tool calls, chats).
5. **Tool logs / traces** — entries for `tools:http:acme:*` tools, ~7% error
   status.
6. **Isolation spot-check** — switch back out of the demo org: your own org's
   pages show no `*@demo.getgram.ai` data anywhere.
7. **Policy Center** — nine policies listed; Action column shows a mix of
   Flag / Warn / Block / Quarantine; Severity shows Medium through Critical;
   "Applies To" varies (All types, Tool Requests, ...); no row renders an
   empty summary. Open the **Quarantines** tab and confirm one active row for
   `gram-demo-quarantine-session-1`, attributed to **Acme session quarantine
   policy**, with a Release action.
8. **Detection rules** — three custom rules (`custom.sensitive_file_read`,
   `custom.env_secret_dump`, `custom.ssrf_metadata_endpoint`), each opening
   with a populated CEL expression that the editor reports as valid, and each
   carrying findings rather than sitting at zero.
9. **Watchdog** — needs the `gram-risk-watchdog` flag on for the org (locally:
   a row in the CSV `GRAM_LOCAL_FEATURE_FLAGS_CSV` points at, keyed by your
   organization id). Over 7 days: ~13 signals spanning **Critical → Low**, not
   one severity; the KPI row shows a non-zero critical count and a findings
   trend against the previous window; grouping by **Team** and by **App**
   both produce named groups (empty groups mean the `team` / `chat_source`
   attribution columns did not land); at least one signal shows a previous
   count of 0 (a newly-emerged signal); top users render as
   `*@demo.getgram.ai` emails, not raw ids.

10. **MCP connections** — on the **Acme Partner Gateway** server's _Clients and
    Sessions_ tab, five registered agents and five connections. Grouped by
    **Agent**: `Partner Reconciliation Agent` carries a green
    **Key-authenticated** badge, `Vendor Sync (misconfigured)` a red **Cannot
    authenticate** one, and the remaining three carry none (a missing badge on
    the first two means the credential columns did not land). `Acme Legacy
Connector` appears under **Inactive** with no connections. Its row menu's
    **View registration** opens the detail sheet with an **Authentication**
    field. The organization **MCP Sessions** page shows the same five
    connections and the same two badges.

11. **Killswitch list and detail (local rewritten seed only)** — Killswitch
    management intentionally rejects demo/support sessions. Verify this contract in
    the local organization after `mise run seed`, not through the demo-org
    impersonation flow. Under **Secure → Killswitch**, the list shows six fictional
    rows: three Active, one Scheduled, one Lifted, and one
    Expired. Scope labels include both selected servers and all current/future
    MCP servers. Filtering to Amara leaves two simultaneously effective rows
    and preserves the principal filter in the URL; their detail overlap panels
    identify each other. Open the changed Jonas row and confirm history narrows
    **Acme Support Tools / Acme Ops / Linear** to **Acme Support Tools** without
    losing the removed-server diff. Open the lifted and expired rows and confirm
    their complete history and terminal status. On the active selected row, the
    external message renders the newline, `<script>alert("demo")</script>`, and
    `**This is plain text, not Markdown.**` literally: no script executes and no
    Markdown formatting appears. Internal notes remain visible only on the
    admin management detail/history surfaces.
12. **Audit logs** — Killswitch history contributes nine rows: six
    **activated**, one **changed**, one **lifted/deactivated**, and one
    **expired**. Mutation rows name the same fictional operator and prescription
    version as their Killswitch history entries; the expiry row is attributed to
    **System**, follows the bounded row's deadline, and exposes no internal note
    in the organization-visible audit snapshot.

## Killswitch J1–J11 acceptance checklist

These are manual browser checks unless a Go test is named explicitly. Reset with
`mise run seed` before each journey, use the local seeded organization as an
ordinary organization admin, and do not use demo impersonation: Killswitch
management rejects demo/support impersonation and the demo organization. The
journey names use Alex and servers A/B/C/D as role labels. In the local seed, use
Amara Okafor as Alex and map A/B/C/D to the available fictional MCP servers. J4's
cross-project persistence proof comes from the Go fixture, which creates the
second project that the default browser seed intentionally does not retain.

### Browser journeys

1. **J1 — Investigate one team member.** On **Team**, select Alex's
   **Killswitched** badge. Confirm Killswitch opens filtered to Alex and explains
   that any matching active row blocks the action. Open a row and inspect its
   capability, selected servers, schedule, exact member message, internal note,
   overlaps, and version history.
2. **J2 — Turn off MCP tool calls on server A for three days.** From Alex's Team
   menu choose **New killswitch…**. Confirm **Who: Alex** is fixed, choose the
   available **MCP tool calls** capability, and confirm one capability creates one
   independently managed killswitch. Leave both coverage options initially
   unselected, choose **Selected servers**, select A, keep **Starts: Now**, set
   **Ends: At a specific time** three days later, and supply the exact member
   message plus the required internal note. Submit once and confirm one active **MCP tool calls · Server A** row with its
   automatic end.
3. **J3 — Schedule all MCP servers until lifted.** Choose **All MCP servers** and
   confirm the copy covers current and future organization servers. Set a start
   two days later and **Until lifted**; confirm the impact summary and a single
   **Scheduled · MCP tool calls · All MCP servers** row with its start and end.
   Use an external client (or the named Go test below), not the browser clock, to
   prove that it blocks nothing before PostgreSQL time reaches the start.
4. **J4 — Turn off MCP tool calls on A, B, and C.** In one server-picker action,
   select A/B/C from the servers retained by the browser seed. Confirm the
   applied summary says three servers, **Now**, and **Until lifted**, then submit
   once. Confirm one active **MCP tool calls · 3 servers** row, not three rows,
   and inspect one complete three-server version-one snapshot. The Go acceptance
   test below is authoritative for the atomic prescription/version, the
   cross-project resource split, and matching activation audit entry.
5. **J5 — Start from one server's sessions.** On **MCP server · Clients and
   Sessions**, choose **New killswitch…** from Alex's actions. Confirm **Who:
   Alex** and **MCP tool calls** are fixed, neither coverage option is selected,
   then choose **Selected servers**. Confirm server A is prefilled but editable,
   submit, and return to **Clients and Sessions**: Alex is **Killswitched** while
   other members on A are unaffected.
6. **J6 — Member receives the exact MCP denial.** From an external MCP client,
   call selected server A as Alex. Confirm `mcp_tool_calls_paused` and the exact
   configured message, with no framework wording or synthetic resume time. A
   call to uncovered D succeeds before the scheduled all-server row starts;
   after that row starts, D is denied with that row's exact message.
7. **J7 — Understand available and upcoming capabilities.** Confirm **What to
   turn off** is single-select, **MCP tool calls** is available, and **More
   capabilities** is **Coming soon** with **Request a capability**. Submit the
   request, confirm interest is recorded, and confirm it creates no Killswitch
   row. Confirm the Killswitch page's
   bottom panel offers the same request path without naming unsupported products.
8. **J8 — Narrow a killswitch safely.** Open **MCP tool calls · 3 servers** and
   choose **Edit killswitch**. Remove B/C and confirm the exact warning **This
   change restores access on 2 servers immediately** plus A unchanged, B removed,
   and C removed. Save once; confirm detail shows version 2, actor, timestamp, and
   A/B/C → A. With the same principal and existing sessions, B/C must now succeed
   while A remains denied; the Go test is authoritative for that enforcement.
9. **J9 — Lift one overlapping killswitch.** From the independent starting state
   with selected A/B/C and all-server rows active, choose **Lift killswitch** on
   A/B/C. Confirm the dialog warns that the all-server row still covers Alex and
   access will not return. After lifting, confirm the selected row is **Lifted**,
   the all-server row remains **Active**, and Alex retains the aggregate
   **Killswitched** badge.
10. **J10 — Choose killswitch, revoke, or both.** On **MCP Sessions**, confirm
    Alex's menu offers **New killswitch…** and **Revoke all connections** as
    separate actions. The revoke dialog must explain that reconnection remains
    possible and link to **New killswitch**. After Killswitch creation, confirm
    success guidance links back to MCP Sessions. This browser composition is not
    claimed by the MCP Go acceptance tests.
11. **J11 — Expire exactly on database time.** At the bounded row's deadline, use
    the external client (or Go test) to confirm PostgreSQL time removes that match
    immediately without waiting for maintenance. Server A remains denied by the
    overlapping all-server row and shows its message. Afterwards, confirm Audit
    Logs contains exactly one System **Killswitch expired** event for the bounded
    row. Browser status/timing is display evidence only, not enforcement proof.

### Exact automated evidence

Run:

```sh
mise run test:server ./internal/mcp/ -run '^TestKillswitchAcceptance' -count=1
```

- `TestKillswitchAcceptancePrivateRemoteAndTunnelProductionComposition` drives
  both private backend modes through the production serving composition, a real
  DB checkpoint, persisted signed user identities, canonical `mcp_servers` IDs,
  real MCP sessions, and counted `httptest` upstreams. It proves successful
  calls before activation and after lift; byte-identical pre/post `initialize`,
  `tools/list`, `prompts/list`, and `resources/list`; semantic denial with no
  upstream call; and successful transport
  for another active member. Private public endpoints reject API-key identities
  before MCP session composition, so the test separately invokes the shared
  production checkpoint with validator-stamped API-key provenance and proves the
  central unsupported-identity result is Continue for both backend modes. It
  does not claim an end-to-end private unsupported-identity request.
- `TestKillswitchAcceptanceHostedTransportContract` reuses server-issued MCP
  sessions; compares byte-identical pre/post `initialize`, `tools/list`,
  `prompts/list`, and `resources/list` behavior; preserves signed-anonymous
  behavior; and checks denial semantically. A configured hosted
  HTTP-tool sentinel reaches its counted downstream before activation and after
  lift, but remains at zero additional calls while denied. It does not claim
  byte-for-byte denial serialization.
- `TestKillswitchAcceptanceSessionRevocationDoesNotChangeActivePrescription`
  revokes the exact existing session through the production user-sessions
  service, proves the version and one-entry history are unchanged, and repeats
  the same prescription-scoped audit/outbox assertions. A newly minted signed
  session reconnects and is still denied by the active prescription.
- `TestKillswitchAcceptanceFutureStartDynamicAllUsesDatabaseTime` proves an
  unchanged version-one all-server prescription permits an existing and a
  later-created server before PostgreSQL `starts_at`, then denies both after it.
- `TestKillswitchAcceptanceScopesLifecycleAndReceipts` verifies one persisted
  three-server version-one snapshot across two real projects in one organization;
  real project/toolset/server/issuer/grant fixtures; same-principal B/C success
  and A denial immediately after A/B/C → A; create replay and operation conflict;
  CAS failure without history growth; exact four-entry history; exact
  prescription-scoped audit actions; selected outbox fields (event type, subject,
  actor, action/version, lifecycle snapshot, operation, and operation ID); and
  internal-note non-leakage from denial, audit-row serialization, and outbox
  messages. It does not claim full payload equality, operation-receipt leakage,
  or every response surface.
- `TestKillswitchAcceptanceLiftSelectedWhileAllRemains` lifts a selected overlap
  while all-server remains active, then restores the same session only after the
  final overlap is lifted.
- `TestKillswitchAcceptanceExpiryOverlapUsesDatabaseTimeAndRecordsOnce` proves a
  bounded selected note wins before PostgreSQL expiry and the overlapping
  all-server note wins immediately afterward, before maintenance. Concurrent and
  repeated maintenance then produces exactly one marker, System audit action,
  and prescription outbox event while the all-server version stays unchanged.

Routine log and metric leakage is not overclaimed as a captured acceptance
assertion: the existing MCP test logger does not expose a bounded record buffer,
and ordinary matched denials do not emit a dedicated routine log. The exact
lower-level proof is `TestEvaluatorUsesOneQueryAndRetainsWinningPolicy` plus the
`EvaluateCurrentPrescriptions` projection, which carries only prescription ID,
definition key, and external note into the match result, and
`TestKillswitchEvaluationMetricsHaveClosedAttributes`, which permits only the
closed `outcome` dimension. MCP coverage metrics are separately pinned to closed
surface, identity-class, and resource-class values by
`TestIdentityCoverageRecord_PinsInstrumentAndDimensions` and
`TestIdentityCoverageRecord_ClampsAtRecordSite`. This does not replace a future
captured full-composition log assertion.

Fail-closed hosted checkpoint/serving behavior and private proxy downstream
sentinels remain covered by `TestServePublic_HostedToolsCallKillswitch`,
`TestHostedCheckpoint_ReevaluatesAndFailsClosed`, and
`TestToolsCallKillswitchStopsProtectedAndUpstreamWork`; the acceptance test does
not add another destructive database fault seam.

## On failure

Fix the seed SQL (see rules in `PAGES.md`), re-run `mise run seed:demo`,
re-check only the failed pages, and commit the SQL change once green.
Screenshots of failures go to `.playwright-cli/` (ignored) — reference them in
the PR, don't commit them.
