# server

## 1.11.0

### Minor Changes

- d7dca3d: Add exact range-bounded activity totals and independent pagination to assistant sessions.

### Patch Changes

- 1b00702: Scope device agent fleet configuration to organization admins. Viewing it
  (`agent.getConfiguration`) now requires `org:admin`, matching the existing
  requirement on `agent.updateConfiguration`, and the dashboard hides the Device
  Agent Configuration tab from non-admins. The Setup tab stays available to
  organization readers.
- eaa0649: Serve the hooks@0.3.22 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- 97c1c32: Apply the phased hooks rollout gate to the publish freshness read. A hooks
  generator version bump used to flip every rollout-gated org's publish status to
  "needs syncing" permanently: publishing carries a gated org's hooks subtree
  verbatim (by design), so the stored hooks version could never reach the current
  constant that GetPublishStatus compared against. The status read now runs the
  same eligibility check as the publish path and skips the hooks version/config
  comparison for orgs the rollout hasn't cleared — their published hooks are
  their target, so only MCP content changes count against freshness. Cleared
  orgs still read a pending hooks bump (or a deferred hook-config change) as
  stale, prompting the publish that applies it.

## 1.10.0

### Minor Changes

- ae7f01b: The assistant detail panel is now fully configurable and observable. Overview settings (name, model, concurrency, warm TTL) are editable in place. The Sessions tab shows aggregate stats (sessions, messages, cost, tokens) over a selectable time range defaulting to the last 30 days, with per-session cost in the list. Triggers expand in place to show their recent traffic via the new `triggers.listEvents` endpoint, with each dispatched event linking to the conversation it was routed to.
- c66958e: Agent sessions routed through LiteLLM keep their LiteLLM association even when the agent's own hook stream captures the transcript: they match the LiteLLM platform filter and display as "<Client> via LiteLLM" in the session list and detail views.
- ca0f1c1: Encrypt platform OpenRouter API keys at rest. New keys are written with an
  AES-256-GCM encrypted copy alongside the plaintext column (dual-write during
  the expand phase), every read path prefers the encrypted copy and lazily
  records ciphertext for legacy plaintext rows, and the credits monitoring
  activity decrypts inside the activity boundary. A new platform-admin
  `adminOpenRouterKeys` service and dashboard page list every organization's
  keys with their credit limit, live usage, and encryption state, with actions
  to encrypt (verify the ciphertext, then clear the plaintext), enable, and
  disable a key. Enable and disable actions are audit logged against the owning
  organization; the encrypt action is internal storage hygiene and is not
  surfaced in customer-visible audit logs.

### Patch Changes

- 4dd8211: Expand user email filters in cost analytics and session lists to include the
  employee's directory email and linked AI account emails. User drill-down pages
  now report the same cost, token, tool-call, and session totals across work and
  personal account identities.
- a2bc9ed: Stop LiteLLM-proxied agent sessions from persisting each assistant turn twice,
  and label Claude Code sessions as Claude Code. The proxy reports the completion
  the moment the model returns it and the agent's own hook stream reports the same
  text when the turn ends; prompts already collapsed onto one row through the
  session's native marker, but assistant turns share no identity across the two
  observers, so both rows survived. A proxied assistant turn is now dropped for
  sessions a Claude hook stream already captured (cost and telemetry for the
  proxied call are unaffected), so those sessions are attributed to the agent that
  ran them rather than to the proxy. Separately, the bare `claude` adapter slug the
  hooks binary sends now resolves to the `claude-code` surface even when a session
  has no OpenTelemetry stream, instead of colliding with the `claude` the Anthropic
  compliance import writes for Claude Chat Desktop and being displayed as that
  surface.
- cfc020f: Harden OpenAPI-from-URL (and in-process image-from-URL) fetches against SSRF. User-supplied URLs are rejected unless they are https with a host that passes the guardian blocklist (loopback, RFC 1918, link-local/metadata, and other reserved ranges), and redirects are capped and re-checked so a hostile target cannot chain into private address space or downgrade to plaintext HTTP.

## 1.9.0

### Minor Changes

- 0e614ac: chat.list accepts a `user_id` filter so callers with project-wide chat visibility can narrow results to a specific Gram user. The Project Assistant dock uses it, together with the dashboard source-kind filter, so "Continue chat" only offers sessions the viewer started from the dashboard.
- 2b6ebb0: Cache OAuth client ID metadata documents instead of refetching them on every
  authorization request, honoring upstream `Cache-Control` and `Expires` headers
  within a 5 minute to 24 hour bound and revalidating with `If-None-Match`. A
  fetch or validation failure leaves the cached document untouched and fails the
  request rather than serving a stale one.
- 6f8d740: Add `userSessionIssuersCimdClients.verifyURL`, which probes a client ID metadata document URL and reports whether it is reachable and spec-compliant without saving anything. Every probe outcome is a successful response distinguishing a malformed URL, an unreachable endpoint, a non-JSON body, and a document that violates the spec, so an operator can fix a URL before adding it rather than discovering the problem when a client fails to authorize. Adding a URL does not fetch the document itself, so configuration never depends on a vendor's host being up; verification is an explicit step taken when it is wanted. The endpoint is rate limited per project, since it is the one place a caller can make Gram fetch a URL of their choosing. The OAuth authorization path is unchanged and still reports these outcomes as a single opaque error, so it cannot be used to probe external hosts.
- 43107ac: Add compact tool-call rows with separately loaded, persisted two-sentence summaries and risk-first detail expansion.
- 5ffabf3: Freeze external key identity: `externalKeys.updateAwsKms` and `externalKeys.updateGcpKms` no longer accept `key_arn` / `resource_name` or `algorithm` and cover only `name`, `external_credential_id` and `customer_grant_reference`, so changing what a key is now means deleting it and creating a new one (a breaking change to those two methods). Deleting a key is refused with a conflict while a JSON Web Key Set or published key still references it, and `createGcpKms` now requires a fully-qualified crypto key version path.
- 3b8d9fc: Catalog entries from external MCP registries now carry the registry's linked source repository and published packages, which the registry client previously dropped. Both are registry declarations — nothing ties a linked repository or package to what a remote endpoint actually runs — and the API descriptions say so. These feed the MCP approval evidence surface and the upcoming artifact pin-and-fetch work.
- 6f44265: Assistants no longer send every MCP tool schema to the model on every call. MCP tools are discovered on demand through a search tool, MCP servers connect on first use instead of during assistant startup, and dropped MCP connections reseat automatically instead of requiring a reconnect tool call. This keeps provider prompt caching effective for assistants with large toolsets and removes MCP handshakes from assistant cold-start latency.
- 8f3fb58: Show the supported client that originated an Agent Session routed through LiteLLM while preserving LiteLLM filtering.
- 6f44265: Assistant runtimes can now run locally: the new `local` runtime provider (the
  local-development default) starts one Docker container per assistant on demand,
  reuses it across turns, and automatically replaces idle containers when the
  runtime image is rebuilt — no Fly.io credentials or registry pushes needed for
  local image development.
- fffe50d: Emit the MCP protocol version to OpenTelemetry on all five inbound MCP paths. Traces now carry `gram.mcp.requested_protocol_version` and `gram.mcp.negotiated_protocol_version`, and a new unsampled `mcp.initialize` counter breaks handshakes down by revision, so client version adoption can be measured and a version-specific failure can be diagnosed.
- 7c02667: Organization names accept punctuation and every script. The old rule allowed
  only letters, digits, spaces, hyphens and underscores, which turned away
  "Acme, Inc.", "Bob's Bakery", "Café Zoë", and — more importantly — every
  company whose name is not written in the Latin alphabet, since a name in
  Japanese, Chinese, Korean, Cyrillic, Arabic or Hebrew could not clear the rule
  at all. Names are now capped at 100 characters (counted in characters, so a
  non-Latin name gets the same room a Latin one does), must carry at least two
  letters or numbers, and may use anything that renders: control characters, bidi
  overrides and other invisible formatting are still rejected, and whitespace is
  normalized. The URL slug is unaffected in shape — it is still derived
  separately, with a generated fallback for names that contain fewer than two
  URL-safe characters.
- c2c59c8: Let organization admins set an organization-wide automatic remote session refresh policy (Disabled, User controlled, or Required) from the MCP Connections page, and surface the effective policy to end users on the OAuth consent screen. Required keeps every eligible connection refreshed and shows the consent control locked; Disabled stops background refresh and states it read-only so users know idle connections will lapse.
- 530feba: Attach files to the Project Assistant. The composer accepts files from the paperclip or by dropping them anywhere on the chat, and the assistant can read them: images and text-like files (including OpenAPI specs) travel with the turn, and anything it cannot read inline comes with a short-lived download link.
- 8ae2c53: Revoke Remote Session credentials upstream via RFC 7009. Remote session issuers gain a `revocation_endpoint`, discovered from the issuer's RFC 8414 metadata document during issuer refresh. When a Remote Session is revoked, Gram now posts the stored token to the issuer's revocation endpoint so the upstream authorization server drops it, instead of leaving a live access/refresh pair that keeps working elsewhere until it expires on its own clock.

  This covers every path that ends a session: revoking one session, revoking all of a client's sessions, deleting a client, which cascades a soft-delete to its sessions, and the consent screen's per-provider "Disconnect" — the one an end user drives rather than an admin. Batches run under bounded concurrency and a single budget for the whole batch, since every session on a client shares one upstream host.

  The upstream call is best-effort by construction: it runs after the local revoke has committed, is bounded by a short timeout, and routes through the guardian egress policy. Failures are logged and metered, never surfaced to the caller — the local revoke is the security control the caller asked for and it has already succeeded. Issuers that advertise no revocation endpoint are recorded as a distinct `skipped` metric outcome rather than folded into success or failure, since that is the expected case for a large share of upstreams. A batch that exhausts its budget before reaching every session records the remainder as `dropped` rather than passing them off as done.

- 82f8bbc: Propagate retroactive risk-exclusion changes into ClickHouse: creating, updating, disabling, or deleting an exclusion now rewrites the affected findings' effective state in the ClickHouse store as well as Postgres.
- a2b272c: Warn when an identity provider duplicates an issuer URL that already exists, at all three tiers and on both create and edit. The warning is advisory and never blocks the write, since duplicating an issuer has legitimate use cases.
- e8d3459: Score Watchdog signals from the matched risk policy's configured score, and simplify the signal drawer to a single Create-exclusion action.

### Patch Changes

- 37c036b: Stop the admin organizations list from returning a next-page cursor on the last full page. When the number of matching organizations was an exact multiple of the page size, the final page still carried a cursor, so the next page came back empty. The endpoint now reads one row past the page to decide whether a next page exists.
- 341be47: Attribute Cursor usage events to sessions by decoding conversationId into the standard GenAI conversation attribute.
- 8f8f280: Stop Codex hooks failing with exit code 127. The hook command Codex caches for
  a session pointed at one versioned plugin cache directory, so a background
  plugin refresh that swapped that directory out left the session invoking a
  bootstrap script that no longer existed — the shell reported "command not
  found" and Codex surfaced it as `SessionStart`/`UserPromptSubmit` failures. The
  bootstrap now persists itself and its deployment config together in Codex's
  version-independent plugin data directory. Once ready, hook commands execute
  that stable bundle and use the installed payload only to refresh it; the newest
  cache sibling remains a first-run migration fallback. Unix and Windows both
  honour the configured install-failure policy with an explanatory diagnostic
  instead of an opaque missing-command failure.

  Also fixes the trusted-hash computation for Codex hooks: it was serialising the
  command with Go's HTML escaping, so any command containing `>`, `<`, or `&`
  hashed differently than Codex computes it and the hook was silently dropped as
  untrusted.

