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
local-only Killswitch, managed-agent, and exact remote-session attachment checks:
verify those as an ordinary human in the local organization, not through impersonation.

## Checks

A check FAILS when the page shows an empty state, an error boundary, or zero
where a value is expected.

0. **Network Access safety**: with no `network_ingress` entitlement for the demo
   organization, `/domains` retains the custom-domain surface but shows no
   Tailscale setup controls. Generic MCP server settings show no Network access
   mutation section. Confirm there is no ingress row, credential, or related
   console/network error.

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
   For Platform MCP, call `list_watchdog_findings` against the local rewritten
   seed with `severity: "all"` and an explicit trailing 30-day window. Each
   primary group is one rule-level alert, with a finding count, distinct
   attributed users and observed client surfaces (`chat_source`), not an
   individual finding. At least one alert must affect multiple users and
   clients. Evidence is a fully redacted sample of the stored display value:
   no partial secret, email domain, URL or judge rationale may pass through.
   Compare with Watchdog using the same detection-time window and severity.
   Optional finding breakdowns are not the dashboard's display sections.

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

11. **Workload sessions** — on the project **MCP Sessions** page, two rows show
    a workflow icon instead of an avatar and a blue **Workload** badge reading
    `Acme CI · Agent …`. One names the active managed agent, the other the
    suspended one, which the badge says in parentheses. Hovering a badge shows
    the issuer URL on `ci-identity.example.com`, the full `repo:acme/…` subject,
    and an **Admitted by** line: the payments workload lists two admissions
    (this project and the organization), the docs workload one. Opening a row's
    **Revoke** dialog lists the stop controls narrowest first, marks
    **Withdraw the admission** and **Delete the issuer** as the two that stop
    the workload reconnecting, and says the rest are not yet available in the
    dashboard.

12. **External OAuth settings** — open **MCP → Acme OAuth Discovery →
    Authentication** (`/mcp/acme-oauth-discovery/authentication`). The page
    shows the existing Gram-hosted metadata configuration, recommends
    provider-hosted metadata, and offers **Review update**. Opening the review
    starts with the fictional `https://auth.example.com` issuer; live discovery
    does not need to succeed for this seeded-page check.

13. **Killswitches on the identity Access tab (local rewritten seed only)** —
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
14. **Audit logs** — Killswitch history contributes nine rows: six
    **activated**, one **changed**, one **lifted/deactivated**, and one
    **expired**. Mutation rows name the same fictional operator and prescription
    version as their Killswitch history entries; the expiry row is attributed to
    **System**, follows the bounded row's deadline, and exposes no internal note
    in the organization-visible audit snapshot.
15. **Employee Shadow AI** — open **Employee Enrollment**, then Priya's detail
    page. The Shadow AI section lists Claude Code, Cursor, Codex, and Ollama in
    deterministic last-seen order, spans Harness and Local model categories,
    and shows two devices where applicable. Installed / Running signals,
    detected versions, and first / last seen values render without exposing
    hardware identifiers. Jonas's detail shows Claude Code, Cursor, and Aider,
    proving each page is filtered to its canonical enrolled-user identity.

16. **Gateway overview Activity** — open MCP → **Acme Agent Gateway** → Overview.
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
17. **Organization setup board** — with the `gram-setup-board` flag enabled,
    open `/acme-demo/setup/board`. Confirm all four columns render, Priya owns
    Set up observability in other platforms, `security-owner@demo.getgram.ai` owns Configure
    integrations in Awaiting Support, and Set up identity provider and Set up
    Anthropic observability sit in To Do. Distribute MCP servers, Configure
    policies, and Set up Platform MCP are hidden by default, so the board shows
    four tasks. As a platform admin, enable **Show hidden tasks** and confirm
    all three appear with a Hidden badge.

