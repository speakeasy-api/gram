# server

## 0.89.0

### Minor Changes

- 82869db: Distribute observability hooks through a pinned, checksum-verified Go binary bootstrapper. The one-time binary install is capped at 45 seconds and runs in the background wherever the agent supports asynchronous hooks. When the binary can't be installed on a developer machine, the outcome follows the org's "Fail Open During Outages" setting: fail open lets hook events pass, the fail-closed default blocks per provider semantics. The binary downloads from your Speakeasy server domain — the same domain hooks already send telemetry to — so restricted or sandboxed developer environments only ever need that one domain allowed.
- 999c323: Environment entries can now be marked non-secret so their values stay readable after save. Secret entries keep today's encrypt-and-redact behavior; flipping a secret entry to non-secret requires supplying a new value, while flipping a non-secret entry to secret encrypts the stored value in place. Callers that never send the new is_secret flag behave exactly as before (entries default to secret).
- 52aaf58: Add the org-level `hooks_fail_open` product feature and remove `observability_mode` (DNO-497): org admins choose whether agent hooks fail open or fail closed (the default) when the Speakeasy control plane is unreachable or erroring and no policy verdict can be obtained. The setting is delivered to hook senders as an `org_settings` entry in every authenticated `hooks.ingest` response's effects map, and toggling it records an `organization:hooks_fail_open_enabled|disabled` audit event. The speakeasy-hooks binary caches the last server-confirmed value next to its credential cache and consults it only on the unreachable/5xx branch of verdict resolution — explicit denies, 4xx responses, and the 401/403 credential ratchet keep failing closed regardless. The cached posture expires after 14 days without server confirmation (reverting to fail closed), and successful exchanges re-stamp an unchanged value daily so actively syncing machines never age out.

  Observability mode is removed outright — fail-open supersedes it (observability mode was equivalent to fail-open plus not creating blocking policies, while also swallowing explicit denies). Generated hook plugins no longer carry a nonblocking variant (`hooksGeneratorVersion` bumped, so connected repos republish), and the binary treats a legacy baked `nonblocking` flag as the fail-open posture so stale plugins keep outage tolerance without bypassing deny decisions.

- 1275b21: Add a project-scoped API for manually managing the Skills registry with immutable canonical versions. Project-bound API keys can no longer select a different project through the project header.
- 3edf806: Plugin assignments: organizations using the Speakeasy device agent can now choose which principals receive each plugin. From a plugin's detail page, admins assign an org-wide default (everyone), specific roles, individual members, or email addresses, and the device agent (`agent.getPlugins`) delivers each plugin only to its resolved recipients (email, user, and RBAC role membership). New plugins — including the auto-provisioned Default plugin — default to everyone, so nothing stops being delivered; admins can narrow the audience afterward. The assignments section is shown only for device-agent organizations; marketplace installs (Claude, Cursor, Codex) continue to receive every published plugin regardless of assignment.
- f4786b5: Show the currently live (published) plugin version on the plugin detail page.
  `getPublishStatus` now reports `live_version` — the version stamped into the
  published plugin.json manifests, read back from the marketplace repo via a
  single Contents API call and cached briefly — and the dashboard displays it
  next to the publish freshness indicator, so it can be compared directly
  against the version plugin clients like Claude Code report for installed
  plugins when debugging sync lag.

### Patch Changes

- b6f3467: Classify Claude sessions authenticated by company credentials (an API key, gateway/proxy, Bedrock, or Vertex) as `team` for the account-type cost breakdown. These sessions emit no `user.account_uuid` (only a personal Claude subscription, which signs in via OAuth, does), so account attribution previously no-op'd and their entire spend fell into the `(unset)` bucket. Attribution now always classifies and stamps `account_type`, and these sessions also teach the device-owner bridge (keyed on the per-device id, not the account UUID) so a personal account later seen on the same device can be attributed to its employee; only the `user_accounts` entity and billing mode, which key on the absent UUID, are skipped.
- dae476c: Persist hook-captured chat messages at their original occurred_at and order transcripts by (created_at, seq) (DNO-536). Previously chat_messages rows were stamped at insert time and read back in insertion order, so downtime backlog replayed from a device's offline spool sorted AFTER the newer live event that triggered the drain — the latest message appeared before older ones. The ingest handler now writes the event's occurred_at (clamped to arrival time so a skewed device clock cannot sort a row into the future) as created_at, and every transcript reader — full lists, keyset pages, risk/search windows — orders by (created_at, seq) with seq as the stable tiebreak. Keyset cursors keep their public seq shape; the anchor row's position is resolved server-side. Non-hook writers (playground, assistants, imports) leave created_at unset and the message store stamps each batch with one shared write-time value, so their ordering semantics are unchanged.
- 2fef155: Add a (chat_id, generation, created_at, seq) index on chat_messages so the DNO-536 transcript ordering — (created_at, seq) within a generation — is served by an ordered index scan and keyset pagination keeps its LIMIT early-stop instead of sorting the generation's full row set per page.
- cb75e1c: Scope the device agent's managed marketplaces to the org's default project plus any project the caller has an assignment in. `agent.getPlugins` previously returned every published marketplace in the org — each synthesizing its always-on observability plugin independent of assignments — so an org with many published projects flooded the device agent with one `speakeasy-observability` per project. The default project still always surfaces as the org-wide baseline; a non-default project now appears only when the caller has a matching plugin assignment there.
- cc8791e: Add project-selectable read and write permissions for skills to RBAC role management.
- a98cbcd: Gate the Skills page by organization entitlement and provision default Skills grants for RBAC-enabled organizations.
- 6429a07: Expand the `hooks.event.duration` metric for DNO-539 dashboard coverage: the unified `/rpc/hooks.ingest` endpoint now records it (it previously emitted no duration/throughput metric at all, leaving the plugin ingest path invisible to the hooks monitors), and every hooks endpoint now tags the metric with a `gram.hook.decision` attribute (allow/deny/ask, or none when the endpoint errored before producing a verdict) so allow/deny rates can be charted independently of the processing outcome. Ingest also distinguishes a new `unauthenticated` outcome (keyless requests acknowledged without processing) from the hard-401 `unauthorized` one.
- 49a4aac: Data migration translating organizations still on the removed `observability_mode` product feature to `hooks_fail_open` (DNO-497): the new fail-open row preserves the outage tolerance those orgs opted into, and the retired observability_mode rows are soft-deleted.
- cbf965c: Accept replayed hook events on hooks.ingest: an optional X-Gram-Replayed header marks deliveries redelivered from a device's offline spool after control-plane downtime. Replayed deliveries claim the idempotency guard for 15 days (covering the devices' 14-day spool retention) (instead of the 10-minute retry-burst window) so competing drain triggers dedupe, and their telemetry rows carry gram.hook.replayed so dashboards can separate backdated backlog from live traffic.
- 7ff9141: Persist the replayed flag on captured chat messages and surface it on risk results: messages redelivered from a device's offline spool after control-plane downtime (X-Gram-Replayed) now carry chat_messages.replayed, and findings produced by scanning them return replayed on the RiskResult type so retroactive findings are distinguishable from live ones.
- 1275b21: Skill version responses now include a `frontmatter` field with every top-level field parsed from the SKILL.md manifest, so spec fields like `license` and tool-specific extensions like `argument-hint` are visible without re-parsing the raw content.
- f96b6fb: Unfurl Gram dashboard links shared in Slack with the Speakeasy logo (the dashboard favicon) and a humanized page title. The generated Slack app manifest now registers the dashboard as an unfurl domain and grants links:write, and the trigger webhook answers link_shared events with chat.unfurl.

## 0.88.0

### Minor Changes

- e50ecd5: Add org-scoped `mcpServers.listForOrg` endpoint that lists MCP servers across all projects in the caller's organization, for organization-administrator flows like the RBAC connection-policy picker.
- 24f54bb: Allow organization admins to rename Shadow MCP inventory servers without changing their canonical URL identity.
- 8e3b7f2: Add a project-scoped API and dashboard detail page for individual Shadow MCP servers.
- a1def6a: Allow projects to disable and re-enable custom model provider keys without deleting or re-entering them.

### Patch Changes