- 8589630: Fix employee usage pages under-reporting tokens and cost. Ingest attributes a
  person's telemetry two different ways — hook events resolve the sender's email
  to a Gram user id and carry both, while the rows that actually carry tokens and
  cost (Claude/Codex OTEL and the Anthropic, Codex and Cursor usage imports) carry
  only the provider account's email. The employee-scoped queries matched a single
  collapsed identifier, so they saw one shape and silently dropped the other: an
  employee page could show sessions and tool calls next to zero tokens and zero
  cost.

  The per-user metrics summary, observability overview, time series, tool
  breakdown and data-flow graph now scope to the employee's whole identity set —
  their Gram user id, their directory email, and the emails of their linked AI
  accounts — resolved from the user directory rather than from telemetry row
  identity, so a stray row cannot hand one person another's usage. Personal
  accounts benefit most, since they usually sign in with an email that differs
  from the directory one and previously joined to nothing. The per-user metrics
  summary also selects cost and cache tokens, which its response has always
  carried but the query never populated.

- 4556bf0: Serve challenge-log buckets from the pre-aggregated ClickHouse summary and
  paginate resolved and unresolved results in ClickHouse.
- 0dd2a37: Serve the hooks@0.3.21 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- abcde04: Logging out now returns `Clear-Site-Data: "cookies", "storage"`, so the browser drops the session cookie across the origin's registrable domain and empties localStorage, sessionStorage, IndexedDB and Cache Storage. Previously teardown relied entirely on an expiring `Set-Cookie` plus a best-effort localStorage sweep, both of which leave data behind when a cookie attribute drifts or a page navigates away mid-logout. The theme preference and project favorites still survive a logout: the dashboard reads them before the request goes out and writes them back once the response lands.
- f65e1ea: Internal groundwork for the MCP approval workflow: summarise what an MCP tool declares it can do, from its MCP annotations and the shape of its input schema. No user-facing behaviour yet.
- 8b09caf: Internal groundwork for the MCP approval workflow: resolve an observed MCP server reference — a remote URL or a stdio launch command — into a stable artifact identity, and record whether that reference names an exact version. No user-facing behaviour yet.
- 3650129: Record an actor display name on audit entries written from organization-less sessions. `sessions.Authenticate` now populates the actor email for sessions with no active organization, and enterprise-trial arming threads the actor email through provisioning so the self-signup callback — which has no auth context — still attributes the `organization:enterprise_trial_armed` entry instead of storing a bare actor id.
- d553497: Route authorization challenge logging through Pub/Sub before persisting events
  to ClickHouse. This decouples authorization request paths from ClickHouse
  availability and makes message redelivery idempotent by challenge ID.
- b05b32b: Slack tool calls that a caller has to fix are no longer reported as server
  errors. The Slack Web API answers HTTP 200 with `ok: false` for essentially
  every argument, permission, and state problem, so a thread timestamp that names
  no thread, a channel the bot was never invited to, a token missing a scope, or
  blocks that fail validation all arrived as untyped failures and were logged at
  error level by the MCP tool layer. A single misconfigured client emitting a few
  hundred of those an hour was enough to hold the component's error-log anomaly
  monitor at its threshold for a whole alert window, masking any genuine
  regression behind it.

  Slack refusals now carry the envelope error code, and the MCP tool boundary
  answers a caller-attributed failure with a 400 logged at warn — still recorded
  with the upstream code and the tool name, and no longer marking the request span
  as errored — while Slack's own failures (`internal_error`, `ratelimited`,
  5xx responses) keep error severity. `platform_slack_add_reaction` also treats
  `already_reacted` as the successful no-op it is, since the reaction the caller
  asked for is already on the message.

## 1.8.0

### Minor Changes

- 4c6fc7f: Widen the assistant runtime's message representation from plain strings to structured text|image content parts, end to end through the Go runtime and the Rust runner. Message content is now a string-or-parts union on both sides of the runner wire protocol (back-compat in both directions), history replay prefers the structured content captured at store time over the plain-text projection, turn requests gain an optional `input_parts` field, and chat persistence strips inline `data:` image bytes to text placeholders before anything is written at rest. Behavior-neutral groundwork: nothing produces image parts yet.
- a85b554: Fetch Slack images server-side and inject them as vision content in assistant turns. Image attachments on a triggering Slack message (up to 4 files, 8 MiB total per turn) are downloaded concurrently through the authenticated Slack API, validated by magic-byte sniffing against an image allowlist (png/jpeg/gif/webp, 10 MiB per file), and attached to the turn as `image_url` input parts with `data:` URIs. For images referenced later in a thread, the assistant gains a generic `inspect_asset` runner tool that fetches any directly reachable image URL, validates it, and attaches it to the conversation as a user message; a new `platform_slack_get_file_url` tool bridges Slack's credentialed downloads by minting a short-lived, sealed download URL served by the Gram server's Slack file proxy. Image bytes live only in the live inference path — persistence continues to sanitize `data:` URIs to text placeholders at rest.
- 91f8234: An organization whose enterprise trial has ended now lands on a page that says so and books an upgrade call, instead of the generic book-a-demo screen a company that had never heard of Gram sees. Anyone still inside a trial can reach the same page from the sidebar countdown to upgrade early.
- c95b913: Add the `risk.getSignals` endpoint and finding attribution ingest backing the internal Watchdog page.
- 3705830: Scan captured skill manifests for prompt injection at capture time and show current-version findings on skill details. Admins can configure the existing Prompt Injection policy from the Skills page. A completed judgement records either a finding or clean coverage; unavailable judgements are retried on a later activation and never become durable clean results. Scanning never fails the upload. Coverage is usage-based rather than catalog-based, so a version no agent ever loads is never judged.
- ca5adf0: Carry Slack file attachment metadata through trigger ingestion into the assistant turn context. Message events that share files (e.g. the `file_share` subtype) now surface each attachment's id, name, mimetype, and size in the turn's message-context block, and the `files` list is addressable from Slack trigger CEL filters. Metadata only — file contents are not fetched.

### Patch Changes

- 85f9ddc: Assistant runtimes on the GKE backend now roll onto the configured runtime image automatically. Previously a claimed sandbox kept its admission-time image forever unless the assistant went idle long enough for the inactivity janitor, so regularly used assistants never picked up deployed runner changes — and even a fresh claim could adopt a warm-pool pod pre-warmed on an older image. The deploy-time image recycle sweep now covers GKE runtimes (idle-gated claim re-adoption), turn admission recycles a stale claim lazily and drains stale warm-pool pods with bounded retries, and persisted runtime metadata records the pod's actual image instead of the configured one.
- 2e00c71: Custom domain ingresses now route /shared/skills to the Gram server, so public skill share pages resolve on custom domains instead of returning an edge 404. Existing domains pick up the new route on their next ingress re-apply (e.g. saving domain settings).
- 6102452: Send lifecycle emails to organization admins when Enterprise trials start and approach expiration through the Loops workflow integration.

## 1.7.0

### Minor Changes

- 5027338: The MCP server Clients and Sessions tab now leads with active session and client counts, and renders both listings as searchable, filterable, sortable tables paginated ten rows at a time, with member avatars and creation dates on sessions. The clients table reports how many active sessions each client holds, backed by a new `active_session_count` field on the user session clients API, and clicking that count narrows both listings to that client behind a clear-filter bar.
- 374394a: The user-session OAuth authorization server now emits the RFC 9207 `iss` parameter on every authorization response, success and error alike, and advertises `authorization_response_iss_parameter_supported` in its metadata document. This satisfies the MCP 2026-07-28 Authorization Response Validation requirement and lets MCP clients holding concurrent flows against several authorization servers detect a mix-up attack.

### Patch Changes

- 19ca2a8: Keep shadow MCP risk finding descriptions generic instead of naming the tool that was called.
- 1fa0caf: Surface that Claude Cowork still needs its own manual setup step when Device
  Agent is selected on the "Instrument agents" onboarding step — Device Agent
  only covers coding assistants running on the developer's machine, not
  Cowork's cloud sandbox. The new note links straight into the Manual Setup
  flow for Cowork.

  Also aligns MDM vendor wording with the Iru rebrand ("Iru (formerly Kandji)")
  across the Device Agent setup page and Codex onboarding copy, matching the
  naming already used on the MDM integrations page.

  Conversation events (`UserPromptSubmit`/`Stop`) are now also written to
  ClickHouse telemetry so the onboarding "Confirm traffic" feed shows prompts
  and assistant replies, not only tool calls.

## 1.6.0

### Minor Changes

- e136806: MCP server detail pages gain a Clients and Sessions tab, which lists the clients registered against the server's session issuer alongside its active sessions, and takes over the user-sessions listing that previously sat under authentication settings. CIMD-resolved OAuth clients are now distinguished from DCR-registered ones in the dashboard.
- 44d50c3: Signing up for Gram now starts a 14-day enterprise trial. Your new organization opens on the enterprise tier with the full enterprise feature set enabled, and the trial window is recorded so the tier returns to free when it ends.
- 46a645f: Platform admins can now consolidate an organization's remote identity provider onto the shared platform catalog entry for the same upstream. A new Convergence tab on a platform provider lists the organizations running their own provider for that upstream, along with how many clients would move and any metadata differences, and consolidating one re-points those clients without anyone having to sign in again. Providers whose issuer URL differs only by a trailing slash or an explicit default port now count as the same upstream, since those near-duplicates are the ones most worth folding together.
- 6ff5ca0: Add user-opted-in automatic remote session token refresh and hide its organization settings until enabled.
- 76592ef: Remove the legacy OAuth proxy provider system now that toolsets are migrated to user session issuers: the `/oauth/*` proxy serving path (its token endpoint now answers `invalid_grant` so clients holding proxy refresh tokens re-authorize against their user session issuer instead of exchanging stale tokens indefinitely, outside issuer consent, session duration and revocation; authorize and register are gone), the `oauth/providers` package, the proxy management endpoints (`toolsets.addOAuthProxyServer` / `updateOAuthProxyServer`), the throwaway migration enablement (`remoteSessionClients.cloneClientFromOAuthProxyProvider`, `userSessionIssuers.migrateLegacyGramRegistrations`), the `AdditionalCacheKeys` cache fan-out mechanism, and the OAuth-proxy audit _emit_ path (historical audit entries still render). `external_oauth_server_metadata` is unaffected. The `oauth_proxy_*` tables and the `toolsets.oauth_proxy_server_id` column are left in place for a later data-drop migration.
- 740746e: Publish compatible Cursor and Codex plugins from a shared Agent Plugins 1.0 package and expose compatibility on plugin responses.
- 16e3bf7: An organization that starts a Gram trial now receives $50 of chat credits instead of the full enterprise amount. Gram applies the same $50 ceiling to the inference it runs on the organization's behalf, such as chat titles and risk analysis. A trial that is already in progress keeps the credits it has.

  The billing page now shows the credit ceiling that is set for your organization. An organization whose ceiling was raised sees that amount instead of the standard amount for its plan.

- 535b669: Enterprise trials now close themselves. An hourly job finds each trial organization whose trial window ended without converting to a contract, returns it to the free plan, drops it from the whitelist, and disables its platform model key so it can no longer spend. Every demotion is written to the audit log under `organization:enterprise_trial_demoted`. A trial that converts before it expires is never touched, and a trial is demoted at most once.

### Patch Changes

- 622942f: Remove legacy deny-effect RBAC grants from the server. Authorization exceptions
  continue to use explicit exclusion scopes, preserving existing dashboard rule
  behavior while simplifying grant storage and evaluation to allow-only rows.
