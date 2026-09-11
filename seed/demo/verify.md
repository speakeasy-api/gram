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
the demo grant set and the impersonation carve-outs. Exceptions are the explicitly
local-only Killswitch and managed-agent checks: verify those as an ordinary
human in the local organization, not through impersonation.

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

11. **External OAuth settings** — open **MCP → Acme OAuth Discovery →
    Authentication** (`/mcp/acme-oauth-discovery/authentication`). The page
    shows the existing Gram-hosted metadata configuration, recommends
    provider-hosted metadata, and offers **Review update**. Opening the review
    starts with the fictional `https://auth.example.com` issuer; live discovery
    does not need to succeed for this seeded-page check.

12. **Killswitches on the identity Access tab (local rewritten seed only)** —
    Killswitch management intentionally rejects demo/support sessions. Verify this
    contract in the local organization after `mise run seed`, not through the
    demo-org impersonation flow. **Secure** carries no Killswitch entry: open
    **Identities**, then a subject's **Access** tab, where the Killswitches panel
    lists that person's rows and nobody else's, a page at a time behind **Load
    more**. Across the seeded subjects the six fictional rows are three Active,
    one Scheduled, one Lifted, and one Expired, and scope labels include both
    selected servers and all current/future MCP servers. Amara's panel holds two
    simultaneously effective rows whose record overlap panels identify each
    other. Open Jonas's changed row and confirm history narrows **Acme Support
    Tools / Acme Ops / Linear** to **Acme Support Tools** without losing the
    removed-server diff. Open the lifted and expired rows and confirm their
    complete history and terminal status. On the active selected row, the
    external message renders the newline, `<script>alert("demo")</script>`, and
    `**This is plain text, not Markdown.**` literally: no script executes and no
    Markdown formatting appears. Internal notes remain visible only on the
    admin management record/history surfaces. Finally, confirm the retired
    addresses still resolve, and that the forward keeps the reader where they
    were: from **MCP Sessions** filtered to a non-default project and a
    non-default date range, the killswitch icon beside a person opens their
    Access tab in that same project with `range`/`from`/`to`/`label` intact;
    `/<org>/killswitch` forwards to **Identities**; and
    `/<org>/killswitch/<killswitchId>?range=…` forwards onto its subject's
    Access tab with that record open and the range still applied.
13. **Audit logs** — Killswitch history contributes nine rows: six
    **activated**, one **changed**, one **lifted/deactivated**, and one
    **expired**. Mutation rows name the same fictional operator and prescription
    version as their Killswitch history entries; the expiry row is attributed to
    **System**, follows the bounded row's deadline, and exposes no internal note
    in the organization-visible audit snapshot.
14. **Employee Shadow AI** — open **Employee Enrollment**, then Priya's detail
    page. The Shadow AI section lists Claude Code, Cursor, Codex, and Ollama in
    deterministic last-seen order, spans Harness and Local model categories,
    and shows two devices where applicable. Installed / Running signals,
    detected versions, and first / last seen values render without exposing
    hardware identifiers. Jonas's detail shows Claude Code, Cursor, and Aider,
    proving each page is filtered to its canonical enrolled-user identity.

15. **Gateway overview Activity** — open MCP → **Acme Agent Gateway** → Overview.
    The Activity section shows non-zero tool calls over the last 7 days, a
    Gateway tool usage chart with all four tools populated, the three discovery tools
    decreasing (list_servers > describe_server > describe_tools) and
    execute_tool counting the member calls, and a Calls-by-member
    table listing Acme Support Tools, Acme Ops, Linear, and Slack with Acme Ops
    carrying most of the errors — and no GitHub row, even though GitHub's
    dispatches from before it left the gateway still count in the totals.
    The "Dispatched calls over time" title opens Tool Logs and the "Gateway
    tool usage" title opens MCP & Tools, both with the Acme Agent Gateway
    server filter applied; on Tool Logs the hook-observed rows are tagged
    Gateway (not Shadow MCP) and link back to the gateway, and each member
    dispatch carries a "via Acme Agent Gateway" marker. Back on the MCP
    listing, the gateway card shows no "never used" marker.
