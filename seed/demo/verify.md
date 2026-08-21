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
7. **Policy Center** — eight policies listed; Action column shows a mix of
   Flag / Warn / Block; Severity shows Medium through Critical; "Applies To"
   varies (All types, Tool Requests, ...); no row renders an empty summary.
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

## On failure

Fix the seed SQL (see rules in `PAGES.md`), re-run `mise run seed:demo`,
re-check only the failed pages, and commit the SQL change once green.
Screenshots of failures go to `.playwright-cli/` (ignored) — reference them in
the PR, don't commit them.