- 1d42bb6: Stop double counting Codex/ChatGPT compliance COSTS events. The feed repeats
  the same `event_id` across log files, and `telemetry_logs` has no uniqueness
  constraint, so each repeat was imported as its own row and inflated every
  token and cost aggregate downstream. The importer now checks the
  `codex.compliance.event_hash` fingerprint it already stamps against rows
  already ingested for the project and drops the repeats, which also makes
  re-polling a window idempotent instead of doubling it.
- 6110071: Support compressed Git upload-pack requests through marketplace URLs.
- ea9a0e4: Serve the hooks@0.3.19 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- 8e56f5a: Prevent organization-less login sessions from failing RBAC grant preparation.
- b5b9a6a: Keep Cursor and Codex marketplace entries on their native package formats while publishing Agent Plugins packages additively.
- aa981b7: Reject organization names that contain fewer than two letters or numbers. A name made up entirely of punctuation, such as `-----` or `___`, previously passed validation and produced an organization whose URL slug was empty. The rule is enforced by the shared name check, so it covers both the sign-up parameter on login and the register endpoint, including requests that skip the sign-up form. Existing organizations are unaffected.
- 793abde: Public skill share links now use your custom domain when one is verified and activated. The share page (and a raw SKILL.md download) is served at `https://<your-domain>/shared/skills/<token>`, scoped so a domain can only ever serve skills belonging to its own organization, and the dashboard copies links with the custom domain automatically.
- 80227fa: Trigger delivery telemetry logs now record a proper `trigger-instance:<uuid>` URN and the active trace context instead of a generic `urn:uuid:` identifier with empty trace and span ids.

## 1.5.0

### Minor Changes

- 546c449: Collect a work email on the sign-up page and hand it to the hosted AuthKit
  screen. `auth.login` takes an optional `email`; when a login carries a company
  name — the marker that it began on `/sign-up` — the server sets WorkOS's
  `login_hint` so the email field arrives pre-filled, and `screen_hint=sign-up` so
  the user lands on the sign-up screen rather than sign-in. The email is validated
  before the login nonce is minted and is never stored. The call to action now
  reads "Start Trial"; it previously named a single identity provider, which
  misdescribed a hand-off that has always been generic.
- 0afb752: Import ChatGPT conversations from the OpenAI Compliance Logs Platform. A new `chatgpt_compliance` AI-integration provider polls workspace-scoped `CONVERSATION_MESSAGE` log files (the supported successor to the deprecated stateful conversations endpoint) and persists them as external chats and messages — the same tables and Agent Sessions surface the Anthropic compliance import feeds. The provider is separate from `codex_compliance` because the scopes differ: COSTS files are per API organization while conversation logs are per ChatGPT workspace, so the new config takes a workspace UUID. Includes the workspace-scoped compliance client, Temporal schedule wiring, and a "ChatGPT Conversations" integration card in org settings.
- 2a6e703: Gram Session OAuth issuers can now control which OAuth Client ID Metadata Document clients they accept, decided before any document is fetched. An issuer can admit Gram's curated catalog of verified MCP clients (Claude Code, Claude, VS Code, Zed, Goose, ChatGPT, Codex CLI, Notion, MCPJam, Factory Droid, ToolHive) plus any URLs configured on it, admit any spec-valid client, or admit none at all; the new `userSessionIssuersCimdClients` service lists the catalog and manages per-issuer URLs, and the admission mode is readable and writable on the existing `userSessionIssuers` endpoints. Issuers that have not chosen a mode currently record what the curated policy would have decided without enforcing it, so nothing changes for existing clients while the platform gathers evidence that enforcement is safe to make the default. Separately, a metadata document that omits `token_endpoint_auth_method` is now accepted as a public client rather than rejected, matching the spec and unblocking clients such as ChatGPT and Codex CLI whose documents omit it.
- 7fd5e1a: Classify Codex account identity and billing mode (DNO-734). Codex sessions on
  every capture path (legacy hooks, OTEL logs, ingest adapter) now stamp
  account_type from email resolution — resolved work email is team, anything
  else personal — and team sessions resolve the org-level billing mode declared
  on the codex_compliance integration config (the session provider "openai" now
  maps to that config, fixing the mapping bug that made the config's
  billing_mode unreachable). Compliance COSTS import rows (codex and
  ChatGPT/Work) carry account_type=team and the config's billing mode directly.
  The estimated-cost tooltip copy mentions ChatGPT plans alongside Claude's.
- b9590ce: Meter Codex cloud usage (GitHub code review, web tasks) from the compliance
  COSTS feed — those surfaces have no OTEL stream, so their token counts now
  promote to `gen_ai.usage.*` and count toward TUM. Device clients keep
  metering via OTEL, and unrecognized clients stay un-metered so a new surface
  cannot silently double count.
- 49e00bb: Import Codex cloud task transcripts as agent sessions (DNO-752). A new
  codex_cloud_sessions schedule on the chatgpt_compliance integration polls the
  workspace-scoped CODEX_LOG compliance feed and persists cloud web-task
  prompts and responses as external chats + messages under the new codex-web
  chat source, with prompt-derived titles and idempotent replays. Only
  CODEX_WEB client events are imported (desktop-app events are counted and
  skipped pending the unified-app verification), and the feed's per-turn token
  counts are deliberately not persisted — cloud tokens meter through the
  compliance COSTS promotion, so carrying them here would double count.
  Enforcement over cloud runs remains impossible (post-hoc batch feed); this
  provides visibility and post-hoc review only. Also fixes a latent
  multi-schedule reset gap: a key or external-scope change on an integration
  now resets every synced sibling schedule's watermark (previously only the
  provider-named schedule reset, so a workspace/org change could leave a
  sibling feed silently skipping the new scope's history).
- c44a461: Extend spend-gate enforcement to Codex and Cursor at parity with Claude. Over-budget actors are now denied on the legacy provider endpoints (`hooks.codex`: PreToolUse, PermissionRequest, UserPromptSubmit; `hooks.cursor`: preToolUse, beforeMCPExecution, beforeSubmitPrompt) and on the unified `hooks.ingest` path for the codex and cursor adapters (case-insensitive match) — previously the ingest spend gate was Claude-only even though risk scanning already ran adapter-agnostically there. Cursor MCP calls are spend-gated exactly once (at beforeMCPExecution, mirroring the risk-scan dedup), tool-call spend denies mint a durable block page whose link rides the deny reason, idempotent redeliveries keep the deny without minting duplicate block rows, and the block page headline falls back to spend-rule framing instead of rendering an empty policy name. The gate keeps running before any risk-policy evaluation and failing open on infrastructure errors; opencode still passes through pending a product decision on its enforcement surface.
- 21f99b0: Add `GET /v1/install/device-agent-macos.pkg`, a stable redirect to the current signed macOS device-agent installer. Resolves the current version from the public device-agent releases manifest server-side and 302s to the versioned pkg, so docs and IT-admin instructions can link to one URL instead of hardcoding a version that goes stale every release.
- 54755b5: Add organization-level device-agent remote configuration to the existing agent
  policy response, with admin management endpoints, versioning, validation, and
  audit logging.
- 28150a9: Add authenticated OTLP trace ingestion for LiteLLM telemetry.
- 13301b5: Add a self-serve path into the shared read-only demo organization. A new `auth.enterDemo` endpoint switches any authenticated session into the demo org (no membership required); request auth, grant resolution, and member/role listings gain demo carve-outs; the demo org always enforces a fixed read-only scope set with a verb-based write guard as backstop. The dashboard gains an `/explore-demo` entry route, an "explore a live demo org" link on the book-a-demo gate, and a demo banner whose exit switches back to the user's own organization without logging out.
- 07b95b5: Expose health and attribution diagnostics for provisioned LiteLLM integrations.
- f926dc1: Add project-scoped LiteLLM integration provisioning, key rotation, revocation, and lifecycle metadata APIs.
- 544c23a: Accept opt-in LiteLLM OTLP operational metrics without adding them to usage billing or sessions.
- 9081d00: Add Microsoft Teams as an assistant trigger source. Bot Framework activities (messages, reactions, membership and installation updates) posted to a trigger webhook are verified against Microsoft's signing keys and dispatched to assistants with the same filtering (event type allowlist + CEL) as other webhook triggers.
- 1d0aafd: Add an OpenRouter platform key lockdown. A locked-down key fails at key resolution with a distinct `inference_disabled` error rather than an upstream rejection, and a limit refresh reinstates it.
- 909b466: The External Services page is now organization-scoped: org admins register how Gram authenticates into their own cloud account, behind a new `customer_managed_encryption_keys` entitlement enforced on both `externalCredentials` and `externalKeys`. The platform-admin UI is removed, though its endpoints remain for HTTP-only management. Two new methods support verification: `externalCredentials.verifyGcpIam` probes that Gram can actually impersonate the named service account, and `externalCredentials.getGcpSetupInfo` reports the Gram service account a customer must grant `roles/iam.serviceAccountTokenCreator` to.
- f95d50f: Platform admins can now curate the shared remote identity provider catalog from the dashboard, under a new Platform Admin section in the sidebar: list, create, edit, refresh discoverable metadata, and delete the providers that every organization inherits. The listing reports platform-owned and tenant-owned client counts separately, so a delete that will be refused says up front which blockers the admin can clear and which belong to an organization. `adminRemoteSessions.listGlobalIssuers` and `adminRemoteSessions.getGlobalIssuer` now return both counts alongside the issuer. Organizations can register a client against an inherited platform provider straight from their own provider list.
- 869a89b: Substitute the OAuth callback URL into the setup guide content that `mcpRegistries.getSetupDocs` returns. Published guides ship with a `{{ gram.oauth.callback_url }}` template key wherever the reader has to register a redirect URI on an upstream provider's OAuth app. The endpoint now replaces that key with this deployment's remote-login callback URL, so `external_markdown` and `speakeasy_markdown` carry a value the reader can paste directly.
- 546c449: Add a `/sign-up` page that collects the company name before handing off to the
  identity provider. `auth.login` takes an optional `org_name` param; when set, the
  server validates it and stashes a signup intent against the login nonce, then
  creates the organization during the auth callback once the identity provider has
  answered. The name never travels through a redirect param or the address bar, and
  a failed signup returns to `/sign-up` rather than `/register`. Signup attempts and
  the resulting org creation are captured as `onboarding_event` / `new_org_created`
  with `created_via: "signup"` so the funnel can be measured end to end.

### Patch Changes

- 02da0b1: Apply shadow-MCP policy to Codex's built-in MCP resource tools. Codex reaches
  MCP servers through three meta-tools — `list_mcp_resources`,
  `list_mcp_resource_templates` and `read_mcp_resource` — that carry no `mcp__`
  prefix and name their target in `tool_input.server`. The unified ingest
  endpoint decides whether a call is an MCP call from resolved MCP data or an
  MCP-shaped tool name, and neither recognizes these, so they were classified as
  ordinary tool calls: the risk scan ran but the shadow-MCP policy never did. A
  `block_all` policy therefore did not stop a Codex session from reading any MCP
  server's resources, while the legacy Codex endpoint denied the same call.

  The gate now recognizes them for the codex adapter, and the named server is
  resolved against the session's MCP inventory so a Gram-hosted target is still
  allowed and a denied one is named. A meta-tool whose server cannot be resolved
  is denied rather than allowed — an unproven target is not an absent one.
  Sessions now cache their MCP inventory on the ingest path under the same key
  and TTL the legacy per-provider endpoints use.

  Rolled out on client capability rather than deploy order: releases before this
  one report no adapter version and no MCP inventory, so enforcing on them would
  deny every meta-tool call — including reads of Gram-hosted servers that work
  today. Those clients keep their current behavior and are counted in the logs
  until they upgrade.

  A capable client that reports no inventory is denied. That can mean no MCP
  servers are configured, but collection is best-effort and also comes back empty
  when the codex binary cannot be located, `codex mcp list` fails, or the
  session's inventory never reached the cache — in which case a meta-tool call is
  denied even though servers are configured. That is the intended fail-closed
  posture rather than an accident: the guard cannot clear a target it cannot see.