- 4dde5e0: Billing tokens-under-management reads over attribute_metrics_summaries now filter tombstoned rows (is_active = 1), matching the costs page reads, so generations soft-deleted by the backfill runbook are excluded from billed totals and breakdowns.
- 5ac5f91: Employees list linked accounts now attach by directory ownership (summary email resolved to the org user, or the account's own email) instead of by the raw telemetry user_ids folded into a summary. Stray telemetry rows that pair one person's email with another person's user id could previously hand an account — and the role bucket in the by-role view — to the wrong employee (DNO-509).
- 703a22b: feat(risk): add an assistant filter to risk events. The Risk Events page gains an "Assistant" select listing the project's assistants plus a "No assistant" option, so findings from chats not linked to an assistant (the ones most likely missing user attribution) can be surfaced on their own — or scoped to a single assistant. API: `assistant_id` and `non_assistant` params on `listRiskResults`/`listRiskResultsForAgent`.
- efe608b: PI detection now uses the LLM judge for all orgs. The L0 heuristics layer and its feature flag are removed.
- 6e7a771: Stop forwarding browser-only headers (`Origin`, `Referer`, `Cookie`) from the inbound request to remote MCP upstreams. When the dashboard drove a remote MCP server, its `Origin` was relayed verbatim and upstreams enforcing the MCP spec's DNS-rebinding protection (e.g. Langfuse) rejected the request with 403 "Access forbidden", surfacing as "Something went wrong loading tools" in the Tools tab. Dropping these headers makes dashboard-proxied requests match those from a headless MCP client and prevents the dashboard session cookie from leaking upstream.
- 63008ae: Restore Claude MCP inventory capture in the Go hooks relay. Session start and configuration-change hooks now send a locally redacted inventory snapshot through canonical ingest so external MCP URLs appear in Shadow MCP inventory before a tool is called.

## 0.87.0

### Minor Changes

- 4da1ceb: Assistant completions now route through a project's own model provider key when one covers the assistants slot. Projects without a key keep the current platform-covered behavior. The key slot a completion uses is derived from the authenticated caller rather than request headers.
- 0d36d3c: Projects can now bring their own model provider key for the risk-policy judge and the prompt-injection classifier, each as an independent key slot. Unset slots fall back to the project default key, then the platform key.
- 15b6f77: Projects can now store their own model provider API keys (BYOK), scoped per responsibility slot with a fallback chain: a slot-specific key wins over the project default key, which wins over the platform key. Keys are validated with the provider on save, stored encrypted, and never returned by the API. Configuration is gated behind the custom model keys product feature; with no keys configured, behavior is unchanged.
- 50097b0: Implement remote MCP server header management API
- 15618be: Add the project-scoped API for listing users and usage for a Shadow MCP server, with generated dashboard SDK support.
- 7cef3fe: Redefine tokens under management as observed agent traffic: the billing page now counts the tokens the platform observes coming from users' agent sessions (input, output, and cache writes — cache reads excluded), never inference the platform spends itself (risk-policy analysis, hosted chat). Breakdowns now offer model, agent, provider, account type, project, user, division, department, and role; the project filter dropdown is replaced by the Project breakdown section.

### Patch Changes

- db26157: Label cowork tool calls as `cowork` in tool logs so filtering by Cowork source works
- b8a6e78: Fix MCP attribution never promoting when the Claude plugin authenticates with an org-wide hooks key. The transcript-attribution tuple was keyed in Redis by the project resolved from the plugin's `GRAM_HOOKS_PROJECT_SLUG` (default `"default"`), while the promotion worker looked it up by the staged OTEL row's project — set by the OTEL exporter's own credential. With an org-wide key the two disagree, so the join always missed and staged rows promoted verbatim as `custom` after the timeout. The tuple is now keyed by org id — both ingest paths always agree on the org, and cross-org isolation is preserved — with the row's org materialized onto `telemetry_logs_staging` as the lookup scope.
- b270dc9: Remove the dormant telemetry.queryRiskTokens endpoint (no consumers; it computed the pre-DNO-491 billed population and no longer matched any billing surface)

## 0.86.0

### Minor Changes

- 4d22067: Add "Suggest with AI" to the exclusion create/edit form, backed by a new dedicated `risk.suggestExclusion` endpoint (separate from `risk.suggestCustomRules`). It returns structured match fields (match type, match value, rule id/source filters) that the dashboard serializes into the exclusion criteria expression — regex suggestions are validated (RE2 compile, length cap) server-side before they reach the form.
- f3ea11b: Add the project-scoped Shadow MCP inventory listing API and generated client SDK support.
- b10e52d: Restore Claude's redacted MCP attribution on cost telemetry via session transcripts. Claude stamps `mcp_server.name='custom'` on api_request OTEL rows for user-configured MCP servers; those rows now park in a `telemetry_logs_staging` ClickHouse table while the Claude hook plugin's Stop/SubagentStop hooks ship the unredacted `(request_id → server/tool)` attribution extracted from the local session transcript. A per-session Temporal workflow joins the two, rewrites the attribution inside the row's attributes JSON, and promotes the row into `telemetry_logs` — so `attribute_metrics_summaries` aggregates true server/tool names. Rows whose attribution never arrives promote verbatim after 30 minutes via a scheduled sweep.

### Patch Changes

- 00ac3b8: Fix deletion of organization-level remote session clients, derive tunnel gateway URLs from the active environment, and detach remote identity providers without deleting shared clients.

## 0.85.0

### Minor Changes

- ceb150d: Forward each organization's tokens-under-management usage to PostHog (AGE-2289): hourly group properties on the organization group (current/previous cycle tokens, contracted allowance, utilization) plus a once-per-day organization_token_usage event, emitted from the billing usage refresh workflow.
- b8e7fe0: Hook plugin browser sign-in is now opt-in per organization. By default, published plugins never open a browser: they authenticate with explicitly configured credentials, a previously cached key, or the organization-wide key, and the login helper prints manual setup instructions instead. Organization admins can re-enable the interactive browser sign-in from the org settings page.
- 83f97ec: Judge timeouts now surface as a dedicated `outcome:timeout` metric tag, with retuned duration histogram buckets near the 10s call timeout.
- fff8efc: Assistant runtimes can now run locally: the new `local` runtime provider (the
  local-development default) starts one Docker container per assistant on demand,
  reuses it across turns, and automatically replaces idle containers when the
  runtime image is rebuilt — no Fly.io credentials or registry pushes needed for
  local image development.
- dfe9fd9: feat: require a user_session_issuer for every remote and tunneled MCP server. The server mints the issuer in the same transaction as the mcp_servers row and it lasts for the server's lifetime: `user_session_issuer_id` is removed from both the create and update APIs, and the update query COALESCEs to the stored value, so no code path can supply, strip, or swap it. Enforced at the schema level by a `mcp_servers` CHECK constraint (added NOT VALID, then validated). Toolset-backed servers are exempt (their issuer lives on the toolset).

### Patch Changes

- 4c57fa5: Stop the chat session list visibility check from recording an authz challenge. Listing sessions probes `chat:read` only to decide whether the caller sees all sessions or just their own; a member without the grant is the normal case, not a denial. Logging it as one polluted the access diagnostics with spurious `chat:read` denials (the insights dock lists chats on every page load), making it look like `chat:read` was required to view unrelated pages such as the Cost dashboard.
- a29bea1: feat: expose `is_default` on the plugin API and use it in the dashboard instead of matching on the "Default" name/slug. The onboarding distribute-servers step and plugin card/detail pages previously identified the org's fallback plugin by string comparison (`name === "Default"` / `slug === "default"`), a proxy that predates the server's `is_default` column and unique-per-project index. Both now read the real `is_default` flag returned by `listPlugins`/`getPlugin`.
- fe3ddb2: fix: batch toolsets.list queries to eliminate N+1. `toolsets.list` used to loop over every toolset in a project issuing 11+ DB round trips each (plus one more per external-MCP tool), making the endpoint take seconds for projects with many toolsets and slowing the dashboard home page, which prefetches it on every project route. Replaced with a single batched fetch across all toolsets, cutting round trips from `O(toolset_count)` to a fixed ~10 regardless of how many toolsets a project has.
- 7c637c7: Refresh the OpenRouter model list: add Claude Fable 5 (marked Expensive) and the GPT-5.6 series (Sol/Terra/Luna), replace the playground picker's "(Expensive)" label suffixes with a badge, and remove deprecated models (Claude Sonnet 4, GPT-4.1, o3, o4-mini, Gemini 2.5 Pro/Flash, DeepSeek R1).
- 4a98092: Address review feedback from the OpenRouter model refresh: pin explicit per-provider fallback models in ResolveModel so de-listed or unknown models never silently resolve to a premium model (previously anthropic/\* fell back alphabetically to Claude Fable 5), give elements an explicit DEFAULT_MODEL (Claude Sonnet 5) instead of MODELS[0], and remove Gemini 3.5 Flash from the prompt-policy judge picker (the judge disables reasoning, which that model rejects).
- 125059e: Reduce project overview latency by running independent ClickHouse aggregations concurrently, tracing each query, and computing chat resolutions in a single PostgreSQL pass.

## 0.84.0

### Minor Changes

- da79525: Redesign the Plugins pages and add MCP server readiness surfacing:

  - Marketplace card now reflects real setup state: an uninitialized/warning
    variant (skeleton repo link, "Not published" badge, "Publish now"/"Add
    collaborators" CTA) shown until the marketplace repo exists **and** has at
    least one collaborator who has accepted their GitHub invite, distinct from
    the connected/published state.
  - Install flow reworked: a single "Install" dropdown (GitHub installation via
    marketplace, preferred, or direct zip download) replaces the old split
    button, on both the Plugins index and detail pages, and no longer disables
    zip download just because the marketplace isn't set up yet.
  - Default plugin gets special treatment (badge, description, auto-heal on
    read for projects that predate the feature) and plugin membership no
    longer N+1-queries its servers.
  - New collapsible readiness bar on the MCP server ("x" route) sidebar,
    summarizing Server URL / Authentication / Source / Included in Plugin
    status with links to fix each.
  - Server: `GetPublishStatus` now reports whether the marketplace repo has a
    real (accepted, not just invited) collaborator, cached briefly to avoid
    hitting GitHub's API on every dashboard poll, and invalidated immediately
    after publishing adds one.

- 48a97e2: Implement remote MCP server header management API

### Patch Changes

- da79525: Attach MCP servers to the Default plugin when they're enabled, not just when their first endpoint is created — remote MCP servers are created disabled with a pre-staged endpoint, so they previously never auto-attached and manually adding them failed with "mcp server is disabled or has no published endpoint". Also fixes creating a second endpoint for an already-attached server (previously failed on a duplicate-attach conflict), hides endpointless servers from the plugin's add-server picker, and asks for confirmation before removing a server's last address.
- ae3fc4b: The billing page's Model breakdown now splits into "Risk Policy Analysis Model" — the platform's own risk-policy scanning inference, the metered unit of the TUM contracts — and "Completion Model" for user-facing completion surfaces (playground, elements, MCP chat, Slack). The "Sessions & messages" section and the risk-findings chart stacking are removed: billing meters the act of scanning observed traffic, not the customer's message population. Risk-analysis inference is attributed to the scanned user, so the User, Role, and Division breakdowns now report whose traffic was analyzed.
- b06aa04: The enrollment page no longer shows 0 tokens and a stale last activity for employees whose telemetry rows split across identity keys: usage rows carrying a user id but no email now merge into the employee's email-keyed summary, linked AI accounts attach to that merged summary, and role breakdowns resolve those users instead of bucketing them as Unassigned. The employees and agents tables also render their pagination footer flush against the table instead of floating below a gap.
- e3cf1d1: The hooks setup dialog's Claude Code instructions now install from your org's published plugin marketplace (with copyable commands and managed-settings snippets), instead of a public repository marketplace that carried no credentials. Publish status now reports the observability plugin slugs so install instructions always show the exact plugin name.
- 020dfdf: Avoid rebuilding every platform tool descriptor for each tool returned by `toolsets.list`, significantly reducing latency for projects with large toolsets.

## 0.83.0

### Minor Changes

- 5a0f98a: Add organization-scoped `externalCredentials` management API for CRUD of external credentials (AWS/GCP IAM) used to authenticate Gram into a customer cloud account. Per-provider create/update/get/delete plus a generic supertype-only list with an optional provider filter. Gated on `org:read`/`org:admin` and audited under per-provider subjects (`aws_iam_credential`, `gcp_iam_credential`).
- 317d86e: Hook browser login now delivers the minted API key to the local listener as a form POST instead of appending it to the callback URL, keeping the key out of browser history and request logs, and the sign-in tab closes itself once authentication completes. Older dashboards that still redirect with query parameters keep working.
- 02ac329: Issuer discovery now parses RFC 8414 `service_documentation`, `op_policy_uri`, and `op_tos_uri` and persists them on `remote_session_issuers` across the project, organization, and global admin surfaces.
- 4fa3e51: Split the org-admin `organizationRemoteSessionIssuers` service into three per-resource services mirroring the project-scoped layer: `organizationRemoteSessionIssuers`, `organizationRemoteSessionClients`, and `organizationRemoteSessions`. Pure refactor with no behavior or RBAC change, but breaking for the management API and SDK: every method drops its redundant resource suffix, so the RPC paths and SDK method names change (e.g. `organizationRemoteSessionIssuers.createClient` becomes `organizationRemoteSessionClients.create`).

### Patch Changes

- e223d08: fix(telemetry): keep deleted MCP servers' tool-usage classification. Tool-usage `target_type` now resolves against live + soft-deleted MCP servers, so a managed remote/tunneled server's history no longer flips to `shadow_mcp_server` once the server is deleted or recreated.
- dfee73b: fix: make Claude session user attribution deterministic. The hook-supplied device-enrolled employee email now always wins over the OTEL-cached account email (the AI account's own report, e.g. a personal gmail) when both are present — previously whichever ingest stream created the chat row first determined the session's `external_user_id`. The account's own email is unaffected and remains surfaced via `user_accounts` / `account_email`.
- 11da690: feat: show which users are running the device agent. The org Device Agent page gains an admin-only "Active Users" tab listing who has synced, attributed by the email each agent reports on its ~60s `agent.getPlugins` poll, with `Page.Toolbar` search (name/email) and an Active/Stale status filter. A best-effort per-`(org, email)` last-seen record (throttled to ≤1 write/min) backs a session-secured, org-admin-gated `agent.listSyncedUsers` endpoint.
- 74dbfed: feat: add a token usage breakdown to the billing page's Tokens Under Management section (DNO-404). A billing-cycle picker scopes the TUM usage card and a new "Token usage" panel to any contracted cycle; the panel renders a stacked bar chart of org-wide tokens for that cycle, sliced via a grouped, searchable breakdown picker — total, by token type (input / output / cache read / cache write), by risk involvement (tokens from sessions with at least one active risk finding, via the new org-scoped `telemetry.queryRiskTokens` endpoint), or by analytics dimensions — with daily/weekly/monthly granularity and a cumulative view. Beneath the chart, a usage details table lists per-metric cycle totals with sparklines: token types, agent sessions, tool calls, and message-level stats (tokens in messages with risk findings and tokens from tool-call messages, read from Postgres per-message token counts). The table's measures arrive in a single `telemetry.queryTumDetails` request, and its totals and time-based overage attribution are normalized to match the billed tokens-under-management numbers exactly, with finalized cycles served from the durable billing snapshots. The section also supports drill-down: clicking a chart bar (or dragging across bars) narrows the whole view to that range (re-bucketing daily), and a time-range picker beside the cycle selector accepts any custom period — typed in natural language or picked from a calendar — with billed normalization and overage reserved for full organization cycles; the usage card is labeled with the billing cycle its totals describe. Cycles are named by month ("June Billing Cycle"), table sections collapse individually or all at once, and a Reset button restores the initial view.
- 0517e60: Restrict the Observe dashboard section (Costs, MCP & Tools Insights, Employee Enrollment, Agent Sessions, Tool Logs) to org admins. The Observe nav stays visible (like the Secure section), but each Observe page is gated on `org:admin`, so basic members see an "Access restricted" notice. Basic members also no longer receive `environment:read` by default.
- dfee73b: feat: surface the AI account email on agent sessions. `chat.listChats` and `chat.load` now return `account_email` from the linked AI account, and the dashboard shows the personal account's email (e.g. a gmail on Claude Max) on session list rows, the transcript's user messages, and the session details popover — instead of only the attributed employee's work email.
- 3f15c7c: fix: apply the Tool Logs `http.response.status_code` filter at the trace level so status-less rows no longer leak 200/success traces into "Non-2xx responses", and add a first-class Error/Success/Blocked/Pending Status filter to the Tool Logs page.

## 0.82.0

### Minor Changes

- 7882ed7: Add a built-in preset exclusion library that suppresses known false positives (test credit cards, example API keys/tokens, module/content hashes, placeholder emails) across all detection sources. Adds the `risk.listBuiltinPresets` endpoint and a read-only "Built-in library" section on the Exclusions tab that lists the live catalog.
- 3e492c4: Add backend APIs and runtime routing for tunneled MCP server sources.

### Patch Changes

- f6ad2fc: fix: key session active/expired status off refresh expiry

## 0.81.0

### Minor Changes

- 25ce5ea: Email org admins when a new access request is submitted.
- c9eaac0: Skill activations in Codex sessions are now tracked best-effort: opening a skill's SKILL.md and explicit $skill-name prompt mentions surface as skill.activated events in observability, matching Claude Code sessions.
- f92917c: Add the `adminRemoteSessions` management service for curating a platform-wide catalog of remote session providers. A "global" provider is a `remote_session_issuer` paired with one or more `remote_session_client` records that have no owning project and no owning organization (`project_id IS NULL AND organization_id IS NULL`), so it is shared across every organization rather than scoped to one. The service exposes CRUD over global issuers and clients (`createGlobalIssuer`, `listGlobalIssuers`, `getGlobalIssuer`, `updateGlobalIssuer`, `deleteGlobalIssuer`, and the matching `*GlobalClient` methods, plus `listGlobalClients` by issuer). Every method is gated to platform admins (Speakeasy employees) and is session-authenticated only. Issuer slugs are unique within the global scope, deleting an issuer is blocked while a live client still references it, and client secrets are write-only. This ships the creation/administration surface only; the runtime consumption path (projects inheriting global providers) is a separate follow-up, so global rows exist but nothing reads them yet.
- f16bde1: Re-introduce the unified `/rpc/hooks.ingest` endpoint with working self-serve authentication for hook plugins. On session start the plugin opens the Gram dashboard in a browser, receives a hooks-scoped API key on a localhost callback, and caches it per device — no python or manual key setup required. Machines that have never authenticated are not blocked: sessions proceed with a warning, Claude is prompted to offer connecting via the bundled login helper, and enforcement only becomes strict after the first successful sign-in.
- e9ff915: Add the Non-Corporate Accounts risk-policy category (detection source `account_identity`). Policies can now flag sessions authenticated with a personal AI account (`identity.personal_account`) or with an AI-account email domain outside a configurable approved list (`identity.unapproved_domain`), reusing the account attribution captured by session ingest. The create/update policy endpoints accept `approved_email_domains`, findings are emitted once per session, and the Policy Center exposes the approved-domains input in the category's Customize sheet (flag-only, like other agent-integrity detectors).
- ad4e76d: Adds a prompt guardrail replay endpoint with per-message judge verdicts, cost and latency details, and CEL scope support.
  Adds persistent reviewer verdict save, list, and delete endpoints for policy eval regression sets.

### Patch Changes

- 548e704: Assistants can now attach MCP servers directly, including remote (external‑SaaS) and tunnelled servers that aren't backed by a Gram toolset. The assistant setup chat can list the project's MCP servers and attach one by name, and the assistant's runtime connects to it alongside its toolsets.
- 34b8a1b: Editing an environment now requires `environment:write` instead of `project:write`. Creating, updating, and deleting environments previously gated on `project:write`, so principals holding only `environment:write` were rejected. The dashboard gates for these actions were realigned to match.
- 8104660: chore: use icons to delineate team vs personal accounts
- ed49c7d: Clear stale cached hooks credentials on auth rejection so Claude prompt submission can continue and prompt users to reconnect.
- 5828815: Preserve assistant setup chat history: list prior onboarding threads and make them URL-addressable (scoped by source_kind).

## 0.80.0

### Minor Changes

- 9275f02: Adjust API endpoint paths to follow existing RPC API conventions
- fedda7c: Add a `cliAuth` service for device-agent enrollment (DNO-388). `cliAuth.authorize` (session-authenticated, member `org:read` scope) stores a PKCE-bound one-time code, and `cliAuth.redeem` (no session — the PKCE code + verifier is the credential) atomically exchanges it for a per-user `[agent, hooks]` API key, returned once. The dashboard CLI callback uses this flow when the request carries `client=device-agent`, so the raw key never travels in a URL; the existing CLI producer-key login is unchanged.

### Patch Changes

- 59a1029: Drop US_DRIVER_LICENSE Presidio findings at the finding level so they never surface, even when a policy pins no entities and Presidio scans its full default recognizer set.
- 9bc41b9: Surface Claude attribution dimensions in telemetry query results and the cost explorer.
- 4adc65b: Disable HTTP keep-alives on function-runner calls and give that path its own timeout, so retries dial fresh connections instead of reusing pooled connections to Fly machines that were autostopped mid-flight (which surfaced as instant EOFs). The function-runner timeout now sits above the runner's 5-minute execution budget so long tool calls are no longer cancelled by the caller.
- b95233f: Risk Events now shows historical findings for turned-off policies. Filtering the Risk Events page by a disabled policy previously returned no results because the query required the policy to be enabled; explicit policy filters now surface a disabled policy's past matches. The dashboard flags the inactive policy (a banner plus an "(inactive)" label in the filter) so it's clear the data is historical. The default unfiltered view is unchanged and still lists only active policies.
- d09b418: Fix a nil pointer panic in telemetry SearchUsers when called without a filter.

## 0.79.0

### Minor Changes

- 57bf9af: Public well-known OAuth/MCP metadata responses now send `Cache-Control: public, max-age=60` and a strong `ETag` with `If-None-Match` 304 revalidation, so clients and proxies can cache them. The OAuth Client ID Metadata Document keeps `max-age=3600` and gains an `ETag`. This is a prerequisite for fronting these responses with an ingress cache or CDN.
- 2186673: Support organization-level remote session clients. A `remote_session_client` can now be created with no project (organization-level) so every project in the organization can attach and use it, mirroring organization-level remote session issuers. On `organizationRemoteSessionIssuers.createClient` and `createCimdClient` an omitted `project_id` under an organization-level issuer creates an organization-level client (the same `project_id`-omission convention `createIssuer` already uses), while a supplied `project_id` scopes the client to that project. The consent/token runtime resolver, the project-scoped client reads, and the attach-time single-client invariant now resolve both a project's own clients and organization-level clients in its organization, so a project admin can attach, detach, and use an organization-level client from their own user session issuer but cannot edit or delete it (those stay on the org-admin surface). The `RemoteSessionClient` API shape adds `organization_id` and allows an empty `project_id` for organization-level clients, mirroring the issuer change.
- 5c825a9: Default to Claude Sonnet 5 (`anthropic/claude-sonnet-5`) for in-app model usage and newly created assistants. The model is added to the allowlist and all model pickers (playground, elements, onboarding). The backend `DefaultChatModel`, the platform-managed assistant, the onboarding assistant default, and the playground/MCP chat surfaces now select Sonnet 5. Specialized models (risk/PromptIntel judges, chat segmentation, embeddings, follow-on suggestions) are unchanged.
- fcfd78e: Add server-side controls for unmasking redacted secrets
- 400f471: Plugin marketplaces now send a human-readable `displayName` to Claude Code, so plugins show with their admin-entered name and capitalization (e.g. "MoonPay MCP Servers") instead of the de-slugified lowercase name ("Moonpay mcp servers"). The synthesized observability plugin displays as "<Org> Observability". The plugin `name` remains the kebab-case slug used for namespacing and claude.ai marketplace sync. Older Claude Code clients ignore the field and fall back to prior behavior.
- c8597b1: Add the unified `/rpc/hooks.ingest` endpoint for third-party hook ingestion while preserving existing provider-specific hook endpoints. Hook plugins now authenticate each developer locally through the browser callback flow and store a hooks-scoped key on the device.

### Patch Changes

- d7b8ec9: Gate the "click to reveal" secret action in Risk Events behind the `chat:read` scope. Users without `chat:read` now see flagged secret values as a non-interactive "Hidden" placeholder (with an explanatory tooltip) instead of a reveal control, and the page-level "Reveal all" toggle is hidden for them. The `chat:read` scope description in the role editor is updated to note that the grant also controls unmasking flagged secrets in Risk Events.
- 98de65f: mig: add session_capture_exclusions table

## 0.78.0

### Minor Changes

- 0d7ba58: Add outbound OAuth Client ID Metadata Document (CIMD) support to remote-session OAuth. A `remote_session_client` can now be created in CIMD mode via a dedicated `remoteSessionClients.createCimd` endpoint: Gram generates the `client_id`, hosts a public client metadata document at `/.well-known/oauth-client/{id}`, and sends that platform-canonical URL as the `client_id` on every outbound `/authorize`, `/token`, and refresh call, with no symmetric secret and `token_endpoint_auth_method=none`. Issuer discovery now parses and persists `client_id_metadata_document_supported`, which gates the createCimd endpoint. The document endpoint is pinned to the platform host (404 on custom domains) so a strict upstream AS only ever validates the canonical URL. New management surface: the `createCimd` endpoint, `client_id_metadata_uri` on the client view, and the issuer CIMD-support flag on the issuer forms/views.

## 0.77.0

### Minor Changes

- fc47698: Allow editing the permissions of system roles (`admin`/`member`) per organization, while keeping their name and description platform-managed. The Admin role is guarded against losing the `org:admin` permission to prevent org lockout. The roles tab is reworked: the whole role row opens the edit sheet (gated on `org:admin`), scope groups no longer auto-expand and show a description when collapsed, and the members column uses a new interactive member facepile (hover focus, click to view all members) that also replaces the facepile on the org home projects list. Adds Directory Sync (SCIM) info alerts on the team, roles, and identity pages explaining that members and roles are managed by the identity provider while SCIM is enabled.

### Patch Changes

- 8116a4c: Improved Codex shadow MCP enforcement so calls are checked against the session MCP server inventory.
- efe6163: Fix Cursor shadow MCP enforcement wrongly blocking Gram-hosted MCP servers when a shadow MCP risk policy is enabled — access is now decided by the server URL rather than requiring the agent to echo an internal identifier.
- c6ddf0e: Fixed the MCP catalog listing duplicate servers (count doubling) when loading more

## 0.76.0

### Minor Changes

- f04e8b0: Add a `chat:read` RBAC scope that gates access to other members' agent session transcripts. The `chat.load` endpoint and the dashboard agent-sessions list are scoped by `chat:read`: anyone can always read sessions they own (the handler grants owner access directly — no `chat:read` grant needed), while reading every member's session requires an unrestricted `chat:read`. The scope is not a default of any system role — not even `admin` — so it must be granted explicitly via a custom role. On the agent-sessions page, callers without `chat:read` see a banner noting they only see their own sessions (with a link to the roles page for org admins). Each dashboard session open is recorded in the audit log as a `chat_session:access` event. The scope is selectable in the role editor (Agent Sessions group) and the dev RBAC override toolbar.

## 0.75.0

### Minor Changes

- 0cd8e96: Add an agent type filter to the Agent Sessions page, populated from the agent sources actually present in each project's chats via a new `chat.listSources` endpoint.
- 7763a1b: Tool-call blocks are now durable, first-class entities with a stable `/blocks/<id>` URL and 👍/👎 feedback. When the risk engine blocks a tool call, the block is persisted and its reason is injected into the agent-facing response (Claude `PermissionDecisionReason`, Cursor `AgentMessage`, Codex `reason`) along with a link to the block page, so the agent can reason about the denial instead of hallucinating one. New session-scoped, org-admin-gated `getRiskBlock` and `submitRiskBlockFeedback` endpoints back an in-app `BlockDetailPage` (under `AppLayout`) and a slug-free redirect resolver for the agent's external link, with a "More Info" link from the Risk Events modal.

### Patch Changes

- 3464cb8: Show the assistant's creator as its owner. Assistants already recorded who created them; that attribution is now surfaced as a profile avatar (reusing the org-home member avatar treatment) on both the assistant card and the assistant setup page's overview panel. The owner resolves to one of three states: the creating member (avatar + name, full name on hover), "No owner" when the assistant was never attributed, or "Orphaned, no owner" when the creator is no longer a member of the organization. Backed by a new optional `created_by_user_id` field on the `Assistant` API type.
- a5d57cb: Fix the chat detail "Risky only" filter and rework search-within-thread. The filter previously showed nothing on threads whose findings sat on other transcript pages, and only worked for org admins via the separate risk-results endpoint. `chat.load` (risk_only) now returns `risk_seqs` — the seqs of the flagged messages — so the panel windows the full thread and filters on the authorized load (the toggle is shown only to org admins). Search now steps through every occurrence in document order — within a message's text and inside a tool call's arguments and output — with the active occurrence highlighted distinctly, instead of stepping per message and washing every hit the same colour.
- e13497f: Claude Code prompt correlation no longer stalls on high-volume sessions. Previously a chat with a large backlog of unlinked prompts could exceed the correlation time budget and fail entirely, leaving prompts unlinked from their telemetry; correlation now bounds its work and drains the backlog incrementally so prompts stay reliably linked.
- d3bad97: Shorten risk policy bypass ("Request access") links. The blocked-tool-call message now embeds a short cache-backed `rpbr2.<id>` token instead of a 1000+ char encrypted blob in the URL fragment. Links already issued in the legacy `rpbr1` format keep working until they expire.

## 0.74.1

### Patch Changes

- 24b41d9: Improve tool observability filter performance by returning hosted MCP server display names from telemetry filter options, allowing the logs and insights pages to avoid hydrating full toolset resources for server filter labels.
- 1751a59: Publish plugins straight from the plugin detail page. After adding or removing a server, or editing a plugin's metadata, a "Publish now" prompt offers a one-click republish — or opens the first-publish dialog for projects not yet connected to GitHub — so there's no need to return to the plugins list to re-publish. The detail page now also shows publish freshness: an "Unpublished changes" badge when the project's current plugin state differs from what was last published, or the last published time when up to date, alongside a durable publish button and a marketplace install banner.

  This is backed by new `up_to_date` and `last_published_at` fields on the `plugins.getPublishStatus` API, which compare the project's live plugin fingerprint against the fingerprint last pushed to GitHub. Both fields are absent when the project has no GitHub connection.

- bbdda53: Pinned chats: pin/unpin conversations on the /chat page. Pinned chats surface in a dedicated "Pinned" section above Recent Chats. Adds a `setPinned` chat API and a `pinned` filter on `listChats`, backed by the `chats.pinned_at` column.

## 0.74.0

### Minor Changes

- f479a1b: Org admins can now register a standalone `remote_session_client` directly from the Remote Identity Provider details page. A new `organizationRemoteSessionIssuers.createClient` endpoint creates a client under an existing issuer with no `user_session_issuer` attachments; the client inherits a project-specific issuer's project, or the admin names a project (downscoping) when the issuer is organization-level. The dashboard surfaces a `New Client` button on the issuer's Clients tab that opens a sheet supporting Dynamic Client Registration (when the issuer advertises a `registration_endpoint`) or manual `client_id` / `client_secret` entry.
- 9b85ddd: feat(telemetry): include the chat title on `listSessions` results (resolved from Postgres, batched per page) and show it in place of the chat id in the cost dashboard's session table

### Patch Changes

- 4f9b199: Project Assistant chats can now be renamed from the live chat view. The dock header shows the active conversation's title and lets you click to edit it inline. Manually chosen names are preserved — automatic, session-context title generation skips any chat a human has renamed (clearing the title re-enables auto-naming).
- 3298a99: Add hook event processing duration metrics for Claude, Codex, and Cursor hook traffic.
- 4a44fcb: Make the Claude hook shadow-MCP guard resilient to a missing SessionStart MCP inventory snapshot (DNO-286). The MCP inventory captured at SessionStart is now persisted to a per-session file, and the blocking PreToolUse hook replays it in its own payload so enforcement no longer depends on the server having cached the async SessionStart snapshot in time. The server prefers a payload-supplied inventory, writes it back to the cache so the telemetry path self-heals, and falls back to the cached snapshot (still failing closed) when neither is available.
- 9349794: fix(telemetry): match `listSessions` dimension filters per-chat instead of per-row so combining a user-directory filter (e.g. department) with `hook_source` no longer returns empty when those attributes live on different rows of the same chat

## 0.73.0

### Minor Changes

- ea9f56b: Gram Functions tool-call and resource-read POSTs now retry on a saturated runner's `429 + Retry-After` and Fly's `503` (both guaranteed before the function runs) instead of surfacing transient saturation as a hard failure, with jittered backoff to spread simultaneous retries and avoid a thundering herd. Transport errors that are transparently retried now log at `WARN` rather than `ERROR`, so recovered attempts no longer look like failures while the final unrecovered failure is still logged as an error.
- c1ef552: `remoteSessionClients` and the org-admin client views now source the `user_session_issuer` relationship entirely from the join table. The `RemoteSessionClient` result replaces the single `user_session_issuer_id` with a `user_session_issuer_ids` array (breaking), create/clone accept zero or more `user_session_issuer_ids` so a client can be created standalone, and a client's issuer attachments are now managed through the new `attachUserSessionIssuer` / `detachUserSessionIssuer` endpoints instead of `update`. No more reads or writes of the legacy `remote_session_clients.user_session_issuer_id` column.
- 4b45485: `chat.load` now returns a `totals` object with whole-generation trace-entry counts (`total`, `user_messages`, `assistant_messages`, `tool_calls`, `tool_results`, `risk_only`). Because the detail-sheet transcript is paginated, the filter bar previously derived its counts from the loaded page — showing e.g. "Showing 150 of 150 entries" on a 19k-message chat, and a risk count that disagreed with the (generation-scoped) risk-only transcript. The dashboard now renders these counts from the server totals. Totals are scoped to the returned generation so they stay consistent with the messages on screen.
- 1ba5adb: feat(dashboard): search within a chat thread. The chat detail sheet gains a find-in-conversation bar backed by full-thread server-side text search (`chat.load` `query` param returns the messages matching the query plus surrounding context, mirroring the risk-windowed view). Jump between matches with the prev/next controls or Enter/Shift+Enter (wrapping at the ends), Escape clears. The active match is highlighted bright yellow and the rest pale — across message text, tool names, and tool argument/output sections — and the tool holding the active match expands, collapsing again as you navigate away.
- 0d23d1f: Add `mcp_server_id` as an optional filter on the observability overview query surface (`getObservabilityOverview`), threaded through the ClickHouse telemetry builders, the Goa payload, and the logs platform tool. A single `mcp_server_id` scopes a fronting MCP server's activity across both remote-backed and toolset-backed sources.
- ef2f5ef: Add an organization-level observability mode that makes generated hook plugins fully non-blocking. When enabled, hooks only observe and report and can never deny or delay a tool call. Defaults off, preserving existing behavior. Toggle it from the organization logging settings.
- 6f3180d: chat.load now paginates a generation's messages by `seq` keyset (`limit`, `before_seq`, `after_seq`) and exposes each message's `seq` plus `has_more_before`/`has_more_after`. A new `risk_only` flag returns just the messages with active risk findings padded with surrounding context, grouped into contiguous `risk_segments` that can be expanded on demand. The chat detail sheet consumes this with a virtualized transcript (`@tanstack/react-virtual`, constant DOM node count regardless of how many pages are loaded) and infinite scroll (scroll up to load older messages, anchored so the viewport doesn't jump), and renders the risk-only view as expandable segments with load-above/below and gap-fill controls.
- 465ac0d: Function deployments now prefer the operator-set `memory_mib_override` / `scale_override` columns over the config-driven memory and scale, and carry those overrides forward across redeploys so they are not reset by a later customer deploy.
- a942a2a: Add a common webhook-trigger abstraction and use it to ship Slack, Linear, and GitHub webhook triggers. A new `HMACScheme` + `WebhookVendor` spec in `triggers/webhook.go` centralizes signature verification (HMAC-SHA256/SHA1, hex/base64, prefix, timestamped templates with replay window) and envelope assembly, so a new webhook source lands as a small vendor file describing its signing scheme, event types, and an ingest function. Slack is rebuilt on the abstraction (no behavior change); Linear (HMAC-SHA256 hex over the bare body, `Linear-Delivery` dedup, comments fold onto their parent issue's conversation) and GitHub (`sha256=`-prefixed hex, `X-GitHub-Delivery` dedup, PR/review/comment correlation onto the PR, pushes onto repo+branch) are added as new triggers. All three share the same default-deny event-type allowlist + CEL filter semantics.

### Patch Changes

- d6d459e: assistants now reap individual stopped runtime VMs once they've been idle for 14 days, instead of waiting for the entire assistant to fall silent for a week. Busy projects no longer accumulate orphaned per-thread Fly machines, and the next event on a dormant thread cold-launches into the same Fly app — keeping its IP and secrets.
- f0b8e05: Assistants now pick up MCP server additions and removals on the next turn instead of only on a fresh runtime bootstrap. The per-turn dispatch sends the current MCP set to the runner, which reconciles its live connections without recycling the VM. Previously a newly attached integration (e.g. GitHub MCP) stayed invisible to the running assistant until the runtime was restarted, leaving the model unable to use it or to invoke `mcp_force_reconnect` for it.
- 23000bc: Isolate Claude Code session identity per `session.id` when an OpenTelemetry Collector or gateway re-batches multiple sessions into one OTLP logs export, so a session is never cached or authorized with another session's `user.email` / `organization.id`.
- 84df8f5: Gram Functions tool calls now size their Fly concurrency limits to real execution capacity (so memory bumps no longer inflate the request cap), return a retryable `429 + Retry-After` when a runner is saturated instead of dropping the connection, and retry tool-call POSTs only on safe pre-response transport errors.
- 2fe346b: Public MCP and OAuth routes now start a fresh server-side trace per request and record the inbound W3C trace context as a span link, instead of adopting the client-supplied `traceparent` as the span parent. This stops third parties from merging unrelated requests into one trace or steering our trace ids, and drops client-supplied `baggage` on those routes before it reaches handlers. The trusted `/rpc` and `/admin` surfaces keep end-to-end parent-child trace continuity and their inbound baggage unchanged.
- b0002bc: The Challenge UI now suppresses challenges raised by users outside the organization. Previously, when a Speakeasy staff member impersonated a customer org their authz decisions appeared as challenge entries — and because internal users switch accounts frequently, these entries repeatedly cluttered the list. `access.listChallenges` and `access.listChallengeBuckets` now only return challenges whose principal is an active member of the organization or has no Gram user identity (e.g. API keys and external end-users); challenges from Gram users who are not members of the org are filtered out in ClickHouse so counts and pagination stay correct.
- d9604a2: fix(assistants): stop a single bad assistant turn from tearing down and recreating its runtime forever. Errors returned by a live runtime are now treated as terminal (and capped) instead of being mistaken for a dead machine, and a hard ceiling fails an event after repeated teardowns so a stuck event can no longer churn machines indefinitely.
- 3955c10: Better performance on tool logs page
- b968804: Exclude tools lists from registry list view to lean out the response size and make the catalog experience more reliable in flake-y network conditions
- 44acd27: Deleting a chat that backs an active assistant is now blocked and returns a conflict. Previously the chat could be soft-deleted out from under a running assistant, which broke the assistant's ability to load its conversation and could leave it silently wedged.
- e0da996: A chat that backs an active assistant now clears its soft-deleted state automatically when it receives another message, so an assistant whose chat was deleted out from under it recovers instead of staying wedged. Chats with no active assistant are left deleted, so this never resurrects a chat a user intentionally deleted.
- 081259c: Costs and session views now show a correct total token count for AI-coding sessions (Claude Code, etc.). These providers report input and output tokens but never emit `gen_ai.usage.total_tokens`, which previously made per-session and per-user totals read "0 tokens". The telemetry queries now derive the total from input + output when the provider omits an explicit total, while sessions that do carry one are unchanged.
- 9da601f: fix(assistants): stop assistant threads from getting stuck when a model response is cut off mid-tool-call. A truncated generation used to be saved with malformed tool-call arguments, which made the thread fail and retry forever (silent assistants, wedged cron digests). Such generations are now dropped at capture while the preceding messages are kept, so the thread stays usable.
- 6453492: fix(hooks): harden hook ingest against transient connection resets. Plugin hook senders now retry a dropped request with backoff instead of blocking the tool call or silently losing the event, and the server de-duplicates redelivered events so a retry is recorded exactly once across all coding assistants.
- 789beea: Improve failure handling and diagnostics for plugin and server-generated hooks.

  - The Cursor hook now fails closed (emits a `deny` with a readable reason) when Gram is unreachable or returns an error, instead of silently allowing the call and bypassing blocking policies. Only a `2xx` is treated as a decision; a `3xx` (e.g. an unfollowed redirect) now fails closed too.
  - Hook success is restricted to `2xx` across the Claude and Cursor hooks (previously `2xx`–`3xx`).
  - The Cursor hook surfaces missing credentials, accepts both `GRAM_HOOKS_*` and legacy `GRAM_API_KEY`/`GRAM_PROJECT_SLUG` env vars, and passes its API key via a mode-`600` curl config file instead of the command line.
  - The Claude hook now explains `mktemp` failures instead of blocking with an empty reason.
  - The MCP inventory payload is sent on stdin (`--data-binary @-`) instead of as a command-line argument, so large inventories no longer risk an `ARG_MAX` failure that silently drops telemetry.
  - The fire-and-forget MCP inventory and identity scripts gain an opt-in `GRAM_HOOKS_DEBUG=1` channel that reports why inventory or user attribution was skipped.

- 365542d: fix(hooks): clearer message when an MCP tool call can't be verified. The deny reason now tells you to restart Claude or run /reload-plugins instead of suggesting the session is still initializing, and includes an error code so you can tell why the call couldn't be verified.
- bb7592f: Add a nullable `match_config` JSONB column to `risk_custom_detection_rules`.
  Detection rules will evaluate this structured condition config instead of the
  single `regex` pattern; `regex` is retained (nullable) as a fallback until a
  later backfill+contract migration. Schema-only.
- 4576472: Rename the internal `mcpname` package to `toolref` and route the Codex hook's
  MCP tool-name attribution through `toolref.AttributeTool` instead of a
  hand-rolled `mcp__<server>__<tool>` split. No behavior change.
- 3ec3917: User sessions enhancements: facet filters (status, client, user, MCP server) on the User Sessions page; a sessions panel on each MCP server's Authentication tab; revoke via right-click and ⋮ menus with brand-themed status badges; and two read-only assistant platform tools (list_user_sessions, get_user_session).
- 3ec3917: Add user sessions feed: enrich the userSessions list API with issuer slug, client name, resolved subject identity, and a status filter; add a filterable User Sessions page (under the org Identity nav group) with revoke.

## 0.72.0

### Minor Changes

- 1cd0ff9: Add an organization administrator "Refresh now" action for remote sessions. The
  `organizationRemoteSessionIssuers` management service gains a `refreshSession`
  method that forces an upstream `grant_type=refresh_token` exchange on a single
  session regardless of its current access-token expiry, persists the rotated
  tokens, and returns the updated session. The shared refresh code path is now
  used by both the lazy MCP token-resolution path and this explicit admin action;
  the upstream token POST runs outside any database transaction. The
  `RemoteSession` type exposes a `has_refresh_token` flag (the encrypted token
  itself stays unexposed) so the dashboard Sessions tab can offer "Refresh now"
  only for sessions that can actually be refreshed. Operator-actionable refresh
  failures (an upstream rejection of the refresh token, an unreadable stored
  token, a missing token endpoint) surface as a bad-request with a clear "Unable
  to refresh: ..." reason and each refresh is recorded as a
  `remote-session:refresh` audit event.
- 442d05c: Codex sessions now report the user's configured MCP servers to Gram on session start, giving shadow MCP servers the same observability as Gram-managed ones and letting access approvals scope to the server URL.
- 7c8677b: Record `mcp_server_id` across `/mcp` runtime telemetry so MCP server activity can be sliced from either the remote or the fronting-server perspective.
- 596af3f: Add `telemetry.listSessions`, an org-scoped endpoint for listing cost-bearing chat sessions filtered by the same dimensions as `telemetry.query`.

### Patch Changes

- 783b5cc: Resolve multiple remote-session authorizations per user session issuer at the
  MCP runtime, keyed by remote session issuer, and enforce at most one client per
  (user session issuer, remote session issuer) at attach time. The runtime
  resolves a per-issuer token map and re-auths when any attached remote session
  is missing or invalid; an application-level attach guard plus a runtime
  invariant replace the database one_per_issuer index. Issuer-gated dispatch
  fails closed when it cannot route among multiple upstream tokens.

## 0.71.0

### Minor Changes

- 4b2f64c: Allow defining audiences when configuring policies.
- ec6d14c: Add an organization administrator UI for managing Remote Identity Providers
  (remote session issuers), their clients, and sessions across the organization.
  The `organizationRemoteSessionIssuers` management service gains an org-scoped
  admin surface: a combined listing of organizational and project-specific issuers
  with client counts and project names, drill-downs into each issuer's clients
  (with MCP server attachment counts), each client's attached MCP servers and
  sessions, authoritative delete pre-flight summaries, and write operations to
  update or delete issuers and clients, detach a client from an MCP server, revoke
  a single session, and revoke all of a client's sessions. Reads require `org:read`
  and writes require `org:admin`; destructive actions are audited, with a bulk
  revoke-all recorded as a single audit event.
- e594e20: Add a step to user session migrations that port existing client registrations from oauth proxy to user sessions

### Patch Changes

- 7c010e9: The Codex observability plugin install script now works on machines where the `codex` CLI is not on PATH: it probes well-known install locations, including the Codex desktop app bundle, before falling back to manual instructions. It also writes feature flags inside the `[features]` table instead of as root-level dotted keys, fixing a "duplicate key" config error on machines whose `config.toml` already has a `[features]` table, and cleans up dotted keys left behind by earlier versions of the script.
- 3b32954: Codex sessions now record the final assistant message at end of turn, matching Claude Code behavior.
- bcda11d: Upgrade the default assistant model to Claude Opus 4.7. The platform-managed Project Assistant, the assistant onboarding flow, and the onboarding system prompt's default recommendation now use `anthropic/claude-opus-4.7` instead of `anthropic/claude-sonnet-4.6`. Existing assistants are unaffected; only newly created assistants pick up the new default.
- b6aafce: increase graceful-shutdown drain window to 60s
- 2135280: MCP tool calls that return a JSON object now also include `structuredContent`, so clients can consume a parsed object instead of re-parsing the text result.
- 5ea8559: Fix the per-tool `mcp:connect` RBAC checks in the remote MCP proxy to use the `mcp_servers` id instead of the `remote_mcp_servers` id, so they resolve grants against the same resource as the server-level check and the toolset path.
- 0710154: Slack-connected assistants now decide whether a reply adds value before posting: ambient thread messages can be answered with silence, while @-mentions always get a reply. The `platform_slack_set_thread_status` tool accepts an empty status to clear the thread's loading indicator on silent turns.
- 32c4165: Unify Tool Logs across hosted MCP servers, shadow MCP servers, local tools, and skills.

## 0.70.2

### Patch Changes

- b8128f3: demote trigger webhook auth failures to warning

## 0.70.1

### Patch Changes

- f18da55: fix(slack): suppress the ingress "thinking" indicator for ambient events. Plain channel messages, reactions, and other passive Slack events that may end in a silent turn no longer light up the loading indicator, which previously stranded it until Slack's two-minute timeout. Only events the assistant always replies to (@-mentions, DMs, Block Kit interactions) show the indicator.

## 0.70.0

### Minor Changes

- 0d51b12: Assistant tool-call audit events no longer appear in the platform audit logs feed or its facets. They are surfaced instead on a new "Audit log" tab on the Assistants page, filterable by assistant, backed by new `subject_type` / `subject_id` filters on `auditlogs.list`.

### Patch Changes

- 0d51b12: Record an audit trail entry (assistant, thread, tool, scrubbed params) for every tool call made by an assistant runtime, covering both MCP toolset calls and platform toolset calls.

## 0.69.0

### Minor Changes

- 774367b: Assistant runtime VMs are now rolled onto new runtime images right after a deploy, while they sit idle, so the next conversation turn no longer pays the image upgrade cost.
- 6945807: Scheduled assistants now summarize their conversation history after every run, so long-lived schedules no longer accumulate unbounded context that slowed responses and risked hitting model limits. Interactive assistant threads (Slack, dashboard) also compact their history earlier, keeping long conversations responsive.
- 3dfffb6: Assistants now boot their runtime as soon as they are created, so the first message no longer pays the cold-start wait.
- 80b95db: Add risk exclusions: suppress false-positive risk findings by exact value, regex, rule_id, source, or presidio entity type, scoped per-policy or globally. Exclusions are applied going forward by the scanner and retroactively by a Temporal reconcile sweep that flags matching rows in `risk_results` (no presidio re-run); removing an exclusion restores the findings. Exposes `risk.exclusions.{list,create,update,delete}` on the management API.
- 430deac: Add tokens under management (TUM) billing for enterprise organizations. The billing page now shows enterprise orgs their TUM consumption for the active billing cycle against the contracted monthly allowance, replacing the self-serve usage meters. TUM counts token usage only from agent sessions Gram has stored non-metrics data for (chats, tool calls), excluding OTEL-forwarded token metrics from uninstalled users. Platform admins get an admin-only section on the billing page to set the contracted monthly token limit, an alert email (alerting to follow), and the billing cycle anchor day, backed by the new `usage.getTokensUnderManagement` and `usage.setBillingMetadata` endpoints and a `billing_metadata` table. Contract changes emit `audit_log.billing_metadata_event_v1` audit events.
- 430deac: Tokens under management is now computed from the new `chat_token_summaries` ClickHouse aggregate instead of raw `telemetry_logs`. The summary table buckets token usage and stored-session evidence per chat per UTC day and is retained for 2 years, so TUM remains accurate across full billing cycles and historical cycles stay computable after the 30-day raw telemetry TTL expires. A backfill script captures the raw data still within the TTL window.
- 430deac: The tokens under management endpoint now returns usage history: the trailing 12 billing cycles, each with a per-UTC-day breakdown. Chat qualification is evaluated per cycle, so daily points sum exactly to each cycle's TUM. The enterprise billing page renders this as a bar chart with day and billing-cycle granularity toggles, including a contracted-limit line in the cycle view.
- 0c7373d: Added unified Tools insights for hosted MCP servers, shadow MCP servers, local tools, and skills.

### Patch Changes

- 7ed5260: Return every published-project plugin to all org members from `agent.getPlugins`.

  The endpoint previously returned only plugins assigned to the caller's exact
  email or the org wildcard, so assignments via `role:`/`user:` principals never
  reached a device — and there is no UI to create assignments yet. As an interim
  step pending RBAC-backed assignment management, the per-principal assignment
  filter (and the `@principal_urns` query param) is dropped: every non-deleted
  plugin in the org's published projects is now returned to every org member.

  The supplied email is still validated so the request contract is unchanged, and
  the view's existing collapse handling keeps colliding-name and cross-org
  isolation intact. No schema change.

- 5294a58: Give each published project its own device-agent marketplace instead of
  collapsing an org to one.

  Previously `agent.getPlugins` derived the marketplace name from the org alone, so
  every project in an org computed the same name and all but one were dropped — and
  which one survived depended on alphabetical project-slug order, so a multi-project
  org could receive the wrong project's marketplace (its observability hooks then
  reporting to the wrong project). The view also ignored the per-project name
  override entirely.

  Marketplace names are now project-scoped: the org's default project (its oldest,
  by id ASC) keeps the bare `<org>-speakeasy` name it always had, and every other
  project gets `<org>-<project>-speakeasy`. The agent resolves each name exactly the
  way the publish path does — per-project override if set, else this default — so a
  device now receives every marketplace the org has published, each pointing at its
  own project. Names that still genuinely collide (e.g. two equal overrides) collapse
  deterministically to the default project.

  No schema change. Single-project orgs and every org's default project keep their
  existing name, so their installs don't churn; only non-default projects get a new
  name, and the automated generator rollout republishes them (their content
  fingerprint changes) so the published marketplace.json matches what the agent
  emits.

- 26855c3: Fix project-assistant thread titles all rendering as the assistant's name. New
  threads now get a unique title generated from the conversation's first turn.
- 2e738a7: Attribute message type + destructured tool name to LLM-judge evaluation.

  The judge now receives structured context — the message type (as an actor/role
  label), and for tool calls the destructured MCP server + function — instead of
  one ambiguous text field, so prompt-based policies can target message types,
  actors, and specific MCP servers/functions. Also: the chat-session risk view
  renders the judge rationale (instead of "llm_judge · llm_judge"), shows a
  tooltip when the annotation truncates, and drops the no-op "Create exclusion"
  action for judge findings.

  Hardens the judge against adversarial input: the policy and message are now sent
  as a single structured JSON payload framed as untrusted data, so a hostile body
  can't spoof prompt headings or steer the verdict via embedded instructions;
  oversized bodies are head+tail truncated before the call so a padded payload
  can't blow the model's context window into a fail-open allow; and multi-tool-call
  messages render each call with its own MCP attribution instead of an opaque blob.

- c5da8ff: Fix the prompt-based risk policy feature flag (`gram-prompt-policies`) being
  treated as disabled for orgs that enabled it via a PostHog group. The backend
  now forwards org/project group memberships when evaluating the flag, so
  group-targeted releases match server-side the same way they do in the
  dashboard — unblocking policy create/update and enforcement.
- d857151: Open prompt-based ("LLM-judge") risk policies to all message types.

  Previously the judge was hard-scoped to `tool_request` in both the realtime
  scanner and the batch analyzer, regardless of the policy's `message_types`. The
  judge now runs on whatever types a policy declares (`user_message`,
  `tool_request`, `tool_response`, `assistant_message`), and the policy form lets
  you choose them instead of locking to tool requests.

- 685c90a: Assistant runtime machines on Fly.io now retain access to private-network DNS, so traces export reliably to the OpenTelemetry collector.
- 91c6568: Fix shadow MCP access requests failing with a 403 ("different requester") when the request link was minted for an agent-reported identity that differs from the authenticated dashboard user (multi-domain orgs, duplicate accounts, or a shared block link). `access.shadowMcp.requests.create` no longer gates on the token's requester; org-match and project-membership checks remain, and approval stays org-admin gated.
- 9723f90: Replace the Slack assistant's rotating loading indicator with honest, single-phrase status.

  The thread indicator no longer cycles through a fake "Routing… → Calling tools… →
  Composing…" pipeline. On ingress it shows just "Routing…", and once the assistant is
  running it reports what it's actually doing through the set-thread-status tool — one
  phrase at a time, updated as the work progresses. The tool now also instructs the
  model to phrase the status mid-sentence (Slack renders it after the app's name) and
  pins the indicator to the status text when no loading message is given, instead of
  letting Slack rotate its own generic defaults.

## 0.68.0

### Minor Changes

- c409a84: Assistant runtimes can now export agent traces (turns, tool calls) over OTLP
  to any OpenTelemetry-compatible backend such as Sentry, Datadog, or Honeycomb.
  Export is enabled by configuring an OTLP endpoint for assistant runtimes, with
  gRPC and HTTP transports supported; traces are tagged with the assistant and
  project they belong to.
- bedfe84: Add backend risk policy bypass request workflow support for the risk-owned request URL flow, backed by current-state request records and principal grants.
- 1dda609: Add prompt-based (LLM-judge) risk policies. Risk policies gain a `policy_type` discriminator (`standard` | `prompt_based`) plus `prompt` and `model_config` fields. A new `llm_judge` evaluation is wired into the realtime enforcement scanner (scoped to tool-call messages) and the batch analyzer, with findings flowing into `risk_results`. The feature is gated behind the `gram-prompt-policies` flag.
- cc9d8ee: Add optional `name` (display name) and `logo_asset_id` to remote session issuers across both the project-scoped (`remoteSessionIssuers`) and organization-scoped (`organizationRemoteSessionIssuers`) services. On create, `name` is trimmed and stored as NULL when empty; on update it follows the same three-state semantics as the nullable endpoint fields (omitted keeps, empty string clears). `logo_asset_id` is set-only for now (no clear path, no upload UI yet). The dashboard renders the display name as the primary label with the issuer URL as the secondary line, exposes an optional Display name input on the attach and modify sheets, and renders a logo when one is present. On the attach sheet the Display name auto-derives from the Issuer URL hostname until the operator edits it, matching the existing Slug behavior.

### Patch Changes

- 06b1f0d: Add generic access webhook event names for audit logs. Shadow MCP approval requests now emit `audit_log.access_request_event_v1`, and access rules emit `audit_log.access_rule_event_v1`; the previous Shadow MCP-specific event names remain in the webhook catalog with deprecated descriptions for compatibility.
- ba8bdd4: Direct assistant MCP authentication prompts to the assistant's owner instead
  of whoever happened to trigger the assistant. Slack onboarding now records the
  owner's Slack identity in the assistant's instructions, runtime guidance
  delivers OAuth links to the owner (ephemeral or DM) and tells anyone else that
  the owner has to complete the connection, and prompts shown when the owner is
  unknown now say explicitly that authentication is for the owner — so an
  unexpected auth message is no longer mistaken for a failed setup.
- 9d59f83: Assistants now connect to all of their MCP servers in parallel when a thread
  starts, so startup time no longer grows with the number of servers and one
  slow or unreachable server cannot stall the rest. Connection attempts are
  bounded by connect and handshake timeouts, so a hung server fails fast instead
  of blocking the assistant.
- 9a78d97: Default the device-agent command list in the generated observability plugin
  `identity.sh` to `speakeasyd`, the binary the daemon actually ships as. The
  previous default (`device-agent,speakeasy-device-agent`) never resolved on a
  standard install, so identity enrichment was skipped and hook events reached
  Gram anonymously (no `user_email`). The fix applies to the Claude Code, Cursor,
  and Codex plugin templates. Installs that still use a differently-named binary
  can override via `GRAM_DEVICE_AGENT_COMMANDS`.

## 0.67.0

### Minor Changes

- 489f7fe: Support publishing Remote MCP-backed `mcp_servers` to collections alongside toolset-backed servers. `collections.attachServer` / `collections.detachServer` accept either `toolset_id` or `mcp_server_id` (exactly one), `collections.create` accepts `mcp_server_ids` in addition to `toolset_ids`, `collections.listServers` returns both backends merged by publish time, and `ExternalMCPServer` exposes `mcp_server_id`. In the dashboard, the Publishing section, the create-collection form, and the collection detail edit-servers picker all offer Remote MCP-backed servers, and the Remote MCP server settings page gains a Publishing section.
- ee1c922: Remove the `value_hash` field from environment entries. It was documented as a way to identify matching values across environments, but every code path computed it from the already-redacted display value (`val[:3] + "*****"`), so it collided for any two values sharing a 3-character prefix and never reliably identified matching values. The only dashboard consumer grouped by it, and because colliding values also render identical redacted strings, the grouping was never observable. Replaced the dashboard's value-hash grouping with direct per-environment value tracking and dropped the field from the API surface.

### Patch Changes

- de92585: Order and filter agent sessions by their latest persisted chat message instead of original session creation time, and show that activity time in the dashboard sessions list.
- c6eb5e8: Stop logging client cancellations (`context.Canceled`) as 500 server faults. When an HTTP client disconnects mid-request, `oops` now detects the cancellation at the error boundary, logs it at info level (no error log, no errored span, no exception event), and maps it to HTTP 499 instead of a 500 fault. Detection requires both a `context.Canceled` cause and a canceled request context, so server-initiated cancellations (e.g. graceful shutdown) and application-initiated cancellations (e.g. an `errgroup` or an explicitly cancelled derived context, whose parent request context is still live), along with `context.DeadlineExceeded` and all other errors, keep full error severity.
- ca3dd21: Export OTel metrics as delta temporality for Datadog. The exporter previously defaulted to cumulative temporality, which forced the per-node Datadog Agent to do a stateful cumulative-to-delta conversion that corrupted counter values in our horizontally scaled deployment. Counters now emit delta at the SDK (UpDownCounters stay cumulative), making each pod self-contained and the Agent a pass-through.
- cfd120a: Removed the deprecated standalone Slack app feature. The dedicated Slack app pages, their backend endpoints, and the associated event-handling workflow have been retired. Slack continues to work through assistants and triggers, which is the supported path.
- 5ba126c: Slack-triggered assistants now show a native "is thinking…" loading indicator on the thread the moment a message comes in, so there's immediate feedback during the wait instead of silence. The assistant can update the status as it works, and it clears on its own as soon as the reply lands.
- c3a7c13: Disable "Give Access" button while challenge resolution is pending.

## 0.66.0

### Minor Changes

- ba4f20c: Add backend risk policy bypass request workflow support for the risk-owned request URL flow, backed by current-state request records and principal grants.
- 77715a2: Grant the project's managed assistant (the dashboard Project Assistant) the full observability and AI Insights tool catalog the old client-side copilot had. It can now search and inspect activity (`search_logs`, `search_tool_calls`, `search_chats`, `search_users`), pull project- and user-level metrics and overviews (`get_project_metrics_summary`, `get_user_metrics_summary`, `get_observability_overview`, `list_attribute_keys`), list and load chats (`platform_list_chats`, `platform_load_chat`), enumerate the organization's user directory (`platform_list_organization_users`), summarize risk findings without exposing secret content (`platform_list_risk_policies`, `platform_list_risk_results_for_agent`, `platform_list_risk_results_by_chat`, `platform_get_risk_policy_status`), and fetch deployment logs (`platform_get_deployment_logs`). Scoped to the managed assistant's platform toolset, so other assistants are unaffected.
- 5d59ae9: Support adding Remote MCP-backed `mcp_servers` to plugins alongside toolset-backed servers. `plugins.addPluginServer` accepts either `toolset_id` or `mcp_server_id` (exactly one), `PluginServer` exposes `mcp_server_id`, and `display_name` is now optional (defaulting to the backing toolset or mcp_server name). Plugin bundle generation resolves the preferred endpoint for mcp_server-backed servers (custom-domain over platform, then oldest) and emits them as OAuth HTTP servers with no static auth header. In the dashboard, the plugin add-server picker and server cards offer and render Remote MCP-backed servers (gated on the `gram-remote-mcp` feature flag).

### Patch Changes

- edd6834: Give the managed (Project) Assistant temporal grounding by stamping each dashboard turn with its timestamp. `dashboardAdapter.DecodeTurn` now adds a `Timestamp: <RFC3339 UTC>` line to the turn's `<message-context>` envelope, sourced from the event's immutable `created_at`. This restores the relative-time anchoring the old AI Insights sidebar had ("errors since Monday") but does it per-turn and append-only — it rides on the user message instead of the cached system prompt, so it stays fresh across long-lived sessions without busting the prompt cache, and re-decoding on retry/replay is byte-stable.
- 9703d10: Use device-agent identity in generated and checked-in observability hooks when available, while preserving existing hook attribution fallbacks when the daemon is missing or not running.
- 4f289ec: The Project Assistant no longer adds all of a project's MCP servers when it's first set up. A new Project Assistant now starts with only its built-in and platform tools; admins attach the project MCP servers they want it to use.
- 47f6d68: Drop a much larger class of Presidio `IP_ADDRESS` false positives. The filter now consults a unified catalog covering IANA-reserved space (RFC1918, loopback, link-local, multicast, CGNAT, documentation, 6to4 deprecated, class E, benchmarking, this-network, limited broadcast), well-known public DNS resolvers, common placeholder IPs, IPv4 `/8` network addresses and sparse IPv6 shapes, plus a cloud / CDN / managed-hosting bucket resolved against an embedded DB-IP ASN snapshot. On the production sample used to size this change (8,391 events) the new catalog suppresses about 80% of IP findings vs. ~10% under the previous filter.
- fb3f0ca: Strip `<message-context>` source-adapter framing from chat messages before generating thread titles. The framing (EventID/UserID lines, MCP auth events) is needed by the runner for replay but is noise for title generation — left in, the title model fixated on the boilerplate and produced the same generic title for every project-assistant thread.

## 0.65.0

### Minor Changes

- 9565e61: The public `/mcp` handler now supports filtering exposed tools by variation tag via the `?tags=` URL query parameter (comma-separated, OR/union). Tool variation overrides are resolved from the MCP server's or toolset's configured tool variation group, falling back to the project default.
- 69d8cdb: Add read-only tool filtering visibility on the MCP details page Tools tab. New `mcp:read`-scoped `listToolFilters` methods on the `toolsets` and `mcpServers` services resolve the effective tool variations group (`mcp_servers` then `toolsets`) and return the available filter scopes (tags) with their member tools plus the tools excluded from all filters, mirroring the runtime `?tags=` behavior. The dashboard Tools tab renders a scopes panel above the tool list when filtering is enabled, with per-tag tool membership and a tag chip that filters the list below.
- 526bb14: Project Assistant turns sent from the dashboard now run under the sender's user identity instead of the user who first enabled the assistant for the project. MCP tool calls, audit attribution, and any per-user RBAC inside a turn reflect the user who actually sent the message. Non-interactive sources (cron, wake), Slack-sourced turns, and system-initiated MCP-auth resumptions continue to run under the assistant's creator.
- e39ea7e: The dashboard Project Assistant now reads its conversation straight from the chat service instead of a separate mirror. `assistants.sendMessage` takes an optional `chat_id` to continue a conversation (from `chat.list`), or omits it to start a new one — the server mints and returns the chat id. The redundant `assistants.listMessages` endpoint is removed; clients poll `chat.load` for the assistant's replies, which now surface as plain assistant messages.
- cdf7772: Add `POST /rpc/assistants.ensureManagedAssistant`: returns the project's built-in Project Assistant, provisioning it (idempotently) on first access so the dashboard sidebar can resolve it out of the box. Gated by project read access. Also renames the managed assistant to "Project Assistant for {project}" to match the dashboard's "Project Assistant" branding. Foundation for the AGE-2631 sidebar cutover.
- 4feb400: Add the enterprise onboarding wizard at `/<org>/setup` that walks new orgs through five steps end-to-end: SSO setup via WorkOS, directory sync, publishing a private plugin marketplace to GitHub, instrumenting each agent platform (Claude Code, Claude Cowork, OpenAI Codex, Cursor) with the org's marketplace and observability plugin, and confirming traffic is flowing.

  Includes:

  - New `Create Plugin Marketplace` step that wraps the same GitHub publish flow as the Plugins page, with a typeahead-driven GitHub-user picker for collaborator access (replaces the old comma-separated input).
  - `Instrument Agents` step that surfaces per-platform setup instructions with auto-generated API keys, marketplace URL / repo URL / plugin slug substitution, eligibility gating (Claude Teams/Enterprise check), and platform-specific screenshots. Coming-soon entries for GitHub Copilot, Gemini, Glean, and AWS Bedrock are rendered as a half-width muted grid and excluded from the configured/total count.
  - Wizard resume logic backed by `organizations.getOnboardingStatus` and `plugins.getPublishStatus` — reloading lands on the deepest known-incomplete step instead of step 0.
  - `organizations.sendEnterpriseAdminOnboardingEmail` endpoint and a super-admin "Onboarding" tab for dispatching the enterprise-admin invite email (Loops template `cmpqyxnzl00hj0jwtkibhyjdz`), which deep-links recipients into the active org's wizard.
  - `organizations.verifyOnboardingHooksSetup` polling endpoint that surfaces recent hook events from ClickHouse for the `Confirm Traffic` step.
  - Wizard chrome: header with Docs / Get Support (Pylon) / Go to Dashboard buttons, footer with the moonshine ThemeSwitcher, and a project-slug query-string override on the SDK provider so the wizard can hit project-scoped endpoints from org-level routes (falls back to the `default` project when unset).

- 51fadba: Make the generated marketplace name configurable per-project. Adds `plugins.getMarketplaceSettings` and `plugins.updateMarketplaceSettings` on the management API plus a Marketplace settings dialog in the Plugins tab. The default is now `<org-slug>-speakeasy` (previously `<org-slug>-gram`); the org-slug prefix keeps defaults unique across customers so end users installing from two Gram marketplaces don't collide. Saving an override on a project that already has a published marketplace auto-republishes the new manifest to GitHub. References to "Gram" in the generated README, plugin descriptions, and hook scripts are rebranded to "Speakeasy"; URLs, env-var names, and HTTP header names are unchanged.
- 51fadba: Add the `project_marketplace_settings` table to hold per-project marketplace configuration. Schema-only change; the table is consumed in a follow-up PR that exposes a configurable marketplace name on the plugins management API.

### Patch Changes

- 4856d7e: Preserve a configured Authorization header for external MCP passthrough tools instead of overwriting it with the gating OAuth token.
- 938c251: Add the `platform_dashboard_send_message` egress tool so a dashboard assistant can deliver its reply to the conversation log: it resolves the target chat from the assistant principal's thread id and appends an `assistant` row to `assistant_dashboard_messages`. The user's turn is recorded as a `user` row at ingest, atomically with the thread event (idempotent on retry). Assistant-agnostic and keyed by the configurable correlation id. Foundation for AGE-2631.
- 622cc7b: Fix `organizations.getOnboardingStatus` returning 500 in production by switching the WorkOS connection/directory lookups to the official WorkOS Go SDK (`sso.Client`, `directorysync.Client`). The previous raw-HTTP wrapper used the wrong path `/directory_sync/directories` (the correct WorkOS endpoint is `/directories`), which the type system could not catch.
- fe4f5d2: Use a non-empty inviter fallback for organization invite emails when the inviter's stored display name is blank, preventing Loops from rejecting invites that require `inviter_name`.
- 51c6acc: Add safe instrumentation for issuer-gated MCP OAuth registration, token exchange, and revocation flows to improve Datadog debugging of client credential and grant failures.

## 0.64.0

### Minor Changes

- 55a25ac: Add management APIs and dashboard UI for enabling and configuring MCP server tool filtering via tool variation groups.

### Patch Changes

- 8f3591d: resolve /mcp/<slug> OAuth flow handlers via mcp_endpoints with toolset fallback
- a1f25dc: Prepare RBAC grants for issuer-gated private remote MCP servers so `tools/list` and `tools/call` no longer fail for RBAC-enforced callers. Previously the issuer-gated path skipped grant preparation, causing the proxy's `mcp:connect` interceptors to reject the request with a missing-grants error and return zero tools.
- 13551ec: Add the `assistant_dashboard_messages` table — the user-visible conversation log for the AI Insights sidebar (user messages + the assistant's delivered replies), kept separate from the raw `chat_messages` transcript. Keyed by chat with a monotonic `seq` for incremental polling. Foundation for AGE-2631.
- 3011492: Add an endpoint for a dashboard user to send a message to an assistant. The reply is delivered asynchronously — the response returns the chat to poll for it. The caller chooses the conversation thread via a correlation key (send the user id for one continuing thread per user, or a fresh value to start over), and can pass an idempotency key so a retried send doesn't enqueue the message twice.
- 1078e46: Add an optional `user_id` filter to the risk events list. The Risk Events page now exposes a "User contains..." search box that filters findings by the chat's external user id (case-insensitive substring match), alongside the existing policy and rule filters.
- 3eaa1cf: Add `message_types` to risk policies so admins can target enforcement and batch scanning to user messages, tool requests, tool responses, or assistant text.

## 0.63.0

### Minor Changes

- b20bb88: Wire `organization_id` into remote session issuers and expose a new `organizationRemoteSessionIssuers` service to manage organization-level remote session issuers
- 0653bf4: Add `agent.getPlugins` management API method consumed by the Speakeasy device agent. The endpoint accepts an `email` query parameter, resolves plugin assignments for that email plus the `*` wildcard within the caller's org, and returns the published plugins as Claude Code marketplace + plugin references (drops directly into Claude Code's `extraKnownMarketplaces` and `enabledPlugins` settings). Authenticates with an org-scoped API key carrying the new `agent` scope.

  Adds `agent` as a selectable scope on the existing API Keys page so admins can mint these tokens from the same place every other scope is minted.

  Adds `email` as a first-class principal URN type (`urn.PrincipalTypeEmail`) so admins can assign plugins by email address. Existing `user:` and `role:` URNs are unchanged; the wildcard `*` is now exported as `urn.PrincipalWildcard`.

### Patch Changes

- 91e166d: Add an employee data-flow graph endpoint and dashboard visualization for workforce observability.
- 2ca1372: MCP install pages no longer ask for a GRAM API key on private servers whose identity is delegated to a `user_session_issuer` (the newer OAuth scheme). Previously `resolveSecurityMode` only recognized the legacy `oauth_proxy_server_id` / `external_oauth_server_id` fields, so an issuer-gated private server fell through to the Gram-key prompt even though OAuth handles authentication. The check now also honors the `user_session_issuer` on the toolset and on the bridging `mcp_server`, matching the public serve path.
- 827615b: Add managed-assistant provisioning: `EnableManagedAssistant` / `DisableManagedAssistant` / `GetManagedAssistant` toggle a project's platform-managed assistant (AI Insights sidebar). Enabling creates the assistant with the ported Insights prompt and all MCP-reachable project toolsets attached and records the `project_managed_assistants` mapping; disabling tears both down. Idempotent and race-safe. Foundation for AGE-2631.

## 0.62.2

### Patch Changes

- 50cfe28: Remote OAuth client lookups no longer surface clients whose bound user session issuer lives in a different project or has been soft-deleted. The legacy `user_session_issuer_id` fallback path now scopes both the client and its user session issuer to the request's project and excludes soft-deleted clients, remote issuers, and user session issuers — matching the join-table read path. In practice this is a no-op for existing data (no production rows are in that state); it closes the gap going forward.
- 9b8f59a: resolve /mcp well-known OAuth metadata via mcp_endpoints with toolset fallback
- 585578b: Retry chat completions when the upstream model returns an empty response, and report the upstream details when it still fails, reducing transient playground and chat errors.

## 0.62.1

### Patch Changes

- d7c9904: New assistants default to 5 concurrent warm runtimes (was 1) and a 60-second warm TTL (was 300s) so they handle bursts without queueing while letting idle runtimes reclaim resources faster. Existing assistants keep their saved values.
- e8f7b31: Route telemetry-only Codex observability hooks through a shell background wrapper instead of Codex's unsupported async hook flag.
- ce35930: Removed the FreeTierReportingUsageMetrics activity from the CollectPlatformUsageMetricsWorkflow workflow since it is no longer a requirement to report on free tier usage.

## 0.62.0

### Minor Changes

- a00e7aa: serve mcp_endpoints/mcp_servers from /mcp/{slug} with fallback to the legacy toolsets lookup
- 6039fe5: Add `risk.customRules.suggest` endpoint that calls OpenRouter to turn a one-line description ("what do you want to detect?") into a prefilled custom detection rule. The dashboard's New Custom Detection Rule sheet now opens on a single textarea, calls the new endpoint, and lands the operator in the editable review form with the suggested rule_id, title, description, regex, and severity.
- 6039fe5: Add a rule playground: from the Detection Rules detail sheet, the operator pastes a sample into a textarea and the dashboard calls the new `risk.rules.test` endpoint which dispatches to the same scanner code (gitleaks, Presidio, prompt-injection, regex) the worker uses. The response is a list of `TestDetectionRuleMatch`es mirroring the runtime risk_result shape.

  Drop the severity-override UI from the rule detail sheet. The override edit / reset affordances will return in a follow-up PR; default severity continues to render as a row badge for context.

- 05805bb: Add management APIs for Shadow MCP approval requests and access rules.

### Patch Changes

- 7fe4787: Svix app portal now correctly grants full capabilities to org admins and read-only access to non-admin members.
- e60b876: Updated the create portal session endpoint for svix webhooks to request all capabilities for admins explicitly. Previously it was specifying an empty slice of capabilities, which appeared to result in a read only session.
- 72ccf7b: Fixes login journey for allowed orgs
- 1c428e4: Enforce Shadow MCP Access Rules at runtime, allowing approved Access Rule exceptions while preserving existing block policy behavior.

## 0.61.0

### Minor Changes

- 37158f0: ingest tags declared on Gram Function tools (top-level `tags` on the manifest and `tags?: string[]` on the TS framework `ToolDefinition`) and expose them through the management API; the playground tool editor now opens for function tools the same way it does for HTTP tools
- 50ab453: Add SSO and SCIM feature flags with WorkOS event sync. Admin settings now includes product feature toggles for SSO and SCIM. The Identity page shows connection status and gates configure buttons on these flags. Team page invite button is disabled when SSO is active. WorkOS event processing now handles all SSO connection and SCIM directory sync lifecycle events.

### Patch Changes

- 4a65626: Tag the assistant runtime image with a content hash so deploys that don't change the runtime image sources reuse the existing fly machines instead of recycling them on every commit.
- 1871808: Fix the triggers page failing to load whenever a wake trigger has fired or been cancelled. The triggers list response advertised a status enum of `active | paused`, but wake triggers transition through `fired` and `cancelled` too, so the dashboard's response validation rejected the payload and surfaced a generic "Response validation failed" error. The status enum now includes all four states, and the triggers page renders distinct badges for fired and cancelled triggers instead of mislabelling them as "Paused".

## 0.60.0

### Minor Changes

- 95a8f12: add `remoteMcp.discoverProtectedResourceMetadata` endpoint that probes a remote MCP server for an RFC 9728 OAuth Protected Resource Metadata document server-side under `guardian.Policy`, since external resource servers are unlikely to allowlist the Gram dashboard origin via CORS; follows RFC 9728 §3.1 path-style + origin-style discovery and returns typed unavailability codes with backend-composed user messages

### Patch Changes

- 23d2150: expose tags on tool variations and add a tags row to the playground tool editor for HTTP tools, with chip input, base-source quick-add, override indicator, and reset-to-source affordance
- 9afce8d: Derive org IDs as deterministic UUIDv5 from WorkOS org ID during Register and auto-provisioning, replacing the previous `"org_" + random UUID` format which was not a valid UUID.

## 0.59.0

### Minor Changes

- 5f4c259: Add admin API endpoints for managing organizations and OAuth/OIDC configuration, protected by a dedicated admin security middleware. Includes a mock OIDC server for local development and testing.
- 0c431a0: initial MCP resource method interceptors
- 8e247f9: Chat loading is now paginated by generation, returning one generation per request. The chat detail panel fetches older generations in parallel until the full transcript is assembled, so long-running sessions no longer stall on the initial fetch.
- b58bf0f: Adds an org-level AI Integrations product surface with Cursor as the first provider. Organization admins can connect a Cursor Admin API key from org settings, and an hourly Temporal workflow polls Cursor for token and cost usage events and writes them into ClickHouse `telemetry_logs` so the dashboard shows Cursor usage and cost alongside Claude Code data. The dashboard cost copy is updated to reflect Cursor and Claude Code coverage, and the employee detail page now shows cost beside total tokens.
- ed12a35: Add multiple role support to the RBAC system. Users can now be assigned multiple roles simultaneously, replacing the previous single-role assignment model.
- 3b8bfb4: Adds `risk.results.listForAgent` — a redacted variant of `risk.results.list` for AI assistant / MCP consumption. The new endpoint returns the same fields as `listRiskResults` but replaces the `match` field with `match_redacted`, an opaque token of the form `<redacted len=N sha=XXXXXXXX>` where `N` is the byte length and `XXXXXXXX` is the first 8 hex characters of `sha256(match)`. Identical secrets produce identical fingerprints so agents can dedupe leak counts without ever seeing secret content.

  `shadow_mcp` findings pass `match` through verbatim because the value is a server URL or stdio command identifier (already shown unmasked in the dashboard), and exact byte positions are coarsened to a single `position_known` boolean to remove reconstruction signals.

  The dashboard's AI Insights sidebar gains risk-aware suggestions on the Security Overview and Policy Center pages, plus a system-prompt rule that bars the assistant from echoing `match_redacted` values verbatim.

### Patch Changes

- 9d6ba7b: `/rpc/telemetry.getObservabilityOverview` now accepts an optional `remote_mcp_server_id` filter so callers can scope summary, time-series, and per-tool breakdown metrics to a single Remote MCP source. Combinable with the existing `toolset_slug` filter.
- 9d6ba7b: `/x/mcp` tools/call traffic now writes a structured row to ClickHouse `telemetry_logs` per invocation, mirroring the existing `/mcp` emit. The row carries `gram.remote_mcp_server.id` and `gram.tool.name` attributes so the Source Activity panel for a Remote MCP source can filter telemetry by the originating remote server. Emission is fire-and-forget so ClickHouse latency does not appear in tool-call tail latency.
- fae81e1: Public-MCP `/authorize` accepts a new `requireUserIdentity=1` query parameter that forces the caller through the IDP so the resulting session is bound to a user subject rather than an anonymous one. Without the parameter, public-toolset `/authorize` continues to mint an anonymous subject regardless of ambient cookies or Bearer tokens. Callers from outside the endpoint's organization receive a 403 from the IDP callback — public toolsets that need cross-organization access should omit the parameter and use anonymous sessions.

  The assistant runtime sets the parameter when initiating MCP authorization flows against Gram-served endpoints so subsequent tool calls can be attributed to the user. Foreign (non-Gram) authorization endpoints discovered via `.well-known/oauth-authorization-server` do not receive the parameter.

- d4ab97a: Assistants are now instructed to treat OAuth/MCP authentication as owner-only and to avoid pre-emptively prompting for auth on toolsets they have not yet needed.
- 508aef1: Always emit the `result` field in JSON-RPC success responses from the MCP server. Empty-result handlers (notably `ping`) previously sent `{"jsonrpc":"2.0","id":N}`, which violates JSON-RPC 2.0 and the MCP spec. Cursor's MCP SDK rejected those frames with `invalid_union` zod errors and dropped the transport to a failed state after each keep-alive ping.
- 20706f4: Make the assistant-runtime reaper resilient to Fly Machines API calls that hang on missing machines. Each Destroy/List call is now bounded by its own timeout, and the Temporal janitor activity uses a heartbeat for liveness rather than relying on a short overall timeout that turned tombstone-machine hangs into elevated workflow-failure alerts.

## 0.58.0

### Minor Changes

- d755880: Assistants spec panel now has a "Sessions" quick link that opens Agent Sessions filtered to that assistant.

## 0.57.0

### Minor Changes

- 3db9f30: Deleting a custom domain now soft-deletes every `mcp_endpoints` row registered under it across all projects in the org, emits one `mcp-endpoint:delete` audit event per cascaded row, and the dashboard delete-confirmation modal previews the impacted endpoints via the new `/rpc/domain.listMcpEndpoints` endpoint.
- 3531836: Add a nullable `audience` column to `remote_session_clients` and surface it on the remoteSessionClients management API. When set, the upstream OAuth dance attaches the `audience` parameter to the authorize redirect, the authorization-code → token exchange, and every refresh-token request; when unset the parameter is omitted entirely.
- 3531836: Add a nullable `scope` column to `remote_session_clients` and surface it on the remoteSessionClients management API. When set, the upstream OAuth dance requests these scopes instead of echoing the issuer's full `scopes_supported`, which avoids over-granting Gram access on providers that advertise broad scope sets.
- 3452d17: Cron triggers now accept an optional `note` field, matching wake triggers. The note is included in every scheduled tick the assistant sees, letting one assistant carry multiple cron triggers with distinct per-schedule steering (e.g. "run daily digest" vs "check deploy status").
- 12a0fa3: Add risk overview summary metrics, charts, and trend data for recent policy findings.

### Patch Changes

- 4f00967: Fix token graph blanking when filtering by agent type on /insights/costs. Claude Code usage metrics were missing the hook_source attribute, causing the filter to return no data for non-cursor agents.
- 12a0fa3: Add risk overview summary metrics, charts, and trend data for recent policy findings
- 35a7938: Improved server names in hooks logs. Improved UI for inspecting indiivudal logs
- bf85fad: Slack-triggered assistant chats now open a fresh assistant thread for each top-level message instead of folding distinct conversations onto a single per-channel thread. Top-level Slack messages and DMs used to share one Gram thread (and one Fly runtime) per channel, so unrelated users' messages bled into the same context window.
- 99d3d7f: Assistants on Slack now surface MCP OAuth re-auth via an ephemeral Block Kit button instead of dumping the raw URL into the thread, so only the user that needs to authenticate sees the prompt.

## 0.56.0

### Minor Changes

- 978d13f: Integrate `/x/mcp` with `mcp_servers.user_session_issuer_id`. The `mcpServers.create` and `mcpServers.update` management endpoints now accept an optional `user_session_issuer_id`, and `McpServer` carries it on read. When set on an `mcp_server`, `/x/mcp` requests are issuer-gated: callers without a valid Authorization receive 401 + `WWW-Authenticate` pointing at `/.well-known/oauth-protected-resource/x/mcp/{slug}`, and the full OAuth surface — dynamic client registration, authorize, IDP callback, consent, token, revoke — is mounted under `/x/mcp/{slug}/...` against the same JWT machinery `/mcp` uses, with audience bound to `urn.NewUserSessionIssuer(...)` so tokens stay portable across toolset-backed and remote-backed servers under the same issuer. Both well-known metadata routes under `/x/mcp` now return the issuer-gated metadata shape for any addressed `mcp_server` with an issuer set, including remote-backed servers (previously 404). The `/oauth/proxy-register` DCR helper now also registers `<server>/x/mcp/remote_login_callback` so remote-OAuth `mcp_servers` reached via `/x/mcp/{slug}/connect` can complete the upstream callback against the same upstream client registration.
- 9aa2fed: Assistants can now authenticate with OAuth-protected MCP servers. When a configured MCP server requires user authentication, the assistant relays the authorization link through an available output tool; once the user completes authentication, the assistant reconnects and continues its task.
- 0ef489c: Slack assistants can now manage the full message and channel lifecycle: edit, delete, and ephemeral messages; pull permalinks; open DMs; create, join, leave, invite, archive, and rename channels; manage pins, bookmarks, usergroup membership, reminders, file uploads, canvases, and presence/DND. Closes the previous gap where assistants could read Slack but barely write to it.

### Patch Changes

- 4f16ea3: Chat completions no longer generate hidden reasoning tokens. Previously, OpenRouter could route requests through models that produced reasoning output Gram discarded before storage — yet still billed. The proxy and every internal completion caller (chat title generation, Slack agent loop, risk policy naming, structured object completion) now explicitly disable reasoning, eliminating that silent cost without changing observed behavior.
- 11d0b70: Anthropic prompt caching now actually takes effect for assistant chats. The `/chat/completions` proxy used to strip `cache_control` markers off the request body before forwarding to OpenRouter, so every Anthropic call billed at the full input rate. The proxy now preserves the markers at the top level, on tool definitions, and on message content blocks, so Claude requests with stable prefixes can serve from cache.
- 5746c4e: Assistants can now update their own triggers. Previously, calling `configure_trigger` on an existing trigger returned a generic internal error every time, even though the assistant could read its triggers fine — its scoped tool was being silently swapped for a stricter variant that demanded fields the assistant isn't allowed to send. As a side effect, an assistant's trigger list no longer leaks sibling assistants' triggers in the same project.
- 4e1be24: Outbound OpenRouter chat completions now carry a session ID, user, source metadata, and distributed-trace identifiers so OpenRouter's dashboard can group requests per conversation and roll up cost per customer, and so Datadog traces correlate with OpenRouter's request records.
- 31bafa1: Deprecated obsolete outbox event types and explicitly adds versioning in the name scheme of events. In particular, `risk_finding.created` is replaced by `risk_finding.created_v1`.

## 0.55.1

### Patch Changes

- cb50037: Allow client_secret_post as an optional auth method in remote session negotiation

## 0.55.0

### Minor Changes

- ecdd727: support remote mcp interceptor payload mutation; implement shadowmcp and mcp:connect interceptors
- a8cf1e0: Emit audit log entries for collection mutations: `mcp_collection:create`, `:update`, `:delete`, `:attach_server`, and `:detach_server`. Update/AttachServer/DetachServer now run in a transaction alongside the audit insert, and a new `urn.McpCollection` identifier (prefix `mcp_collection`) is used as the audit subject.
- 4ea14f3: Enforce RBAC on the collections API. `List` and `ListServers` now require `org:read`; `Create`, `Update`, `Delete`, `AttachServer`, and `DetachServer` require `org:admin`. The dashboard's sidebar, collections list, and detail pages open up to `org:read` members, while create/edit/delete and server attach/detach controls stay behind `org:admin`.
- 5dcb8aa: `RiskResult.rule_id` and `RiskResult.description` now follow a consistent shape across every detection source.

  `rule_id` is lowercase, snake_case, with an optional dot-separated category prefix:

  - `secret.<rule>` for credentials and secrets (e.g. `secret.anthropic_api_key`)
  - `pii.<rule>` for personal, financial, and medical data (e.g. `pii.credit_card`, `pii.medical_license`)
  - `shadow_mcp` for unverified MCP tool calls
  - `destructive.tool` for MCP tool calls flagged as destructive
  - `destructive.<category>.<name>` for destructive shell, git, database, and cloud commands (e.g. `destructive.shell.rm_rf`, `destructive.git.push_force`)
  - `prompt_injection` for prompt injection findings

  `(source, rule_id)` is the stable identifier downstream consumers should match on. The dotted prefix alone is enough to bucket findings by risk category.

  `description` is a short human-readable sentence describing the finding. It never echoes the matched value and is safe to display verbatim.

  Historical rows written before this release keep their original `rule_id` and `description` values; a follow-up migration will rewrite them.

- 4eadd44: Show assigned roles on pending organization invites and allow org admins to change the role before acceptance. Invite creation and invite role changes now emit audit log entries.
- 95e1458: The webhooks feature now generates a catalog of event types and schemas for them. This is emitted as an OpenAPI 3.1 document that is synced to svix.
- 376a74b: Added granular webhook event types for audit log entries — each auditable subject (deployments, projects, MCP servers, API keys, toolsets, risk policies, sessions, and more) now emits its own typed webhook event (e.g. audit_log.deployment_event_v1), enabling subscribers to filter by subject domain rather than receiving all audit activity under a single event type.

### Patch Changes

- bede6e6: Exclude per-request plugin download API key creation from the audit log to prevent flooding with `api_key:create` events.
- 4aceb60: skip WorkOS reads when org already linked locally
- 4eadd44: Invite acceptance now uses Gram invite tokens plus WorkOS User Management Magic Auth codes.
  The server validates the invite token, creates and consumes the Magic Auth code for the invited email, verifies the email match, and completes provisioning.
- 1562656: Drop Presidio IP_ADDRESS false positives produced from short-form IPv6 strings (`b::`, `dead::`, `1::`, …) and IPv4 unspecified `0.0.0.0`. Analysis of prod risk_results showed these single-hex-group `<hex>::` matches dominated IP_ADDRESS noise alongside the existing `::` filter; they're now dropped before becoming findings.

## 0.54.0

### Minor Changes

- 0f52a3e: The playground's Connect button now drives the issuer-gated OAuth flow when a toolset is bound to a user-session issuer, so connecting to MCP servers like `speakeasy-team-github` lands an upstream session that the runtime can resolve. The connection-status badge and the 401 challenge on `/mcp/{slug}` both read from the issuer-gated session store for these toolsets, and the security-check fallback now always emits a non-empty `resource_metadata` URL.

### Patch Changes

- e40ac39: Assistant runtimes no longer get stuck unresponsive after a Gram release. When the assistant runtime image was upgraded in place, the underlying VM was being left stopped, so the next chat turn timed out and the assistant stopped responding. Subsequent turns now bring the runtime back up cleanly.
- 9ee283c: Issuer-gated MCP servers now accept an assistant-runtime JWT and use the assistant owner's linked upstream account, so the runtime can call `/mcp/{slug}` without re-prompting for login. Requests with no linked upstream still return a 401 + WWW-Authenticate as before.
- 48779ef: Fixed a bug where snapshot and metadata fields in audit log outbox entries were being base64-encoded instead of preserved as inline JSON objects.

## 0.53.0

### Minor Changes

- bdb246a: monitor OpenRouter credits usage for enterprise organizations
- 73f273e: auto-reconcile OpenRouter per-key credit limits via metrics workflow
- 21dd9c7: Lay the groundwork for the v2 assistant runtime path: optional `ThreadID` claim on assistant runtime tokens (assistant-scoped tokens omit it), a `runtime_version` column plus partial unique index on `assistant_runtimes`, a new `/rpc/assistants.getThreadBootstrap` endpoint that lets a runner pull a thread's bootstrap state on demand, and an assistant-scope check on `/chat/completions` that rejects writes whose `Gram-Chat-ID` resolves to a chat outside the caller's assistant. Existing v1 admit, configure, and run-turn flows are unchanged.

### Patch Changes

- 733bf43: Allow tool URNs to use MCP-valid tool names, including camelCase, PascalCase, dotted, and kebab-case names.

## 0.52.1

### Patch Changes

- e129f0a: Assistant platform toolsets are now served from `/platform/mcp/{slug}` instead of `/x/platform-mcp/{slug}`, in line with `/mcp` prefix for MCP servers.
- 89588d7: dedupe chat asset writes and idempotently upload to prevent GCS 429s
- 5f00991: Make hook routes (Claude / Cursor / Codex / OTEL Logs / OTEL Metrics) filterable in Datadog by `gram.org.id`, `gram.project.id`, `gram.hook.source`, and `gram.hook.event`. Replace nested `value` payloads with top-level slog attrs attached via `slog.With`, and log on every early-return path — including unauthorized requests and missing-session-id branches — so a silent 401 or no-session request is still visible when debugging hook setup for a given org/project.
- 1240c7a: fix: get stop hook working in cowork again

## 0.52.0

### Minor Changes

- 512a432: assistants now self-heal when the inference provider rejects a chat as malformed: the runtime trims history to the last 5 user messages, prepends a recovery notice that nudges the agent to recover lost context via its tools, and retries — instead of leaving the thread stuck.
- 6cf658b: Every assistant now exposes a platform toolset to its runtime alongside its user-attached toolsets, with no user-facing toolset row and no setup required. Removes the `assistant_memory` product feature flag in the process: `GET /rpc/productFeatures.get` no longer returns `assistant_memory_enabled`, and `POST /rpc/productFeatures.set` no longer accepts `"assistant_memory"` as a `feature_name` — the assistant memory tools are always-on.
- 707bc98: Outbound Slack messages can now render rich Block Kit content. `chat.postMessage` and `chat.postEphemeral` accept an optional typed `Blocks` field (section, actions+button, context, divider) alongside the existing text fallback. Button clicks come back as `block_actions` interactions on the existing Slack trigger webhook, are correlated to the originating thread, and reach the assistant as a new turn carrying `action_id`, `action_value`, and `block_id` — so assistants can present options and receive the user's choice in the same conversation.
- fa5ef43: Add Codex (OpenAI) hooks support. A new `/rpc/hooks.codex` endpoint accepts all six Codex hook events (SessionStart, PreToolUse, PermissionRequest, PostToolUse, UserPromptSubmit, Stop), enforces org-level risk policies on blocking events, and records telemetry to ClickHouse. The plugin generator now produces a downloadable Codex observability plugin (ZIP and install script) that registers the hooks with a Gram marketplace entry in `~/.codex/config.toml`. The install instructions dialog gains a Codex tab alongside Claude Code and Cursor.
- eb65287: Remove the legacy Speakeasy IDP authentication layer and migrate to WorkOS-native auth. Authorization, token exchange, and session management now go directly through the WorkOS SDK instead of the intermediate Speakeasy IDP proxy. Deterministic UUIDv5 user/org IDs bridge cross-system identity without runtime lookups. Adds OAuth CSRF nonce validation and browser-binding cookie to the login flow.
- bbfecc5: Allow adding multiple GitHub collaborators when publishing plugins to a marketplace. The publish dialog accepts a list of usernames as chips, and the `publishPlugins` API now takes `github_usernames` (array) instead of `github_username` (string).
- 1057ea9: Add OTEL forwarding: customers can configure a URL and headers on the Org Logs page, and a body-tee middleware mirrors every payload received on `/rpc/hooks.otel/v1/*` to that endpoint. Forwarding is org-wide, async (bounded worker pool, fire-and-forget on failure), capped at 4 MiB per request, and gated behind `org:admin` for writes / `org:read` for reads. Header values are encrypted at rest and never returned by the API.
- a5e0990: Added support for configuring webhooks to deliver audit log events to external destinations.

### Patch Changes

- 491f3b8: add an opt-in L1 ML prompt-injection classifier (deberta-v3) that runs alongside the heuristic baseline. enable the new "ML classifier (deberta-v3)" rule under the Prompt Injection category in the policy editor to layer the classifier on top of L0 heuristics. detection runs in a sidecar service; configure with `PI_CLASSIFIER_URL` and `PI_CLASSIFIER_THRESHOLD` (default `0.9`)
- 7290607: Removed the 1-public-MCP-server cap on accounts without an active subscription. Users can now enable as many public MCP servers as they want on any plan.
- ad3c963: `/rpc/tools.list` now accepts a `tool_types` filter and can return direct external MCP tools, unblocking the toolset editor's "Add Tools" picker for tools from already-attached external MCP servers.
- ec37cf7: quiet false-positive Temporal workflow failure alerts: benign `ContinueAsNewError` and `CanceledError` log at Info, and `VerifyCustomDomain` is non-retryable on NXDOMAIN.
- 6305bd6: harden AnalyzeBatch against Presidio degradation
- 44ccc02: The assistant runtime now spills oversized MCP tool results to a file inside the assistant workdir instead of letting them 413 the provider. The in-band tool result is replaced with a pointer (`{ truncated, saved_to, original_bytes }`) so the model can read or grep the full output via the filesystem tools — no information loss, no provider error.
- f872cc2: Drop trigger dispatches whose target assistant has been deleted instead of failing the activity; retrying can't recover a missing row.
- 44be24a: Fix plugin re-publish so Claude Code, Cursor, and Codex marketplace clients refresh installed copies. Every plugin manifest now ships with a per-publish version (`0.1.<unix_ts>`) instead of a hardcoded `0.1.0`, so platform clients see a newer version on republish and pull the updated content.

## 0.51.2

### Patch Changes

- fcf3fd6: Auto-enable MCP on toolsets when they are attached to an assistant, so the runtime can build a startup config without manual toggling.
- a6f005f: Tag users who sign up with `disposition=assistants` with a PostHog person property so the assistants feature flag can target them.

## 0.51.1

### Patch Changes

- 58d3e52: Assistant Fly runtime now provisions one app per assistant (with one machine per thread) instead of one app per thread. Reduces Fly app churn and speeds cold starts; reap continues to drain old per-thread apps automatically.
- fce5ff5: OpenRouter responses indicating exhausted credits now surface as 402 Payment Required to chat callers instead of a generic 5xx, and the chat-resolution analyzer stops burning retries against a request that cannot succeed.

## 0.51.0

### Minor Changes

- 280b7ef: The assistant runtime now compacts conversation history as it approaches the model's context window: older turns are summarised so long-running assistants can keep going past the original window limit. System prompt, context items, and the most recent turns are preserved.
- f2fd934: Adds an endpoint to consume workOS webhooks to sync data from workOS
- e7dfe3c: Add wake triggers: one-shot self-wakes that an assistant schedules from inside its own turn to resume work later. New `platform_schedule_wake` and `platform_cancel_wake` tools let an assistant set a future fire time (up to 30 days out) with an optional self-note; when the wake fires, dispatch lands on the same thread it was scheduled from. Pending wakes are cancelled automatically when the owning assistant is deleted.

## 0.50.0

### Minor Changes

- 2609588: Add assistant memory: per-assistant long-term memory backed by vector embeddings. Agents can remember, recall, and forget facts across threads via three new platform tools (gated by the `assistant_memory` product feature). Includes a management API for listing and deleting memories, and a background reaper that hard-deletes soft-deleted rows on schedule.
- ca625e0: Propagate assistant runtime image upgrades to existing fly.io machines: on the next admission, an idle machine running an older runtime image is recycled in place to the latest version. Mid-turn admissions are left alone so a future idle window picks up the upgrade.

### Patch Changes

- 2c84295: Surface `environment:read` / `environment:write` in the RBAC dev toolbar and the
  `access.listGrants` fallback so the env-clone permission picker works end-to-end.

## 0.49.0

### Minor Changes

- 5136b45: Add optional `remote_mcp_server_id` and `toolset_id` filter parameters to `mcpServers.list` so callers can scope the result to MCP servers backed by a single remote MCP server or toolset. The two filters are mutually exclusive.
- 5136b45: Add `remoteMcp.verifyURL` for probing a candidate remote MCP server URL by issuing an MCP `initialize` request and reporting whether the URL is reachable. A `401` or `403` response counts as verified — auth verification is intentionally out of scope.

### Patch Changes

- 7834695: Fix generated observability plugin hooks not firing correctly in production. Hook events now carry explicit `async` flags matching the public Gram plugin (`false` for blocking events like `PreToolUse` and `UserPromptSubmit`, `true` for fire-and-forget events like `Stop` and `PostToolUse`). The generated `hook.sh` script now captures the HTTP response body and status code separately, forwarding the body to stdout for Claude to read `permissionDecision` from on `PreToolUse`, and exiting with code 2 on 4xx/5xx so an unreachable Gram server cannot silently bypass blocking policies.
- 0b356a5: Fix Claude Code plugins not loading after restart. The `git-subdir` source
  type used by the marketplace proxy does not persist the plugin cache path
  across Claude Code sessions, causing "not cached at (not recorded)" errors
  on every relaunch. The marketplace URL returned by `getPublishStatus` now
  points directly at the git proxy (`/marketplace/p/{token}.git`) and the
  install instructions emit `"source": "git"` in the `extraKnownMarketplaces`
  snippet, which Claude Code caches reliably between sessions. The
  URL-based manifest endpoint and its rewrite logic have been removed.

## 0.48.0

### Minor Changes

- 0168857: Decorate `/chat/completions` responses with the upstream model's context window via a `gram_metadata` extension. The size is fetched from OpenRouter's per-model endpoints listing (smallest `context_length` across providers) and cached for 72h. The streaming path injects the value into the final SSE frame.
- 658ff47: Auto-provision an org and attach the free-tier Polar subscription when an unauthenticated user lands on Gram with `?disposition=assistants` and has no org after IDP signin. Generates a legible random org name (e.g. `Swift Otter 42`), eagerly materializes the default project and environment, marks the org as whitelisted so it bypasses the BookDemo gate, and redirects to `/<org>/projects/default/assistants` so the credit benefit is in place before the user reaches the assistants page.
- 9dcc221: Add `cli_destructive` risk-policy source for flagging destructive CLI commands.

  Mirrors the existing `destructive_tool` shape (post-hoc batch scan, flag-only,
  no live blocking) but is content-driven instead of annotation-driven. A
  curated regex set covers shell (`rm -rf`, `dd`, `mkfs`, fork-bomb,
  `chmod -R`, `chown -R`, `sudo <arg>`), git (`push --force`, `reset --hard`,
  `clean -f`, `branch -D`), database (`DROP`, `TRUNCATE`, unguarded
  `DELETE FROM`, `dropdb`), and cloud (`aws ec2 terminate-instances`,
  `aws s3 rb`, `gcloud projects delete`, `kubectl delete ns/workloads`).

  The scanner walks every recorded tool call's parsed arguments — no MCP
  filter — so native Bash and `run_terminal_cmd` are now in scope alongside
  MCP-routed calls whose arguments happen to carry destructive content.
  First-match-wins iteration over map keys is sorted so rule_ids are
  deterministic across runs.

  PolicyCenter exposes the new source as a "Destructive CLI Commands" rule
  category (category-toggle UX matching `destructive_tool`).

- 188e614: Add a credit-balance gate on `/chat/completions` for **free-tier** orgs: pre-request check returns HTTP 402 `insufficient_credits` once the cached Polar Chat Credits balance is exhausted. Pro and enterprise stay bounded by the existing OpenRouter monthly key cap; unifying the two limit sources is tracked separately. Speakeasy-internal orgs (`specialLimitOrgs`) bypass; cache misses fail open. Self-serve top-up checkout (`usage.createTopUpCheckout`) opens a one-time Polar product configured via `POLAR_PRODUCT_IDS_TOPUP`.
- 3547f8e: Add management APIs for user sessions:

  - **userSessionIssuers**: configure the authorization servers that mint user sessions for your MCP servers.
  - **userSessionClients**: inspect and revoke the OAuth clients that have dynamically registered against those issuers.
  - **userSessions**: list the sessions minted for end users and revoke any that should no longer be honored.
  - **userSessionConsents**: list and withdraw the consent records that gate which (subject, client) pairs skip the consent prompt.

### Patch Changes

- b29be67: Capture a `gram_assistants_signup` PostHog event when the auth callback auto-provisions an org for a user landing with `?disposition=assistants`. The event is keyed on the user's email (matches `is_first_time_user_signup`) and carries `organization_id`, `organization_slug`, `disposition`, and `has_assistants_subscription` so the funnel from signup → benefit attach is observable.
- 6b4b80d: Fix OAuth discovery for MCP servers that host well-known metadata at the origin root regardless of endpoint path (e.g. Atlassian). When the remote URL has a path and prior discovery strategies find no authorization server metadata, the discovery chain now retries both `/.well-known/oauth-protected-resource` and `/.well-known/oauth-authorization-server` probes against the origin root with the path stripped.
- ce6603e: Fix catalog registry pagination so infinite scroll fetches all entries beyond the first page.

  `ListServers` now returns the upstream registry's `nextCursor` alongside the server list. `ListCatalog` passes that cursor through to the API response so the frontend's `getNextPageParam` receives a non-null value and `hasNextPage` becomes `true`. Previously `NextCursor` was always `nil`, causing the intersection observer to never trigger a second fetch and silently dropping any entries past the first 50.

- 5bafa07: Fix private Claude Code plugins showing "not cached at (not recorded)" after restarting Claude Code. The marketplace proxy now fetches the current HEAD commit SHA and embeds it alongside `ref` in each `git-subdir` plugin source, giving Claude Code a stable cache key that survives restarts.
- 8ce7444: scan risk policies for prompt injection. enable the new "Prompt Injection" category in the policy editor to flag or block instruction overrides, role hijacks, system-prompt leaks, encoded payloads, delimiter injection, and shell tool-abuse attempts

## 0.47.0

### Minor Changes

- f3f2070: Add listChallenges and resolveChallenge endpoints to the access service for the challenge resolution UI
- f65466b: Add a marketplace proxy and end-to-end install UX so users can install Gram-published plugins in Claude Code, Claude Cowork, and Cursor without making the upstream GitHub repo public.

  - **Server routes**: `GET /marketplace/m/{token}/marketplace.json` (URL-based Claude Code marketplace) and `/marketplace/p/{token}.git/...` (git Smart HTTP proxy for plugin source clones). Both stream directly from GitHub via the same GitHub App installation token used for publishing — no local mirror state, stateless. Proxy is mounted on the existing `gram start` server and wrapped with the recovery middleware so panics don't crash the process.
  - **Token-as-secret model**: `plugin_github_connections` gains a nullable `marketplace_token` column with a partial unique index. Tokens are auto-minted on first publish and preserved across subsequent publishes; rotation is a separate (deferred) admin path. Handler-level format precheck rejects malformed tokens before the DB lookup.
  - **Hook layout fix**: the publish flow now writes generated observability hooks at `hooks/hooks.json` (with the script alongside) instead of at the plugin root. Without the `hooks/` subdir, Claude Code and Cursor register the plugin successfully but never wire the hook events up — silently dropping every PreToolUse / PostToolUse signal.
  - **Plugin source rewrite**: rewritten manifests use the `git-subdir` source type per the official Claude Code marketplace schema (the only valid types are `npm`, `url`, `github`, `git-subdir`; plain `"git"` produces a confusing "source type your version does not support" install error).
  - **Dashboard**: the Plugins page surfaces the marketplace as a labeled panel with an "Install instructions" button that opens a HooksSetupDialog-styled modal. Three working provider tabs:
    - **Claude Code** — per-user `/plugin marketplace add` plus an org-wide rollout section with a copy-paste `extraKnownMarketplaces` snippet for Claude.ai's Managed Settings.
    - **Claude Cowork** — three-step admin walkthrough for adding the GitHub repo on Claude.ai's Plugins page.
    - **Cursor** — three-step team-admin walkthrough for cursor.com/dashboard, mirroring what's already documented in the published repo's README.
  - **Management API**: `plugins.getPublishStatus` now returns a `marketplace_url` field once a token has been minted; the dashboard reads from that. SDK regenerated.

- f3955c2: Add Slack reaction platform tools (`platform_slack_add_reaction`, `platform_slack_remove_reaction`, `platform_slack_get_reactions`, `platform_slack_list_reactions`, `platform_slack_list_emoji`) so assistants can react to messages and discover available emoji.

### Patch Changes

- 504c815: Allow setting custom policy messages to be shown to end users

## 0.46.1

### Patch Changes

- 8553711: Increase CPUs to 4GiB and lower soft limit to 20% of hard limit.

## 0.46.0

### Minor Changes

- 02712dc: Teams installing Gram-published plugins now get observability automatically.
  Each org's published marketplace ships a `base` plugin containing the team's
  hooks with credentials embedded — no manual SessionStart configuration, no
  credential paste, no risk of forgetting the setup step. Install once per
  machine and tool events flow into the Gram dashboard for the org regardless
  of how many feature plugins a team member also installs.

### Patch Changes

- f8fe13d: Fix MCP install page rendering required external MCP headers in the install snippet even when the operator had configured those env vars as System or Omit.
- 88174e4: Build well-known OAuth metadata response body before writing 200 status so error paths surface as the real status code instead of 200 with an error body

## 0.45.0

### Minor Changes

- cc00be4: Assistants v0: server-side service, Temporal workflows + reaper, Fly.io / local Firecracker runtime providers, per-thread token manager, and the dashboard create/edit/onboarding UI for assistants with model, instructions, toolset and environment bindings.
- de9a6af: Add management APIs and queries for MCP servers and MCP endpoints
- 399ade0: Record plugin actions in the audit log. Plugin create, update, delete,
  server add/update/remove, role assignments, and publish each emit an
  audit entry inside the same transaction as the mutation, surfacing the
  events in `auditlogs.list` and the dashboard activity views.
- 4f152ca: Extend plugin publishing to generate Codex-compatible packages alongside
  Claude Code and Cursor. Each published plugin now also includes a
  `.codex-plugin/plugin.json` manifest and `.mcp.json` server config, with a
  top-level `.agents/plugins/marketplace.json` listing all plugins for
  installation via `codex plugin marketplace add`.
- a85e350: reject private/reserved IPs in Remote MCP Server URL validation

### Patch Changes

- 506d221: Reduced per-batch concurrency against Polar /quantities
- 745d0b2: feat(access): reassign members to the default role on role deletion and surface the affected members in the dashboard delete dialog
- 16cbc66: fix(mcp): filter tools/list response by RBAC grants so users with tool-scoped mcp:connect permissions only see their authorized tools
- 04c2dbf: Improve automatic setup of OAuth Settings for Remote MCP servers
- d7d9fc0: Stop logging expected missing MCP install page metadata lookups.
- 4163c3e: Stop logging expected .well-known OAuth probe misses
- 7721e8e: Add a one-click "Auto-Configure" path on the OAuth wizard's path selection step for OAuth 2.1 MCP servers, and drop the requirement that custom OAuth proxy configurations supply scopes.
- 7c3be05: Support for shadow mcp blocking (block unapproved MCP servers org-wide)
- 506d221: reduce concurrency on polar meter requests

## 0.44.0

### Minor Changes

- 58b4498: Support tool-level RBAC for MCP servers. Grants now use typed selectors with `resource_kind`, `resource_id`, `disposition`, and `tool` fields instead of untyped string maps. The dashboard scope picker stores toolset UUIDs (not slugs) as resource identifiers, fixing a bug where grants created via the UI never matched backend authorization checks. Public MCP servers correctly skip per-tool RBAC enforcement.

## 0.43.0

### Minor Changes

- 42e4248: Add support for scaling the number of instances and memory for machines deployed for a Gram Function. It is now possible to go up to 5 machines per function and up to 4096 MiB for each machine.

## 0.42.1

### Patch Changes

- 2b2d423: added per-skill time series data to the hooks summary API to power skill usage charts.

## 0.42.0

### Minor Changes

- ea3e1aa: Add GitHub publishing for plugins. Admins can publish generated plugin
  packages to a GitHub repository via a configured GitHub App, enabling
  distribution through Claude Code and Cursor team marketplaces.

### Patch Changes

- 672795f: Updated fly app reaping to target all apps used by old deployments, leaving only the most recent deployment's app(s) untouched. This is a more aggressive strategy that is coming ahead of support for scaling up fly apps to multiple machines per deployment.
- f03a7d2: Fix a data race in concurrent OpenAPI tool extraction that could corrupt schemas or crash deployments when the same schema was referenced by multiple operations.
- 00a8f2a: Cursor hooks native MCP support. Token use tracking support for Cursor sessions

## 0.41.0

### Minor Changes

- d8c6ce1: add support for publishing external servers into collections.
- 78e3323: Add remote MCP server management API endpoints with CRUD operations, RBAC scopes, header encryption, and audit logging
- 1ee9f95: Improved Hooks dashboard with new charts, refined visuals, and smarter default filters.
- 04c6c30: Add team invite flow with accept page, configurable expiry, and security hardening

### Patch Changes

- afe4b80: Normalize the `Source` column on `chat_messages` for Claude Code hook
  intake so tool-call messages use the OTEL `service.name` like user and
  assistant messages, instead of hardcoding `ClaudeCode`.
- bbe494e: Fix chats breaking when switching providers mid-conversation. Assistant turns that contained both a text reply and a tool call could cause the next turn to fail with a validation error on some provider routes, leaving the conversation unrecoverable. Affected chats now continue to work seamlessly across providers.
- 8c5d6e9: Add a defense-in-depth 413 guard on the `/completion` chat proxy — reject any
  single tool-result message over 200KB with a clean HTTP 413 / `request_too_large`
  error instead of forwarding to OpenRouter where it would surface as an opaque
  "prompt is too long" 400. Clients are expected to truncate tool outputs
  before sending (see `@gram-ai/elements` `tools.maxOutputBytes`), but this
  guard keeps the error surface clean if they don't.

## 0.40.1

### Patch Changes

- 3d9188f: Change ID Token syncing behavior to be slighlty less eager

## 0.40.0

### Minor Changes

- ea1e23d: Add organisational collections and the capability to publish MCP servers to share within the organisation.
- f749a53: Add plugins feature for distributing MCP server bundles to teams and allowing zip distribution

### Patch Changes

- d2bf604: Adds a new project metrics summary endpoint containing new data to power the new homepage
- 1ea6dff: Adds a super-admin interface for enabling RBAC to organisations.
- f127399: Set a hard limit on concurrent HTTP requests to Gram Function runners deployed on Fly. This prevents OOM errors when a large number of tool calls are made in a short period of time. This can cause memory exhaustion and crashes.
- 8e4fd98: Adds a better error handler for failed role resolution in the case that the user winds up with a corrupt session.
- 7b925e4: Remove the legacy column sso_connection_id
- 7376613: Add database migration for plugins tables (plugins, plugin_servers, plugin_assignments)
  to support the upcoming Plugins feature for distributing MCP server bundles.
- be476e6: feat: use pre-aggregated summary endpoint for hooks analytics charts and KPIs
- ba580e4: Fixes a race condition where concurrent `collections.List` calls could fail with `"default registry collection already exists"` while bootstrapping the default Registry collection. The ensure routine now treats unique-constraint violations as success and re-fetches the existing rows.

## 0.39.0

### Minor Changes

- 98d322b: Add support for triggers across Gram.

  This introduces webhook and scheduled triggers end to end, including server APIs, worker execution for trigger dispatch and cron processing, SDK support, and dashboard UI for managing trigger definitions and instances.

### Patch Changes

- 04e0240: Disabled the logger for the retryablehttp client to avoid noisy logs that can clutter the output.
- 6a23890: Fixed an issue where toolset lookup for install pages had fallback logic that, when a custom-domain-scoped query returned no rows (e.g. because the toolset was deleted), would retry with a slug-only query ignoring the domain. This caused the install page to serve a different org's active toolset that shared the same MCP slug instead of returning 404.
- 15a7b25: Ensure telemetry logs continue to be inserted into ClickHouse even if the
  request context has been canceled.
- 4b1aa8c: Allow resolving a server without a custom domain attached when the user is authenticated and a custom domain is available.

## 0.38.0

### Minor Changes

- 0e42ed2: Add UserPromptSubmit, afterAgentThought and afterAgentResponse hooks capture for Cursor
- 61cc193: Add team invite flow with accept page, configurable expiry, and security hardening

### Patch Changes

- 0b296d6: Stop serializing the full role object into the after_snapshot column of the audit log when a role is created. This data bloats the database unnecessarily. A future dashboard update will link directly to the role instead for this audit log event.

## 0.37.0

### Minor Changes

- 3a3acd3: Add editable OAuth proxy server configuration.

  Admins can now edit an existing OAuth proxy server's audience, authorization endpoint, token endpoint, scopes, token endpoint auth methods, and environment slug without having to unlink and recreate the configuration. The new `POST /rpc/toolsets.updateOAuthProxyServer` endpoint accepts partial updates with PATCH semantics (omit fields to leave them unchanged; pass an empty array to clear array fields). The dashboard's OAuth proxy details modal now exposes an Edit button that opens the existing OAuth modal in edit mode with the current values pre-filled.

  Slug and provider type remain immutable after creation. Gram-managed OAuth proxy servers stay view-only.

- b328938: Add static platform tools to tool discovery and the built-in MCP logs server.

## 0.36.0

### Minor Changes

- 58d44eb: Add team management endpoints (invites & members)

### Patch Changes

- 252cbca: fix: allow platform domain to serve MCPs with custom domains
- 494f76c: Adds support for tracking skills in hooks dashboard

## 0.35.0

### Minor Changes

- ba10ce4: Add Cursor hooks support with authenticated endpoint, plugin, and setup

### Patch Changes

- 0a3af53: Adds support for full session capture from Claude. Complete transcripts of prompts, responses, and tool calls
- c28788e: Add MCP App support across the playground, local functions runner, and the functions SDK.

  Improve local runner lifecycle handling for proxied tool and resource responses, and only seed MCP App function assets when the functions backend is local.

- 86dbcd6: Redesign the Available Tools section on MCP install pages to use a compact expandable table instead of overflowing badges. Each tool row shows its name and description, with an inline detail panel revealing the title and color-coded annotation badges (read-only, destructive, idempotent, open-world) on click. Servers with more than 10 tools show a "Show N more" button.

## 0.34.2

### Patch Changes

- bfae9f2: Adds role based access control enforcement to projects (behind feature flag)
- f2ec00c: Fixes issue with Oauth validation checks.
- c0d3215: Fix custom domain verification to fail fast on transient database errors instead of incorrectly creating a new domain record

## 0.34.1

### Patch Changes

- 9f179d5: Ensure `DeleteProject` returns idempotent success for non-existent project.
- a1c64a1: Fix toolset cache not being invalidated when a template is deleted.
- a64842e: Removes grants api endpoints (replaced by roles management).

## 0.34.0

### Minor Changes

- c9d23f8: Adds an API for role, membership and grants management.
- e177e45: Improve user-facing deployment logs with source processing details and aggregate summary

### Patch Changes

- 0c07035: fix: revert "feat: allow other security schemes when public OAuth is configured"
- 7978914: Validate that default_environment_id belongs to the caller's project before storing it in MCP metadata

## 0.33.0

### Minor Changes

- 2850644: Allow multiple security schemes even when OAuth servers are configured on public servers

### Patch Changes

- 6160abf: Moved control server initialization after all routes and middleware are attached, and added a /healthz endpoint to the main API mux so the control server can verify the API is actually serving traffic before reporting healthy.

## 0.32.1

### Patch Changes

- 1295324: Strip tools from toolset audit log snapshots

  The Tools field on Toolset can be very large. Cloning the before/after snapshots and nilling out Tools avoids serializing this data into audit log entries where it is not needed.

## 0.32.0

### Minor Changes

- fbb1c43: Introduced faceted search capabilities to the audit logs, allowing users to filter logs based on actor and action attributes.

  A new endpoint, `GET /rpc/auditlogs.listFacets`, is introduced to retrieve available facets for actors and actions. The existing `GET /rpc/auditlogs.list` endpoint is updated to support filtering by these facets.

### Patch Changes

- e97105d: Normalized OpenAPI HTTP auth scheme casing so extraction and stored metadata behave gracefully for variants like Bearer and Basic

## 0.31.0

### Minor Changes

- 658bef4: Adds new API endpoints for access and permissions management.

### Patch Changes

- 0e5f639: Prevent clobbering API Key Headers when Client Credentials exchange is unconfigured

## 0.30.0

### Minor Changes

- 6265f73: Introduced the audit logs API service and supplementary code to start recording audit logs in other services including new URN types to represent various subjects in Gram.

## 0.29.1

### Patch Changes

- 41d507c: Fixed `GET /rpc/chat.creditUsage` authentication so org-scoped credit usage works correctly for customers with multiple projects, requiring only session auth and no longer allowing chat-session access.

## 0.29.0

### Minor Changes

- 9c75407: Updated the Gram Function runners to run with 1GB of memory instead of 512MB providing more headroom for memory-intensive operations.

## 0.28.1

### Patch Changes

- 7aaeb96: Fix playground OAuth discovery to use toolset-level configuration instead of removed tool-definition fields.

  The frontend now detects OAuth requirements from `toolset.oauthProxyServer` and `toolset.externalOauthServer` instead of inspecting individual external MCP tool definitions (whose `requiresOauth` field was removed in a prior PR). The backend `getExternalOAuthConfig()` gains two new resolution paths — OAuth proxy providers with pre-configured client credentials (skipping DCR) and external OAuth server metadata — before falling back to the legacy tool-definition lookup for backward compatibility.

## 0.28.0

### Minor Changes

- 8c72d8c: Renames attribute_filters to filters in searchLogs, and introduces "in" operator.

### Patch Changes

- 3b0c2c9: Modified deployment logging so that non-https server urls in openapi documents are logged as warnings instead of errors. These urls do not block deployment processing. They are ignored when present.
- d8133af: Suite of hooks improvements
- 3bbf15a: Adds agent loop support for all tool types (mainly applicable to slack apps)
- 686fee5: Add gpt-5.4 support in playground.

## 0.27.1

### Patch Changes

- 1765931: Removes the logs enabled flag in the telemetry API responses.
- e616da7: Add admin-only cache purging functionality

## 0.27.0

### Minor Changes

- 63d10d0: ## Changeset

  External MCP servers now use the same OAuth configuration pathway as all other toolsets — no more special-cased token resolution.

  The "Configure OAuth" button is now enabled for external MCP servers that require OAuth. When discovered OAuth metadata is available, the configuration form can be auto-populated with a single click.

### Patch Changes

- 0c90e1e: Add hooks dashboard page

## 0.26.1

### Patch Changes

- 1821e46: Adds an initial pass "POC" implementation of Gram hooks for tool capture
- fb7439b: Improve settings page with tabs routing and logging API
- 0dab374: Adds ability to track external auth user IDs in telemetry logs.
- 998102f: Update telemetry search logs API response to sent unix nano timestamps as strings instead of int.

## 0.26.0

### Minor Changes

- 125d6c9: adds the ability to install slack apps through the Gram UI

## 0.25.0

### Minor Changes

- f364cc0: Adds listAttributeKeys endpoint to retrieve distinct attribute keys for telemetry filtering.

### Patch Changes

- e2c00cb: Adds a new filtering option to the search logs endpoint to filter any attribute.

## 0.24.0

### Minor Changes

- 0f4f5dd: Adds an opt-in toggle for recording tool call inputs/outputs in logs

### Patch Changes

- 3f5e4e9: Open CORS policy on /openapi.yaml and serve as text/yaml to avoid browser download.
- c4baf37: Redesign source detail page with two-panel deployments and invocation activity to give users a high level overview of a sources's utilisation in any MCP servers.

## 0.23.5

### Patch Changes

- 3c3e2c2: Refactored the server codebase to make the Temporal task queue configurable to unblock staging and preview deploys.

## 0.23.4

### Patch Changes

- 62c6784: Show Elements errors inside the actual chat

## 0.23.3

### Patch Changes

- bc50d89: Attempt OAuth discovery for MCP servers returning AuthRejectedError. Previously when a user adds a catalog MCP server without OAuth2.1 (like HubSpot) to their project and opens it
  in the playground, there's no way to configure authentication — the AUTHENTICATION section is completely missing. This happens because the server returns `401` without a `WWW-Authenticate header` (or `403`)
  during the initial connection probe, which triggers the `AuthRejectedError` path. That path currently just logs and continues, storing zero auth metadata. The frontend then sees no OAuth config and no header
  definitions, so it shows "No authentication required." Servers like linear with Oauth2.1 works correctly because its MCP server returns 401 with a WWW-Authenticate header, triggering the `OAuthRequiredError` path which runs full OAuth discovery.
- e00adba: Fix same-origin requests failing with "Origin does not match audience claim" error in chat sessions CORS middleware.

  Browsers don't send Origin headers for same-origin GET/HEAD requests. The middleware now validates the Host header against audience claims when Origin is absent, allowing legitimate same-origin requests while still preventing cross-origin bypass attacks.

## 0.23.2

### Patch Changes

- 84736c7: Support tool annotations in functions framework. Adds `ToolAnnotations` type allowing function authors to specify annotations via `Gram.tool({ annotations: { ... } })`
- 7dae1a8: Persist annotations from external MCP servers in the Catalog to the database

## 0.23.1

### Patch Changes

- 02503b5: fix an issue wherein we fail to account external MCP tools in deployment stats

## 0.23.0

### Minor Changes

- 9df7d84: Add observability features including telemetry logs, traces, chat logs with AI-powered resolution analysis, and an overview dashboard with time-series metrics.

## 0.22.5

### Patch Changes

- f635e22: Support for [MCP tool annotations](https://modelcontextprotocol.io/legacy/concepts/tools#tool-annotations). Tool annotations provide additional metadata about a tool’s behavior,
  helping clients understand how to present and manage tools. These annotations are hints that describe the nature and impact of a tool, but should not be relied upon for security decisions.

  The MCP specification defines the following annotations for tools that Gram now supports for external mcp servers sourced from the Catalog as well as HTTP based tools.

  | Annotation        | Type    | Default | Description                                                                                                                          |
  | ----------------- | ------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------ |
  | `title`           | string  | -       | A human-readable title for the tool, useful for UI display                                                                           |
  | `readOnlyHint`    | boolean | false   | If true, indicates the tool does not modify its environment                                                                          |
  | `destructiveHint` | boolean | true    | If true, the tool may perform destructive updates (only meaningful when `readOnlyHint` is false)                                     |
  | `idempotentHint`  | boolean | false   | If true, calling the tool repeatedly with the same arguments has no additional effect (only meaningful when `readOnlyHint` is false) |
  | `openWorldHint`   | boolean | true    | If true, the tool may interact with an "open world" of external entities                                                             |

  Tool annotations can be edited in the playground or in the tools tab of a specific MCP server.

## 0.22.4

### Patch Changes

- b2347fc: Adds a new telemetry endpoint to fetch user usage data
- cd7a003: feat: record api key id in telemetry logs
- a34d18a: Adds chat resolution stats in telemetry metrics

## 0.22.3

### Patch Changes

- e246458: Starts writing chat resolution telemetry data.
- a7422f8: feat: add OAuth support for external MCP servers in the Playground
- a753172: feat: customize documentation button text on MCP install page
- 4ef4d5e: fix: allow surfacing openapi parse errors in the UI
- 6e29702: Adds a new endpoint to get metrics per user. Allows filtering logs per user.
- 1f74200: Fixes issue with loading of metrics when logs are disabled.

## 0.22.2

### Patch Changes

- 26ddbdd: Adds backend support for generating chat resolutions

## 0.22.1

### Patch Changes

- 0fe62df: Fix internal: billing_usage_report now start_time to be correctly parsed in Loops
- c9b74af: Adds a new endpoint to list chats grouped by ID

## 0.22.0

### Minor Changes

- ca387c6: Add urn_prefix filter to tools.list API for server-side filtering of tools by URN prefix

## 0.21.0

### Minor Changes

- 2d520cb: Add support for follow-on suggestions within the Elements library
- b85bfd5: Last accessed date is now available for Gram API keys and can be viewed via the
  API and dashboard settings page.

### Patch Changes

- 89bcd84: Support custom HTTP headers for external MCP servers, enabling authenticated access to registries requiring API keys
- ed006b1: Support custom domains for MCP export api
- afb9fbb: Adds new endpoint to retrieve summarized project metrics
- 90ad1ba: Add support for install page redirect URLs

## 0.20.1

### Patch Changes

- f3f6c82: Add machinery for tracking mcp header / environemnt configuration

## 0.20.0

### Minor Changes

- 834a770: Removes old tool toolmetrics logs logic and endpoints.

### Patch Changes

- 4e50632: Adds clickhouse logging for GenAI events
- f8a3eae: Show all envirnoment variables for basic auth in mcp details and install page

## 0.19.0

### Minor Changes

- e5e4127: Introduced an internal OpenRouter Go SDK generated with Speakeasy and makes use of it in the Gram server's chat service to deserialize requests. This SDK is intended to be replaced by a future official OpenRouter SDK when that becomes available.

## 0.18.5

### Patch Changes

- 7daaf31: Added endpoints for creating presigned URLs for chat attachments and accessing them using JWT tokens with a limited TTL. This is currently an exploratory feature and may be removed or changed in the near future.
- e4c02a1: proxy fully metadata objects for external oauth servers

## 0.18.4

### Patch Changes

- 5c6f78a: Embed Elements chat in logs page

## 0.18.3

### Patch Changes

- a0b7e13: feat: Use Gram Elements for the Playground UI

## 0.18.2

### Patch Changes

- 0abff4c: Updated the cursor format on /rpc/deployments.logs endpoint to be based on off of the sequential ID of the deplayment logs rather than the UUID of the log entry. This ensures a strong ordering of logs in the presence of multiple logs created at the same timestamp.

  This problem was pronounced when processing Gram Functions and external MCP servers that would create batches of of logs with overlapping timestamps, leading to out-of-order logs in the API response.

- 0fd8d39: Adds a new Gram endpoint to update a chat title

## 0.18.1

### Patch Changes

- 764b650: Refactored the processing of external MCP servers as part of deployments so that customer-facing logs are emitted. Previously, errors that occurred when processing an external MCP server were only visible internally.

## 0.18.0

### Minor Changes

- dc1b2b8: Updated the assets service to allow chat session to upload and read attachments via the `/rpc/assets.uploadChatAttachment` and `/rpc/assets.serveChatAttachment` endpoints.

### Patch Changes

- 98783c3: fix: return 401 for ext oauth servers even if gram-chat-session is present

## 0.17.4

### Patch Changes

- 6cd7978: This change adds an `Accept: */*` header to requests from the tool proxy. This resolves issues with some APIs (eg. https://api.intercom.io) which rely on the Accept header's presence to return content

## 0.17.3

### Patch Changes

- 54a32f4: Updated the function deployment temporal activity so it spawns multiple goroutines to deploy functions in parallel. This should in theory speed up deployments with several functions.

## 0.17.2

### Patch Changes

- ecafb6f: Fixes an issue where we weren't properly pulling the chat session header, which caused private MCP servers to fail when connected to via elements.

## 0.17.1

### Patch Changes

- f0dad26: Adds support for UNSAFE_apiKey in Elements. This will be used during onboarding to allow users to quickly trial elements without needing to set up the sessions endpoint in their backend

## 0.17.0

### Minor Changes

- bef31df: Added two new API endpoints for uploading and serving chat attachments.

  The `/rpc/assets.serveChatAttachment` endpoint can be accessed with an API key or session cookie. `Gram-Project` is not used on that endpoint to make it easy for session-based clients to embed attachments in chat such as with `<img>` tags for images e.g. `<img src="/rpc/assets.serveChatAttachment?id=...&project_id=..." />`.

## 0.16.0

### Minor Changes

- 5bc733e: Added a new API endpoint `/rpc/projects.get` to Gram server that allows clients to retrieve project details given a project slug. The project must exist within the organization referenced by the provided `gram-session` cookie or `Gram-Key` header.

### Patch Changes

- 122209b: Updated auth logic allowing API keys that have producer scope to access chat session APIs. In other, producer scope becomes a superset of chat and consumer scopes.
- 417c0c6: feat: Support external MCP servers that only have an SSE remote available.

  Previously, Gram could only support external MCP servers that used the
  Streamable HTTP transport. Now, servers that still use the deprecated SSE
  type will be transparently adapted to Streamable HTTP. MCP clients will
  still use Streamable HTTP to interact with the external MCP server via Gram:

  ```
  CLIENT <-(Streamable HTTP)-> GRAM <-(SSE)-> EXTERNAL MCP SERVER
  ```

- d972d1b: Adds ability to filter telemetry logs by multiple Gram URNs
- 3a82c2e: Adds enabled field to telemetry API response indicating whether logging is enabled or not

## 0.15.1

### Patch Changes

- 7e5e7c8: Adds a new telemetry endpoint to the Gram API

## 0.15.0

### Minor Changes

- 3ab2e40: Follow OAuth metadata discovery flow to better resolve authorization servers from external MCPs
- 8c865e1: Introduce the ability to browse entries from MCP-spec conformant registries from Gram Dashboard source import modal

### Patch Changes

- 811989e: Enable private MCP servers with Gram account authentication

  This change allows private MCP servers to require users to authenticate
  with their Gram account. When enabled, only users with access to the
  server's organization can utilize it.

  This is ideal for MCP servers that require sensitive credentials (such as API
  keys), as it allows organizations to:

  - Secure access to servers handling sensitive secrets (via Gram Environments)
  - Eliminate the need for individual users to configure credentials during installation
  - Centralize authentication and access control at the organization level

- 9479883: Adds new API endpoints to query for telemetry logs and traces
- 6e84b55: Allow external mcp sources to be renamed in the Gram UI

## 0.14.2

### Patch Changes

- e0b26ea: Add ListToolExecutionLogs API endpoint for querying structured tool logs with cursor-based pagination and filtering support
- 82f637a: Updates AgentAPI with storing of agent run IDs for a paginated log view. Also changes the access control defensive check to work on project id which is better
- 5482f4c: Introduces infrastructure to run a local MCP registry in a container

## 0.14.1

### Patch Changes

- 45bea6e: Pin to older mcp-remote@0.1.25 to avoid classic claude desktop issue with selecting the oldest node version on the machine. Versions pre v20 such as commonly available v18 make it not possible for people to load an mcp

## 0.14.0

### Minor Changes

- 08ce250: Introducing support for large Gram Functions.

  Previously, Gram Functions could only be around 700KiB zipped which was adequate for many use cases but was severely limiting for many others. One example is ChatGPT Apps which can be full fledged React applications with images, CSS and JS assets embedded alongside an MCP server and all running in a Gram Function. Many such apps may not fit into this constrained size. Large Gram Functions addresses this limitation by allowing larger zip files to be deployed with the help of Tigris, an S3-compatible object store that integrates nicely with Fly.io - where we deploy/run Gram Functions.

  During the deployment phase on Gram, we detect if a Gram Function's assets exceed the size limitation and, instead of attaching them in the fly.io machine config directly, we upload them to Tigris and mount a lazy reference to them into machines.

  When a machine boots up to serve a tool call (or resource read), it runs a bootstrap process and detects the lazy file representing the code asset. It then makes a call to the Gram API to get a pre-signed URL to the asset from Tigris and downloads it directly from there. Once done, it continues initialization as normal and handles the tool call.

  There is some overhead in this process compared to directly mounting small functions into machines but for a 1.5MiB file, manual testing indicated that this is still a very snappy process overall with very acceptable overhead (<50ms). In upcoming work, we'll export measurements so users can observe this.

### Patch Changes

- 1538ac3: feat: chat scoped key access to mcp server
- 1af4e7f: fix: ensure system env compilation is case sensitive
- ea2f173: ensure function oauth is respected in install page
- 90a3b7b: Allow instances.get to return mcp server representations of a toolset. Remove unneeded environment for instances get
- a062fc7: fix: remove vercel check form cors
- 0818c9a: feat: reading toolset endpointa available to chat scoped auth
- c8a0376: - fix SSE streaming response truncation due to chunk boundary misalignment
  - `addToolResult()` was called following tool execution, the AI SDK v5 wasn't automatically triggering a follow-up LLM request with the tool results. This is a known limitation with custom transports (vercel/ai#9178).
- c039dc0: Updated the CORS middleware to include the `User-Agent` header in the `Access-Control-Allow-Headers` response. This allows clients to send the `User-Agent` header in cross-origin requests which is useful for debugging and analytics purposes.

## 0.13.0

### Minor Changes

- 1c836a2: Proxy remote file uploads through gram server

### Patch Changes

- 7bf206e: In a case where an MCP server is being used as a private server and it has a default environment attached. If that environment has a certain variable that's also being passed through directly on use. We should always prioritize the one that is passed through directly on use.
- f29d111: allowed types text/plain
- 25912d8: fix: small custom oauth fixes"
- 5d5fe0b: fix: nullable chat id model billing

## 0.12.2

### Patch Changes

- 24ea062: Updates to openrouter billing tracking
- 949787b: update chat credit billing
- c530931: Adds server-side check on number of enabled MCP server by account type
- ed8c67a: fix: context cancellation for tracking model usage
- c1ebf7f: openrouter keys no longer need to be deleted and manually refreshed. We will utilize the new limit_refresh "monthly" setting for keys
- 664f5fd: feat: fallback temporal workflow for openrouter usage
- 3019ccb: Update Codex CLI installation instructions to use http instead of stdio w/ mcp-remote.
- 80e114e: static oauth callback in oauth proxy
- eab4b38: Remove Windsurf installation instructions and add VSCode install link

## 0.12.1

### Patch Changes

- a5f1e74: Introduces the agent API to offer as an early pre-beta option for dynamically executing cloud based agent workflows in Gram. The structure is based on functionality provided in the OpenAI responses API including async runs, previous_response_id chain building, full support for model switching, use of the store flag to selectively delete agent history.
- 4228c3e: Implements passthrough oauth support for function tools via oauthTarget indicator. Also simplifies the oauth proxy redirect for more recent usecases

## 0.12.0

### Minor Changes

- acb124f: Add instructions column to mcp metadata schema

### Patch Changes

- b69cb2b: Include MCP server instructions in initalize endpoint
- 010561a: Add backend logic to upsert/retrieve MCP server instructions. Also updates API spec to include this new field.
- c2ea282: admin view for creating oauth proxies
- 444da5b: Updated oops.ErrHandle to include panic recovery. There are a few HTTP handlers
  included in some services (alongside Goa endpoints) that needed this protection.
  The log messages will also include stack traces for easier debugging.

## 0.11.0

### Minor Changes

- 6716410: Add the ability to attach gram environments at the toolset level for easier configuration set up

### Patch Changes

- a2ff014: fix: incorrect mapping of openrouter model pricing
- e34b505: updating of openrouter key limits for chat based usage
- e016bcc: fix: capture of openrouter usage data streaming
- 2788cf3: Fixed a type mismatch in the Polar client when creating events with metadata
  following an update to the Polar Go SDK
- 38b9b22: Apply simple HTTP status code heuristic for estimating successful tool calls

## 0.10.6

### Patch Changes

- 6b04cc2: Updates playground chat models to a more modern list. Add Claude 4.5 Opus and ChatGPT 5.1

## 0.10.5

### Patch Changes

- bddc501: start tracking chat usage in polar

## 0.10.4

### Patch Changes

- 0dfdc43: add table for tracking toolset environments

## 0.10.3

### Patch Changes

- 67c2a5e: Increased the batch size for the fly app reaper from 50 to 200 to more aggressively recover fly machines.
- 8bf8710: Introduces v2 of Dynamic Toolsets, combining learnings from Progressive and Semantic searches into one unified feature. Extremely token efficient, especially for medium and large toolsets.

## 0.10.2

### Patch Changes

- cf3e81b: non blocking deployment creation

## 0.10.1

### Patch Changes

- 55616f6: Improves the initial description for the find_tools tool in the semantic search dynamic MCP mode. Provides an overview of what tool categories exist in the server.

## 0.10.0

### Minor Changes

- c249bb0: Adds the ability to attach an environment to a source such that all tool calls originating from that source will have those environment variables apply

## 0.9.14

### Patch Changes

- d445fa1: Modified the function reaping process to reduce noise in user deployment logs by suppressing routine informational messages.
- d445fa1: Updated the database query to list reapable fly apps so that it can be scoped to a specific project ID. This allows project-scoped reaping. Previously, the project-scoped reaper was not passing the project ID to the query and it was acting as a global reaper.

## 0.9.13

### Patch Changes

- 51f5349: Added the necessary Authorization header to the Fly API delete machine request
  to ensure proper authentication. We also increase the reap batch size to 50.
- ab8d2fe: adds experimental gram-mode:embedding for dynamic MCP tool selection based on semantic search
- 43f8702: Fixed a bug in logging the chosen OpenAPI parser.
- 0f70699: Fixed a bug in `ExecuteProjectFunctionsReaperWorkflow` where it was running the
  wrong workflow (`ProcessDeploymentWorkflow` instead of
  `FunctionsReaperWorkflow`).
- 181971a: fix resource env config incorrectly unmarshaled

## 0.9.12

### Patch Changes

- 31e555b: feat: Add gram install command for MCP server configuration & support common clients

  **Automatic Configuration**

  ```bash
  gram install claude-code --toolset speakeasy-admin
  ```

  - Fetches toolset metadata from Gram API
  - Automatically derives MCP URL from organization, project & environment or custom MCP slug
  - Intelligently determines authentication headers and environment variables from toolset security config
  - Uses toolset name as the MCP server name

  **Manual Configuration**

  ```bash
  gram install claude-code
  --mcp-url https://mcp.getgram.ai/org/project/environment
  --api-key your-api-key
  --header-name Custom-Auth-Header
  --env-var MY_API_KEY
  ```

  - Supports custom MCP URLs for non-Gram servers
  - Configurable authentication headers
  - Environment variable substitution for secure API key storage
  - Automatic detection of locally set environment variables (uses actual value if available)

- 29aee79: fixes potentially duplicate env vars from functions in the UX and MCP config

## 0.9.11

### Patch Changes

- 3d46253: implements adding redacted http security headers to the opt in tool call log view
- db29a12: adds http server url to clickhouse data model
- 77446ee: fully connects server url tracking feature in opt in tool call logs

## 0.9.10

### Patch Changes

- ff7615f: Added an endpoint to download Gram Functions assets at `GET /rpc/assets.serveFunction`.
- bb37fed: creates the concept of user controllable product features, opens up logs to self-service enable/disable control
- 6f5ddb8: Updated the Gram Functions Fly.io orchestrator to deploy runner apps in multiple
  regions instead of a single region _by default_. Previously, all machines
  resided in `sjc` which created an availability risk.

## 0.9.9

### Patch Changes

- 145295a: Changes default install method for Cursor MCPs to HTTP streaming

## 0.9.8

### Patch Changes

- d0cd8ba: fixes trimming fragments in plan execution
- 2db3a23: Add filtering support to the tool call logs table

## 0.9.7

### Patch Changes

- bab05ce: Adds support to the Playground for any tool type, notably enabling function tools to be used there
- 7afda6e: Allows the MCP metadata map to accept arbitrary value types as supported by the server

## 0.9.6

### Patch Changes

- 69e766a: Adds a page for viewing tool call logs from ClickHouse with a searchable table interface displaying tool call history and infinite scroll pagination with cursor-based navigation for efficient data loading.

## 0.9.5

### Patch Changes

- 7334ac8: fix the mcp server passthrough in gram functions. We receive the result content and respond with that

## 0.9.4

### Patch Changes

- 5b8a324: Supports returning meta tags in list tools and list resources. Supports a specific gram.ai/kind meta tag that tells us to treat the underlying function as an MCP server and a direct passthrough

## 0.9.3

### Patch Changes

- 4ae6852: Adds an icon to the mcpb installation method that will render in Claude Desktop alongside your tool calls
- 5038166: Introduced the ability to register \_meta tags for tools and resources

## 0.9.2

### Patch Changes

- 3c00725: Set of improvements for functions onboarding UX, including better support for mixed OpenAPI / Functions projects
- 99ef7d6: reinstroduced oauth protected resource, the way we are exposing this is generally correct even though many clients don't really process it yet
- 1a46e29: Allows MCP to work in browser based MCP inspector which was the original intention
- 6a2eecf: Sets up the ability to track gram functions memory and cpu usage per tool call coming from the function runner
- 12fef9e: Prevent nil pointer dereference panic during server and worker shutdown. This
  was happening because the Gram Functions orchestrator was retuning nil shutdown
  functions at various code paths.

## 0.9.1

### Patch Changes

- d6f5579: Adds a basic toolset UX for managing resources in the system adding/subtracting them per toolset
- 44cfc3b: Pass the appropriate uintptr value in the slog Record when logging in `oops.ShareableError.Log()`. Previously, all log messages had their source location being the Log method itself which was not helpful.
- 2fb24e6: Adds UI hints for custom tools, indicating which "subtools" are missing (if any), or just surfacing the list of subtools otherwise. Begins tracking the required subtools more powerfully in order to support Gram Functions.

## 0.9.0

### Minor Changes

- 7cd9b62: Rename packages in changelogs, git tags and github releases

### Patch Changes

- 671cc0e: Fixes two issues: 1) Producer scoped keys were incorrectly not able to access MCP servers, the app documents them as a superset on consumer and we had a bug. 2) The MCP install page was incorrectly forming a URL without the MCP Slug.
- 4680971: Implements listing resources into our actual MCP Server layer. Also implements the gateway proxy for resources currently only being served from functions. Billing/Metrics wise we still treat fetching a resources as a tool call, but there are resource attributes added onto this that would allow us to separate in the future.

## 0.8.1

### Patch Changes

- f3cea34: The first major wave of work for supporting MCP resources through functions includes creating the function_resource_definitions data model with corresponding indexes and resource_urns columns in toolset versions. It also introduces the function manifest schema for resources and implements deployment processing for function resources. A new resource URN type is added, which parses uniqueness from the URI as the primary key for resources in MCP. Additionally, this work enables adding and returning resources throughout the toolsets data model, preserves resources within toolset versions, and updates current toolset caching to account for them.

## 0.8.0

### Minor Changes

- f3ffd00: Preserve redirect URLs during log-in for unauthenticated browsers.

### Patch Changes

- 6c5d329: Remove errant authorization from image serving
- ac5cb3d: Add correct resolution of custom domains for private MCP servers in install pages

## 0.7.2

### Patch Changes

- 0fa05ce: Fix custom install page logos on custom domains
- 660c110: Support variations on any tool type. Allows the names of Custom Tools to now be edited along with all fields of Functions.
- 9f7f5ea: Correctly use the custom domain on install pages
- cb7fc5a: Update the gateway to check the `Gram-Invoke-ID` response header from Gram Functions tool calls before proxying the response back to the client. This is an added security measure that asserts the server that ran a function had access to the auth secret and was able to decrypt the bearer token successfully.

## 0.7.1

### Patch Changes

- 3ea6da7: feat: treat producer keys as a superset of consumer
- 8890c9e: Remove references to the `deleted` column for deployments_functions.
- d2283dd: Pass through only relevant environment variables to a given Gram Functions tool, as specified in the manifest, when invoking it.

## 0.7.0

### Minor Changes

- 9df917a: Adds the ability for users of private servers to load the install page for easy user install of MCPs.

### Patch Changes

- 3fa88db: Allow PCRE regex on incoming JSON sources, despite not necessarily being supported by Go's native regexp parsing.
- f15d1fe: Implements the boilerplate of being able to parse openIdConnect securitySchemes and treat the accessToken produced as a possible implementation of MCP OAuth
- 9df917a: fix: update to use mcpb instead of dxt nomenclature for MCP installation pages

## 0.6.0

### Minor Changes

- 806beca: Introducing support for Gram Functions as part of deployments. As part of deployment processing, each function attached to a deployment will have a Fly.io app created for it which will eventually receive tool calls from the Gram server.

  ## What are Gram Functions?

  Gram Functions are serverless functions that are exposed as LLM tools to be used in your toolsets and MCP servers. They can execute any arbitrary code and make the result available to LLMs. This allows you to go far beyond what is possible with today's OpenAPI artifacts alone

  At its code, a Gram Function is zip file containing at least two files: `manifest.json` and `functions.ts`.

  ### `manifest.json`

  This is a JSON file describing the tools including their names, descriptions, input schemas and any environment variables they require. For example:

  ```json
  {
    "version": "0.0.0",
    "tools": [
      {
        "name": "add",
        "description": "Add two numbers",
        "inputSchema": {
          "type": "object",
          "properties": {
            "a": { "type": "number" },
            "b": { "type": "number" }
          },
          "required": ["a", "b"]
        }
      },
      {
        "name": "square_root",
        "description": "Calculate the square root of a number",
        "inputSchema": {
          "type": "object",
          "properties": {
            "a": { "type": "number" }
          },
          "required": ["a"]
        }
      }
    ]
  }
  ```

  ### `functions.js` / `functions.ts`

  A JavaScript or TypeScript file exporting the actual function implementation for tool calls. Here's a function that implements the manifest above:

  ```javascript
  function json(value: unknown) {
    return new Response(JSON.stringify(value), {
      headers: { "Content-Type": "application/json" },
    });
  }

  export async function handleToolCall({ name, input }) {
    // process.env will also containe any environment variables passed on from
    // Gram.

    switch (name) {
      case "add":
        return json({ value: input.a + input.b });
      case "square_root":
        return json({ value: Math.sqrt(input.a) });
      default:
        throw new Error(`Unknown tool: ${name}`);
    }
  }
  ```

  Notably:

  - The file must export an async function called `handleToolCall` which takes the tool name and input object as parameters.
  - This function must return a `Response` object.
  - You can use any npm packages you like but you must ensure they are included in the zip file.

  ## What is currently supported?

  - We currently only support TypeScript/JavaScript functions and deploy them into small Firecracker microVMs running Node.js v22.
  - Each function zip file must be a little under 750KiB in size or less than 1MiB when encoded in base64.
  - Third-party dependencies are supported but you must decide how to include in zip archives. You may bundle everything into a single file or include a `package.json` and node_modules directory in the zip file. As long as the total size is under the limit, it should work.
  - The code will be deployed into `/var/task` in the microVM.
  - The code will only have permission to write to `/tmp`.
  - The code must not depend on data persisting to disk between successive tool calls.

- 104896e: Support tool calling to Gram Functions. This now means that you can deploy
  javascript/typescript code to Gram and expose it as tools in your MCP servers.
  This code runs in a secure sandbox on fly.io and allows you to run arbitrary
  that performs all sorts of tasks.

### Patch Changes

- c88b97f: Trim slugs to comply with 128-character limits.
- d8bd8c1: Restore security for HTTP tools in the MCP tool calling handler
- 143d76e: A database migration to support Gram Functions is added which includes:
  - A new table called `fly_apps` to store details about provisioned fly.io apps.
  - Columns in both `projects` and `deployments_functions` tables that allow pinning to a specific version of the Gram Functions runner.

## 0.5.0

### Minor Changes

- 31d661e: Add cache in front of describe toolset

### Patch Changes

- 2905669: Improve fallbacks when reading period usage. Fixes a minor race condition when a customer has only just subscribed
- 36d7a3a: Properly set schema $defs when extracting tool schemas. Resolves an issue where recursive schemas were being created invalid.
- e768e4d: Introduce “healing” of invalid tool call arguments. For certain large tool input JSON schemas, LLMs can sometimes pass in stringified JSON where literal JSON is expected. We can unpack the correct json object out of this, even after the LLM mistake.

  **Before healing**

  ```json
  {
    "name": "get_weather",
    "input": "{\"lat\": 123, \"lng\": 456}"
  }
  ```

  **After healing**

  ```json
  {
    "name": "get_weather",
    "input": { "lat": 123, "lng": 456 }
  }
  ```

- a3b4abe: feat: propogate through function environment variables on toolset

## 0.4.0

### Minor Changes

- 276d265: Support API key validation (/rpc/keys.verify)
- 7912397: Add endpoint to expose a project's active deployment

### Patch Changes

- e76199f: fill default schema for prompt templates
- 004e017: fix: consistent environment overrides"
- 148c86f: install page reflects pure toolset name
- 85ceb4c: Add JSON schema validation to tool schema generation
- 6a331ac: feat: connection function tools to toolset concept
- 6f11e8e: add ability to configure install pages and render configurations onto pages
- ae5a041: Add clickhouse dependency
- 094c3ee: Extract tools concurrently from incoming specs.
- 5a32fd7: fix: ensure custom domain ingress has proper regex annotation
- 41b5a22: feat: add consistent trace id to tool call requests
- 4fd085a: Update sanitization logic to properly coerce into the regex
- 8d7852e: add table for install page metadata
- 40ef4c9: feat: add project id to function tools model
- 663c572: omit access token which overrides intended oauth behavior
- 36454a3: patch nil dereference
- c40d9c0: fix: adjust cors policy for mcp oauth routes
- 180bfca: restore old location for install page (no /install)
- dcd0055: feat: billing usage tracking federation

## 0.3.0

### Minor Changes

- f17c187: Support uploading Gram Functions as part of deployments
- 9a93cdd: adds branding and improved install instructions to mcp install page

### Patch Changes

- b449904: Properly pass in user_config to dxt files
- b96cb53: Add functions_access table
- 155c2e1: Add gram cli v0.1.0
- bd15d15: Fixes mobile layout for install page
- e68386d: fix openrouter key refresh
- 4e0646e: Allow leading and trailing underscores and dashes in tool names and slugs
- ee7b023: Add basic validation for deployment attachments
- 395b806: small fixes to mcp install page
- 49a5851: support non security scheme input header parameters
- a91a5eb: make billing stub no-op in local dev thus preserving desired state

## 0.2.0

### Minor Changes

- 6d8ee87: Add an improved MCP installation page that offers one-click install to several popular clients as well as a more aesthetically pleasing presentation
- c7864b6: Improved revision of the server install page with simpler ergonomics and more install options
- 87136d0: Rename deployment fields for asset/tool count to prefix with openapiv3 and make room for new tool types/sources.

### Patch Changes

- ceb108f: Fix flakes in global ordering unit test.
- ece9cbb: ensure the latest tools in the system reflect from the latest successful deployment
- db11042: Add tool type field to HTTP tool definitions
- 33cdfa7: Repairs errant release of install page by actually including assets
- bc7faae: fix scope oauth variables to security key
- f5dc8b5: Include org id in tracing spans for polar
- 61f419f: Add OpenTelemetry tracing around OpenAPI processing

## 0.1.5

### Patch Changes

- 635a012: Avoid a nil pointer dereference on API-based requests to create deployments.
- 94c0009: Clear tools from previous deployment attempts when retrying deployments
- c270b33: fix implement hardcoded limit for tool calls until polar max can be trusted
- 7b65af4: Fill in project id and openapi document id when creating http security records during deployment processing
- bb6393f: handle subscription downgrade in polar webhook
- 0158ef8: Fall back to free tier for orgs with canceled subscriptions
- f150c54: correct openrouter threshold for pro tier
- fbcbeee: start checking tool call usage in free tier

## 0.1.4

### Patch Changes

- ef1eff3: fix a bug updating account type from polar

## 0.1.3

### Patch Changes

- a160361: update openrouter playground credits on account upgrade/downgrade

## 0.1.2

### Patch Changes

- dd769ee: update proxy parsing to better handle large numbers in params

## 0.1.1

### Patch Changes

- acf6726: Expose the kind of prompt templates, and do not count higher order tools as prompts in the dashboard.

## 0.1.0

### Minor Changes

- d4dbddd: Manage versioning and changelog with [changesets](https://github.com/changesets/changesets)