18. **Managed agents (local rewritten seed only)** — run `mise run seed` and
    use an ordinary human session in the local organization, with permission to
    view all agents and authorize credentials (for example, the local seeded
    admin). Shared demo impersonation remains intentionally restricted by the
    ordinary-human authorization requirement; it is not the browser verification
    target. Enable `agent-management` for inventory and
    `agent-identity-credentials` for API key management.
    - Open **Agents**. Confirm **Release assistant** is Active, **Support triage**
      is Suspended, and **Retired documentation bot** is Revoked. Confirm
      **Release notes assistant** is Active with its attachment-backed session. List and detail
      show Amara Okafor, Jonas Lindqvist, and Priya Raman respectively, with
      readable owner names and initials fallback rather than raw IDs or broken
      avatars. Local seeded fixtures must be visible to the authorized human.
    - Open Release assistant's sessions. Its one display-only session is
      expired; suspended/revoked agents have no seeded sessions. This fixture has no signing token, an invalid refresh
      hash, and empty delegation: it must not authenticate or refresh. The
      `agent-identity-credentials` flag gates live MCP authorization, not
      the inventory check; no live connection is promised by these fixtures.
    - API keys are empty after the shared SQL runs. With the credentials flag
      enabled, only the active agent permits key creation; suspended/revoked
      agents must not offer usable credentials. With the flag disabled, confirm
      the unavailable-rollout state, not a misleading empty-key success state.
      Never add usable keys to shared SQL.
    - Both active release agents have one direct `mcp:connect` grant scoped to
      the demo project and Linear MCP server. Release assistant also inherits
      a wildcard read-only `mcp:connect` grant from the Read-only Tools role.
      Confirm the wizard offers only the intersection with owner/caller access. If that intersection is empty,
      explain that no permissions can be delegated; never widen policy silently.
    - **MCP credential setup:** Create API key opens a full-page, routable
      setup flow. Check both ordinary MCP servers and gateways in the server
      picker. Select servers, connect or reuse the signed-in human's upstream
      accounts, explicitly authorize agent use, narrow existing delegable
      grants, then review. Creating a credential must never edit agent policy.
      The result shows the secret once and the selected server endpoints.
      Select individual member servers for Meta MCP: aggregate membership is
      not yet a supported agent consent/delegation target. Unproxied servers
      cannot receive Gram credential grants.
      Leaving setup must not delete connected accounts; start setup for the
      same or another eligible agent and confirm owned accounts are offered.
    - **Agent OAuth consent:** select an agent, finish required third-party
      connections (or reuse an owned account), explicitly authorize its use,
      and only then complete consent. Missing connections must block approval.
      Verify policy is unchanged. Use a locally executable provider for live
      exchange; the inert shared seed cannot prove third-party authentication.
    - Seed rationale: four identities cover active, suspended and revoked
      states; the two active release agents have exact project/server-scoped
      Linear grants and share one inert human-owned upstream session. API keys
      remain empty and the agent credential session is expired. Confirm the
      release agents' policy views show their scoped grant; the other two
      identities retain an explicit empty direct-policy state.
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
      `agent-identity-credentials` controls OAuth agent selection and agent
      API-key issuance and management, not standalone API-key admission.
      Display-only seeded tools cannot prove
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
    - Rerun `mise run seed`: the same four identities/lifecycles return, agent
      policy grants are reset, and no visitor-created API keys survive the
      shared SQL. Local-only developer keys may be restored by
      `RunLocalFixtures`; do not mistake those for managed-agent seed keys.

19. **Billing meter usage** — select a custom trailing 14-day window. Storage
    shows s-tokens of stored content, bandwidth shows ingress and egress bytes,
    and risk content shows all six scanners. Department breakdown includes
    missing attribution and a remainder. Chart series and table totals sum to
    the headline exactly, including after weekly/monthly and cumulative toggles.
    Hide/show a legend series and drill into a day without losing the selected
    family. The dashboard and API report ordinary usage only. A historical empty
    range shows “No meter readings recorded,” not legacy telemetry usage.
    Invoice/contract estimates remain in their separate section. Compare the API
    totals with the 864 ordinary rows in `billing_meter_readings_by_time FINAL`
    across all nine meters. The nine intentional duplicate physical deliveries
    must not increase API usage, while the incremental summary records all 873
    deliveries. Run the seed twice and repeat.

20. **Billing spend availability** — in the enterprise demo organization, open
    Billing. The spend heading, controls, chart, and product table must be absent,
    while the ordinary usage explorer stays visible. `usage.getSpendBreakdown`
    must return `availability: "unsupported_plan"`, `products: []`, and
    `total_cost_usd: "0"` with reporting-window metadata.
    For the available visualization, use a PAYG organization in the local stack
    only; restore any temporary local tier change afterward. Never change the
    shared demo tier to exercise this path. Select a custom trailing 14-day
    window and confirm `availability: "available"` with non-zero estimated costs
    for storage, per-scanner risk, and MCP egress only; inference must not appear.
    Compare quantities against `billing_meter_daily_summaries` ordinary-usage
    totals (including physical duplicate deliveries). Apply current PAYG rates
    and verify exact product costs sum to the response total; display rounds only
    at presentation. A PAYG period with no usage remains `available`, with three
    zero-filled product series and a visible zero-spend visualization.
    Switch daily/weekly/monthly and cumulative modes, preserving the total.
    Remove a product and confirm its stack, table row, and cost contribution
    disappear together; clear the selection and confirm the selection prompt.
    Restore all products, check the current in-progress bucket, and select an
    empty historical range. Check desktop and mobile layouts and confirm Usage,
    Rate, and Estimated cost values share their respective column's right edge.
    Repeat after reseeding. Local rewritten-seed checks alone do not qualify
    this shared-demo row for `[x]`.