- 5fb7ccb: Classify Codex OTEL rows as provider OTel telemetry, matching Claude's. The
  canonical event URN had cases for `claude-code:otel:logs` but none for
  `codex:otel:logs` or `codex:otel:metrics`, so Codex's provider-native stream
  fell through to the agent-hook default and was typed
  `urn:telemetry:agent_hook:log:unknown` — with no event name, since those rows
  carry a producer `event.name` rather than a Gram hook event. Any filter that
  selects provider-OTel rows by URN prefix therefore excluded Codex while
  including the equivalent Claude traffic. Codex OTEL logs now type on their
  producer event name (`codex.sse_event`, ...) and metric points on their
  metric name.
- 5fb7ccb: Route Codex OTEL telemetry from every client mode to the Codex stream, not
  just the interactive CLI. Codex reports a different OTEL `service.name` per
  mode and does not use one separator convention — `codex_exec` for headless
  `codex exec` (what CI and scripted runs use), `codex_tui`, `codex_mcp`, and
  `codex-app-server` for Codex mode in the unified ChatGPT desktop app — but
  the ingest matched only `codex_cli_rs`. Those payloads were not dropped: they fell through to the
  Claude path and were persisted as `claude-code:otel:logs` rows carrying
  Claude's hook source and account attribution, so Codex traffic silently
  inflated Claude surfaces while never being metered as Codex usage. The
  ingest now matches the whole Codex service-name family, both separators
  included.

  Routing is also now per OTEL resource rather than per payload: a collector
  that fans several clients into one export previously had the whole batch
  routed by whichever client matched first, mislabeling the other client's
  records.

- e062cd3: Probe the unified ChatGPT desktop app when installing the Codex plugin
  (DNO-737). OpenAI merged the standalone Codex app into the ChatGPT desktop
  app, which ships the codex CLI at
  `/Applications/ChatGPT.app/Contents/Resources/codex`; the install script only
  probed the legacy `/Applications/Codex.app` path, so on any machine with just
  the post-merge app it failed to find the binary and degraded to printing
  manual instructions. The unified bundle is now probed first, with the legacy
  path kept for pre-merge installs.
- a3735b7: Stop rejecting current Codex clients at the Figma MCP allowlist (DNO-765).
  Codex renamed its MCP client User-Agent — 0.144 sent `codex_cli_rs/…`, the
  0.146 unified-app build sends `codex-mcp-client/…` — and the allowlist only
  carried the old token, so every Codex → Figma MCP call proxied through Gram
  was rejected as an unapproved client. Both tokens are now listed so neither
  older deployed clients nor current ones are blocked.
- 5b97690: Serve the hooks@0.3.13 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- 82a0689: Stop reading an unreadable MCP inventory as proof a session has no MCP servers.

  The Codex meta-tool shadow-MCP guard denies a call it cannot clear against the
  session's inventory, so an empty inventory decides whether legitimate traffic is
  blocked. But "the agent has no MCP servers configured" and "we could not read
  the list" both arrive as zero entries, and only the sender can tell them apart —
  collection is best-effort and comes back empty when the agent binary cannot be
  located or the probe fails. Hook events now carry `mcp_inventory_collected`, and
  the guard enforces only on a list that was actually read. Senders predating the
  field omit it and keep their current behavior until they upgrade.