16. **Organization setup board** — with the `gram-setup-board` flag enabled,
    open `/acme-demo/setup/board`. Confirm all four columns render, Priya owns
    Set up observability in other platforms, `security-owner@demo.getgram.ai` owns Configure
    integrations in Awaiting Support, and Set up identity provider and Set up
    Anthropic observability sit in To Do. Distribute MCP servers, Configure
    policies, and Set up Platform MCP are hidden by default, so the board shows
    four tasks. As a platform admin, enable **Show hidden tasks** and confirm
    all three appear with a Hidden badge.

17. **Managed agents (local rewritten seed only)** — run `mise run seed` and
    use an ordinary human session in the local organization, with permission to
    view all agents and authorize credentials (for example, the local seeded
    admin). Shared demo impersonation remains intentionally restricted by the
    ordinary-human authorization requirement; it is not the browser verification
    target. Enable `agent-management` for inventory and
    `agent-identity-credentials` for API key management.
    - Open **Agents**. Confirm **Release assistant** is Active, **Support triage**
      is Suspended, and **Retired documentation bot** is Revoked. List and detail
      show Amara Okafor, Jonas Lindqvist, and Priya Raman respectively, with
      readable owner names and initials fallback rather than raw IDs or broken
      avatars. Local seeded fixtures must be visible to the authorized human.
    - Open Release assistant's sessions. Its one display-only session is
      expired; suspended/revoked agents have no seeded sessions. This fixture has no signing token, an invalid refresh
      hash, and empty delegation: it must not authenticate or refresh. The
      `gram-agent-mcp-authorization-m2` flag gates live MCP authorization, not
      the inventory check; no live connection is promised by these fixtures.
    - API keys are empty after the shared SQL runs. With the credentials flag
      enabled, only the active agent permits key creation; suspended/revoked
      agents must not offer usable credentials. With the flag disabled, confirm
      the unavailable-rollout state, not a misleading empty-key success state.
      Never add usable keys to shared SQL.
    - Delegable permissions are empty out of the box, because the shared SQL
      seeds no agent policy grants. Confirm **Create API key** explains that
      none can be delegated rather than showing an editor or a raw grant
      field — the empty state is the correct result here, not a failure.
    - Seed rationale: retain the three lifecycle fixtures, zero agent policy
      grants, zero API keys, and expired non-authenticating session. They expose
      the later policy-viewing entry point and its empty state; existing seeded
      MCP servers/tools supply selector choices. Open Release assistant's policy
      view and confirm an explicit empty policy, not a loading/error state.
      No SQL changes are needed: populated policies are verified through local
      UI creation below, not claimed as seeded data.
    - **Fresh creation is required acceptance evidence.** As an ordinary human
      in the local organization, create a new agent owned by that human using
      the dashboard. Enter a unique test name and configure initial permissions
      in the creation form: add `mcp:connect`, choose a project and a
      toolset-backed MCP server, and narrow to one harmless tool. Use structured
      controls, not raw JSON, grant strings, or free-text tool names. The
      owner/caller must have matching delegable access; missing access is a
      setup blocker, not a reason to prepopulate the agent's grants.
      **API-, SQL-, or script-prepared agent grants are not an acceptance
      substitute**, even if they make the key picker work. Seeded identities
      and API-only creation do not prove fresh UI creation.
    - **Atomic failure and retry:** force a controlled local rejection of an
      initial policy grant during submission using a test-only server validation
      failure. Do not assume deleting a selected resource invalidates a policy
      selector: policy ceilings can describe resources without current access.
      Confirm a visible error, no persisted
      agent or partial grants (no orphan identity), and the creation form retains
      the name and permission draft. A network failure before submission does
      not prove rollback. Correct the draft through the UI and retry: exactly
      one agent appears with its initial server/tool-narrowed policy. Reopen its
      detail and reload; the saved policy must remain unchanged.
    - Open the new agent's **Create API key** picker without out-of-band grant
      preparation. Candidates must intersect the saved agent policy, owner's
      permissions, and calling human's permissions, never broaden to other
      servers/tools. No delegable access produces an explanatory empty state,
      not raw grant input.
    - With candidates present, confirm the structured narrowing: a wildcard
      candidate offers a **Server** choice listing the seeded MCP servers, a
      chosen toolset-backed server then offers its **Tool** list, and **Tool
      disposition** and **Project** narrow without a server choice. A
      dimension the candidate pins to a concrete value renders as a
      "Restricted to" chip and must not be editable; a dimension the candidate
      leaves as `*` is still narrowable. Server and project choices constrain
      each other — a server from another project must not be offered once a
      project is pinned or chosen.
    - Remote-MCP-backed servers carry no tool metadata in the seed, so
      selecting one shows the no-tools state ("No tools are recorded for this
      server"), not a tool list. That is the expected seeded result. To
      exercise the loaded and failed paths, first materialize metadata from
      that server's **Inspect** tab, then reselect it; revoking your access to
      the owning project instead surfaces **Retry tools**. Neither path ever
      offers a free-text tool name.
    - **Subsequent policy edits:** use the new agent's structured policy
      editor to add, narrow, and remove permissions. Save and reload after each
      change; the policy view must show the saved selectors. Reopen the key
      picker after each save, before reloading: candidates must refresh, with
      additions only when owner/caller also permit them, removed scopes absent,
      and narrowed scopes no longer offering the broader choice. Removing
      matching access from either the local owner or caller must also remove
      the candidate; restore temporary test access when done.
    - **Allowed and denied usage (local only):** use a private standalone MCP
      endpoint without a session issuer and a locally executable harmless tool.
      Send the UI-issued key in `Authorization: Bearer <key>` to its `/mcp/…`
      endpoint. Issuer-gated gateways require OAuth/session credentials instead;
      `gram-agent-mcp-authorization-m2` controls OAuth agent selection, not
      standalone API-key admission. Display-only seeded tools cannot prove
      successful tool execution; filtered `tools/list` proves discovery only.
      Create a short-lived key from the UI's narrowed candidate. Confirm the
      secret appears exactly once and **Expires** shows an absolute future
      date (never "ago"). With that key, prove a selected tool succeeds and
      an unselected tool/server is denied by authorization, not merely absent
      from the picker or failing downstream. After narrowing/removing the
      policy, retry the previously allowed call with the existing key: it must
      be denied, not retain stale access. Record only redacted outcomes.
    - Revoke every verification key, including after failed or interrupted
      runs, and remove the temporary local agent. Never retain usable demo
      keys, paste secrets into the repo, a PR, screenshots, or logs, or promote
      a test key into shared SQL or `RunLocalFixtures`. Do not mutate the shared
      demo remotely for these checks. Missing live-usage prerequisites mean
      the check is blocked, not passed.
    - Rerun `mise run seed`: the same three identities/lifecycles return, agent
      policy grants are reset, and no visitor-created API keys survive the
      shared SQL. Local-only developer keys may be restored by
      `RunLocalFixtures`; do not mistake those for managed-agent seed keys.

18. **Billing meter usage** — select a custom trailing 14-day window. Storage
    shows s-tokens of stored content, bandwidth shows ingress and egress bytes,
    and risk content shows all six scanners. Department breakdown includes
    missing attribution and a remainder. Chart series and table totals sum to
    the headline exactly, including after weekly/monthly and cumulative toggles.
    Hide/show a legend series and drill into a day without losing the selected
    family. Switch to adjustments: signed positive/negative activity is separate
    from ordinary usage, whose total does not change. A historical empty range
    shows “No meter readings recorded,” not legacy telemetry usage. Invoice/
    contract estimates remain in their separate section. Compare the API totals
    with `billing_meter_readings_by_time FINAL` per reading kind: the nine
    duplicate deliveries must not increase usage. Run the seed twice and repeat.

## On failure

Fix the seed SQL (see rules in `PAGES.md`), then re-run the target that owns
the failed check: `mise run seed` for the local-only Killswitch and managed-agent checks, or
`mise run seed:demo` for shared demo-org checks. Re-check only the failed pages,
and commit the SQL change once green.
Screenshots of failures go to `.playwright-cli/` (ignored) — reference them in
the PR, don't commit them.