21. **Admin billing spend by product** — sign in to the admin dashboard and open
    the enterprise demo organization's **Billing** page without impersonation or
    changing its account type. Select a trailing 14-day window. The spend section
    and `/admin/organization.spendBreakdown` must report non-zero storage,
    per-scanner risk, and MCP egress estimates from the existing meter fixtures,
    even without a Stripe subscription. These are current PAYG list-price
    comparisons, not the organization's actual invoice or contracted charges.
    Open both `/organizations/<ORG_ID>/billing` and
    `/organizations/<ORG_SLUG>/billing` with the same explicit date range. Call
    the spend API with each identifier too: the reports must match apart from
    `queried_at`, and both routes must render the same product table.
    Check that API product costs sum exactly to the total, displayed USD amounts
    round to two decimals (nonzero amounts rounding to zero show `<$0.01`), and
    ingress and inference are excluded. Changing the
    product selection must update the chart, table, and selected total together.
    Storage is blue, risk scanning purple, and MCP amber in both the spend and
    usage graphs, including cumulative views and light/dark themes. Check
    billing-cycle selection, custom dates, an empty historical range,
    refresh/error recovery, and desktop/mobile layouts. Delay and then fail a
    new-range request: retain the previous chart, table, and total while loading
    and after failure; retry must replace them with the requested range.
    Switch organizations and confirm no previous organization's values remain.
    An unauthenticated request must be rejected; the customer-facing enterprise
    spend endpoint must still return `unsupported_plan`.

    Verification recorded 2026-09-19 on the local admin UI against the
    shared-demo tenant, not the rewritten developer organization. Enterprise
    tier and absence of a Stripe subscription were retained. Browser checks
    covered selection, date ranges, grouping, cumulative mode, empty states,
    retry, organization switching, mobile width, and both themes; API checks
    covered exact arithmetic, dense buckets, invalid bounds, missing
    organizations, and authentication. ID/slug API reports and rendered tables
    matched; delayed and failed range changes retained the prior estimate and
    recovered on retry.
    [Visual evidence on PR #6602](https://github.com/speakeasy-api/gram/pull/6602#issuecomment-5742380327).

22. **Exact remote-session attachments (local only)**
    - With the feature enabled, inspect Linear session 6 for the fictional
      account and the two active release agents owned by the same human.
      Both attachments must show the same upstream session, with no token
      material exposed. Shared demo impersonation is not an authorized caller.
    - Attachment mutations require the actual owner as an ordinary human
      caller. The local developer is not automatically that fictional owner;
      use a separate local-only account/session fixture for mutation checks.
    - Detach one agent: the other binding and upstream account must survive.
      Reattach the same exact session twice: only one active binding per
      agent/issuer/client slot. A replacement session must not silently take
      over a binding to the original session.
    - Rerun `mise run seed` twice. Expect four agents, one inert upstream
      session, two bindings and two exact project/server-scoped grants, with no
      usable upstream credentials. This
      fixture proves display and identity relationships, not live execution.

23. **Identity-chaining registration evidence**
    - Run `mise run seed` twice in an isolated local stack. In its `default`
      project, open **Remote Identity Providers → Identity chaining example**.
      The Overview must render `https://authorization.example.com` and
      JWT-bearer under **Identity Provider Details → Grant Types**, without an
      error boundary. This is issuer capability, not a client's grant evidence.
    - Open **Clients → demo-resource-client**. Its Overview must render client
      ID `demo-resource-client`, scope `documents:read`, and token endpoint
      authentication method `none`, without an error boundary.
    - Open the client's **Sessions** tab. Expect **No active sessions for this
      client** and disabled **Revoke all sessions**. Search for
      `demo-resource-client` in the project's **MCP Sessions** page: expect no
      matching connection. Do not create a session or attach this inert public
      registration merely to populate a selector or connection inventory.
    - The current client Overview does not display `grant_types`, and the issuer
      Overview does not display the ID-JAG profile. Supplement the browser check
      with the real management API responses: issuer `grant_types_supported`
      includes JWT-bearer and `authorization_grant_profiles_supported` includes
      ID-JAG; the separate client's `grant_types` includes JWT-bearer and `scope`
      is `documents:read`. Do not describe these raw fields as rendered UI.
    - Check the client's deletion preflight: `session_count=0`,
      `ema_binding_count=0`, and no trusted user-session issuers. A read-only
      query against the isolated seeded database must also confirm no encrypted
      client secret, EMA binding, or remote session. Do not print credentials.
    - The fixture must make no provider-acceptance or usable-human-access claim.
      Capture the actual issuer/client pages before changing PAGES.md from
      `[~]` to `[x]`; API-only checks and the separate synthetic consolidation
      blocker demo do not complete this fixture's display verification.

## On failure

Fix the seed SQL (see rules in `PAGES.md`), then re-run the target that owns
the failed check: `mise run seed` for the local-only Killswitch, managed-agent,
and exact remote-session attachment checks, or
`mise run seed:demo` for shared demo-org checks. Re-check only the failed pages,
and commit the SQL change once green.
Screenshots of failures go to `.playwright-cli/` (ignored) — reference them in
the PR, don't commit them.