- 0ea5ffd: Classify idle-timeout terminations of proxied MCP SSE streams correctly. The standalone GET listen stream ending on the proxy's 60s idle bound is now a clean close (200, no error log) instead of a 500 "unexpected error" — clients reconnect per spec, so quiet upstreams no longer produce one spurious 5xx per minute per connected client. A POST response stream going idle mid-reply now returns a 502 gateway error naming the idle bound instead of a bare context cancellation. Access logs also no longer relabel an already-committed response's status when a late error-path WriteHeader fires.
- 4491521: Scope the shared LLM judge rate limiter to the OpenRouter key a call spends: platform-key calls share one bucket per model (matching OpenRouter's account-wide shared-capacity limits), while BYOK calls bucket per customer key. This stops chat analysis and risk judges from exhausting OpenRouter's per-model capacity and failing with 429s.
- 0eed8b8: Accept dedicated MCP inventory events and cache explicit empty snapshots.
- 817174d: Make RBAC always on, provision built-in roles and grants for new organizations,
  and assign the first organization user the Admin role.
- 81ba8cf: Search agent sessions by resolved member and AI account email addresses.
- 68f1afa: Internal changes to risk finding reveal.
- 9c784b9: Keep an enforcement block even when its optional links cannot be resolved. The
  block URL is handed to the agent before the row is written, so a rejected
  insert leaves the user opening a page that does not exist. Enforcement runs
  before the hook's chat and finding rows are persisted, so a block early in a
  session races its own chat row and the foreign key rejects — silently, since
  the insert is detached and only logged. The write now drops whichever link the
  database names and retries, so the block always lands and only its enrichment
  is lost. Applies to every provider: all block paths share this writer.
- a2da454: Fix a live tool-listing probe for unproxied MCP servers taking up to a minute or more against an unreachable vendor server instead of the intended ~10 second bound. Two independent retry layers (HTTP-transport retries and the MCP SDK's own reconnect retries) compounded on top of each other, and the SDK's own cleanup after a context deadline could itself run well past that deadline. The probe now disables both retry layers for this one-shot check and time-boxes the response to the caller independently of how long the SDK's internal cleanup takes.
- c61c3f2: Fix a credential leak in published plugin configs: an unproxied MCP server (one whose URL points directly at a vendor, never through the platform's own gateway) could have the org's API key attached as a static Authorization header, sending it straight to the third-party vendor's server instead of the platform. Unproxied servers now carry no Gram-managed credential in any generated client config (Claude, Cursor, Codex, OpenCode), and no longer trigger an unnecessary API-key prompt during install.
- ba561ad: Webhooks are now available to every organization, marked Beta. The Webhooks page
  no longer shows a preview gate, and delivery is controlled solely by the
  organization's own webhooks toggle.

## 1.4.0

### Minor Changes

- ca05169: Add an authenticated LiteLLM Generic Guardrail endpoint that enforces prompt policies before model calls and captures blocked prompts.
- 7f4e1b8: Capture LiteLLM model responses with consistent per-call session and user attribution for asynchronous risk analysis.
- 73b9590: Requests and approvals now understand allow-all shadow MCP policies. Approving a bypass request (from the approvals page or the inventory review flow) on an allow_all policy revokes the server URL's risk_policy:block grant — a project-wide unblock with no principal-scoped bypass grants. Revoking restores the block grant; denying leaves it untouched. The request status change is audited like every other bypass-request decision. The approval UIs skip the audience/policy pickers for these requests and explain that approval unblocks the server for everyone in the project.

### Patch Changes

- 798db6b: The project assistant can now distribute the skills it creates. `platform_distribute_skill` and `platform_undistribute_skill` attach a skill to a plugin or assistant and revoke it again, and `platform_list_plugins` resolves a plugin by name to the ID they take. All three reuse the same permissions, feature gating, and audit logging as the dashboard.
- bd3aac6: Shadow MCP inventory and status surfaces are now disposition-aware. Under an allow-all policy the inventory reports servers as allowed by default and blocked only when a block rule lists them, the policy status banner explains the allow-by-default posture, and the primary per-server action flips from managing allow rules to Block Server / Unblock Server — which add and remove the policy's risk_policy:block grants through dedicated inventory endpoints.

## 1.3.0

### Minor Changes

- 18bc769: Enforce allow-all shadow MCP policies in the hook path. Under an allow_all policy every non-Gram-hosted MCP server is permitted unless a risk_policy:block grant names its URL; bypass grants and the fail-closed inventory checks remain block_all concepts and are skipped. Projects are now limited to one enabled shadow MCP blocking policy so dispositions can never conflict at enforcement time.
- becc03b: Add `shadow_mcp_blocked_urls` to risk policy create/update payloads. Allow-all shadow MCP policies carry a canonical blocked-URL list stored as `risk_policy:block` RBAC grants held by the all-users principal — the mirror of `shadow_mcp_allowed_urls`, which reconciles into `risk_policy:bypass` grants on block-all policies. The two lists are disposition-exclusive: blocked URLs are only valid on allow_all policies and allowed URLs only on block_all policies. Blocked URLs may name servers not yet observed in the project inventory (proactive blocking).

### Patch Changes

- 679d489: Let the project's managed assistant act on risk findings, not just read them.
  Adds `platform_list_risk_exclusions` and `platform_create_risk_exclusion` for
  suppressing a whole class of findings, plus
  `platform_mark_risk_false_positive` and
  `platform_unmark_risk_false_positive` for dismissing (and restoring)
  specific findings. The writes go through the same risk service methods the
  dashboard uses, so they stay gated on org admin and audited against the
  invoking user. Exact and regex match values are fingerprinted before they reach
  the model, so `platform_create_risk_exclusion` reuses an equivalent existing
  exclusion rather than duplicating one the model had no way to recognise.

  Also keeps the assistant's context from ballooning while it triages: the
  agent-facing findings listing now defaults to 25 results and caps at 50
  (a 200-row page was tens of thousands of tokens that stayed in context for the
  rest of the turn), and the new `platform_get_risk_rule_breakdown` answers
  "which rules fire most" in one small call instead of many large pages.

- 6ca548f: Add the `chatgpt` and `chatgpt-work` sources to the product-surface taxonomy now that ChatGPT/Work compliance usage is admitted to the summaries. The compliance importer now routes Work rows to the `chatgpt-work` hook source (ChatGPT and unknown surfaces stay `chatgpt`) so the per-product split survives summarization — hook_source is a summary GROUP BY dimension while the raw `codex.compliance.product` attribute is not, and summaries outlive the raw-row TTL. Also: a `chatgpt` chat source alias, ChatGPT/ChatGPT Work labels and the OpenAI mark in the dashboard label/icon maps and onboarding live-tail, broadened "OpenAI Compliance Logs" settings copy, and local seed fixtures emitting compliance-shaped `chatgpt:usage:metrics` rows for both products.
- f8ff561: Codex-product compliance COSTS rows (`codex:usage:metrics`) now meter cost only. Their token counts previously rode on `gen_ai.usage.*` keys, which the ClickHouse agent-usage predicates sum on top of the Codex OTEL stream — the token source of truth — double counting token metering for orgs running both feeds. The raw counts are preserved under new `codex.compliance.*_tokens` attributes (summed by nothing) because the compliance feed also covers surfaces OTEL never sees (cloud-delegated tasks, GitHub code review); a future surface-partitioned metering pass can promote them. Parked non-Codex rows (`chatgpt:usage:metrics`) keep their `gen_ai.usage.*` token counts since the compliance feed is ChatGPT/Work's only usage source.
- 62fce4c: Emit OpenTelemetry metrics for the device-integration sync pipeline: `gram.device_integration.sync.outcome` (sync runs by provider and outcome) and `gram.device_integration.sync.auto_pause` (schedules auto-paused after a streak of credential rejections). These back the sync-failure-rate and auto-pause monitors for the MDM integrations rollout.
- 11b3586: Fix OAuth token exchanges failing with invalid_client against providers that strictly decode HTTP Basic credentials (e.g. Snowflake): client id and secret are now form-urlencoded before being placed in the Authorization header, per RFC 6749 §2.3.1.
- d4d8de2: The project assistant can now create project skills from complete `SKILL.md` content. The new `platform_create_skill` tool uses the same validation, versioning, permissions, feature gating, and audit logging as manual skill creation.
- 9161dc7: Internal data changes to the risk findings backfill tooling.
- af439f5: Internal schema changes to the risk findings store.
- 1dbad64: Internal data changes to risk finding ingestion.
- ae3979c: Admit `chatgpt:usage` rows (ChatGPT/Work usage+spend from the OpenAI compliance COSTS import, previously retained but unread) into the agent-usage predicates of `attribute_metrics_summaries_mv` and `chat_session_summaries_mv`, via atomic MODIFY QUERY migrations. ChatGPT tokens now count toward tokens-under-management and appear in usage/cost analytics going forward, matching how Claude Chat (Anthropic Admin Analytics) and Cursor (Admin API) polled usage already bill. Applies to new rows only — previously parked rows are retained but not backfilled into the summaries. Also updates the stale MV comments that claimed no new `codex:usage` rows are written (the compliance import writes them, cost-only since the token double-count fix).
- 3e1ad9e: Exclude unattributed authz challenges from the challenge buckets endpoint so the Challenges page pagination and totals match what is rendered
- b131cea: Project assistant tool calls now render Claude-style: the assistant precedes each tool batch with a terse activity phrase ("Investigating failures in the last 30 days") which becomes the heading of a single collapsed tool group. Consecutive batches merge into one group whose heading advances (with shimmer) as the investigation progresses, groups never auto-expand, and the global thinking loader hides while a tool group is streaming. The dashboard output-channel guidance instructs the model to emit the phrase before every tool call.

## 1.2.0

### Minor Changes

- c0395b5: Selectively extract high-value, reusable business memories from completed chats, omit semantic duplicates, and add an organization-admin corpus browser with semantic search, source-transcript navigation, and a complete content-scope tree with distinct-memory counts.
- eed5e6a: Add a weekly usage summary email for customer billing contacts. Every Monday a Temporal sweep emails each organization's billing alert contact their tokens-under-management total for the active billing cycle, with a percent-change badge against the same elapsed point of the previous cycle. The TUM token components are now defined once in a `billing.TumComponents` registry that the ClickHouse billing measure and the email's total both derive from, so changes to the TUM definition propagate to billing and reporting in lockstep. Organizations with no usage in either compared window are skipped, and sends are deduplicated per organization and run date.
- e9dc39b: Add `mcpRegistries.getSetupDocs`, which returns the published setup documentation for an upstream MCP server from the `github.com/speakeasy-api/mcp-setup-docs/go` catalog. A guide can be located by the upstream server's endpoint URL, by its registry specifier as returned by `listCatalog`, or by both at once. Matches from the two lookup keys are deduplicated by guide slug and returned in descending specificity, and each guide reports how it matched and which documented endpoint the lookup selected. Servers with no published guide return an empty list rather than an error.

### Patch Changes

- 59a371a: Saving an OpenAI app-submission verification token now reconciles the custom domain's ingress. Domains provisioned before the challenge feature shipped were missing the `/.well-known/openai-apps-challenge` route, so the token endpoint returned a 404 until an unrelated setting change rebuilt the ingress; setting or clearing the token now triggers that rebuild directly.
- 1359212: Serve the hooks@0.3.12 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- 9d0f259: The project assistant can now read the AI control plane product documentation. Two new managed-assistant tools, `platform_list_docs` and `platform_get_doc`, expose the ~110 pages under speakeasy.com/docs/ai-control-plane: the first returns the page index (built from the docs sitemap and cached hourly), the second returns one page's markdown along with its public permalink to cite. `platform_get_doc` only serves paths present in the index, so it reads documentation rather than acting as a general-purpose fetcher.
- 92d743b: Serve the project-wide Risk Events listing from ClickHouse behind the new
  `risk-list-from-clickhouse` per-org flag, keeping the same ordering, filters
  and pagination behavior as before. Also fixes a pagination bug where the first
  result after a page boundary was skipped.

## 1.1.0

### Minor Changes

- 9373aea: Support OAuth Client ID Metadata Documents (CIMD) on the Gram Session OAuth authorization server, gated per organization behind the `gram-user-session-cimd` feature flag. MCP clients that identify themselves with a URL-shaped `client_id`, such as Claude Code and VS Code, can now complete the OAuth flow without Dynamic Client Registration, including loopback redirects on any port.
- 3b66258: Custom domains can now route their root URL to a default MCP server. Pick one of the domain's MCP endpoints as the default and `https://your-domain.com/` serves that server directly — MCP clients connect at the root and browsers see the installation page — while renaming the endpoint's slug updates the routing automatically. Custom domains can also serve an OpenAI app-submission verification token at `/.well-known/openai-apps-challenge`, so ChatGPT app reviews can verify domain ownership without any changes on your site. Both settings live on the custom domain page; the default server can also be set from an MCP server's own settings.

### Patch Changes

- debaf8e: Shadow MCP inventory server names now resolve reliably after renames. Name updates written in quick succession could previously tie on their stored version and intermittently revert to an older observed name; versions are now stored at full nanosecond precision and each update is guaranteed to supersede the state it was based on.
- 80b855f: Stop enumerating supported coding agents (Cursor, Claude Code, Codex, …) in Shadow MCP detector copy and other user-facing product strings. Prefer generic wording so new agents like opencode do not require list updates.

## 1.0.0

### Major Changes

- 228f828: feat: evidence records carry per-device attestation strength

  Pushed Drata/Vanta coverage records replace assignedUserAgentActive /
  assignedUserAgentLastSeenAt with agentActive, agentAttestation, and
  agentLastSeenAt. agentAttestation is "device" when the record is backed by
  that machine's own agent heartbeat (matched on hardware serial) and "user"
  when only its assigned user's, so a single push can carry both strengths
  truthfully. Breaking for the customer-declared Drata/Vanta record schemas.

### Minor Changes

- 2822d51: `remoteSessionIssuers.get` can now look an identity provider up by its upstream issuer URL, returning the one the project would use (preferring project over organization over platform) or 404 when nothing describes that URL yet. The dashboard's automatic setup flows use it to decide whether to reuse an existing provider instead of scanning the provider list in the browser, which also lets them reuse platform-catalog providers for the first time.
- b5f47cb: Auto-provision the Drata Custom Connection on connect. When an evidence-sink provider implements the new optional `Provisioner` capability, the connect flow creates its vendor-side object and stores the resulting ids, so the customer no longer hand-crafts it against the vendor API. Drata implements it: it find-or-creates the dedicated Custom Connection with the exact record schema and `required` list (omitting `agentLastSeenAt` so never-seen-agent records are never rejected), keyed on a deterministic name so a re-save reuses the connection instead of duplicating it. A new optional `workspace_id` field defaults to 1, and `connection_id` becomes optional — filled in automatically.
- 5cfbb83: Expose MCP Client Metadata to Gram Functions tool calls
- 5bf2d45: Select project skills as additional context for an individual Project Assistant turn.

### Patch Changes

- d5e1ea6: Fix three Drata evidence-push defects found running against the live API. The stranded-session sweep failed to decode the session listing (Drata returns numeric session ids inside a data/pagination envelope; the sweep decoded them as strings and misreported the failure via a bare-array fallback) — session ids now tolerate numbers or strings, a null/absent data field counts as an empty sweep, and the envelope's real decode error surfaces. An empty fleet now clears evidence by deleting records directly, because Drata refuses to complete a session with no records. Per-record schema-validation rejections hidden inside 2xx upload responses now fail the push instead of silently publishing a partial fleet.
- 1d888d5: Add `message_created_at` and `assistant_id` columns to the ClickHouse
  `risk_findings` table and stamp them at ingest from the chat-message
  attribution lookup. `message_created_at` (defaulting to scan time for
  pre-existing rows) will let the Risk Events listing sort and paginate by
  event time from ClickHouse; `assistant_id` will power the assistant filter
  without a cross-store join.
- eca5c54: Fix three Vanta evidence-sink defects against the real CustomResource API, all verified live. Every pushed record now carries the required top-level `externalUrl` base field (an omission was rejected with 400). `agent_last_seen_at` is always sent — an empty string when no agent has ever reported, rather than omitted — because Vanta's console cannot author an optional-property schema, so a device-declared record schema marks every property required and an omitted field fails at sync. And the response check now matches Vanta's actual full-state PUT contract — 200 `{"success": true}` on a valid set, 4xx on any schema violation — instead of requiring an `accepted`/`rejected` accounting object the API never returns, which was failing every push.

## 0.95.0

### Minor Changes

- b6d3a27: Add skill feedback metrics, grouped review evidence, and manually triggered suggestion analysis.
- 703756b: Add `fetchMetadata` and `refreshMetadata` across all three remote identity provider tiers. `fetchMetadata` is keyed by issuer URL and persists nothing, as the pre-create step; `refreshMetadata` is keyed by issuer id and re-reads an existing provider's RFC 8414 document, persisting only discovered values (endpoints, the `*_supported` arrays, `client_id_metadata_document_supported`, and the documentation URLs) while leaving Gram's own behavior and display fields untouched. A "Refresh Discoverable Metadata" action is available from the Remote Identity Providers listing.
- 4bf8450: Let tenants inherit and attach clients to platform (global) remote identity providers while the issuers themselves stay read-only, and keep tenant clients on a platform issuer fully manageable through the organization-admin surface. The dashboard renders the new `Platform` tier and resolves issuers by `project > organization > platform` precedence.
- 725bfaa: Skill edit suggestions now support batch apply: select individual proposed changes and apply them together as a single new version. The batch controls moved from the per-change comment box to a control bar above the diff.
- 4225015: Custom domains that stay unhealthy for over a week (7+ consecutive failed daily checks) are now automatically disabled: their routing and TLS certificate are removed, and the dashboard explains what went wrong and walks admins through fixing the issue and reverifying the domain. Gram-side check failures never count toward disabling.
- b89c5ae: Custom-domain health checks go live: daily check results are now persisted and shown in organization settings, and organization admins receive an email the first time a domain turns unhealthy. This removes the observation-only dry-run mode used to validate detection accuracy in production.
- 6f24919: Add Temporal scheduling for device integrations: a five-minute coordinator workflow fans out one child workflow per due sync (workflow-id deduped per org and sync), and a sync runner executes inventory pulls and evidence pushes. Inventory syncs upsert the MDM-reported fleet — resolving assigned emails to org members — and mark absent devices missing only in the transaction that records a fully completed snapshot, so a partial pull can never report unvisited devices as missing. Evidence pushes build the org's coverage snapshot and skip delivery when its digest matches the last successful push. Failures back off exponentially (capped at the schedule interval) and repeated credential rejections auto-pause the schedule; successes clear failure state by contract so recovered schedules render as healthy. Workflow and activity payloads carry sync ids only — credentials are decrypted inside the running activity and never enter Temporal history.
- d3ad7d3: Add the device integrations framework: a capability-based provider registry (`InventorySource` for MDM fleet pulls, `EvidenceSink` for compliance evidence pushes) and a new `deviceIntegrations` management service. Organizations can connect a provider with secret credentials stored as an encrypted write-only document and non-secret settings kept readable, validated against the provider's declared field spec; credential rotation updates the config in place so synced device inventory is never orphaned. The service exposes provider discovery (credential specs drive dashboard form rendering), config CRUD with audit logging, a bounded test-connection probe through the SSRF-hardened guardian client, per-schedule state with distinct user-disable and system-auto-pause semantics, and agent-coverage reads: a bucketed summary (active / stale / no agent / no email / unresolved / missing, plus unmanaged agent users) and a paginated device listing, both computed as read-time joins between MDM inventory and the per-user agent heartbeat.

  Settings updates merge per key with the stored document (omitted keys keep their values), credential rotation resets the schedules' sync execution state and pushed-snapshot digest, and audit before-snapshots are read inside the upsert transaction. The dashboard's OTel forwarding section is updated for a generated SDK type rename.

- 3558aa7: Device integrations: enabling a connection (and "Sync now") triggers the
  sync coordinator immediately instead of waiting for its next tick; the
  configure sheet disables the connection test while the draft has unsaved
  changes and explains that tests run against saved credentials; managed
  device and schedule tables get properly spaced empty states; and the Iru
  provider rejects the tenant console URL with an error naming the correct
  API URL.
- e123ec3: feat: accept and store device-agent hardware identity

  agent.getPlugins now accepts optional Gram-Device-Serial and
  Gram-Device-Hostname headers and records a per-device heartbeat alongside the
  existing per-user one. Coverage is unchanged — this only builds the data the
  device-level join will read.

- 866a555: mig: add device_agent_device_syncs for per-device agent heartbeats

  Sibling of device_agent_syncs, keyed on (organization_id, serial_number)
  instead of email, plus the case-insensitive serial indexes both sides of the
  coverage join will need. Schema only — nothing reads or writes the table yet.

- 432d06c: feat: device-level agent coverage behind a rollout flag

  Coverage can now match a device's hardware serial against per-device agent
  heartbeats instead of its assigned-user email, falling back to email when no
  serial match exists. Adds an `agent_other_device` bucket for "the user runs
  the agent, just not on this machine", and an `attestation` field so clients
  word the coverage claim to match the mode. Gated per org by the
  `device-level-coverage` PostHog flag; evidence pushes stay user-level until
  the sink field names change with them.

- 8457c8a: Add the Drata evidence-sink provider to device integrations: pushes
  per-device agent-coverage evidence into a customer's Drata workspace through
  the Custom Connections API, using batched session uploads whose completion
  atomically replaces the previous evidence set. Field names scope the
  attestation to the assigned user (never "device monitored").
- cda58dc: Remove the unused `replayed` field from the `RiskResult` API type. The flag was
  denormalized from the scanned chat message onto every risk listing row but never
  rendered by any consumer; dropping it shrinks the listing queries ahead of
  serving the Risk Events page from ClickHouse.
- efe9101: Add the Microsoft Intune inventory-source provider to device integrations:
  Entra ID client-credentials auth (classifying Entra's 400 invalid_client
  shape as a credential rejection), field-selected managed-device pulls via
  Microsoft Graph with server-driven nextLink pagination (cursor validated to
  stay on the Graph host), and mapping into the normalized managed-device
  shape with emailAddress-then-UPN user attribution.
- 48b13b7: Add the Iru (formerly Kandji) inventory-source provider to device
  integrations: static bearer-token auth against the tenant API URL,
  limit/offset-paginated device pulls mapped into the normalized managed-device
  shape, and a connection test via a single-record page.
- c5ca622: Add the Jamf Pro inventory-source provider to the device integrations framework, plus its dashboard presentation entry (Apple-fleet icon and console setup steps for minting the least-privilege API client). Organizations connect a Jamf Cloud tenant with an instance URL and least-privilege API Client credentials (an API Role with only "Read Computers"); the provider authenticates via the OAuth client-credentials grant with the token cached until expiry, pulls the computer inventory in stably ordered, section-filtered pages, and maps each device's serial, hostname, OS, assigned-user email, and last check-in into the managed-device store — preserving the full vendor record. Credential rejections, including tokens expiring mid-pull, classify as auth errors feeding the scheduler's auto-pause streak, and every API request carries the unique User-Agent header the Jamf Technology Partner Program requires.
- 30cc54d: Ingest opencode observability events natively. The hook ingest pipeline recognizes the `opencode` source (`parseOpencodeHookEvent`), giving opencode events native event-name fidelity instead of a generic fallback, and counts opencode tool calls in the telemetry summaries. Per-turn token/cost usage rows are populated from the OpenCode turn-end usage forwarded by agenthooks v0.4.0.
- df696de: Page the skills table and move its default search, filters, and sorting to the server.
- ce74cd3: Paginate scored skill sessions, collapse their table by default, and link chats to the agent sessions explorer.
- 3f11ea3: Remove the unused Redis-backed Shadow MCP access-rule and approval-request API in favor of risk policy bypass grants.
- 86d4d18: Add a `shadow_mcp_disposition` field to risk policies. Shadow MCP blocking policies now carry a default disposition — `block_all` (the existing behavior, and the default) or `allow_all` — chosen at creation time. The disposition is immutable after create: switching posture requires deleting and recreating the policy.
- d60dcf8: Review suggested skill edits one change at a time. A suggestion now proposes separate changes, each carrying its own summary and citing only the agent reports behind that change, so unrelated evidence no longer appears next to an edit. Applying a single change records a new version carrying only it and leaves the rest of the suggestion open against that version. Changes are stored as diffs, so they survive unrelated edits to the skill and are retired individually when they no longer apply.
- c49af44: Add management APIs to list, approve, dismiss, and bulk approve skill edit suggestions.
- 3f61966: Add the Vanta evidence-sink provider to device integrations: OAuth
  client-credentials auth with a per-run token cache (Vanta allows one active
  token per application), and per-device agent-coverage evidence pushed as a
  full-state Custom Resource sync whose property names scope the attestation
  to the assigned user. Rejected records fail the push loudly, since
  full-state semantics would otherwise read them as departed devices.

### Patch Changes

- 8746659: Stop remote-session MCP requests from looping on a dead upstream refresh token. When an upstream token endpoint returns a definitive RFC 6749 `invalid_grant`, the stored session is now soft-deleted (compare-and-swapped on `updated_at` so a concurrent refresh or re-link is never clobbered) instead of being retried on every request. The next request establishes a fresh upstream session rather than replaying the dead grant.
- d20126d: Stop asking MCP users to reconnect when several of their requests refresh an upstream token at the same time. Concurrent resolves for one subject all presented the same stored refresh token, so a provider that rotates single-use tokens honoured the first and rejected the rest, and every rejected caller was told to reconnect a session the winner had already repaired. Refresh is now single-flighted per (subject, remote session client) with a short Redis lock — losers wait for the winner's write and adopt its token instead of calling the provider — and the write itself is a compare-and-swap on `updated_at`, so a losing writer can no longer persist a refresh token the provider has already consumed.
- 411844e: Plugin-scoped skill activations now record under the skill's canonical name, so the same skill attributes consistently across plugins instead of being rejected as invalid.
- 7734a63: Chart skill activations by version across the rolling 30-day window.
- 84e7f4f: Device integration syncs now record database rejections of vendor-supplied
  row content (for example a device record whose name carries a Unicode NUL
  escape that jsonb refuses) as visible, backed-off schedule failures instead
  of retrying them as infrastructure errors, and URL-kind integration settings
  are syntax-checked at save time.
- 3f61966: fix: cancel stranded Drata sessions before pushing coverage evidence

  Drata permits only one IN_PROGRESS upload session per custom-connection
  resource, so a push that died mid-upload left a session that blocked every
  later push. Each push now sweeps and cancels any stranded session before
  opening its own.

- 8c68e21: Add support for signing with GCP Cloud KMS keys, so a signing key's private half never leaves the key management service holding it. Groundwork only: no API or dashboard surface uses it yet.
- 83ed7b1: Serve the hooks@0.3.7 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- 77d707b: Serve the hooks@0.3.9 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- 189bf8e: Explain that MCP connection access was restricted by the `mcp:connect` permission and link users to their organization's authorization challenges grant flow.
- 9e3c281: Capture Claude Code prompt attachments from local transcripts and submit them on hook ingest. The server stores each attachment as a scannable `prompt_attachment` chat message with first-class prompt linkage and display-path metadata.
- 8eafabf: Tunneled MCP servers can now be published with public visibility, letting anyone call them anonymously with no login. Turn on **Public Access** for a tunnel source, then set an MCP server fronting it to Public. Public tunneled servers expose every tool to the open internet, so a high-friction confirmation guards the toggle and the MCP server visibility control stays locked to Private until the source opts in.
- dbd31a9: Honor URL-, stdio-, and whole-policy bypass grants during offline Shadow MCP scans, preventing approved servers from generating recurring findings.
- 8880982: Polish trial-facing setup and administration surfaces: use current Speakeasy
  branding on public install pages, return an empty custom-domain list without a
  404, remove invalid DOM and SVG attributes, explain unavailable collection
  installs, and focus observability setup on supported integrations.

## 0.94.0

### Minor Changes

- f1d60da: Add a platform-admin surface for the chat analysis pipeline's per-organization settings. A new `adminChatAnalysis` management service (`getSettings` / `upsertWorkUnitsSettings`, session-only, gated on the platform-admin flag) reads and writes the organization's `chat_analysis_settings` row for the work-units judge, taking the same organization advisory lock the reservation transaction holds and recording before/after audit snapshots under the new `chat_analysis_settings` subject. The developer toolkit's Features tab gains a matching "Work Units Chat Analysis" section: an org-wide enable/disable control plus the daily evaluation cap, with a suggested cap prefilled when enabling an organization that never had one. A third method, `triggerAnalysis`, wakes the chat analysis coordinator of every project in the organization on demand — surfaced as a "Run now" button in the same section — so an admin can start a pass immediately instead of waiting for a chat write or the periodic sweep.
- 861e650: Add on-demand LLM session summaries (`chat.summarize`) and pin controls on Agent Sessions: persisted summaries in the session side panel, pin/unpin on list rows and the detail sheet, and a Pinned filter.
- e1b188a: `chat.load` now accepts a producer-scoped API key (`Gram-Key`) in addition to a dashboard session and a chat-session token, so backend integrations can pull chat transcripts programmatically without a browser session. Only a **direct** producer API key is treated as a first-party project credential: like the dashboard session (and the way RBAC already exempts API keys via `ShouldEnforce`), it can load any chat in its project, including chats owned by an external user. External-user callers and chat-session tokens stay owner-matched even when the token carries the minting key's `APIKeyID`, and the project/org boundary still applies. The dashboard's producer key-scope description now notes it can export chat transcripts, and the endpoint is added to the public SDK/docs allowlist so its API-key auth is captured in the published API docs.
- ffae6fa: Add daily custom-domain routing and TLS certificate health checks in an observation-only first release: checks log their findings, including the admin notifications a future release will send, without persisting health state or emailing anyone yet. The dashboard groundwork for health warnings and a manual recheck ships alongside but stays dormant until observation ends.
- cb9189c: Add Claude Opus 5 (`anthropic/claude-opus-5`) to the supported model catalog and make it the default for in-app chat and newly created assistants. Specialized judge, embedding, and other purpose-specific model selections remain unchanged.
- 35fad1f: Add the schema foundation for device integrations — the framework that connects an organization to external device-management and compliance vendors. Three new tables: `device_integration_configs` (the audited, per-org, per-provider integration identity, with secret credentials as an encrypted write-only JSON blob and non-secret settings in readable jsonb), `device_integration_syncs` (scheduler state per config and schedule, modeled on `ai_integration_syncs` including the separate auto-paused vs user-disabled markers, plus a pushed-snapshot digest so evidence sinks can skip no-op pushes), and `mdm_devices` (the MDM-reported hardware inventory, keyed by config with both the raw MDM-reported user email and a resolved `users.id`, and a `missing_since` lifecycle instead of deletes). Also adds a case-insensitive `(organization_id, LOWER(email))` index to `device_agent_syncs`, the agent-heartbeat side of the upcoming coverage join.
- 03b0c2e: Add platform-admin management of Gram's own platform-level external credentials (starting with the ambient GCP identity) via a new `adminExternalCredentials` API (create, read, update, delete) and an "External Services" section in the organization settings with a creation sheet and a per-credential detail page. Includes a live "who am I" Verify probe backed by a reusable `gcpauth` identity resolver.
- 084cc71: Add Budgets v1: org-scoped per-person budget rules with CEL actor targeting over directory-synced attributes. A periodic Temporal evaluator sums each matched actor's LLM spend from ClickHouse against the rule's per-person limit for UTC calendar windows, records warning/breach events, and publishes circuit state to Redis. Rules with action=block deny the blocked user's Claude Code traffic (UserPromptSubmit and PreToolUse, before risk-policy scans) until the window resets. Rules are append-only version snapshots: editing archives the current version row and creates a successor (version + 1), and rules are archived — never deleted — so historical events always resolve to the exact config that fired them. In the dashboard, Budgets renders as a tab on the Costs page wired to the new `spendrules` management API (rule create/edit/archive, live actor preview, overview cards, events tab); the tab only appears when the `gram-budgets-page` PostHog flag is enabled, so the surface can be released to select users.

### Patch Changes

- 6801c36: Make Codex MCP tool calls joinable to their recorded provenance (DNO-604). Codex hook payloads carry no per-call tool-call id, so the recorded chat tool-call id (previously the tool name) and the telemetry trace id (previously derived from the session id) could never satisfy the shadow-MCP provenance join `trace_id = sha256(tool_call_id)[:16]` — every Codex MCP call fell back to `x-gram-toolset-id` signature validation. Both sides now derive from a shared `sessionID + "|" + toolName` key, which also moves Codex trace grouping from one-trace-per-session to one-trace-per-(session, tool): Tool Logs rows now carry the actual tool name instead of an arbitrary one per session. The canonical ingest path applies the same shared-key fallback for any sender that omits per-call tool ids.
- 995ac90: Enabling an MCP server no longer fails when the project's Default plugin already lists a server under the same display name. The Default-plugin attach now picks the first available display name — the requested one, then a backend-id-suffixed variant — instead of letting the `(plugin_id, display_name)` unique index abort the enclosing transaction, so a same-named toolset attachment or a stale row can't block enablement. Deleting an MCP server also detaches it from its plugins (recording a `plugin:server_remove` audit event per detachment), releasing the display name for a replacement server.
- 32df5c0: MCP servers backed by an external OAuth authorization server now serve RFC 8414 authorization-server metadata whose `issuer` matches the Gram resource URL, so spec-compliant OAuth clients no longer reject the document.
- 52cc585: Serve the hooks@0.3.4 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- 8fa329b: Serve the hooks@0.3.5 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- e6d11cd: Remove the unused Kubernetes Gateway API custom-domain provisioner. No environment ever enabled it and no cluster has the Gateway API CRDs installed; custom domains are provisioned exclusively through Ingress. This also unblocks the custom-domain health sweep, which failed while trying to list HTTPRoutes on clusters without the Gateway API.
- 8bfe95d: Dashboard session tokens are now generated from 256 bits of `crypto/rand` entropy (base64url-encoded) instead of a v4 UUID. Session tokens are bearer credentials validated by a bare cache lookup, so they must be unguessable; a UUID carries only 122 bits of entropy in a recognizable, structured format and is not intended for use as a security token. Existing sessions remain valid, this only affects newly issued tokens.
- 3f3e59e: Keep published plugin server names in sync when their MCP server is renamed.
- dd0089e: Remove the `telemetry-logs-pubsub-shadow` PostHog killswitch from the telemetry Pub/Sub shadow dual-write; rows written to `telemetry_logs` are now always mirrored to the `gram-telemetry-v1-log-record` topic. The flag was evaluated locally with a constant distinct ID and no groups, which cannot satisfy a group-targeted release condition — evaluation failed on every batch and the fail-closed gate meant nothing was ever published (while emitting a warn log per batch). The publish path is already best-effort and non-blocking, so the extra killswitch added more failure surface than safety.

## 0.93.0

### Minor Changes

- 5e8e13f: Speed up the Employee Enrollment page (DNO-618). `telemetry.searchUsers` gains a `metrics` level: `full` (default, unchanged) computes the complete set of aggregates, while `basic` projects only user identity, first/last activity, input/output token sums, and the raw user ids the account-enrichment join needs — skipping the per-tool and per-hook-source map aggregations (`sumMapIf`), chat-cardinality (`uniqExactIf`), and cost/cache/avg columns that dominate the per-row ClickHouse work. The enrollment list, which renders only the lean fields (linked accounts come from Postgres), now requests `basic`, so its query no longer builds breakdowns it discards.
- cc076e2: Serve the Employee Enrollment list from the pre-aggregated `attribute_metrics_summaries` view (DNO-618). `telemetry.searchUsers` gains a `source` level: `logs` (default, unchanged) scans raw `telemetry_logs`, while `agent_metrics` reads the pre-aggregated view — canonical observed agent usage (Claude Code, Codex, Cursor, Claude Chat), keyed by email — which is far cheaper (the enrollment query drops from ~seconds to tens of milliseconds on large projects). Identities that never carry an email in the window (which have no token usage) are surfaced separately from raw logs with activity but no token counts, so unknown users stay visible.

  Note the enrollment token numbers change: they now reflect the same canonical agent-usage measure the costs/billing pages use, rather than the previous raw `gen_ai.usage.*` sum that mixed in Gram-hosted completions and duplicate usage-metric rows while missing Claude Code OTEL usage. Only the enrollment list opts in via `source=agent_metrics`; all other `searchUsers` consumers are unchanged.

- d85fa7a: Add tool metadata management methods to the mcpServers service.

  Two batch writes with deliberately different contracts:

  - `setToolMetadataBatch` is authoritative — it upserts every tool in the payload and soft-deletes every stored tool the payload omits.
  - `addToolMetadataBatch` is strictly additive — it inserts the tools in the payload, leaves stored tools absent from the payload untouched, and deletes nothing. A tool that already has a live stored entry fails the whole batch with a 409 rather than being upserted or skipped, so a caller working from a stale view of stored state is told so instead of having the discrepancy silently absorbed. A tool whose only prior entry is soft-deleted is recorded fresh.

  Also adds `listToolMetadata`, `setToolMetadata`, and `deleteToolMetadata`. Mutations require mcp:write, are scoped to the target MCP server, and record one collection-level audit entry per write; reads require mcp:read.

### Patch Changes

- c4f6057: Show actionable permission errors when an organization's access policy blocks an MCP server or tool.

## 0.92.0

### Minor Changes

- c08521f: Expose per-schedule state for AI integrations and rebuild the dashboard page around it. New aiIntegrations.listSchedules, setScheduleEnabled, and retrySchedule endpoints surface each sync schedule's status (pending/success/failed/auto-paused/disabled), last error, and timestamps, backed by a new user-controlled disabled_at pause that is independent of auto-pause. Each schedule also carries a backend-owned product-level stream identifier and kind (e.g. claude.chat.message events, cursor.usage and claude.chat.cost.usd metrics). The AI Integrations dashboard section moves to a dedicated page with one expandable row per provider connection showing its event and metric streams, each with live status, inline errors, retry, an independent pause toggle, and a link to where the imported data lands.
- 3ca88b2: Add organizationRemoteSessionIssuers.migrate API and UI to consolidate two remote identity providers that point at the same upstream authorization server, re-pointing the source's clients onto the target and soft-deleting the source without forcing anyone to re-authenticate
- 797c761: Secret scanning no longer flags an AWS access key id as a leaked secret — it's an identifier, used only to anchor detection of the co-located secret access key. Findings now mask just the secret value, not its surrounding label.
- 372b70b: Secret scanning now flags AWS secret access keys and session tokens, not just the access key id — and masks them while leaving the access key id (an identifier, not a secret) visible.
- 041e7af: feat: add Codex compliance cost polling
- bb9aac8: Enable project assistants in the dashboard to emit Elements chart and generative UI blocks by sharing the canonical widget prompts between the client and server.
- 1b9057a: Add organization-scoped `externalKeys` management API for CRUD of external keys (AWS/GCP KMS) Gram signs with, each backed by an external credential. Per-provider create/update/get/delete plus a generic supertype-only list with an optional provider filter. Gated on `org:read`/`org:admin` and audited under per-provider subjects (`aws_kms_key`, `gcp_kms_key`).
- 628ffbc: Add organization-wide skill efficacy sampling settings endpoints.
- ccdc7f4: Add project skill efficacy, activation, attributed session cost, estimated savings, trends, and scored-session insights.
- ad4fcee: The `pending_messages` and `total_messages` fields on `RiskPolicy` are now optional and omitted from `risk.listPolicies` responses. Computing them re-aggregated every risk result for the project on each list call, and no consumer read them from the list. Single-policy responses (`risk.getRiskPolicy`, create/update) still populate both fields, and analysis progress remains available via `risk.getRiskPolicyStatus`.
- e980481: Add atomic Shadow MCP policy setup with project inventory URL selection, searchable modal review, and URL allow-rule reconciliation.
- afe7ab4: Add public share links for skills. New management endpoints `skills.share` and `skills.unshare` mint and revoke an unguessable share token per skill, and the unauthenticated `skills.getShared` endpoint serves a redacted public view (name, display name, summary, latest content) by token. Archiving a skill revokes its active share link, share and revoke events are audited, and skill list/get responses surface the active `share_token`.
- fd17ed6: Split the tool-usage summary into per-panel endpoints so the MCP & Tools dashboard streams in each card as its data arrives instead of blocking on the slowest aggregate (INC-417).

  `getToolUsageSummary` now has seven sibling endpoints — `getToolUsageTotals`, `getToolUsageTargets`, `getToolUsageUsers`, `getToolUsageTargetTimeSeries`, `getToolUsageUserTimeSeries`, `getToolUsageUsersByTarget`, and `getToolUsageTargetToolBreakdown` — each returning one section of the summary from the same shared query helpers and filter payload. The aggregate endpoint is unchanged for the platform agent tool that wants everything in one call. The MCP & Tools page fetches the seven sections in parallel (the cheap totals query gates the page shell; each panel shows its own loading skeleton and, if its section query fails, its own error state rather than a misleading empty chart), and the MCP overview "Top users" table now fetches only the users section it needs.

### Patch Changes

- 5778d9a: Move the shadow-MCP inventory upsert off the synchronous hook request path. The capture previously ran inside SessionStart/ConfigChange handling and issued one `custom_domains` query plus one sequential ClickHouse point-SELECT per inventory entry, holding hook responses for seconds on large inventories. The whole unit now runs detached from the request, the existing-row lookup is batched into one query per project, and the org's trusted hosts are resolved once per capture. Enforcement is unaffected — the shadow-MCP guard reads the Redis snapshot, which is still written synchronously.
- 8197aec: Add the one-time production backfill runbook for chat_session_summaries, covering telemetry history before the materialized view's live-ingestion cutoff.
- 1705403: Add the chat_session_summaries ClickHouse table and materialized view: per-chat hourly session rollups (tokens, cost, message/tool-call counts, status, model, and filter-dimension value sets) so the org-scoped sessions list can read pre-aggregated data instead of scanning raw telemetry_logs.
- eb24395: Serve the org-scoped sessions list from the chat_session_summaries materialized view on windows of 48 hours or more, keeping narrow windows on the raw telemetry_logs scan. Filters, sorting, and cursor pagination translate onto the pre-aggregated table, and a sync test pins the shared session predicates across the Go constants and the MV definition.
- 3203363: Fix Claude Desktop agent sessions showing an opaque user ID instead of the user's name. The Anthropic compliance import no longer clobbers a previously resolved chat owner when a later sync activity carries no actor identity (empty strings defeated the upsert's COALESCE guard — NULL is passed instead), and connected-user email resolution is now case-insensitive on both the server and the dashboard. When a session's owner still can't be matched to an org member, the agent-sessions list and session details now show a tooltip explaining why.
- a0a0410: Trace every ClickHouse client call by default: the connection is wrapped once at creation so all Query/QueryRow/Select/Exec calls — from any repo, current or future — emit client spans labeled with the target table and issuing function (no SQL text), forward their span context to ClickHouse's server-side execution spans, and record a duration histogram (clickhouse.client.query.duration) per table, operation, and outcome for dashboards and monitors. The Logger's per-write wrapper spans and the telemetry.clickhouse.write.duration metric are removed — the connection layer now measures writes and reads in one place (no Datadog dashboards or monitors referenced the old metric).
- c2e07e2: Remove stale references to the retired prompt-injection ML classifier. Dashboard copy and the managed-assistant instructions now describe the LLM judge, which has been the only prompt-injection engine since the classifier and heuristics were dropped.
- 12fa9a3: Hook traces now begin on the device. The speakeasy-hooks binary starts the trace for each hook invocation and reports on-device telemetry — operating system, architecture, binary build, coding-agent harness, and on-device elapsed time — which the server stamps onto hook endpoint spans so hook performance can be measured end to end and issues diagnosed per platform.
- d382998: Serve the hooks@0.3.1 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- 50cd8c5: Serve the hooks@0.3.2 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- 7acc38a: Serve the hooks@0.3.3 binary to hook installations. Previously pinned releases stay available so installations that have not regenerated their bootstrap script can still install.
- 6bc2557: Record exact skill versions loaded by assistants for skill efficacy attribution.
- 54689df: Stop rejecting proxied MCP tool calls that do not echo back the internal `x-gram-toolset-id` property. Gram no longer adds that property to tool schemas served by the remote MCP proxy, so calls to remote and tunneled MCP servers succeed even when the model omits the value or invents its own.
- ccd67f6: Serve the hooks@0.3.0 binary to hook installations. Activated skills now upload their manifest content, so captured skills show a summary and a version instead of a name-only entry. The previously served 0.1.1 archives stay available so existing installations can still cold-install.
- aea545a: Simplify the ClickHouse connection wrapper to span-context forwarding only. The client-side spans, the `clickhouse.client.query.duration` histogram, and the operation/table label derivation introduced in the previous release are removed: per-query latency is investigated via ClickHouse's own `query_log`/`opentelemetry_span_log` (joined by trace id, which the wrapper still forwards on every call), and service-level latency comes from the ClickHouse Cloud Datadog integration.
- 6ee26e5: Add `skill_version` grouping and filtering to the generic telemetry query API.
- 96f7f73: Skill summaries now stay in sync with the current version: publishing a new version — whether recorded manually or captured from a session — updates the skill's registry summary from the manifest description, so skills captured before their contents arrived no longer show "No summary" forever. The dashboard's Add Skills dialog on the plugin page is now a multi-select that batches distributions, keeps skills without a distributable version listed but disabled with the reason and a Fix link, and the skill page's distribution banner turns red and explains what blocks distribution when a skill has no versions or none pass validation. Version badges in the version history table no longer overlap the Validity column.
- fd17ed6: Speed up the MCP & Tools observability endpoints (INC-417): the tool-usage and tool-logs queries over `trace_summaries` now add a slop-padded `WHERE start_time_unix_nano` pre-filter so the new minmax skip index prunes the scan to roughly the query window instead of the project's full 90-day history (the exact window predicate stays in `HAVING` over the per-trace minimum), and `GetToolUsageSummary` / `GetToolUsageFilterOptions` run their independent aggregate queries concurrently instead of sequentially.

## 0.91.0

### Minor Changes

- 6b474d3: Add assistant skill attachments to the management API and assistant read model. Skill distribution mutations now target exactly one plugin or assistant, with plugin and assistant target fields optional on mutation responses; plugin-only distribution lists retain required plugin fields. Assistants expose resolved skill references, and skill detail responses report active assistant usage.
- 5c3f00a: Capture observed coding-agent skills and request unknown manifest content through the hooks API.
- ce5571d: Rename and edit captured skills, preserve immutable version lineage, and expose curation controls in the dashboard.
- 6ea128d: feat: surface issuer setup documentation when creating clients. `remote_session_issuer` records now expose a `client_setup_documentation_url`, settable on create and update across the project-scoped, org-admin, and platform-admin (global) issuer surfaces. The dashboard edits it on the issuer Settings tab and shows it on the Overview tab alongside the discovered RFC 8414 `service_documentation`. Both are linked from the New Client sheet — as **Client Setup Documentation** and **Service Documentation** — so customers can set up an OAuth client with the provider themselves, owning its credentials, access, and rate limits rather than sharing a Gram-owned client. `client_setup_documentation_url` must be an absolute `http(s)` URL (validated with `urls.IsAbsoluteHTTP`, since it is rendered as a link); an empty string clears it.
- e5800a5: Flag inactive MCP servers on the Distribute MCP listing. A new `telemetry.getMcpServerActivity` endpoint reports per-server tool-call activity, and each card/row now shows a subtle indicator when a server has never received a tool call and a warning when it has had no tool calls in the last two weeks.
- 27dbfcf: Warn organization billing contacts before their managed OpenRouter credits run out. The periodic credit-usage poll now emails the billing alert contact when usage of either platform-managed key — the chat key (playground, elements, assistants, completions proxy) or the internal key (risk-policy judges, prompt-injection detection, titles, resolutions, memory) — crosses 50%, 75%, 90%, and 100% of its monthly cap. Each key type has its own email template and thresholds dedup independently per key with monthly re-arming. Chat-key warnings are suppressed for organizations with a chat-serving BYOK key; internal-key warnings always apply since that usage is platform-billed. Organizations without a billing alert email are skipped.
- d9f2bf0: Add a `tokenExchange` service for device-agent enrollment (DNO-383). `tokenExchange.exchange` trades an org-scoped `agent` API key plus a vouched user email for a long-lived, per-user API key carrying the narrower `agent_user` scope: the email is verified to belong to a real member of the authenticated org, each enrollment mints its own uniquely-named key (no singleton rotation, so a user's other enrolled devices keep working), and the raw key is returned exactly once. Hooks do not route through the device agent, so the minted key carries no `hooks` scope. A new `agent_user` scope is the per-user data credential; `agent` implies `agent_user` (one-way), so an existing org `agent` key keeps working on the data endpoints with no re-provisioning, while a minted `agent_user` key cannot re-enter the mint endpoint. `agent.getPlugins` now requires `agent_user` and resolves the enrolled user by credential type: for a per-user key the enrolled user is the key owner (the vouched `email` param is ignored); for an org install `agent` key — the MDM zero-touch path, whose owner is an admin rather than the developer — the vouched `email` param supplies attribution and is required. The plugin set is still resolved by organization.

### Patch Changes

- a1750eb: Give the project's managed assistant (Project Assistant) a new `platform_get_changelog` tool that reads the public Speakeasy changelog feed, so it can answer questions about what recently shipped on the platform and dashboard.
- eacabda: Make the cost explorer's breakdown machinery treat the "(unset)" bucket as a first-class group everywhere, fixing the hidden Account Type breakdown on drilled slices that mix classified and unclassified spend (DNO-425).

  Server: telemetry.query's dimension_values now keeps the '' bucket for every groupable dimension — it is the "(unset)" row a breakdown by that dimension renders, so consumers can count it. Only dimensions where '' means "not applicable" (the Claude attribution cuts and query_source, flagged in the dimension registry) still drop it. Empty role/group arrays likewise surface as the "(unset)" bucket.

  Dashboard: the breakdown axis is resolved against the slice's actual group counts by one shared resolver, at drill time (using the clicked row's dimension values) and on load — a division whose spend all sits in one department lands directly on its users with no Department selector, while a division splitting into a named department plus department-less spend keeps the Department cut (previously hidden). The entity/detail query no longer depends on the axis (removing an internal resolution cycle), grouped queries wait for the resolved axis instead of fetching twice, a `?by=` naming a pinned or un-splittable dimension falls back to the level's default, and the URL is rewritten in place whenever the rendered axis diverges from `?by=` so links always reflect the view.

- 1dc6d5e: feat: add prompt scanner pubsub handlers
- 78a7ba8: Add a `hook_hostname` sort-key dimension to `attribute_metrics_summaries` (non-destructive ALTER + atomic MV MODIFY QUERY). The device hostname the Go hooks report rides into the aggregate so the user breakdown can fall back to the device for sessions that carry no email. Historic buckets read empty for the new column; live ingestion populates it from `gram.hook.hostname`.
- 1a04494: Fall back to the device hostname on the user cost breakdown when a session carries no email. The Go hooks report the machine's hostname on every event; it now rides the session cache onto Claude OTEL cost rows, and the `email` telemetry dimension groups identity-less spend per device instead of pooling it all into one bucket. Only sessions with neither email nor hostname remain under "Team-wide API Usage".
- 72855da: Normalize provider names and product-surface labels across reporting, agent sessions, tool logs, and cost views. Anthropic compliance imports now persist canonical Claude desktop/web source slugs, while historical source aliases remain filterable.
- 7d27a96: Expose recommended risk detection scopes through the management API, with per-policy per-category detection scope overrides.

## 0.90.1

### Patch Changes

- 9f39b44: Deprovision user access on SCIM deactivation. WorkOS `organization_membership` events with status `inactive` and `dsync.user` events with a non-active state now soft-delete the user's organization relationship and role assignments and invalidate their cached user info. Login-time and backfill membership syncs only import active memberships, directory-user upserts no longer resurrect soft-deleted rows unless the incoming state is explicitly active, and organization rosters exclude deleted users.
- 50289f1: Show the number of active skills carried by each plugin on the Plugins page.

## 0.90.0

### Minor Changes

- e98f891: Give the Project Assistant read-only platform tools to list project skills, inspect the latest skill content, review version history, and inspect plugin distributions.
- bdf8c48: Skills can now be distributed to plugins. New management endpoints let you attach a skill to a plugin (tracking the latest valid version or pinning a specific one), revoke a distribution, and list a project's active distributions. Deleting a plugin or archiving a skill automatically revokes the affected distributions. Distributed skills ship inside the published plugin packages for Claude Code, Cursor, and Codex, and distribution changes mark the plugin as having unpublished changes so the next publish or marketplace auto-sync picks them up.

### Patch Changes

- ca1e87f: Add a production runbook to backfill an org's historical "(unset)" account-type spend as team on attribute_metrics_summaries (POC-305), staged as generation 2 via the generation/is_active tombstone machinery. Ships with a ClickHouse migration that atomically swaps attribute_metrics_summaries_mv's query in place (ALTER TABLE ... MODIFY QUERY, no ingestion gap) so live ingestion is also stamped generation 2 — the backfill and fresh rows share one generation, immune to the generation-0/1 cutover flips, converging on a single live generation once the old ones are cleaned up.
- f83c87f: Manage skill distributions from the dashboard. Skills now open as a dedicated detail page with an at-a-glance sidebar, section navigation, and a plugin distribution banner for distributing the skill to plugins and revoking distributions. The plugin detail page's Skills section replaces the coming-soon placeholder with the actual list of skills the plugin carries, including add and remove controls. Skill distributions can now also be listed filtered by skill or plugin.

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
