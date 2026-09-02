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

13. **Gateway overview Activity** — open MCP → **Acme Agent Gateway** → Overview.
   The Activity section shows non-zero tool calls over the last 7 days, a
   discovery funnel with all four steps populated and decreasing
   (list_servers > describe_server > describe_tools), and a Calls-by-member
   table listing Acme Support Tools, Acme Ops, Linear, and Slack with Acme Ops
   carrying most of the errors. Back on the MCP listing, the gateway card
   shows no "never used" marker.


## On failure

Fix the seed SQL (see rules in `PAGES.md`), then re-run the target that owns
the failed check: `mise run seed` for the local-only Killswitch checks, or
`mise run seed:demo` for shared demo-org checks. Re-check only the failed pages,
and commit the SQL change once green.
Screenshots of failures go to `.playwright-cli/` (ignored) — reference them in
the PR, don't commit them.
