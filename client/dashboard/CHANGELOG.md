# dashboard

## 0.112.0

### Minor Changes

- eb0b4bd: The composer's stop button now actually stops the assistant. It previously only aborted the browser's view of the turn: the reply kept generating server-side, kept calling tools, kept spending, and reappeared in the transcript on reload. Pressing stop now calls a new `assistants.interruptTurn` endpoint, which cancels turns still queued on the conversation's thread — the case that matters while a cold runtime is booting — and asks the runtime to interrupt the turn in flight. The runner cancels cooperatively, so the partial reply stays in the transcript instead of being discarded, and a terminal frame goes out on the turn stream so every tab watching the chat settles rather than tailing a turn that has ended.
- 0be309a: The Shadow MCP inventory reports enforcement truthfully, from one server-computed verdict. Each row now carries a typed access summary — state, the reach of explicit allow and block mechanisms, the blocking default, and the recorded decision with how much of it enforcement delivers — and the dashboard renders from it instead of re-deriving enforcement from policy lists and review status.
  
  Reach is a per-policy set question, not a principal string match: a URL is allowed for everyone when its bypass grants cover each deny-by-default policy's audience — the all-users principal, or grants naming the policy's whole audience, which under a targeted policy frees everyone it ever blocked. Several over-reports fall out: a server approved for part of an audience reads Restricted ("Allowed for selected users") instead of a blanket Allowed; a denial only a targeted policy carries reads Restricted instead of Blocked; a decision the current rules contradict or nothing enforces says so, naming what displaced it ("Denied by review, but overridden by an allow rule", "Approved by review, but its allow grants were removed", "not enforced until a blocking policy exists") instead of vanishing; and decisions on local stdio commands, which record without writing enforcement, report coverage none. Stdio targets join the policy-state universe, so a page of only local commands still reports the project's posture, and one row's verdict no longer depends on which rows share the page. The Pending badge turns information-blue on both the inventory and the detail strip, leaving orange to the partial-access family. access_summary ships optional for one release so a client ahead of a rolled-back server degrades to the legacy access string — which stays one release, with its values corrected the same way — before both flips land together.
- 8f79411: Policy URL-list edits can no longer contradict recorded MCP access decisions silently. Editing an already-blocking shadow MCP policy's allow or block list now reviews the change against the project's standing decisions: unchecking an approved server, allow-listing a denied one, block-listing an approved one, or unblocking a denied one is refused with a conflict unless the save explicitly confirms superseding those decisions (`supersede_decisions` on `risk.updatePolicy`). A confirmed save transitions each displaced review to the new `superseded` status — actor-attributed, audit-logged (`mcp_approval_request:supersede`), decision history and rationale preserved — and the policy replay and drift recheck stop deriving enforcement from it until someone re-decides. Ordinary re-saves also stop rewriting decision-written grant audiences with the policy audience: a scoped approval's blast radius now survives unrelated policy edits. The dashboard policy editor shows a confirmation dialog listing the contradicted servers before such a save, and superseded reviews render with their own badge and inventory filter.
- 4bd52d2: Rework the Shadow MCP server detail page around a summary strip and remove the repetition beneath it. Status, calls, people and last-called now read as one bordered strip of metric tiles sharing a row with the review's own header, replacing a status badge and two lines of prose that left most of the row empty. The figures the strip reports no longer appear a second time as an "Are we already exposed?" fact list — the traffic table answers that question directly, and the one thing it cannot say, what a denial would cost, rides beside its heading instead. Per-source counts drop out of the traffic table when a user has a single source, where they only restated the call count next to them.
  
  The evidence groups line up: the caveats that used to sit as paragraphs under each question now hang off an icon beside it, so every box in a row starts at the same top edge, and a group whose whole answer is one sentence stretches to its row's height with that sentence centred rather than stranded in a corner. The gaps banner no longer repeats failures that the group beneath it already reports as unknown, and the "unreviewed" badge reads Unreviewed instead of "Review requested", which contradicted the notice under it saying no one had asked. Long user lists collapse behind a toggle, and the web research panel drops its intro, its credits warning and its empty state when research is not part of the plan.

### Patch Changes

- 9f53288: Loading the assistant onboarding page directly — a fresh navigation or a browser refresh on `/assistants/new` or `/assistants/:id` — no longer crashes the page with "Rendered more hooks than during the previous render". `FrontendTools` called each tool component inline instead of rendering it, so every tool's hooks ran inside `FrontendTools`' own hook list. The onboarding tool set is gated on RBAC grants and `hasScope` returns false until those grants load, so the first render after a hard load saw a trimmed set and a later render saw the full one — growing the component's hook count and tripping React's hook-order invariant. Reaching the page by in-app navigation happened to work because the grants query was already cached. Each tool now renders as its own keyed element, giving it its own hook list.
- 81e7f18: Remote session issuers now capture the upstream's advertised PKCE support (RFC 8414 `code_challenge_methods_supported`) through discovery, refresh, and the create/update forms, and report it without refusing anything. The value is exposed on issuer reads and drafts (serialized without `omitempty` so null, meaning never captured, stays distinct from an empty array), rendered read-only on the issuer Overview tab, and instrumented with an unsampled `gram.remote_session.upstream_authorize` counter dimensioned by the issuer's PKCE support state at every authorize-URL build. Discovery also warns operators when an identity provider does not advertise S256, since MCP requires clients to verify PKCE support and a future change may enforce it. A null value ("never captured") stays distinct from an empty array ("the issuer advertises no methods") end to end.
- 2198e8b: The command palette's "MCP Servers" group now searches `mcp_servers`-backed servers as well as toolset-backed ones. Previously the group was built entirely from toolsets, so a remote-, tunneled-, or unproxied-backed server never appeared in ⌘K and could only be reached by navigating to the MCP list by hand. Both kinds are listed under the one heading, matching how the MCP list page presents them, and each row still matches on name and slug.
- 82582b2: The Secure section's navigation is now Watchdog, Guardrails, and Shadow MCP: the Risk Policies page is renamed Guardrails and gains a Detection Rules tab alongside Policies and Exclusion Rules, replacing the standalone Detection Rules page (its old URL redirects, deep links included).
- 1011140: access.createRole no longer requires a description. The Create Role dialog accepts an empty description field and omits it from the request, so roles can be created without one.
- d95abb5: Add the meta MCP control plane: a `metaMcp` management service for creating meta MCP servers with explicitly managed, ordered member sets, plus MCP endpoint support for addressing either an MCP server or a meta MCP server as its backend.
- a33a07b: OTEL forwarding updates now preserve encrypted values for unchanged headers while still treating the submitted header list as the complete desired set. Adding or removing one header no longer clears the values of retained headers.
- d3d58fe: The Risk Events page is back in the Secure navigation, directly below Watchdog (previously it was hidden whenever Watchdog was enabled).
- cdd18f1: Session portability can now be enabled per organization from both admin surfaces: the standalone admin Features page and the in-app platform-admin Features page. Previously the flag was provisioning-time state with no toggle anywhere.
- 9a19eb0: Proxy `/shared/handoffs` to the Go server in local dashboard dev so session-handoff capability URLs return markdown instead of the SPA shell. `/shared/skills` stays on the dashboard — it is a public React page, not a server document.
- 1c55b90: The signal drawer and the signal list's multi-select toolbar now offer one "Suppress" dropdown in place of the separate suppress and exclusion-rule buttons, with "Suppress Once" (manual suppression) and "Create Rule" (exclusion rule creation) options.
- 2211d34: Watchdog suppression now collects a signal's findings without the page's time window, fixing the silent no-op when a signal contained findings whose messages predate the window (signals exist by scan time, the listing filters by message event time). An empty collection now shows an error toast instead of doing nothing. The signal drawer labels its window-scoped stats and unwindowed latest-evidence list so their counts can't read as contradictory, and the top-affected-users list no longer shows an "Unknown user" row for findings with no user attribution (matching the Users count).
- e09ea80: The Windows device-agent setup walkthrough now installs from the signed MSI instead of raw binaries. The install step downloads the installer from a stable link that always resolves to the current signed version, and `msiexec` registers the machine-wide service itself — no separate service-registration step. The raw-binary PowerShell script remains available as an alternative for scripted installs, an Intune Win32/LOB note covers fleet deployment, and the enroll/verify commands now invoke the CLI by its install path (the MSI does not add it to PATH).

## 0.111.0

### Minor Changes

- 92c1087: Add a Remote sessions platform walkthrough to Device Agent setup for Anthropic-hosted Claude Code on the web: network access, a verified pinned daemon install with managed enrollment, a SessionStart hook that revives the agent each session, and organization-default guidance.
- 71931d5: The research agent now persists its per-action trace — every search and page fetch a run made, in order, with the outcome, the injection judge's verdict, and a bounded preview of the untrusted text it saw. The report is a run-level synthesis that drops most of what was read; the trace is what the agent actually did, surfaced on the review page under "what the agent did." No page bodies are stored, only previews, and no new inference is run — the runner already produced this and discarded it.
- 55032f2: Suppressed findings now live in a collapsible section on the Watchdog page instead of a "False Positives" tab under Risk Policies. The section lists every suppressed finding — exclusion rule, manual dismissal, or automated sweep — with the provenance behind each one, a restore action for manual and automated suppressions (single or bulk), and a link to the rule behind an exclusion-suppressed finding. Clicking a row opens a detail drawer with the finding's details, its redacted match, its session, and the same restore or view-rule action. Suppression copy across the Watchdog now says "Suppress" / "Restore" rather than "Mark as false positive".

### Patch Changes

- c1eae5f: Bill both customer-facing and platform-initiated inference spend for PAYG organizations.
- b22a844: Stack the Device Agent platform tiles vertically (logo on top, name and subtitle underneath) so all four tiles fit a row without the labels wrapping one word per line, and drop the "Cowork needs separate setup" callout from the onboarding instrument-agents step.
- 2fe82c3: Allow product-feature APIs to target an authorized organization while preserving active-organization behavior for existing callers.
- f4a077d: Scope product-feature requests and organization-owned form state to the active organization.
- a2a67e0: Require an explicit organization for every product-feature API request.
- e76b4a2: Session moves now record a lineage edge linking the original session to its continuation. The device agent can pass the continuation's session id in `agent.reportSessionMoved`, a new `chat.listSessionLinks` endpoint resolves the edges touching a set of chats, and the Agent Sessions detail panel shows a "Linked sessions" section — "Moved to Cursor" on the original, "Derived from …" on the continuation, with navigation between the two when both are captured.
- 3f1dcaf: The Shadow MCP inventory now distinguishes a server blocked for everyone from one blocked only for some. A deny-by-default policy scoped to a subset of users (audience type "targeted") no longer reports every server as "Blocked" project-wide; those servers now carry a new `restricted` access state, rendered as an orange "Restricted — Blocked for some users" badge. A denied review is also named as its own reason: "Blocked by policy & review" when a block policy already stops the server and a review also denied it, or "Blocked by review" when an allow-by-default rule blocks it solely because of the review. Servers blocked for everyone still read "Blocked" / "Blocked by policy" as before.

## 0.110.0

### Minor Changes

- 2fb5b71: A blocked employee now says why they need the server. The request page redeems the block link into a short form instead of filing the ask the moment it loads, and `risk.createPolicyBypassRequest` carries that justification onto the review as the requester's note. Previously every requester's note was the policy's block reason — the same sentence for everyone the policy stopped — so the review page's "who asked, and why" told a reviewer nothing about any individual ask. A client that sends no note still falls back to the block reason.
- ae6a17c: MCP Connections now reads as a graph: a row names a person, provider, or client, and opening it shows the nodes on the other side of its edges — a person's clients, a client's people — on the same columns. Rows are ordered by when they last carried traffic, and the list splits into active connections and the inactive ones (dormant for over a week, or no longer usable), which stay visible and revocable rather than filtered away. The same list renders on the MCP server detail tab and the employee page, replacing the drill-down that filtered a separate table.

  The shared list toolbar is restyled to match: a white bar, filter chips carrying a per-dimension colour from the brand spectrum, and no result count.

- 6f6a133: The MCP evidence dossier gains three deterministic sources. The assembler now
  consults the code host about a package's declared source repository (stars,
  forks, contributors, commit recency, archived status), asks OSV.dev which
  published vulnerability advisories name the package, and reads the domain
  registry's registration record for a remote server's registrable domain.
  Package registries also surface their declared repository and homepage URLs.
  Each source follows the dossier's existing contract — found, not-found, and
  could-not-look stay distinct, with failures recorded as gaps — and the
  approval page renders the new facts in the evidence panel, including a
  dedicated advisories group where checked-and-clean is shown as a finding
  rather than an absence.
- ce7b28a: The MCP research agent goes live end to end. A new
  `mcpApproval.startResearch` endpoint (decide-scoped) opens a report row and
  enqueues a Temporal workflow that runs a bounded tool-calling loop over the
  research web tools — search and page fetch — with the untrusted-content
  posture pinned in its prompt, then extracts a schema-held report: summary,
  independent-coverage level, and tiered claims where every web-sourced claim
  carries its citations or is dropped. Reports land on `mcp_research_reports`
  with model, prompt version, and per-run spend metadata; re-runs are additive
  and at most one run per request is in flight. The approval page's research
  section gains a Run Research button, polls while a run is live, and renders
  the report with its coverage callout, tier chips, citation links, and run
  footer.
- 1c8fa7b: Persist catalog MCP icons on mcp_metadata and render them in the dashboard. A new `assets.fetchImageFromURL` endpoint downloads a catalog server's registry icon into an image asset at install time, and the install workflow stores it as the server's MCP metadata logo. The MCP server detail sidebar now renders the persisted logo, and collection listings populate `icon_url` from it for both toolset-backed and mcp_server-backed servers. Remote-backed servers with no catalog icon now get the vendor's favicon as a default logo, matching the existing unproxied-server behavior.
- ce7b28a: The research agent now runs the prompt-injection judge over every page it fetches, and records a flagged page as a finding on the report. A vendor page that tries to steer whoever is reviewing the server says more about that server than any claim in the report, so the attempt is surfaced as evidence rather than only defended against: the agent still sees the page, labelled as material that tried to instruct it, and the finding is attached by the runner after extraction so a model that just read the manipulating page cannot leave it out. Pages the judge could not answer for are counted separately, because an empty findings list next to a judge outage does not mean nothing was tried.

  Starting a research run also serializes properly: the check for an in-flight run and the insert that creates one now share a transaction behind a row lock, so two clicks that land together buy one run instead of two paid agent runs.

### Patch Changes

- 6e8ce76: Scope the staff-only platform-admin chat analysis settings and triggers to the organization explicitly selected in each request. This is a coordinated dashboard/API correctness fix rather than a change to a customer-facing contract.
- 3fe01fd: Keep Platform MCP connections active through rotating refresh tokens with a 30-day sliding idle window and a 90-day authorization cap, while surfacing clear reconnect guidance when authorization expires or is revoked.
- ff39efd: Handle Stripe subscription loss by closing PAYG admission, disabling the Other inference key, and reconciling later billing events against current organization state.
- a06a859: Remote identity providers listed on an MCP server's Authentication settings now link through to their detail page. The rows were inert text, so reaching a provider's detail page meant navigating to Remote Identity Providers separately and finding it again by hand. Providers of every tenancy tier resolve through the same tenant-scoped detail page, which already renders an inherited platform provider read-only, and the name falls back to plain text for a viewer without organization read access.
- 08a549b: Add live Stripe PAYG subscription status, a controlled customer portal, and end-of-period cancel and resume controls for organization administrators.
- 2fb5b71: MCP approval change detection and re-review: a daily sweep re-gathers evidence for approved servers and compares the permission-relevant slice (OAuth scopes, authority mode, demanded credentials, published advisories) against the snapshot the approval rested on. Drift sets a changed-since-approval flag — cleared only by a new decision — announces once per distinct change through the audit-log webhook channel, and surfaces on the review page as a diff banner and on the inventory as a badge.
- 2fb5b71: Condense the MCP access review so a full dossier fits in far less scrolling. The request's status now rides beside the section heading instead of a card of its own, requesters and prior decisions sit side by side as framed lists, and the evidence questions lay out in two columns — each fact list picking its column count from the width it actually got. Observed traffic joins the evidence as "Who is currently using it?", beside the tools the server declares, and that tool list sizes itself to whatever it shares the row with rather than a fixed preview count.
- d41ba4a: The MCP connection listings now carry a status dot — green live, amber expiring, red needs re-auth, grey idle or revoked — so a roster is scanned by colour rather than read row by row; healthy rows state Live or Idle where the status column used to be blank. The OAuth client on the other side of a connection is called an agent throughout, matching the rest of the product, and the organization page moves from `/user-sessions` to `/mcp-sessions` with the nav entry and title to match.
- 4b8c41d: Show pay-as-you-go organizations their current billing cycle on Billing: tokens
  under management and their flat-rate cost, Other inference spend through the last completed
  day, and the estimated invoice total for the cycle. The estimate appears once
  Stripe billing has started, and the monthly Other inference spend meter now says plainly
  that it runs on the calendar month rather than the billing cycle.
- efd5ffc: PAYG organization administrators can configure or clear their billing
  notification email through the management API. When no address is configured,
  weekly usage summaries and OpenRouter spend alerts are sent to all effective
  organization administrators; enterprise notification routing is unchanged.
- 6bf1537: Surface pay-as-you-go billing problems where they're felt: a failed payment now heads the billing page with a direct link into the Stripe portal, and an organization that has reached a monthly inference cap gets a banner on every page naming what stopped, with a link straight to that cap's control.
- 6045dca: Let PAYG organization admins view and set independent monthly Security
  inference and Other inference caps for each applicable platform-managed key.
  Each change updates only the selected key, survives lifecycle reconciliation,
  and records a per-key audit event.
- 9533c6a: Add Platform MCP setup recommendations to dashboard empty states and organization navigation.
- 051ce8c: Serve reviewed provider setup guides as Platform MCP resources and add `search_gram_docs`, which answers from that pinned corpus with cited excerpts and links back to the full guide. Content past its revalidation date is flagged, then withheld, rather than presented as current, and a query nothing reviewed answers returns `guide_unavailable` instead of invented steps. Documentation citations now render as passages with resource links in the assistant rather than as raw tool output.
- 9eb0f94: The terse activity phrase the assistant emits before a batch of tool calls no longer flashes into the chat as prose before moving into the tool group's heading. The phrase was classified by looking at whether the next message part was a tool call, but that part does not exist until after the phrase has finished streaming, so it rendered as a paragraph and was then yanked into the group. A still-streaming phrase is now held back until the group opens, and ordinary prose is released as soon as its opening word rules out an activity phrase. Long answers streaming in after the tools finish are judged as a whole rather than by their last line, which previously blinked each new line out of the render as it arrived.

## 0.109.1

### Patch Changes

- d1e0a84: Activate pay-as-you-go billing atomically when Stripe confirms a completed checkout, including replay-safe webhook processing, subscription ownership validation, trial conversion, and entitlement setup for organizations without a prior trial.
- e635665: Offer the self-serve pay-as-you-go checkout above the booking calendar on both dashboard lockout gates, so an organization admin whose organization never trialed or whose trial expired can unlock the workspace with a card instead of only booking a call. Members, organizations that are not walled off, and every unresolved state of the rollout flag keep the booking-only gate.
- e935bdb: Two refinements to the PAYG rate adjustment in the TUM contract price estimator: a cheaper-than-committed PAYG outcome is only attributed to the adjustment when it is a discount — under an uplift the modelling-error warning ("check the platform fee against the baseline") is kept, since an uplift only raises PAYG above list rates; and adjustments at or past -100% are now ignored (list rates) at every layer instead of pricing PAYG as free.
- 3a8f15f: Add pay-as-you-go as a first-class billing tier across server entitlements, management API tier data, and dashboard and admin tier controls. PAYG organizations receive enterprise feature access with capped PAYG billing behavior, and Stripe-authoritative tier state cannot be overwritten by stale Polar data.
- b938a56: Allow eligible organization admins to start a self-serve pay-as-you-go Stripe checkout from the billing page. The server reuses a stable Stripe customer, preserves active trials, and records the checkout request in the audit log.

## 0.109.0

### Minor Changes

- 340f6f3: Add a client detail sheet for user-session clients with a CIMD metadata
  refresh. The per-client view now exposes the metadata document's cache state
  (source URL, last successful read, cache expiry, ETag), and a new
  `userSessionClients.refreshCIMD` endpoint forces a re-read: it purges the
  stored validators before fetching, so a host answering 304 Not Modified cannot
  re-confirm the copy being discarded, and it carries a 30s per-client
  server-side cooldown because purge-then-fetch deliberately bypasses the
  document cache. The dashboard's Clients listing opens the sheet from each row;
  DCR clients show the base detail without the CIMD panel.
- 0e5a8f7: Add an Encryption Keys page where organization admins register the keys in
  their own cloud KMS that Gram signs with. Keys list with their provider and
  algorithm, and each has a detail page with a Verify action that proves Gram can
  reach the key and sign with it, reporting what to fix when it cannot. Editing
  covers the name, the backing credential, and the granted identity; the resource
  name and algorithm are shown read-only, because a key record names one key
  permanently. The page sits behind the same `customer_managed_encryption_keys`
  entitlement as External Services.

  External credential detail pages gain a KMS Keys tab listing the keys that
  credential reaches, which is also where a refused credential delete explains
  itself.

### Patch Changes

- 18ab66a: Extending an enterprise trial now writes an entry to the organization's audit
  feed, so it is no longer the one trial lifecycle event that leaves no trace
  alongside a trial being armed, demoted and re-armed. The entry names the
  Speakeasy team rather than the operator who acted, and carries the end date the
  trial held before the extension, the end date it holds now, and the number of
  days applied. The write and the entry commit together, so an extension can never
  land silently.

  The activity log reads the new entry as "extended enterprise trial", alongside
  the "started", "ended" and "restarted" entries it already gives the rest of the
  trial lifecycle.

- 61d4baa: Fleet configuration now reports a managed tool that is absent from the
  `platforms` map as `User` rather than `Off`. The device agent's `platforms`
  map is opt-out — a tool with no entry is managed at the user layer, and only
  an explicit `false` disables it — but this page defaulted three of the four
  tools to `Off`, so an organization whose configuration predated a tool being
  added here saw it reported as unmanaged while every enrolled device was
  enforcing it. Saving the page then wrote that incorrect reading back, turning
  the tool off for the fleet as a side effect of editing an unrelated field.
  The per-tool default is now a single constant that mirrors the agent's own
  resolution, so a tool added to this list later cannot reintroduce the skew.
- 0e5a8f7: Address review feedback on the organization Encryption Keys page. User-facing
  copy now says Speakeasy rather than Gram, matching the branding convention the
  dashboard already follows, and External Services and Encryption Keys sit
  together directly under API Keys in the Settings nav.

  Overview tabs lay short fields out in columns instead of giving each one a
  full-width stacked block, which had turned a handful of scalar values into a
  long scroll. Long machine values such as a crypto key version path stay full
  width, where they read on one line rather than wrapping in a narrow column.

  An external credential can now be created from the key form itself. A key is
  unusable without one, so an organization's first key previously dead-ended on an
  empty picker linking to another page, losing whatever had been filled in.

- 92cef9b: MCP approval surfaces are now gated on `org:admin`, the same authorization as the Observe pages and the policies that do the blocking. The dedicated `mcp_approval:read`/`mcp_approval:decide` scope family is retired: it required per-organization grant provisioning that existing organizations never received, leaving every approval surface answering 403 in production. Org admins now work on deploy with nothing to provision. Delegable reviewer scopes can return additively if a customer ever needs non-admin reviewers.
- 95177df: Improve org home banner copy after enterprise setup has been started.
- 92cef9b: Clicking a user in the Shadow MCP server page's Top users table now opens their employee detail page instead of the costs explorer.
- 048e1b6: Watchdog signal drawer evidence rows now offer a single action each. Judge
  findings keep the False positive button — they cannot be excluded, since their
  rule id covers the whole detector. Every other detector's rows now show only
  Exclude, where they previously offered both actions side by side, which made
  one-off false-positive dismissal the path of least resistance over creating a
  reviewed exclusion rule.
- 60f8609: The Watchdog findings KPI now follows the selected time range. The tile was
  hardwired to the trailing 24 hours ending at the window's edge, so picking a
  different range with the date picker left the number unchanged while every
  other tile updated. The riskSignals result now reports window-scoped
  `findings` / `previous_findings` (replacing `findings_24h` /
  `previous_findings_24h`), computed from the same deduplicated window counts
  the risk score already used, and the tile compares against the equal-length
  previous period like its neighbors.
- a268d10: Withhold Polar `credits` and `included_credits` from non-platform-admin callers of `usage.getPeriodUsage`. The fields are now optional and omitted unless `authCtx.IsAdmin` is set, matching the existing admin-only billing meter in the dashboard.

## 0.108.0

### Minor Changes

- ed97a31: The MCP evidence dossier gains three deterministic sources. The assembler now
  consults the code host about a package's declared source repository (stars,
  forks, contributors, commit recency, archived status), asks OSV.dev which
  published vulnerability advisories name the package, and reads the domain
  registry's registration record for a remote server's registrable domain.
  Package registries also surface their declared repository and homepage URLs.
  Each source follows the dossier's existing contract — found, not-found, and
  could-not-look stay distinct, with failures recorded as gaps — and the
  approval page renders the new facts in the evidence panel, including a
  dedicated advisories group where checked-and-clean is shown as a finding
  rather than an absence.
- 798360b: A shadow-MCP block link now redeems into the MCP approval workflow: the blocked employee's ask attaches as a requester on the server's single review — deduplicated by canonical URL, evidence gathered — instead of minting a per-user bypass request. The redemption endpoint reports what the token turned into, keeps the legacy bypass request only for identity-only servers and organizations without the approval feature, and the standalone Approval Requests review page retires — the Shadow MCP servers table is the one review surface. The command palette surfaces pending access requests in its place.
- 798360b: Adds the MCP approval review surface, unified into the Shadow MCP servers table: every server row carries its review state, and each server's page renders the gathered evidence grouped by the question an admin is actually asking — who am I trusting, what is it asking me to hand over, what does it say it can do, is it real and maintained, are we already exposed, and what has been decided before. Every not-yet-gathered group renders as an explicitly unknown state rather than an empty one, and decisions are made in place with a required rationale.
- 523d6b1: Allow risk policies to be disabled and re-enabled from Policy Center and the policy detail page, so operators can pause enforcement without deleting the policy.
- 798360b: Unify Shadow MCP server pages with the MCP approval flow. The server detail page absorbs the approval review (evidence, requesters, decision history, decide form) and every allow/deny travels one write path: a recorded approval decision, opened on the spot for servers with no pending request. The standalone allow-rule/block/unblock/bypass action sheets are retired, approval queue rows and command-palette results land on the server page, and request links for URL targets redirect there.
- 798360b: The Shadow MCP page becomes one servers table: the inventory list now unions in review-only targets (requested-but-unobserved URLs and stdio commands, marked by a new target_kind field) on the first page, and the separate Access Requests tab is gone. Every row carries its review state with pending decisions sorted first and filterable; URL rows open the server page, stdio rows open the review sheet.

### Patch Changes

- 2d5e6bb: Platform admins can now put a demoted enterprise trial back on. Until now the
  expiry sweeper's demotion was one-way: it dropped the organization to the free
  tier, put it back behind the book-a-demo gate and switched off its model
  provider keys. Only the keys could be undone, one at a time, through the
  existing admin action for enabling a key; the tier and the gate had no undo at
  all. An operator who wanted to give a customer a second run had no way to do it,
  and extending the trial was not the same thing, because an extension moves an
  end date and leaves the free tier exactly where the demotion left it.

  Re-arming restores all of it at once: the account type the trial grants, the
  whitelist flag, every model provider key the demotion switched off, and a fresh
  run of the length the operator asks for, capped at a year and counted from now. The end date is
  counted from now rather than added to the old one on purpose, because a demoted
  trial's end date is already in the past and adding to it could land in the past
  again, which would leave the sweeper free to demote the organization a second
  time within the hour.

  One caveat on the keys: this is the deployed behaviour. A local development
  stack has no OpenRouter account behind it, and its stand-in client accepts the
  refresh without doing anything, so a re-arm there reports success and restores
  the tier while both key rows stay switched off. Enabling a key locally has the
  same gap.

  The keys come back up before any of the database changes are committed. That
  ordering is the opposite of the demotion's, and it is deliberate: if the model
  provider refuses, the organization stays demoted and on the free tier, and the
  operator can retry. Any key that came back up before the refusal stays up, which
  is what makes the retry cheap. The alternative would advertise a running trial
  to a customer whose keys were still switched off.

  Only a demoted trial can be re-armed. A trial that is already running is
  rejected, so re-arm cannot be used as an extend that ignores the extension
  rules. An organization id that matches nothing is reported as not found, the
  same answer the disable, enable and extend actions already give, so a mistyped
  id does not send an operator off to inspect a trial that was never the problem.

  The activity log reads the new entry as "restarted enterprise trial", credited
  to the Speakeasy team rather than to the operator who ran it, which is the same
  label the log already gives a Speakeasy action inside a customer's
  organization. The admin dashboard row action follows.

- 7da1436: The activity log can now record and render a restarted enterprise trial. The
  entry reads "restarted enterprise trial" and is credited to the Speakeasy team
  rather than to the individual operator, which is the label the log already gives
  a Speakeasy action taken inside a customer's organization.

  Nothing produces the entry yet. The admin action that restarts a trial follows
  separately, and this change is the log's half of it: the action name, the writer
  that records it, and the phrase the dashboard shows for it.

  The collective "Speakeasy Team" label now has one definition instead of two.
  The activity log applies it on read, by matching an actor against the members of
  the Speakeasy organization. A writer that already knows it is acting as staff
  has to apply the same label when it records the entry, because the read-time
  mask can only recognise an actor that has a Gram user id, and an operator
  authenticated through the admin app does not have one. Both paths now read the
  label from the same constant, so one action cannot appear under two different
  names depending on which path wrote it.

- bbaf839: Registers the MCP approval resource type in the role editor's scope picker so the new mcp_approval:read and mcp_approval:decide permissions can be discovered and granted.
- 5016dca: Org home now opens with a welcome banner offering three first moves: enter the
  demo org, get started in a project, or start the enterprise rollout. The third
  card needs an enterprise admin; the other two are open to any member. The
  header's "Finish setup" banner stands down while the banner shows. It appears
  for everyone for now; which orgs count as new is follow-up work.

  Below it, the project search, view toggle, and Add New move into the column they
  act on, and the two rails cap their height and scroll.

## 0.107.1

### Patch Changes

- d7dca3d: Add exact range-bounded activity totals and independent pagination to assistant sessions.
- 5737ee7: Add three browser hardening headers to the dashboard HTML responses: `Cross-Origin-Resource-Policy: same-origin`, `Cross-Origin-Opener-Policy: same-origin`, and `X-Permitted-Cross-Domain-Policies: none`. A penetration test reported all three as missing. Each one is set per location, because an `add_header` inside an nginx location discards every `add_header` inherited from the server block. Static assets under `/assets` and `/external` keep `Access-Control-Allow-Origin: *` and receive no cross-origin policy, so cross-origin image loads continue to work.
- dda81c1: Land `/explore-demo` on the demo org's default project instead of org home, so new visitors see sample data without having to click into the project themselves.
- 1b00702: Scope device agent fleet configuration to organization admins. Viewing it
  (`agent.getConfiguration`) now requires `org:admin`, matching the existing
  requirement on `agent.updateConfiguration`, and the dashboard hides the Device
  Agent Configuration tab from non-admins. The Setup tab stays available to
  organization readers.
- 1fb8f18: Fix "Suggest with AI" exclusion suggestions being rejected as invalid regexes. The exclusion form now validates regex criteria with the same RE2 engine the platform matches with, so valid suggestions like `(?i)`-prefixed patterns save instead of failing with "Invalid regex pattern", server-side validation errors surface in the form, and a suggestion that fails validation is retried once with corrective feedback before falling back.

## 0.107.0

### Minor Changes

- ae7f01b: The assistant detail panel is now fully configurable and observable. Overview settings (name, model, concurrency, warm TTL) are editable in place. The Sessions tab shows aggregate stats (sessions, messages, cost, tokens) over a selectable time range defaulting to the last 30 days, with per-session cost in the list. Triggers expand in place to show their recent traffic via the new `triggers.listEvents` endpoint, with each dispatched event linking to the conversation it was routed to.
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

- e3bb138: Render `@tool` and `/skill` references in the assistant composer as colored
  chips, and make the composer a contenteditable so they can be real inline
  elements rather than paint under a transparent textarea.

  A textarea holds one flat string, so a reference inside a draft could only be
  mirrored underneath the input — and anything painted there that occupied width
  slid the caret off the glyphs after it. The input is now a `plaintext-only`
  contenteditable whose chips are `contentEditable={false}` spans with real
  padding, so the browser places the caret around them. The draft still lives on
  the runtime as a string: the element reports edits as text and exposes
  `value` / `selectionStart` / `selectionEnd` / `setSelectionRange`, so the
  mention autocomplete, prompt recall, and dictation are unchanged.

  Skills move into the draft with them. Picking a skill writes its `/name` token
  into the text (and focuses the input, caret at the end) instead of only
  toggling hidden state, and the composer derives the attached-skill set back out
  of the draft — so deleting the token detaches the skill, and a message carrying
  nothing but a reference can be sent. Tool names containing hyphens now match at
  all; hyphenated source-slug names previously stopped at the first hyphen and
  resolved to nothing. The badge rows above the input are gone, since the tokens
  name themselves, and sent messages render the same chips in a bordered
  white bubble.

- c66958e: Agent sessions routed through LiteLLM keep their LiteLLM association even when the agent's own hook stream captures the transcript: they match the LiteLLM platform filter and display as "<Client> via LiteLLM" in the session list and detail views.
- d893bcb: Fix natural-language input in the dashboard time range picker. The picker's
  "type any date" parsing POSTs to `/chat/completions`, which requires both
  `Gram-Session` and `Gram-Project` headers, but most pages rendered the picker
  without a `projectSlug`, so the request 401ed and parsing silently did nothing.
  The `DashboardTimeRangePicker` wrapper now injects the request project slug
  via `useProjectSlugForRequests()` (callers can still override it), fixing the
  project home, MCP overview, security overview, watchdog, and risk overview
  pages in one place — and org-scoped pages like Billing through the same
  default-project fallback every other SDK request uses.

## 0.106.0

### Minor Changes

- 6f8d740: Configure which OAuth Client ID Metadata Document clients an MCP server accepts, directly from its authentication settings: choose between Gram's verified client catalog (viewable inline), any spec-valid client, or none at all, and allow additional document URLs beyond the catalog. Custom URLs can be verified before they are added, confirming the document is reachable and valid and naming the client it belongs to.
- 8ae2c53: Revoke Remote Session credentials upstream via RFC 7009. Remote session issuers gain a `revocation_endpoint`, discovered from the issuer's RFC 8414 metadata document during issuer refresh. When a Remote Session is revoked, Gram now posts the stored token to the issuer's revocation endpoint so the upstream authorization server drops it, instead of leaving a live access/refresh pair that keeps working elsewhere until it expires on its own clock.

  This covers every path that ends a session: revoking one session, revoking all of a client's sessions, deleting a client, which cascades a soft-delete to its sessions, and the consent screen's per-provider "Disconnect" — the one an end user drives rather than an admin. Batches run under bounded concurrency and a single budget for the whole batch, since every session on a client shares one upstream host.

  The upstream call is best-effort by construction: it runs after the local revoke has committed, is bounded by a short timeout, and routes through the guardian egress policy. Failures are logged and metered, never surfaced to the caller — the local revoke is the security control the caller asked for and it has already succeeded. Issuers that advertise no revocation endpoint are recorded as a distinct `skipped` metric outcome rather than folded into success or failure, since that is the expected case for a large share of upstreams. A batch that exhausts its budget before reaching every session records the remainder as `dropped` rather than passing them off as done.

- 7df6ad7: Add a new UI for the Watchdog page.
- a2b272c: Warn when an identity provider duplicates an issuer URL that already exists, at all three tiers and on both create and edit. The warning is advisory and never blocks the write, since duplicating an issuer has legitimate use cases.

### Patch Changes

- 0e614ac: chat.list accepts a `user_id` filter so callers with project-wide chat visibility can narrow results to a specific Gram user. The Project Assistant dock uses it, together with the dashboard source-kind filter, so "Continue chat" only offers sessions the viewer started from the dashboard.
- 3fb5ea2: Make skill and environment resources clickable on the access challenges page. Skill rows now link to the project's Skills page and environment rows link to the project's Environments page, instead of rendering a bare resource id.
- 43107ac: Add compact tool-call rows with separately loaded, persisted two-sentence summaries and risk-first detail expansion.
- 07e96cf: Hide the composer's cycling example prompts once a file is attached.
- f552a11: The chat composer recalls past prompts terminal-style: Up walks back through prompts sent from this browser, Down walks forward, and the walk wraps around through the empty draft. History is kept in localStorage, scoped per project.
- 0e7eb7f: Give the composer's "Add context" picker room to read: a wider pane, tool rows titled by their unqualified name so they stop truncating into identical ellipses, a single type scale across both halves, and a labelled header over the results so skills and tools are told apart while browsing, not only while searching.

  The slash-command menu now drops below the composer when there is not enough room above it, so it stays on screen on the welcome surface instead of opening past the top edge.

- 8589630: Read the employee page's usage stat tiles from the per-user metrics query
  instead of the employees-list summary. The list query groups a
  person's telemetry by identity, so those tiles were showing one identity's slice
  of the usage — for someone on a personal AI account, often the slice with no
  tokens or cost in it. The metrics query aggregates the same person's rows
  without grouping them, so the tiles now show their whole total. A failed usage
  query also surfaces as an error instead of rendering as a legitimate-looking
  zero.
- 5ffabf3: Freeze external key identity: `externalKeys.updateAwsKms` and `externalKeys.updateGcpKms` no longer accept `key_arn` / `resource_name` or `algorithm` and cover only `name`, `external_credential_id` and `customer_grant_reference`, so changing what a key is now means deleting it and creating a new one (a breaking change to those two methods). Deleting a key is refused with a conflict while a JSON Web Key Set or published key still references it, and `createGcpKms` now requires a fully-qualified crypto key version path.
- 8540e53: Link durable block pages to the owning project's risk event log.
- 8f3fb58: Show the supported client that originated an Agent Session routed through LiteLLM while preserving LiteLLM filtering.
- abcde04: Logging out now returns `Clear-Site-Data: "cookies", "storage"`, so the browser drops the session cookie across the origin's registrable domain and empties localStorage, sessionStorage, IndexedDB and Cache Storage. Previously teardown relied entirely on an expiring `Set-Cookie` plus a best-effort localStorage sweep, both of which leave data behind when a cookie attribute drifts or a page navigates away mid-logout. The theme preference and project favorites still survive a logout: the dashboard reads them before the request goes out and writes them back once the response lands.
- 164f45d: Merge the composer's two `@` buttons into a single "Add context" picker covering both skills and tool mentions.
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
- af354b1: Add a PAYG rate adjustment (%) input to the platform-admin TUM contract price estimator on the billing page. A positive percentage uplifts every pay-as-you-go band rate and a negative one discounts them, so an admin can price a negotiated swing off the list rates without retyping the bands. The adjustment is reflected in the PAYG card's tier table, blended rate, and the committed-vs-PAYG comparison, and a discount-driven "PAYG is cheaper" outcome is attributed to the adjustment instead of being flagged as a pricing-model error.
- 530feba: Attach files to the Project Assistant. The composer accepts files from the paperclip or by dropping them anywhere on the chat, and the assistant can read them: images and text-like files (including OpenAPI specs) travel with the turn, and anything it cannot read inline comes with a short-lived download link.
- c07751f: Split skill details into focused pages for content, usage, feedback, versions, and settings.
- e8d3459: Score Watchdog signals from the matched risk policy's configured score, and simplify the signal drawer to a single Create-exclusion action.
- 748871c: Watchdog trend percentages now measure change within the current window so
  they move with the sparkline. Fix category badge labels rendering with
  clipped glyphs.

## 0.105.0

### Minor Changes

- 6228ad5: Introduce a paint-by-numbers page-template layer so dashboard pages share one structure. Adds `ResourceListPage`, `DetailPage`, `TabbedPage`, `FormPage`, `SettingsPage`, `OverviewPage`, `WorkbenchPage`, `WizardPage`, `CenteredPage`, and a `FullBleedPage` escape hatch (in `@/components/page-templates`), plus composite widgets `InlineEmptyState`, `StatRow`, `SummaryCard`, and `DetailBody`. Migrates ~34 pages onto the templates.

  Consolidates the design-system primitives: removes the dead `Modal`/`IconButton` subsystem, folds `PrivateInput` into `Input` (new `reveal` prop), `DashboardCard` into `Card.Dashboard`, `ToggleButton` into the `SegmentedControl` module, and `Editable` into `editable-text`; renames the analytics tile `chart/MetricCard` to `StatTile` so `MetricCard` is the sole primitive; and promotes the shared detail-page primitives (`SettingsSection`, `DetailSidebarNav`) out of the mcp path into `@/components/detail`.

- 91f8234: An organization whose enterprise trial has ended now lands on a page that says so and books an upgrade call, instead of the generic book-a-demo screen a company that had never heard of Gram sees. Anyone still inside a trial can reach the same page from the sidebar countdown to upgrade early.
- 3705830: Scan captured skill manifests for prompt injection at capture time and show current-version findings on skill details. Admins can configure the existing Prompt Injection policy from the Skills page. A completed judgement records either a finding or clean coverage; unavailable judgements are retried on a later activation and never become durable clean results. Scanning never fails the upload. Coverage is usage-based rather than catalog-based, so a version no agent ever loads is never judged.

## 0.104.0

### Minor Changes

- 5027338: The MCP server Clients and Sessions tab now leads with active session and client counts, and renders both listings as searchable, filterable, sortable tables paginated ten rows at a time, with member avatars and creation dates on sessions. The clients table reports how many active sessions each client holds, backed by a new `active_session_count` field on the user session clients API, and clicking that count narrows both listings to that client behind a clear-filter bar.

### Patch Changes

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

## 0.103.0

### Minor Changes

- e136806: MCP server detail pages gain a Clients and Sessions tab, which lists the clients registered against the server's session issuer alongside its active sessions, and takes over the user-sessions listing that previously sat under authentication settings. CIMD-resolved OAuth clients are now distinguished from DCR-registered ones in the dashboard.
- 76592ef: Rework the toolset OAuth configuration UI now that the OAuth proxy provider system is removed. The "Configure OAuth" wizard keeps its structure, but its custom path now provisions a user session issuer (creating a remote_session_issuer + remote_session_client and linking the toolset) instead of an OAuth proxy server; the external-OAuth path is unchanged. The separate migrate-from-proxy modal and the Platform/Edit OAuth-proxy modals are removed.
- f6df724: Add opencode to the managed tools list on the device agent fleet
  configuration page, with the same off/user/managed enforcement layer
  selection as the other supported tools.
- d80c633: Restyle the dashboard to the new editorial design language: flat square
  surfaces with hairline borders, serif display page titles with area
  micro-labels, unified uppercase table headers, colorized metric tiles, a
  restrained ink-and-brand chart palette with a dark-mode ramp, muted avatar
  tints, and a styled not-found page for unmatched routes.
- 46a645f: Platform admins can now consolidate an organization's remote identity provider onto the shared platform catalog entry for the same upstream. A new Convergence tab on a platform provider lists the organizations running their own provider for that upstream, along with how many clients would move and any metadata differences, and consolidating one re-points those clients without anyone having to sign in again. Providers whose issuer URL differs only by a trailing slash or an explicit default port now count as the same upstream, since those near-duplicates are the ones most worth folding together.

### Patch Changes

- 04679cd: Show Agent Plugins compatibility status and portable ZIP downloads on plugin list and detail pages.
- 7b746d3: Fix AI Integrations poll-cadence labels to match actual sync intervals: Anthropic Compliance activity feed now reads "Every 5m" (was "Every 10m"), Claude Chat usage/cost metrics now read "Every 4h" (was "Hourly"), and Codex & ChatGPT cost metrics now read "Every 5m" (was "Hourly").
- a9dd912: Clean up the floating "Bulk actions" toolbar on Risk Events and Risk
  Overview category tables: removed the doubled border (the toolbar and its
  "Bulk actions" trigger button each had their own), which read as a boxy
  nested-border look, in favor of a single borderless pill with just a
  shadow. Also insets the toolbar a few pixels from the table's header row
  instead of sitting flush against its top edge.
- b6e2941: Extend the brand-mesh surface treatment from the project home assistant
  card to the full `/chat` landing page: the same neutral card-to-background
  gradient with the brand rainbow breathing in from the top-right corner and
  a film-grain wash. The decorative layers are now a shared `BrandMeshLayers`
  component so the two surfaces can't drift apart, and the landing's scroll
  container moved to an inner wrapper so the mesh and the back button stay
  pinned while the content scrolls.
- 0c1e8b6: Update the macOS setup walkthrough on the Device Agent page to install from
  the signed `.pkg` instead of the retired curl-download-and-chmod script. The pkg installs the daemon, CLI, menu-bar UI, and privileged
  helper together and registers its own LaunchAgents, so the walkthrough now
  covers a manual `installer` run or a normal MDM Package push instead of a
  separate download/chmod/service-register sequence. Windows and Linux are
  unaffected — they still ship as raw binaries.

  Also makes Device Agent the default choice (instead of Manual Setup) on the
  "Instrument agent platforms" onboarding step, and drops its "Preview"
  badge — it's out of preview.

- 8a85b99: Relabel the amber badge that marks platform-admin-only UI from "Dev" to
  "Internal Admin". The badge appears on the platform admin toolbar and on
  admin-only fields of the device agent configuration page; "Dev" read as
  "development build" rather than "visible to Speakeasy staff only", which
  is what it actually means. Renamed the component to `InternalAdminBadge`
  to match.
- 0fad940: Stop the Policy Center table from printing the same thing twice per row. The "Categories / Prompt" column has been folded into the policy column as a second line that only appears when the policy name doesn't already convey it — so "Secrets Exposure Flagger" no longer sits next to "Secrets", and a prompt-based policy whose name is an excerpt of its guardrail shows the name alone. Policies whose name omits a category (or whose guardrail says more than its name) still show the detail, now with the freed-up width.
- 6ff5ca0: Add user-opted-in automatic remote session token refresh and hide its organization settings until enabled.
- 793abde: Public skill share links now use your custom domain when one is verified and activated. The share page (and a raw SKILL.md download) is served at `https://<your-domain>/shared/skills/<token>`, scoped so a domain can only ever serve skills belonging to its own organization, and the dashboard copies links with the custom domain automatically.
- bb83418: Make clickable table rows keyboard-focusable and activatable with Enter or Space.

## 0.102.0

### Minor Changes

- 546c449: Collect a work email on the sign-up page and hand it to the hosted AuthKit
  screen. `auth.login` takes an optional `email`; when a login carries a company
  name — the marker that it began on `/sign-up` — the server sets WorkOS's
  `login_hint` so the email field arrives pre-filled, and `screen_hint=sign-up` so
  the user lands on the sign-up screen rather than sign-in. The email is validated
  before the login nonce is minted and is never stored. The call to action now
  reads "Start Trial"; it previously named a single identity provider, which
  misdescribed a hand-off that has always been generic.
- 54755b5: Add a Device Agent configuration tab for organization administrators to choose
  per-tool enforcement layers, release policy, and reconciliation cadence.
- 13301b5: Add a self-serve path into the shared read-only demo organization. A new `auth.enterDemo` endpoint switches any authenticated session into the demo org (no membership required); request auth, grant resolution, and member/role listings gain demo carve-outs; the demo org always enforces a fixed read-only scope set with a verb-based write guard as backstop. The dashboard gains an `/explore-demo` entry route, an "explore a live demo org" link on the book-a-demo gate, and a demo banner whose exit switches back to the user's own organization without logging out.
- 909b466: The External Services page is now organization-scoped: org admins register how Gram authenticates into their own cloud account, behind a new `customer_managed_encryption_keys` entitlement enforced on both `externalCredentials` and `externalKeys`. The platform-admin UI is removed, though its endpoints remain for HTTP-only management. Two new methods support verification: `externalCredentials.verifyGcpIam` probes that Gram can actually impersonate the named service account, and `externalCredentials.getGcpSetupInfo` reports the Gram service account a customer must grant `roles/iam.serviceAccountTokenCreator` to.
- f95d50f: Platform admins can now curate the shared remote identity provider catalog from the dashboard, under a new Platform Admin section in the sidebar: list, create, edit, refresh discoverable metadata, and delete the providers that every organization inherits. The listing reports platform-owned and tenant-owned client counts separately, so a delete that will be refused says up front which blockers the admin can clear and which belong to an organization. `adminRemoteSessions.listGlobalIssuers` and `adminRemoteSessions.getGlobalIssuer` now return both counts alongside the issuer. Organizations can register a client against an inherited platform provider straight from their own provider list.
- 546c449: Add a `/sign-up` page that collects the company name before handing off to the
  identity provider. `auth.login` takes an optional `org_name` param; when set, the
  server validates it and stashes a signup intent against the login nonce, then
  creates the organization during the auth callback once the identity provider has
  answered. The name never travels through a redirect param or the address bar, and
  a failed signup returns to `/sign-up` rather than `/register`. Signup attempts and
  the resulting org creation are captured as `onboarding_event` / `new_org_created`
  with `created_via: "signup"` so the funnel can be measured end to end.
- ca3e972: Show active trial status in the project and organization navigation, including the current trial day, remaining days, elapsed progress, and a link to Sales.

### Patch Changes

- 847a496: Adding Figma from the MCP catalog now connects your project directly to Figma's official server instead of routing through a proxy, so there's nothing to authorize or allowlist.
- 0afb752: Import ChatGPT conversations from the OpenAI Compliance Logs Platform. A new `chatgpt_compliance` AI-integration provider polls workspace-scoped `CONVERSATION_MESSAGE` log files (the supported successor to the deprecated stateful conversations endpoint) and persists them as external chats and messages — the same tables and Agent Sessions surface the Anthropic compliance import feeds. The provider is separate from `codex_compliance` because the scopes differ: COSTS files are per API organization while conversation logs are per ChatGPT workspace, so the new config takes a workspace UUID. Includes the workspace-scoped compliance client, Temporal schedule wiring, and a "ChatGPT Conversations" integration card in org settings.
- 7fd5e1a: Classify Codex account identity and billing mode (DNO-734). Codex sessions on
  every capture path (legacy hooks, OTEL logs, ingest adapter) now stamp
  account_type from email resolution — resolved work email is team, anything
  else personal — and team sessions resolve the org-level billing mode declared
  on the codex_compliance integration config (the session provider "openai" now
  maps to that config, fixing the mapping bug that made the config's
  billing_mode unreachable). Compliance COSTS import rows (codex and
  ChatGPT/Work) carry account_type=team and the config's billing mode directly.
  The estimated-cost tooltip copy mentions ChatGPT plans alongside Claude's.
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
- 230744c: Refresh the Codex setup copy for the unified ChatGPT desktop app (DNO-737).
  The tile referred to a standalone "Codex desktop app" that OpenAI has since
  merged into the ChatGPT app, and now states that Codex mode there is covered
  while Chat and Work modes are not — those are captured through the OpenAI
  Compliance API integration instead. The two OpenAI hook/plugin doc links were
  redirecting and now point at their current destinations.
- 547bb72: Reduce the Skills table to its most useful overview columns and rebalance their widths for easier scanning.
- da7e758: Display enabled warning policies in the Shadow MCP inventory status card.
- f926dc1: Add project-scoped LiteLLM integration provisioning, key rotation, revocation, and lifecycle metadata APIs.
- 20662cc: The project sidebar now keeps nav groups collapsed by default: only the group containing the current page opens automatically, and a new chevron on each group header lets you pin other groups open without navigating. Vertical spacing between and within groups is tighter, so more of the nav fits on screen.
- ff02538: Make the root TypeScript check pass and keep generated SDK warnings out of dashboard linting.
- 817174d: Make RBAC always on, provision built-in roles and grants for new organizations,
  and assign the first organization user the Admin role.
- 9f35728: Make Skills table columns sortable from their headers while preserving recently updated as the default order.
- ba561ad: Webhooks are now available to every organization, marked Beta. The Webhooks page
  no longer shows a preview gate, and delivery is controlled solely by the
  organization's own webhooks toggle.

## 0.101.0

### Minor Changes

- 73b9590: Requests and approvals now understand allow-all shadow MCP policies. Approving a bypass request (from the approvals page or the inventory review flow) on an allow_all policy revokes the server URL's risk_policy:block grant — a project-wide unblock with no principal-scoped bypass grants. Revoking restores the block grant; denying leaves it untouched. The request status change is audited like every other bypass-request decision. The approval UIs skip the audience/policy pickers for these requests and explain that approval unblocks the server for everyone in the project.
- bd3aac6: Shadow MCP inventory and status surfaces are now disposition-aware. Under an allow-all policy the inventory reports servers as allowed by default and blocked only when a block rule lists them, the policy status banner explains the allow-by-default posture, and the primary per-server action flips from managing allow rules to Block Server / Unblock Server — which add and remove the policy's risk_policy:block grants through dedicated inventory endpoints.

## 0.100.0

### Minor Changes

- 944c4df: Shadow MCP policy creation now offers a default-disposition choice: Block all servers (allow exceptions) or Allow all servers (block exceptions). The server selector flips between picking allowed and blocked servers to match, and the disposition is shown read-only when editing since switching posture requires deleting and recreating the policy.
- b131cea: Project assistant tool calls now render Claude-style: the assistant precedes each tool batch with a terse activity phrase ("Investigating failures in the last 30 days") which becomes the heading of a single collapsed tool group. Consecutive batches merge into one group whose heading advances (with shimmer) as the investigation progresses, groups never auto-expand, and the global thinking loader hides while a tool group is streaming. The dashboard output-channel guidance instructs the model to emit the phrase before every tool call.
- 41626e6: Add a contract value estimator to the TUM Contract section of the billing page (platform admin only). It approximates what an enterprise account is worth under either commercial model — a committed platform fee with tiered overage, or uncommitted pay-as-you-go — off the org's observed tokens under management, and flags accounts whose overage has grown large enough relative to their base contract to warrant an expansion conversation.

### Patch Changes

- 6ca548f: Add the `chatgpt` and `chatgpt-work` sources to the product-surface taxonomy now that ChatGPT/Work compliance usage is admitted to the summaries. The compliance importer now routes Work rows to the `chatgpt-work` hook source (ChatGPT and unknown surfaces stay `chatgpt`) so the per-product split survives summarization — hook_source is a summary GROUP BY dimension while the raw `codex.compliance.product` attribute is not, and summaries outlive the raw-row TTL. Also: a `chatgpt` chat source alias, ChatGPT/ChatGPT Work labels and the OpenAI mark in the dashboard label/icon maps and onboarding live-tail, broadened "OpenAI Compliance Logs" settings copy, and local seed fixtures emitting compliance-shaped `chatgpt:usage:metrics` rows for both products.
- dd9e519: Announce the contract estimator's load failure to screen readers. The message replaces a loading skeleton after the page has settled, so without a live region there was no indication the estimate had failed or that reloading was the way to recover.

## 0.99.0

### Minor Changes

- c0395b5: Selectively extract high-value, reusable business memories from completed chats, omit semantic duplicates, and add an organization-admin corpus browser with semantic search, source-transcript navigation, and a complete content-scope tree with distinct-memory counts.

### Patch Changes

- d243364: The billing page's usage card now follows the selected time range instead of always showing the full billing cycle. A custom range (typed, calendar-picked, or bar-click drill-down) shows the range's billed tokens, a time-attributed overage figure (tokens recorded after the cycle's cumulative usage crossed the allowance, crossing day prorated), and the per-unit average over the range window; the allowance figure, percentage, and meter remain cycle-only concepts and hide on partial ranges. The details table's Overage column uses the same attribution for ranges instead of showing an em dash, so the card and table always agree.
- 57ff66a: Add a typed, reactive PostHog feature-flag hook that distinguishes loading, enabled, disabled, missing, and error states. Project navigation now uses the hook as an integration proof while preserving existing opt-in and opt-out behavior.

## 0.98.0

### Minor Changes

- 3b66258: Custom domains can now route their root URL to a default MCP server. Pick one of the domain's MCP endpoints as the default and `https://your-domain.com/` serves that server directly — MCP clients connect at the root and browsers see the installation page — while renaming the endpoint's slug updates the routing automatically. Custom domains can also serve an OpenAI app-submission verification token at `/.well-known/openai-apps-challenge`, so ChatGPT app reviews can verify domain ownership without any changes on your site. Both settings live on the custom domain page; the default server can also be set from an MCP server's own settings.

### Patch Changes

- 80b855f: Stop enumerating supported coding agents (Cursor, Claude Code, Codex, …) in Shadow MCP detector copy and other user-facing product strings. Prefer generic wording so new agents like opencode do not require list updates.

## 0.97.0

### Minor Changes

- 2822d51: `remoteSessionIssuers.get` can now look an identity provider up by its upstream issuer URL, returning the one the project would use (preferring project over organization over platform) or 404 when nothing describes that URL yet. The dashboard's automatic setup flows use it to decide whether to reuse an existing provider instead of scanning the provider list in the browser, which also lets them reuse platform-catalog providers for the first time.
- 56aa9f2: Restructure the Device Agent MDM integrations page around the source → coverage → destination pipeline. A pipeline banner shows live connected counts (updating as connections are enabled/disabled) and the org-wide fleet coverage, over two role-labeled groups. Detail pages are now role-specific: inventory sources keep their device inventory and "synced" language, while evidence destinations drop the inventory table (a sink owns no devices), show what they publish org-wide, use "pushed" language, and surface a "Fleet sourced from" breakdown linking to the sources that feed them.
- 5bf2d45: Select project skills as additional context for an individual Project Assistant turn.

### Patch Changes

- 936828a: Fix the agent-coverage meter reading as far more filled than its real percentage in dark mode. The track used `bg-muted`, which collapses to the card color in dark mode, hiding the uncovered remainder so the filled portion looked much larger than its true share. The track now uses a foreground tint that stays visible on both light and dark grounds.
- b5f47cb: Tuck optional device-integration settings behind an "Advanced" disclosure in the connect/configure sheet, so the default view shows only the required fields (e.g. Drata is just Region + API Key + Test — the Custom Connection ID is created automatically). Driven by the descriptor's `required` flag, so it applies to every provider; the section auto-expands when an optional field already holds a value.
- 19f2841: Apply RBAC grants regardless of account tier and remove the dashboard rollout flag.
- 1a74d9d: Restore external OAuth configuration on eligible MCP server authentication pages.
- 36aa306: Improve external OAuth setup with automatic metadata discovery, inline validation, connection testing, and clearer authentication actions.
- 1ad71b0: Hide Microsoft Intune from the MDM integrations UI until it is fully supported — a central `isProviderVisible` filter removes hidden providers from the list, the pipeline source count, the fleet-source breakdown, and direct-URL access, while leaving backend registration untouched. Also pluralize the pipeline agent-input label to "Active agents" and label its source as "reported by device agent".
- b5f47cb: Hide the Vanta evidence-push provider from the MDM integrations UI until a supported path exists — Vanta does not support custom resources for partner-built integrations. Uses the existing `isProviderVisible` frontend gate, so backend registration is untouched and revealing it later is a one-line change.

## 0.96.0

### Minor Changes

- 4bf8450: Let tenants inherit and attach clients to platform (global) remote identity providers while the issuers themselves stay read-only, and keep tenant clients on a platform issuer fully manageable through the organization-admin surface. The dashboard renders the new `Platform` tier and resolves issuers by `project > organization > platform` precedence.
- 725bfaa: Skill edit suggestions now support batch apply: select individual proposed changes and apply them together as a single new version. The batch controls moved from the per-change comment box to a control bar above the diff.
- 598799f: Add the MDM Integrations dashboard: an org-level catalog page listing MDM
  and compliance providers from the backend registry, with a credential sheet
  rendered from each provider's field spec (secret values masked and
  write-only), a save → test-connection → enable flow, and per-schedule sync
  status with pause/resume and sync-now controls. The Device Agent page gains a
  coverage section joining MDM-managed devices against agent heartbeats, with
  per-bucket summary tiles and a filterable device list that visually
  distinguishes the stale-agent drift case. Both surfaces are gated behind the
  `gram-device-integrations` PostHog flag.
- 432d06c: feat: device-level agent coverage behind a rollout flag

  Coverage can now match a device's hardware serial against per-device agent
  heartbeats instead of its assigned-user email, falling back to email when no
  serial match exists. Adds an `agent_other_device` bucket for "the user runs
  the agent, just not on this machine", and an `attestation` field so clients
  word the coverage claim to match the mode. Gated per org by the
  `device-level-coverage` PostHog flag; evidence pushes stay user-level until
  the sink field names change with them.

- 811d494: automatically publish tool metadata from dashboard
- a1e2015: opencode observability: surface opencode as a hook source in the dashboard (icon, platform filters) and add its install instructions to the setup flow.
- ce74cd3: Paginate scored skill sessions, collapse their table by default, and link chats to the agent sessions explorer.
- 43018c5: permission remote MCP tools by name in the role editor

### Patch Changes

- 7147f70: Persist edits made from the Playground's Add/Manage Tools dialog. Failed edits remain open for retry instead of reporting success before the update is saved.
- b6d3a27: Add skill feedback metrics, grouped review evidence, and manually triggered suggestion analysis.
- 703756b: Add `fetchMetadata` and `refreshMetadata` across all three remote identity provider tiers. `fetchMetadata` is keyed by issuer URL and persists nothing, as the pre-create step; `refreshMetadata` is keyed by issuer id and re-reads an existing provider's RFC 8414 document, persisting only discovered values (endpoints, the `*_supported` arrays, `client_id_metadata_document_supported`, and the documentation URLs) while leaving Gram's own behavior and display fields untouched. A "Refresh Discoverable Metadata" action is available from the Remote Identity Providers listing.
- c93a9e1: Show the judge's rationale for prompt injection and prompt-based policy findings in Risk Events and the risk overview category drill-down, and drop the rule label for those findings since it only restated the category.
- 023bec6: Use the searchable multi-select skill picker when attaching skills to assistants.
- 6cc0201: Redesign the "Book a demo" enterprise gate on the shared auth shell, so it matches the login, register, and switch-organization screens. The page now carries the brand gradient strip, the control-plane header with a Log out action, and the animated governed-agent session alongside the booking card. The Cal.com embed is framed as a native booking card — its own theme variables are set from the auth-brand palette so the calendar reads as part of the page rather than an embedded iframe — and the details handed to the calendar are footnoted underneath.

  The embed's appearance settings are now sent through Cal's `ui` instruction. They were previously passed as prefill config, where Cal ignored them, so the embed had been rendering its own event-title block over the card header and none of the brand colors were applied.

- 7734a63: Chart skill activations by version across the rolling 30-day window.
- 4225015: Custom domains that stay unhealthy for over a week (7+ consecutive failed daily checks) are now automatically disabled: their routing and TLS certificate are removed, and the dashboard explains what went wrong and walks admins through fixing the issue and reverifying the domain. Gram-side check failures never count toward disabling.
- d3ad7d3: Add the device integrations framework: a capability-based provider registry (`InventorySource` for MDM fleet pulls, `EvidenceSink` for compliance evidence pushes) and a new `deviceIntegrations` management service. Organizations can connect a provider with secret credentials stored as an encrypted write-only document and non-secret settings kept readable, validated against the provider's declared field spec; credential rotation updates the config in place so synced device inventory is never orphaned. The service exposes provider discovery (credential specs drive dashboard form rendering), config CRUD with audit logging, a bounded test-connection probe through the SSRF-hardened guardian client, per-schedule state with distinct user-disable and system-auto-pause semantics, and agent-coverage reads: a bucketed summary (active / stale / no agent / no email / unresolved / missing, plus unmanaged agent users) and a paginated device listing, both computed as read-time joins between MDM inventory and the per-user agent heartbeat.

  Settings updates merge per key with the stored document (omitted keys keep their values), credential rotation resets the schedules' sync execution state and pushed-snapshot digest, and audit before-snapshots are read inside the upsert transaction. The dashboard's OTel forwarding section is updated for a generated SDK type rename.

- 3558aa7: Device integrations: enabling a connection (and "Sync now") triggers the
  sync coordinator immediately instead of waiting for its next tick; the
  configure sheet disables the connection test while the draft has unsaved
  changes and explains that tests run against saved credentials; managed
  device and schedule tables get properly spaced empty states; and the Iru
  provider rejects the tenant console URL with an error naming the correct
  API URL.
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
- c5ca622: Add the Jamf Pro inventory-source provider to the device integrations framework, plus its dashboard presentation entry (Apple-fleet icon and console setup steps for minting the least-privilege API client). Organizations connect a Jamf Cloud tenant with an instance URL and least-privilege API Client credentials (an API Role with only "Read Computers"); the provider authenticates via the OAuth client-credentials grant with the token cached until expiry, pulls the computer inventory in stably ordered, section-filtered pages, and maps each device's serial, hostname, OS, assigned-user email, and last check-in into the managed-device store — preserving the full vendor record. Credential rejections, including tokens expiring mid-pull, classify as auth errors feeding the scheduler's auto-pause streak, and every API request carries the unique User-Agent header the Jamf Technology Partner Program requires.
- df696de: Page the skills table and move its default search, filters, and sorting to the server.
- 8eafabf: Tunneled MCP servers can now be published with public visibility, letting anyone call them anonymously with no login. Turn on **Public Access** for a tunnel source, then set an MCP server fronting it to Public. Public tunneled servers expose every tool to the open internet, so a high-friction confirmation guards the toggle and the MCP server visibility control stays locked to Private until the source opts in.
- 3f11ea3: Remove the unused Redis-backed Shadow MCP access-rule and approval-request API in favor of risk policy bypass grants.
- d960f03: Show the required permission on the page-level access-restricted screen. A "What access do I need?" disclosure expands into a bordered panel naming the scopes an organization admin can grant, so a blocked user knows what to ask for instead of just being told they lack access.
- ea27950: Prioritize addable skills and add search to the plugin skill picker.
- 074cb4b: Show the skill efficacy methodology in the dashboard instead of linking to GitHub.
- 4f941e2: Label skill version history actions as rollbacks or roll-forwards relative to the current version.
- d9fe35f: Redesign the switch-organization screen and the no-access gate on the new auth shell. The dropdown is replaced with a selectable organization list that pins the current org under a "Current" chip, the gate variant shows a no-access banner for the blocked org above the switchable list, and both screens get a Log out link in the shell header.
- 8880982: Polish trial-facing setup and administration surfaces: use current Speakeasy
  branding on public install pages, return an empty custom-domain list without a
  404, remove invalid DOM and SVG attributes, explain unavailable collection
  installs, and focus observability setup on supported integrations.
- b3d06d1: Explain tokens under management on the billing page with the same definition as the public billing docs, and link the info affordances straight to that docs section.

## 0.95.0

### Minor Changes

- 861e650: Add on-demand LLM session summaries (`chat.summarize`) and pin controls on Agent Sessions: persisted summaries in the session side panel, pin/unpin on list rows and the detail sheet, and a Pinned filter.
- 03b0c2e: Add platform-admin management of Gram's own platform-level external credentials (starting with the ambient GCP identity) via a new `adminExternalCredentials` API (create, read, update, delete) and an "External Services" section in the organization settings with a creation sheet and a per-credential detail page. Includes a live "who am I" Verify probe backed by a reusable `gcpauth` identity resolver.
- 084cc71: Add Budgets v1: org-scoped per-person budget rules with CEL actor targeting over directory-synced attributes. A periodic Temporal evaluator sums each matched actor's LLM spend from ClickHouse against the rule's per-person limit for UTC calendar windows, records warning/breach events, and publishes circuit state to Redis. Rules with action=block deny the blocked user's Claude Code traffic (UserPromptSubmit and PreToolUse, before risk-policy scans) until the window resets. Rules are append-only version snapshots: editing archives the current version row and creates a successor (version + 1), and rules are archived — never deleted — so historical events always resolve to the exact config that fired them. In the dashboard, Budgets renders as a tab on the Costs page wired to the new `spendrules` management API (rule create/edit/archive, live actor preview, overview cards, events tab); the tab only appears when the `gram-budgets-page` PostHog flag is enabled, so the surface can be released to select users.
- 5aaea21: Upgrade the dashboard chat runtime to AI SDK 7, including matching major
  versions of the AI SDK integrations and OpenRouter provider plus the AI SDK
  7-compatible assistant-ui adapter.

### Patch Changes

- f1d60da: Add a platform-admin surface for the chat analysis pipeline's per-organization settings. A new `adminChatAnalysis` management service (`getSettings` / `upsertWorkUnitsSettings`, session-only, gated on the platform-admin flag) reads and writes the organization's `chat_analysis_settings` row for the work-units judge, taking the same organization advisory lock the reservation transaction holds and recording before/after audit snapshots under the new `chat_analysis_settings` subject. The developer toolkit's Features tab gains a matching "Work Units Chat Analysis" section: an org-wide enable/disable control plus the daily evaluation cap, with a suggested cap prefilled when enabling an organization that never had one. A third method, `triggerAnalysis`, wakes the chat analysis coordinator of every project in the organization on demand — surfaced as a "Run now" button in the same section — so an admin can start a pass immediately instead of waiting for a chat write or the periodic sweep.
- 465c861: Add an All / Pinned view toggle on Agent Sessions so pinned sessions have a dedicated list instead of living only behind a filter chip.
- 118b7bc: Make common relative date ranges on the Billing page work without an AI request, including "this month", "this year", and "since July 1".
- e1b188a: `chat.load` now accepts a producer-scoped API key (`Gram-Key`) in addition to a dashboard session and a chat-session token, so backend integrations can pull chat transcripts programmatically without a browser session. Only a **direct** producer API key is treated as a first-party project credential: like the dashboard session (and the way RBAC already exempts API keys via `ShouldEnforce`), it can load any chat in its project, including chats owned by an external user. External-user callers and chat-session tokens stay owner-matched even when the token carries the minting key's `APIKeyID`, and the project/org boundary still applies. The dashboard's producer key-scope description now notes it can export chat transcripts, and the endpoint is added to the public SDK/docs allowlist so its API-key auth is captured in the published API docs.
- ffae6fa: Add daily custom-domain routing and TLS certificate health checks in an observation-only first release: checks log their findings, including the admin notifications a future release will send, without persisting health state or emailing anyone yet. The dashboard groundwork for health warnings and a manual recheck ships alongside but stays dormant until observation ends.
- cb9189c: Add Claude Opus 5 (`anthropic/claude-opus-5`) to the supported model catalog and make it the default for in-app chat and newly created assistants. Specialized judge, embedding, and other purpose-specific model selections remain unchanged.
- 5ebeba4: Redesign the login and register screens with the Speakeasy website styling. The left pane replaces the static platform diagram with an animated agent-session walkthrough — a Connected-to-Speakeasy banner followed by five governed policy decisions (grant, flag, deny, hold, audit) lighting up on a loop under a Tobias display headline — and the right pane becomes a white panel with the Speakeasy lockup, pill CTA for SSO login (or the organization creation form on register), and the brand gradient strip across the top of the screen.
- b87e3a4: Make the MCP environment variable "Import .env" control open a local file picker, populate one secret row per assignment, and report invalid files instead of doing nothing.
- 16d009d: Point the profile menu's "Platform Status" link at `https://status.speakeasy.com/`, matching the domain the status page now lives on (the neighbouring Roadmap link already uses `speakeasy.com`).
- 74cf920: Move the detection sensitivity slider from its own policy-editor stage into the Detect stage. The slider now appears as a card below the detector categories, only while a confidence-scored category (Financial, PII, Government IDs, Healthcare, or Off-Policy) is enabled, and the standard policy flow shrinks to Detect → Scope → Action → Review. The slider help text no longer references internal engine names.
- ad71caf: Dead or mistyped public skill share links now show the page's friendly "This skill isn't available" state instead of the full-page crash screen. The crash screen's debug details also redact capability tokens from error messages and request URLs so a share token is never displayed verbatim.
- ffd219f: Send a robots noindex signal for the whole dashboard host: the app shell now carries `<meta name="robots" content="noindex, nofollow">` and nginx adds a matching `X-Robots-Tag` header (including on error responses). Nothing on the host wants search indexing — the app is behind login and `/shared/*` pages are tokenized public share links.

## 0.94.0

### Minor Changes

- cc076e2: Serve the Employee Enrollment list from the pre-aggregated `attribute_metrics_summaries` view (DNO-618). `telemetry.searchUsers` gains a `source` level: `logs` (default, unchanged) scans raw `telemetry_logs`, while `agent_metrics` reads the pre-aggregated view — canonical observed agent usage (Claude Code, Codex, Cursor, Claude Chat), keyed by email — which is far cheaper (the enrollment query drops from ~seconds to tens of milliseconds on large projects). Identities that never carry an email in the window (which have no token usage) are surfaced separately from raw logs with activity but no token counts, so unknown users stay visible.

  Note the enrollment token numbers change: they now reflect the same canonical agent-usage measure the costs/billing pages use, rather than the previous raw `gen_ai.usage.*` sum that mixed in Gram-hosted completions and duplicate usage-metric rows while missing Claude Code OTEL usage. Only the enrollment list opts in via `source=agent_metrics`; all other `searchUsers` consumers are unchanged.

### Patch Changes

- 5e8e13f: Speed up the Employee Enrollment page (DNO-618). `telemetry.searchUsers` gains a `metrics` level: `full` (default, unchanged) computes the complete set of aggregates, while `basic` projects only user identity, first/last activity, input/output token sums, and the raw user ids the account-enrichment join needs — skipping the per-tool and per-hook-source map aggregations (`sumMapIf`), chat-cardinality (`uniqExactIf`), and cost/cache/avg columns that dominate the per-row ClickHouse work. The enrollment list, which renders only the lean fields (linked accounts come from Postgres), now requests `basic`, so its query no longer builds breakdowns it discards.
- b8d43ac: Fix the Employee Enrollment page showing enrolled employees with 0 tokens (their usage appearing under "Unknown users" instead). When a member's telemetry splits across identity keys — an opaque user_id with no email (e.g. Gram tool calls) plus their email (Claude/Cursor usage) — the id-keyed, token-less summary was shadowing the member's token-bearing email summary. `buildEmployees` now matches a member against both their id and their email and merges the results, so their tokens, activity, and linked accounts are attributed correctly and their usage is no longer orphaned into the unattributed list.
- c773dae: Keep MultiSelect trigger controls inside the container: selected badges now
  shrink and truncate long labels with an ellipsis instead of pushing the
  clear "X" and dropdown chevron past the right edge, as seen on the plugin
  Manage assignments sheet.
- e62d157: Add organization Skills settings for content upload and efficacy sampling.

## 0.93.0

### Minor Changes

- c08521f: Expose per-schedule state for AI integrations and rebuild the dashboard page around it. New aiIntegrations.listSchedules, setScheduleEnabled, and retrySchedule endpoints surface each sync schedule's status (pending/success/failed/auto-paused/disabled), last error, and timestamps, backed by a new user-controlled disabled_at pause that is independent of auto-pause. Each schedule also carries a backend-owned product-level stream identifier and kind (e.g. claude.chat.message events, cursor.usage and claude.chat.cost.usd metrics). The AI Integrations dashboard section moves to a dedicated page with one expandable row per provider connection showing its event and metric streams, each with live status, inline errors, retry, an independent pause toggle, and a link to where the imported data lands.
- 8567fb6: Promote OpenAI Codex to a supported platform in the onboarding "Instrument agent platforms" step. The tile now covers both the Codex CLI and the Codex desktop app, and its walkthrough documents the two required steps: deploying the Speakeasy device agent via MDM (linking out to the device agent setup page) and forwarding Codex OpenTelemetry logs to Speakeasy. Also refreshes the "coming soon" list (adds opencode, Devin, and Mistral; drops AWS Bedrock) ahead of those integrations landing, and makes the onboarding stepper's step numbers clickable to preview any step (forward or back) without advancing onboarding progress.
- 572392a: Restructure the costs explorer around a stacked cost-over-time chart and one unified control bar. The chart (the billing token-usage panel generalized into a shared `StackedTimeSeriesPanel`) stacks daily spend by the current breakdown axis, with weekly/monthly bars for week-over-week comparison and click/drag drill-down into a date range. The search box, breakdown axis track, CSV export, a new Reset button, the dataset selector, and the date-range picker now form a two-row control bar under the headline stats that pins to the top of the page when scrolled past. Re-pivoting or drilling updates the page in place instead of flashing back to skeletons. The billing page renders through the same shared chart with no visual change, and `Page.Toolbar` gains multi-row (`Row`/`Leading`) composition.
- ccdc7f4: Add project skill efficacy, activation, attributed session cost, estimated savings, trends, and scored-session insights.
- a07dcff: Make the Shadow MCP detector mutually exclusive with other built-in policy detectors in create and edit flows, with disabled-switch tooltips explaining how to change the selection.
- e980481: Add atomic Shadow MCP policy setup with project inventory URL selection, searchable modal review, and URL allow-rule reconciliation.
- fd17ed6: Split the tool-usage summary into per-panel endpoints so the MCP & Tools dashboard streams in each card as its data arrives instead of blocking on the slowest aggregate (INC-417).

  `getToolUsageSummary` now has seven sibling endpoints — `getToolUsageTotals`, `getToolUsageTargets`, `getToolUsageUsers`, `getToolUsageTargetTimeSeries`, `getToolUsageUserTimeSeries`, `getToolUsageUsersByTarget`, and `getToolUsageTargetToolBreakdown` — each returning one section of the summary from the same shared query helpers and filter payload. The aggregate endpoint is unchanged for the platform agent tool that wants everything in one call. The MCP & Tools page fetches the seven sections in parallel (the cheap totals query gates the page shell; each panel shows its own loading skeleton and, if its section query fails, its own error state rather than a misleading empty chart), and the MCP overview "Top users" table now fetches only the users section it needs.

### Patch Changes

- 3ca88b2: Add organizationRemoteSessionIssuers.migrate API and UI to consolidate two remote identity providers that point at the same upstream authorization server, re-pointing the source's clients onto the target and soft-deleting the source without forcing anyone to re-authenticate
- 3203363: Fix Claude Desktop agent sessions showing an opaque user ID instead of the user's name. The Anthropic compliance import no longer clobbers a previously resolved chat owner when a later sync activity carries no actor identity (empty strings defeated the upsert's COALESCE guard — NULL is passed instead), and connected-user email resolution is now case-insensitive on both the server and the dashboard. When a session's owner still can't be matched to an org member, the agent-sessions list and session details now show a tooltip explaining why.
- 041e7af: feat: add Codex compliance cost polling
- c2e07e2: Remove stale references to the retired prompt-injection ML classifier. Dashboard copy and the managed-assistant instructions now describe the LLM judge, which has been the only prompt-injection engine since the classifier and heuristics were dropped.
- bb9aac8: Enable project assistants in the dashboard to emit Elements chart and generative UI blocks by sharing the canonical widget prompts between the client and server.
- 96f7f73: Skill summaries now stay in sync with the current version: publishing a new version — whether recorded manually or captured from a session — updates the skill's registry summary from the manifest description, so skills captured before their contents arrived no longer show "No summary" forever. The dashboard's Add Skills dialog on the plugin page is now a multi-select that batches distributions, keeps skills without a distributable version listed but disabled with the reason and a Fix link, and the skill page's distribution banner turns red and explains what blocks distribution when a skill has no versions or none pass validation. Version badges in the version history table no longer overlap the Validity column.

## 0.92.0

### Minor Changes

- ce5571d: Rename and edit captured skills, preserve immutable version lineage, and expose curation controls in the dashboard.
- 7728555: Playground: tunneled MCP servers can now be selected and chatted with (behind the `gram-tunneled-mcp` flag).

### Patch Changes

- 792f487: Fix a crash in the policy scope traffic preview when a matched message produced no highlight spans.
- 2d80662: Add a "What's shipped this week on the platform?" suggestion to the chat landing page and the project assistant's slash-command menu. Picking it starts a session that asks the assistant to check the changelog for the week's platform releases.
- eacabda: Make the cost explorer's breakdown machinery treat the "(unset)" bucket as a first-class group everywhere, fixing the hidden Account Type breakdown on drilled slices that mix classified and unclassified spend (DNO-425).

  Server: telemetry.query's dimension_values now keeps the '' bucket for every groupable dimension — it is the "(unset)" row a breakdown by that dimension renders, so consumers can count it. Only dimensions where '' means "not applicable" (the Claude attribution cuts and query_source, flagged in the dimension registry) still drop it. Empty role/group arrays likewise surface as the "(unset)" bucket.

  Dashboard: the breakdown axis is resolved against the slice's actual group counts by one shared resolver, at drill time (using the clicked row's dimension values) and on load — a division whose spend all sits in one department lands directly on its users with no Department selector, while a division splitting into a named department plus department-less spend keeps the Department cut (previously hidden). The entity/detail query no longer depends on the axis (removing an internal resolution cycle), grouped queries wait for the resolved axis instead of fetching twice, a `?by=` naming a pinned or un-splittable dimension falls back to the level's default, and the URL is rewritten in place whenever the rendered axis diverges from `?by=` so links always reflect the view.

- 8571971: Add the standard toolbar search box to the costs breakdown section, filtering the visible breakdown rows (or sessions) client-side while keeping the preset breakdown-axis controls in place.
- afdca04: Polish the Device Agent install page: replace the distorted Linux mascot with the official Tux, add an MDM rollout recommendation, and drop the preview badges on the page title and sidebar nav item.
- 86f8a76: Serve the project homepage's hook/agent-view metrics (Total Spend, Sessions, Top Users, Most Agent Sessions by User, Most Used Agents) from the pre-aggregated `attribute_metrics_summaries` table via `telemetry.query` instead of paginating every user through `telemetry.searchUsers` (which scanned raw `telemetry_logs`). This is the same source the Costs page uses, so the homepage and Costs figures now agree. The MCP-hosting fallback view is unchanged.
- 1a04494: Fall back to the device hostname on the user cost breakdown when a session carries no email. The Go hooks report the machine's hostname on every event; it now rides the session cache onto Claude OTEL cost rows, and the `email` telemetry dimension groups identity-less spend per device instead of pooling it all into one bucket. Only sessions with neither email nor hostname remain under "Team-wide API Usage".
- 6ea128d: feat: surface issuer setup documentation when creating clients. `remote_session_issuer` records now expose a `client_setup_documentation_url`, settable on create and update across the project-scoped, org-admin, and platform-admin (global) issuer surfaces. The dashboard edits it on the issuer Settings tab and shows it on the Overview tab alongside the discovered RFC 8414 `service_documentation`. Both are linked from the New Client sheet — as **Client Setup Documentation** and **Service Documentation** — so customers can set up an OAuth client with the provider themselves, owning its credentials, access, and rate limits rather than sharing a Gram-owned client. `client_setup_documentation_url` must be an absolute `http(s)` URL (validated with `urls.IsAbsoluteHTTP`, since it is rendered as a link); an empty string clears it.
- e5800a5: Flag inactive MCP servers on the Distribute MCP listing. A new `telemetry.getMcpServerActivity` endpoint reports per-server tool-call activity, and each card/row now shows a subtle indicator when a server has never received a tool call and a warning when it has had no tool calls in the last two weeks.
- 72855da: Normalize provider names and product-surface labels across reporting, agent sessions, tool logs, and cost views. Anthropic compliance imports now persist canonical Claude desktop/web source slugs, while historical source aliases remain filterable.
- 223394c: Add a search bar to the plugin detail page that filters the plugin's MCP servers, assignments, and skills, with distinct empty states for "nothing added yet" vs "no search matches".
- 0f7a061: Fix dashboard React warnings caused by nested log controls, duplicate risk-rule keys, command-palette focus restoration, and unsupported Moonshine button children.
- 1a04494: Label the empty user bucket on cost breakdowns as "Team-wide API Usage" instead of "(unset)". Claude Code sessions authenticated with a company API key or gateway emit no user identity, so their pooled spend is the shared team account's usage, not a data gap. The label applies everywhere the user dimension renders — the cost table, the Top spenders widget (which now includes the bucket instead of hiding it), the drill-in profile, breadcrumbs, and the billing token-usage breakdown — while other dimensions keep "(unset)".
- 1dc4aec: Upgrade recharts from v2 to v3 in the Elements chart plugins, migrating off the deprecated Cell component with no visual changes.

## 0.91.1

### Patch Changes

- 50289f1: Show the number of active skills carried by each plugin on the Plugins page.

## 0.91.0

### Minor Changes

- f83c87f: Manage skill distributions from the dashboard. Skills now open as a dedicated detail page with an at-a-glance sidebar, section navigation, and a plugin distribution banner for distributing the skill to plugins and revoking distributions. The plugin detail page's Skills section replaces the coming-soon placeholder with the actual list of skills the plugin carries, including add and remove controls. Skill distributions can now also be listed filtered by skill or plugin.

### Patch Changes

- b1e392a: Fix the plugin assignments dropdown not scrolling when it contains many users/roles. The multi-select popover is portaled inside the modal assignments sheet, so make its popover modal to restore mouse-wheel scrolling of the options list.
- f4f4f92: Add a copy-to-clipboard button next to the plugin version badge on the plugin detail page so the version string can be copied in one click.
- 4f4eff9: Resolve chat session owners from organization members instead of displaying opaque external user IDs.

## 0.90.0

### Minor Changes

- 2afa6cf: Include device agent setup instructions in onboarding and revamp device agent page
- 5d1ed01: Environment variable values can now be viewed after save when they are not secret. Both the environment detail page and the MCP server environment settings gain a Secret toggle, masked values with an eye reveal, a copy action for readable values, and a shared edit dialog with per-row edit and delete actions. Secret values keep today's redacted-preview behavior.
- 52aaf58: Add a "Fail Open During Outages" toggle to the org Logging & Telemetry page and remove the Observability Mode toggle it supersedes (DNO-497). Org admins can choose to let agent tool calls proceed when Speakeasy is unreachable instead of blocking them, with copy spelling out the trade-off: blocking policies are not enforced during the outage, events are still recorded and scanned after recovery, and broken credentials always block regardless.
- 67d672a: Inline the Gram Elements library into the dashboard as application code at `src/elements/`. The `elements/` workspace package is removed and `@gram-ai/elements` is no longer published from this repo; the dashboard now imports Elements via `@/elements`.
- 9b7b385: The catalog "Add to Project" flow now installs servers as Remote MCP servers: one remote MCP server per selected endpoint, a linked private MCP server with a pre-staged default endpoint, OAuth auto-configuration when the upstream supports dynamic client registration, and optional upstream header values collected in the dialog. This replaces the deployment/toolset pipeline (no deployment polling, tool URNs, or fork naming); collection installs and onboarding distribution bundle MCP servers directly, and catalog Added indicators match installed servers by endpoint URL.
- 3edf806: Plugin assignments: organizations using the Speakeasy device agent can now choose which principals receive each plugin. From a plugin's detail page, admins assign an org-wide default (everyone), specific roles, individual members, or email addresses, and the device agent (`agent.getPlugins`) delivers each plugin only to its resolved recipients (email, user, and RBAC role membership). New plugins — including the auto-provisioned Default plugin — default to everyone, so nothing stops being delivered; admins can narrow the audience afterward. The assignments section is shown only for device-agent organizations; marketplace installs (Claude, Cursor, Codex) continue to receive every published plugin regardless of assignment.
- d3eb9ca: Add a Skills registry page at /skills for recording, inspecting, comparing, and archiving project skill manifests.
- d32a5ed: Add right-click context menus to every table row, card, and list entry with per-entry actions, mirroring each entry's "⋯" menu: sources, deployments, exclusions, policy center, shadow MCP inventory, team members, roles, remote identity provider tabs, tool lists, chat logs, project cards, and plugin cards. Menus share one action definition with the visible kebab so the two stay in sync, and sources table rows are now real links (native open-in-new-tab and copy-link).

### Patch Changes

- cc8791e: Add project-selectable read and write permissions for skills to RBAC role management.
- a98cbcd: Gate the Skills page by organization entitlement and provision default Skills grants for RBAC-enabled organizations.
- f4786b5: Show the currently live (published) plugin version on the plugin detail page.
  `getPublishStatus` now reports `live_version` — the version stamped into the
  published plugin.json manifests, read back from the marketplace repo via a
  single Contents API call and cached briefly — and the dashboard displays it
  next to the publish freshness indicator, so it can be compared directly
  against the version plugin clients like Claude Code report for installed
  plugins when debugging sync lag.
- 7ff9141: Persist the replayed flag on captured chat messages and surface it on risk results: messages redelivered from a device's offline spool after control-plane downtime (X-Gram-Replayed) now carry chat_messages.replayed, and findings produced by scanning them return replayed on the RiskResult type so retroactive findings are distinguishable from live ones.
- f96b6fb: Unfurl Gram dashboard links shared in Slack with the Speakeasy logo (the dashboard favicon) and a humanized page title. The generated Slack app manifest now registers the dashboard as an unfurl domain and grants links:write, and the trigger webhook answers link_shared events with chat.unfurl.

## 0.89.0

### Minor Changes

- e50ecd5: The Roles & Permissions "Specific Servers" picker for `mcp:connect` now lists remote and tunneled MCP servers alongside toolset-backed ones, storing the id each backend's enforcement actually checks (mcp_servers id for remote/tunneled, toolset id otherwise). The challenge view resolves mcp_servers ids to server names, and the "Specific tools" panel explains that remote/tunneled servers resolve tools dynamically and cannot be tool-permissioned.
- 24f54bb: Allow organization admins to rename Shadow MCP inventory servers without changing their canonical URL identity.
- 8e3b7f2: Add a project-scoped API and dashboard detail page for individual Shadow MCP servers.
- a1def6a: Allow projects to disable and re-enable custom model provider keys without deleting or re-entering them.

### Patch Changes

- b92d93d: Improve model provider key table alignment and make disabled keys easier to identify.
- 079ec92: Replace the remaining user-visible "Gram" product mentions in dashboard UI copy with "Speakeasy" (auth descriptions, onboarding steps, insights tooltips, detection rule description, tunneled MCP setup). Add an accessible `aria-label` to the brand logo so screen readers announce it.
- 16c8fa3: Improve model provider key editing and row actions.
- 703a22b: feat(risk): add an assistant filter to risk events. The Risk Events page gains an "Assistant" select listing the project's assistants plus a "No assistant" option, so findings from chats not linked to an assistant (the ones most likely missing user attribution) can be surfaced on their own — or scoped to a single assistant. API: `assistant_id` and `non_assistant` params on `listRiskResults`/`listRiskResultsForAgent`.
- c06dab9: The risk policies table now shows Created and Updated columns with compact relative dates; hovering either reveals the exact timestamp.
- Updated dependencies [b931eb5]
  - @gram-ai/elements@1.42.2

## 0.88.0

### Minor Changes

- bbdd764: Manage bring-your-own model provider keys from project settings: set a project default OpenRouter key, override individual surfaces, and see which surfaces run on your key versus the platform key.
- 15618be: Add the project-scoped API for listing users and usage for a Shadow MCP server, with generated dashboard SDK support.
- 7cef3fe: Redefine tokens under management as observed agent traffic: the billing page now counts the tokens the platform observes coming from users' agent sessions (input, output, and cache writes — cache reads excluded), never inference the platform spends itself (risk-policy analysis, hosted chat). Breakdowns now offer model, agent, provider, account type, project, user, division, department, and role; the project filter dropdown is replaced by the Project breakdown section.

### Patch Changes

- 60653f7: The billing cycle panel gains an average tokens-under-management stat with a per hour / day / week unit toggle (defaulting to per day, computed over the elapsed window for the active cycle), and its headline stat is now labeled "Tokens Managed" instead of "Tokens consumed".
- e33b788: Model provider key settings list the risk policy judge and prompt injection classifier slots.
- d50a779: Fix the MCP server Authentication tab persisting the server-redacted placeholder (e.g. `sup*****`) as the real environment variable value. Saving now only writes a value the user actually typed, removes on an intentional clear, and otherwise leaves the stored secret untouched — covering both the state-toggle path and untouched saves that swept in unmapped required variables.
- 22111d5: Show actionable guidance with a Configure authentication deep link when MCP tools fail to load on a server with no authentication configured, instead of a generic error.
- 124b3eb: Assistant onboarding now finds integrations that are already set up in your project instead of telling you they aren't available yet. It checks your existing toolsets first, and its tool search now includes tools proxied from external MCP servers.
- b270dc9: Remove the dormant telemetry.queryRiskTokens endpoint (no consumers; it computed the pre-DNO-491 billed population and no longer matched any billing surface)
- 9023fd1: Fix the environment variables table on the MCP server page reordering its rows on every page refresh and tab focus change. The toolset API now returns security, server, and function environment variables in a stable sorted order.

## 0.87.0

### Minor Changes

- 4d22067: Add "Suggest with AI" to the exclusion create/edit form, backed by a new dedicated `risk.suggestExclusion` endpoint (separate from `risk.suggestCustomRules`). It returns structured match fields (match type, match value, rule id/source filters) that the dashboard serializes into the exclusion criteria expression — regex suggestions are validated (RE2 compile, length cap) server-side before they reach the form.
- f3ea11b: Add the project-scoped Shadow MCP inventory listing API and generated client SDK support.

### Patch Changes

- 00ac3b8: Fix deletion of organization-level remote session clients, derive tunnel gateway URLs from the active environment, and detach remote identity providers without deleting shared clients.

## 0.86.0

### Minor Changes

- b8e7fe0: Hook plugin browser sign-in is now opt-in per organization. By default, published plugins never open a browser: they authenticate with explicitly configured credentials, a previously cached key, or the organization-wide key, and the login helper prints manual setup instructions instead. Organization admins can re-enable the interactive browser sign-in from the org settings page.

### Patch Changes

- 78898af: Include Codex alongside Claude Code and Cursor in example agent lists across risk policies, logging, insights, and the connect-server dialog.
- a29bea1: feat: expose `is_default` on the plugin API and use it in the dashboard instead of matching on the "Default" name/slug. The onboarding distribute-servers step and plugin card/detail pages previously identified the org's fallback plugin by string comparison (`name === "Default"` / `slug === "default"`), a proxy that predates the server's `is_default` column and unique-per-project index. Both now read the real `is_default` flag returned by `listPlugins`/`getPlugin`.
- 7c637c7: Refresh the OpenRouter model list: add Claude Fable 5 (marked Expensive) and the GPT-5.6 series (Sol/Terra/Luna), replace the playground picker's "(Expensive)" label suffixes with a badge, and remove deprecated models (Claude Sonnet 4, GPT-4.1, o3, o4-mini, Gemini 2.5 Pro/Flash, DeepSeek R1).
- 4a98092: Address review feedback from the OpenRouter model refresh: pin explicit per-provider fallback models in ResolveModel so de-listed or unknown models never silently resolve to a premium model (previously anthropic/\* fell back alphabetically to Claude Fable 5), give elements an explicit DEFAULT_MODEL (Claude Sonnet 5) instead of MODELS[0], and remove Gemini 3.5 Flash from the prompt-policy judge picker (the judge disables reasoning, which that model rejects).
- Updated dependencies [7c637c7]
- Updated dependencies [4a98092]
  - @gram-ai/elements@1.42.1

## 0.85.0

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

### Patch Changes

- da79525: Attach MCP servers to the Default plugin when they're enabled, not just when their first endpoint is created — remote MCP servers are created disabled with a pre-staged endpoint, so they previously never auto-attached and manually adding them failed with "mcp server is disabled or has no published endpoint". Also fixes creating a second endpoint for an already-attached server (previously failed on a duplicate-attach conflict), hides endpointless servers from the plugin's add-server picker, and asks for confirmation before removing a server's last address.
- ae3fc4b: The billing page's Model breakdown now splits into "Risk Policy Analysis Model" — the platform's own risk-policy scanning inference, the metered unit of the TUM contracts — and "Completion Model" for user-facing completion surfaces (playground, elements, MCP chat, Slack). The "Sessions & messages" section and the risk-findings chart stacking are removed: billing meters the act of scanning observed traffic, not the customer's message population. Risk-analysis inference is attributed to the scanned user, so the User, Role, and Division breakdowns now report whose traffic was analyzed.
- b06aa04: The enrollment page no longer shows 0 tokens and a stale last activity for employees whose telemetry rows split across identity keys: usage rows carrying a user id but no email now merge into the employee's email-keyed summary, linked AI accounts attach to that merged summary, and role breakdowns resolve those users instead of bucketing them as Unassigned. The employees and agents tables also render their pagination footer flush against the table instead of floating below a gap.
- e3cf1d1: The hooks setup dialog's Claude Code instructions now install from your org's published plugin marketplace (with copyable commands and managed-settings snippets), instead of a public repository marketplace that carried no credentials. Publish status now reports the observability plugin slugs so install instructions always show the exact plugin name.

## 0.84.0

### Minor Changes

- 317d86e: Hook browser login now delivers the minted API key to the local listener as a form POST instead of appending it to the callback URL, keeping the key out of browser history and request logs, and the sign-in tab closes itself once authentication completes. Older dashboards that still redirect with query parameters keep working.

### Patch Changes

- e3760a0: add confirmation dialog for deleting risk policies
- 11da690: feat: show which users are running the device agent. The org Device Agent page gains an admin-only "Active Users" tab listing who has synced, attributed by the email each agent reports on its ~60s `agent.getPlugins` poll, with `Page.Toolbar` search (name/email) and an Active/Stale status filter. A best-effort per-`(org, email)` last-seen record (throttled to ≤1 write/min) backs a session-secured, org-admin-gated `agent.listSyncedUsers` endpoint.
- 832f997: feat(observe): show device agent status on the Employee Enrollment table. A new admin-only "Device Agent" column surfaces whether each member's Speakeasy device agent has checked in — Active (synced recently), Stale (enrolled but not seen lately), or Not Enrolled (no agent activity) — attributed by email via the org-scoped `agent.listSyncedUsers` endpoint. The column only appears when the `gram-device-agent` feature is enabled, self-refreshes on a 30s tick, and is sortable so admins can surface who is (or isn't) running the agent alongside their telemetry enrollment.

  The standalone "Active Users" tab on the org-level Device Agent page is removed in favor of this column, so per-user agent activity lives in one place next to telemetry enrollment. The Device Agent page now shows only setup/installation instructions.

- 74dbfed: feat: add a token usage breakdown to the billing page's Tokens Under Management section (DNO-404). A billing-cycle picker scopes the TUM usage card and a new "Token usage" panel to any contracted cycle; the panel renders a stacked bar chart of org-wide tokens for that cycle, sliced via a grouped, searchable breakdown picker — total, by token type (input / output / cache read / cache write), by risk involvement (tokens from sessions with at least one active risk finding, via the new org-scoped `telemetry.queryRiskTokens` endpoint), or by analytics dimensions — with daily/weekly/monthly granularity and a cumulative view. Beneath the chart, a usage details table lists per-metric cycle totals with sparklines: token types, agent sessions, tool calls, and message-level stats (tokens in messages with risk findings and tokens from tool-call messages, read from Postgres per-message token counts). The table's measures arrive in a single `telemetry.queryTumDetails` request, and its totals and time-based overage attribution are normalized to match the billed tokens-under-management numbers exactly, with finalized cycles served from the durable billing snapshots. The section also supports drill-down: clicking a chart bar (or dragging across bars) narrows the whole view to that range (re-bucketing daily), and a time-range picker beside the cycle selector accepts any custom period — typed in natural language or picked from a calendar — with billed normalization and overage reserved for full organization cycles; the usage card is labeled with the billing cycle its totals describe. Cycles are named by month ("June Billing Cycle"), table sections collapse individually or all at once, and a Reset button restores the initial view.
- 93c2cee: The billing page now reports the billed population exactly. The chart's headline total plots the billed per-day series behind the usage card (matching it to the token), and every breakdown — model, user, division, role, source, token type — reads the new dimensioned billing aggregate with the same qualification and registry-driven source scoping as the billed totals, instead of org-wide analytics aggregates dominated by unbilled agent-fleet telemetry (cache reads included) that overstated usage by orders of magnitude. Assistants usage is tagged but excluded from the billed scope until BYOK. Dimensions billed completions don't carry (provider, account type, skill, MCP server/tool, cache token types) leave the billing page; they live on the costs/insights pages.
- b0b0ef9: fix(insights): count employee "Agent Sessions" from telemetry instead of the Postgres chat list. The card previously counted chats matched by an email substring search, which only reflected sessions mirrored into Postgres via `session_capture` and under-reported real activity. It now uses `summary.totalChats` (distinct `chat_id`s in telemetry, keyed by the same user), so the count is consistent with every other metric on the employee detail page.
- 02ac329: Issuer discovery now parses RFC 8414 `service_documentation`, `op_policy_uri`, and `op_tos_uri` and persists them on `remote_session_issuers` across the project, organization, and global admin surfaces.
- 0517e60: Restrict the Observe dashboard section (Costs, MCP & Tools Insights, Employee Enrollment, Agent Sessions, Tool Logs) to org admins. The Observe nav stays visible (like the Secure section), but each Observe page is gated on `org:admin`, so basic members see an "Access restricted" notice. Basic members also no longer receive `environment:read` by default.
- dfee73b: feat: surface the AI account email on agent sessions. `chat.listChats` and `chat.load` now return `account_email` from the linked AI account, and the dashboard shows the personal account's email (e.g. a gmail on Claude Max) on session list rows, the transcript's user messages, and the session details popover — instead of only the attributed employee's work email.
- 4fa3e51: Split the org-admin `organizationRemoteSessionIssuers` service into three per-resource services mirroring the project-scoped layer: `organizationRemoteSessionIssuers`, `organizationRemoteSessionClients`, and `organizationRemoteSessions`. Pure refactor with no behavior or RBAC change, but breaking for the management API and SDK: every method drops its redundant resource suffix, so the RPC paths and SDK method names change (e.g. `organizationRemoteSessionIssuers.createClient` becomes `organizationRemoteSessionClients.create`).
- c7edd5a: Link entities mentioned in Project Assistant replies to their dashboard pages, styled in Moonshine blue with an external-link icon
- 3f15c7c: fix: apply the Tool Logs `http.response.status_code` filter at the trace level so status-less rows no longer leak 200/success traces into "Non-2xx responses", and add a first-class Error/Success/Blocked/Pending Status filter to the Tool Logs page.
- Updated dependencies [608940e]
- Updated dependencies [c7edd5a]
  - @gram-ai/elements@1.42.0

## 0.83.1

### Patch Changes

- 956bde5: fix: restore Non-Corporate Accounts risk-policy card and remove duplicate Off-Policy Content card

  The risk-policy eval refactor dropped the Non-Corporate Accounts detector (including its approved-email-domains config) from the new policy detail form and rendered Off-Policy Content twice. This re-adds `account_identity` to the available/display/flag-only category sets and payload mapping, restores the approved-domains state and Customize-sheet UI, and de-duplicates Off-Policy Content.

- 7882ed7: Add a built-in preset exclusion library that suppresses known false positives (test credit cards, example API keys/tokens, module/content hashes, placeholder emails) across all detection sources. Adds the `risk.listBuiltinPresets` endpoint and a read-only "Built-in library" section on the Exclusions tab that lists the live catalog.

## 0.83.0

### Minor Changes

- e9ff915: Add the Non-Corporate Accounts risk-policy category (detection source `account_identity`). Policies can now flag sessions authenticated with a personal AI account (`identity.personal_account`) or with an AI-account email domain outside a configurable approved list (`identity.unapproved_domain`), reusing the account attribution captured by session ingest. The create/update policy endpoints accept `approved_email_domains`, findings are emitted once per session, and the Policy Center exposes the approved-domains input in the category's Customize sheet (flag-only, like other agent-integrity detectors).
- ad4e76d: Adds a policy evaluation workbench for prompt guardrails with real chat replay, saved review verdicts, and transcript-level judge results.
- 747b1ba: Add a manual refresh button to the shared list-page toolbar (`Page.Toolbar.Refresh`) and wire it into every page using it: Catalog, MCP, Agent Sessions, Employee Insights, User Sessions, Risk Events, and the Observe Tools/Agents log pages. Restores the refresh control that Tool Logs previously had before it was merged into the unified logs page.

### Patch Changes

- 548e704: Assistants can now attach MCP servers directly, including remote (external‑SaaS) and tunnelled servers that aren't backed by a Gram toolset. The assistant setup chat can list the project's MCP servers and attach one by name, and the assistant's runtime connects to it alongside its toolsets.
- 548e704: Fixed chat MCP tool connections failing with an "Illegal invocation" fetch error, which left chats without their configured MCP tools (including the assistant setup chat, which could no longer call the assistant's own tools). Also fixed opening a chat via a shared `?threadId=` URL sometimes silently landing on a new empty thread instead of restoring the linked conversation.
- 4ab62e6: feat: show most recently used account as a dropdown on the employees Last Activity column
- 34b8a1b: Editing an environment now requires `environment:write` instead of `project:write`. Creating, updating, and deleting environments previously gated on `project:write`, so principals holding only `environment:write` were rejected. The dashboard gates for these actions were realigned to match.
- 2034568: Hide the chat composer for project assistant threads the signed-in caller didn't create. Elements gains an opt-in `history.isOwnChat` callback (mirrors `resolveCreator`) that reports whether the caller owns a thread-list chat; the dashboard wires it up so admins who open another member's chat via their `chat:read` grant see a read-only transcript instead of a "chat not found" error on send — the backend has always rejected replies into a chat you don't own, this just stops the UI offering an action that was never going to succeed.
- 8104660: chore: use icons to delineate team vs personal accounts
- f16bde1: Re-introduce the unified `/rpc/hooks.ingest` endpoint with working self-serve authentication for hook plugins. On session start the plugin opens the Gram dashboard in a browser, receives a hooks-scoped API key on a localhost callback, and caches it per device — no python or manual key setup required. Machines that have never authenticated are not blocked: sessions proceed with a warning, Claude is prompted to offer connecting via the bundled login helper, and enforcement only becomes strict after the first successful sign-in.
- 5828815: Preserve assistant setup chat history: list prior onboarding threads and make them URL-addressable (scoped by source_kind).
- 155ec48: Attribute project assistant chats to the user who created them. Elements gains an opt-in `history.resolveCreator` callback that resolves a chat's `userId`/`externalUserId` to a displayable name/avatar, shown on thread-list rows and above each user turn in the transcript. The dashboard wires this up for the Project Assistant using its existing org member list — no new network requests, and no identity data is fetched from inside Elements itself (avoids leaking org member data into customer-facing embeds, which don't opt in). Also adds the same avatar to the "Recent Chats" list on the assistant home page, and gives user message bubbles an iMessage-style blue treatment.
- Updated dependencies [548e704]
- Updated dependencies [2034568]
- Updated dependencies [155ec48]
- Updated dependencies [730353d]
  - @gram-ai/elements@1.41.0

## 0.82.0

### Minor Changes

- fedda7c: Add a `cliAuth` service for device-agent enrollment (DNO-388). `cliAuth.authorize` (session-authenticated, member `org:read` scope) stores a PKCE-bound one-time code, and `cliAuth.redeem` (no session — the PKCE code + verifier is the credential) atomically exchanges it for a per-user `[agent, hooks]` API key, returned once. The dashboard CLI callback uses this flow when the request carries `client=device-agent`, so the raw key never travels in a URL; the existing CLI producer-key login is unchanged.

### Patch Changes

- ab1450f: Break down and filter AI usage by account type — Team (enterprise) vs Personal (individual, e.g. Claude Max) — and by provider (Claude, Codex, Cursor) across Costs, Insights, Agent Sessions, Tool Logs, and Employees. Personal usage is flagged at a glance on sessions and logs, and each employee's linked AI accounts are listed with the option to scope their metrics to a single account.
- ab1450f: Cost is now shown as an estimate ("Est. cost", with an explanation on hover) wherever it appears in Costs and insights, because the figure is derived from token usage at standard API rates — which only reflects real spend on metered (pay-per-token) accounts, not flat-fee subscription plans like Claude Max/Pro/Team/Enterprise. Admins can declare a provider integration's billing mode (Metered / Flat rate / Unknown) under Settings → Logging & Telemetry; once an account is declared metered, its cost reads as a confident "Cost".
- 9bc41b9: Surface Claude attribution dimensions in telemetry query results and the cost explorer.
- b95233f: Risk Events now shows historical findings for turned-off policies. Filtering the Risk Events page by a disabled policy previously returned no results because the query required the policy to be enabled; explicit policy filters now surface a disabled policy's past matches. The dashboard flags the inactive policy (a banner plus an "(inactive)" label in the filter) so it's clear the data is historical. The default unfiltered view is unchanged and still lists only active policies.
- Updated dependencies [7ce4d76]
  - @gram-ai/elements@1.40.1

## 0.81.0

### Minor Changes

- 2186673: Support organization-level remote session clients. A `remote_session_client` can now be created with no project (organization-level) so every project in the organization can attach and use it, mirroring organization-level remote session issuers. On `organizationRemoteSessionIssuers.createClient` and `createCimdClient` an omitted `project_id` under an organization-level issuer creates an organization-level client (the same `project_id`-omission convention `createIssuer` already uses), while a supplied `project_id` scopes the client to that project. The consent/token runtime resolver, the project-scoped client reads, and the attach-time single-client invariant now resolve both a project's own clients and organization-level clients in its organization, so a project admin can attach, detach, and use an organization-level client from their own user session issuer but cannot edit or delete it (those stay on the org-admin surface). The `RemoteSessionClient` API shape adds `organization_id` and allows an empty `project_id` for organization-level clients, mirroring the issuer change.

### Patch Changes

- d7b8ec9: Gate the "click to reveal" secret action in Risk Events behind the `chat:read` scope. Users without `chat:read` now see flagged secret values as a non-interactive "Hidden" placeholder (with an explanatory tooltip) instead of a reveal control, and the page-level "Reveal all" toggle is hidden for them. The `chat:read` scope description in the role editor is updated to note that the grant also controls unmasking flagged secrets in Risk Events.
- fcfd78e: Add server-side controls for unmasking redacted secrets
- c8597b1: Add the unified `/rpc/hooks.ingest` endpoint for third-party hook ingestion while preserving existing provider-specific hook endpoints. Hook plugins now authenticate each developer locally through the browser callback flow and store a hooks-scoped key on the device.
- c9da9e5: Add callback URL to the Remote Session Client form
- Updated dependencies [5c825a9]
  - @gram-ai/elements@1.40.0

## 0.80.0

### Minor Changes

- fc47698: Allow editing the permissions of system roles (`admin`/`member`) per organization, while keeping their name and description platform-managed. The Admin role is guarded against losing the `org:admin` permission to prevent org lockout. The roles tab is reworked: the whole role row opens the edit sheet (gated on `org:admin`), scope groups no longer auto-expand and show a description when collapsed, and the members column uses a new interactive member facepile (hover focus, click to view all members) that also replaces the facepile on the org home projects list. Adds Directory Sync (SCIM) info alerts on the team, roles, and identity pages explaining that members and roles are managed by the identity provider while SCIM is enabled.
- c637f6b: Risk policies: configurable detection sensitivity. Adds a per-policy minimum
  match-confidence threshold with a "Sensitivity" slider in the policy wizard, and
  lowers the default to 0.5.

### Patch Changes

- c6ddf0e: Fixed the MCP catalog listing duplicate servers (count doubling) when loading more

## 0.79.0

### Minor Changes

- f193c77: Project Assistant: fold the app-injected `<…context>` block in a user turn into a collapsed "Additional context" disclosure (chevron, expand to inspect) instead of rendering the raw tags. The expanded block wraps to the message-bubble width, so opening it no longer widens the bubble.
- f04e8b0: Add a `chat:read` RBAC scope that gates access to other members' agent session transcripts. The `chat.load` endpoint and the dashboard agent-sessions list are scoped by `chat:read`: anyone can always read sessions they own (the handler grants owner access directly — no `chat:read` grant needed), while reading every member's session requires an unrestricted `chat:read`. The scope is not a default of any system role — not even `admin` — so it must be granted explicitly via a custom role. On the agent-sessions page, callers without `chat:read` see a banner noting they only see their own sessions (with a link to the roles page for org admins). Each dashboard session open is recorded in the audit log as a `chat_session:access` event. The scope is selectable in the role editor (Agent Sessions group) and the dev RBAC override toolbar.

### Patch Changes

- Updated dependencies [f193c77]
  - @gram-ai/elements@1.39.0

## 0.78.0

### Minor Changes

- 0cd8e96: Add an agent type filter to the Agent Sessions page, populated from the agent sources actually present in each project's chats via a new `chat.listSources` endpoint.
- 7763a1b: Tool-call blocks are now durable, first-class entities with a stable `/blocks/<id>` URL and 👍/👎 feedback. When the risk engine blocks a tool call, the block is persisted and its reason is injected into the agent-facing response (Claude `PermissionDecisionReason`, Cursor `AgentMessage`, Codex `reason`) along with a link to the block page, so the agent can reason about the denial instead of hallucinating one. New session-scoped, org-admin-gated `getRiskBlock` and `submitRiskBlockFeedback` endpoints back an in-app `BlockDetailPage` (under `AppLayout`) and a slug-free redirect resolver for the agent's external link, with a "More Info" link from the Risk Events modal.

### Patch Changes

- 0405ac9: Distribute → MCP → Tools now shows the same text permission labels (Read-only, Destructive, Idempotent) as Connect → Catalog → MCP, instead of icon-only badges, for quicker at-a-glance access to a tool's behavior hints.
- 3464cb8: Show the assistant's creator as its owner. Assistants already recorded who created them; that attribution is now surfaced as a profile avatar (reusing the org-home member avatar treatment) on both the assistant card and the assistant setup page's overview panel. The owner resolves to one of three states: the creating member (avatar + name, full name on hover), "No owner" when the assistant was never attributed, or "Orphaned, no owner" when the creator is no longer a member of the organization. Backed by a new optional `created_by_user_id` field on the `Assistant` API type.
- dd03a11: Plugin detail now shows an explicit "Up to date" badge and surfaces the last-published time even when there are unpublished changes, rounding out the publish-freshness affordance.
- a5d57cb: Fix the chat detail "Risky only" filter and rework search-within-thread. The filter previously showed nothing on threads whose findings sat on other transcript pages, and only worked for org admins via the separate risk-results endpoint. `chat.load` (risk_only) now returns `risk_seqs` — the seqs of the flagged messages — so the panel windows the full thread and filters on the authorized load (the toggle is shown only to org admins). Search now steps through every occurrence in document order — within a message's text and inside a tool call's arguments and output — with the active occurrence highlighted distinctly, instead of stepping per message and washing every hit the same colour.
- b6a94ad: Fix the risk policy creation Detect step so the Continue button enables when only category-level detectors (Prompt Injection, Shadow MCP, Destructive Tools, Destructive CLI Commands) are selected. These categories have no individual sub-rules, so the previous `hasEnabledDetector` check treated them as empty and kept Continue/Save disabled.
- Updated dependencies [a5d57cb]
  - @gram-ai/elements@1.38.2

## 0.77.1

### Patch Changes

- 24b41d9: Improve tool observability filter performance by returning hosted MCP server display names from telemetry filter options, allowing the logs and insights pages to avoid hydrating full toolset resources for server filter labels.
- 1751a59: Publish plugins straight from the plugin detail page. After adding or removing a server, or editing a plugin's metadata, a "Publish now" prompt offers a one-click republish — or opens the first-publish dialog for projects not yet connected to GitHub — so there's no need to return to the plugins list to re-publish. The detail page now also shows publish freshness: an "Unpublished changes" badge when the project's current plugin state differs from what was last published, or the last published time when up to date, alongside a durable publish button and a marketplace install banner.

  This is backed by new `up_to_date` and `last_published_at` fields on the `plugins.getPublishStatus` API, which compare the project's live plugin fingerprint against the fingerprint last pushed to GitHub. Both fields are absent when the project has no GitHub connection.

- 045b0ae: Stop chat-conversation search from erroring on long queries. The `chat.load` API caps `query` at 200 characters, but the find-in-conversation bar sent whatever was typed, so a long query failed with a hard validation error. The bar now gates the request at 200 characters and flags the over-limit state inline: the search icon turns into a red warning icon (with a tooltip) and the match counter shows the live `length/200` count in red.
- 1751a59: Fix toast notifications rendering twice. A second `<Toaster>` was mounted at the app root in addition to the one inside the provider tree, so every toast appeared (and dismissed) as a duplicate. Removed the redundant root-level Toaster.
- bbdda53: Pinned chats: pin/unpin conversations on the /chat page. Pinned chats surface in a dedicated "Pinned" section above Recent Chats. Adds a `setPinned` chat API and a `pinned` filter on `listChats`, backed by the `chats.pinned_at` column.

## 0.77.0

### Minor Changes

- f479a1b: Org admins can now register a standalone `remote_session_client` directly from the Remote Identity Provider details page. A new `organizationRemoteSessionIssuers.createClient` endpoint creates a client under an existing issuer with no `user_session_issuer` attachments; the client inherits a project-specific issuer's project, or the admin names a project (downscoping) when the issuer is organization-level. The dashboard surfaces a `New Client` button on the issuer's Clients tab that opens a sheet supporting Dynamic Client Registration (when the issuer advertises a `registration_endpoint`) or manual `client_id` / `client_secret` entry.

### Patch Changes

- 81bc532: Audit trail subjects now link to their detail pages. MCP servers, risk policies, environments, assistants, roles, members, and collections render as links in the org audit log, alongside the deployments, toolsets, projects, plugins, and API keys that were already linked. Project-scoped subjects route under the entry's own project (which may differ from the project in the current URL), and risk policies and roles deep-link to open the specific item.
- 4f9b199: Project Assistant chats can now be renamed from the live chat view. The dock header shows the active conversation's title and lets you click to edit it inline. Manually chosen names are preserved — automatic, session-context title generation skips any chat a human has renamed (clearing the title re-enables auto-naming).
- 9b85ddd: feat(telemetry): include the chat title on `listSessions` results (resolved from Postgres, batched per page) and show it in place of the chat id in the cost dashboard's session table
- Updated dependencies [4f9b199]
  - @gram-ai/elements@1.38.1

## 0.76.0

### Minor Changes

- 66fcd5a: feat(assistants): add a search box to the Assistants list. The shared `SearchBar` is always shown above the list and filters the grid by name and model (case-insensitive), with a "no matches" message when a query returns nothing.
- 1ba5adb: feat(dashboard): search within a chat thread. The chat detail sheet gains a find-in-conversation bar backed by full-thread server-side text search (`chat.load` `query` param returns the messages matching the query plus surrounding context, mirroring the risk-windowed view). Jump between matches with the prev/next controls or Enter/Shift+Enter (wrapping at the ends), Escape clears. The active match is highlighted bright yellow and the rest pale — across message text, tool names, and tool argument/output sections — and the tool holding the active match expands, collapsing again as you navigate away.
- 0d23d1f: Add an Analytics tab to the MCP Server details page with observability scoped by `mcp_server_id`, spanning both remote-backed and toolset-backed activity: a time-range picker, trend metric cards (tool calls, failed calls, error rate, average latency), a tool-calls time-series chart, and top tools by call count and by failure rate.
- ef2f5ef: Add an organization-level observability mode that makes generated hook plugins fully non-blocking. When enabled, hooks only observe and report and can never deny or delay a tool call. Defaults off, preserving existing behavior. Toggle it from the organization logging settings.
- 6f3180d: chat.load now paginates a generation's messages by `seq` keyset (`limit`, `before_seq`, `after_seq`) and exposes each message's `seq` plus `has_more_before`/`has_more_after`. A new `risk_only` flag returns just the messages with active risk findings padded with surrounding context, grouped into contiguous `risk_segments` that can be expanded on demand. The chat detail sheet consumes this with a virtualized transcript (`@tanstack/react-virtual`, constant DOM node count regardless of how many pages are loaded) and infinite scroll (scroll up to load older messages, anchored so the viewport doesn't jump), and renders the risk-only view as expandable segments with load-above/below and gap-fill controls.

### Patch Changes

- c1ef552: `remoteSessionClients` and the org-admin client views now source the `user_session_issuer` relationship entirely from the join table. The `RemoteSessionClient` result replaces the single `user_session_issuer_id` with a `user_session_issuer_ids` array (breaking), create/clone accept zero or more `user_session_issuer_ids` so a client can be created standalone, and a client's issuer attachments are now managed through the new `attachUserSessionIssuer` / `detachUserSessionIssuer` endpoints instead of `update`. No more reads or writes of the legacy `remote_session_clients.user_session_issuer_id` column.
- 3955c10: Better performance on tool logs page
- e6c756b: Add drag-to-zoom time window controls to dashboard charts.
- 4b45485: `chat.load` now returns a `totals` object with whole-generation trace-entry counts (`total`, `user_messages`, `assistant_messages`, `tool_calls`, `tool_results`, `risk_only`). Because the detail-sheet transcript is paginated, the filter bar previously derived its counts from the loaded page — showing e.g. "Showing 150 of 150 entries" on a 19k-message chat, and a risk count that disagreed with the (generation-scoped) risk-only transcript. The dashboard now renders these counts from the server totals. Totals are scoped to the returned generation so they stay consistent with the messages on screen.
- b968804: Exclude tools lists from registry list view to lean out the response size and make the catalog experience more reliable in flake-y network conditions
- 03cf22a: Fix two Cmd+K command palette glitches. Recently Visited is now only shown when the search box is empty, so a closer text match always ranks ahead of a recently visited page (AGE-2808). The "Ask AI" row drops below the results while searching, so the closest match keeps the auto-selected highlight instead of requiring an extra ↓ keypress to reach it (AGE-2807).
- b0a9186: Fix filter sheet options not applying when clicked. The multiselect controls in the unified filter sheet opened their dropdown but never registered option clicks, so filters on the MCP & Tools and Insights pages did not respond.
- cd3738b: Link the SSO and Directory Sync (SCIM) "Configure" buttons on the org Identity page to the in-product setup wizard (`/setup?step=connect-idp` and `/setup?step=directory-sync`) when no connection has been set up yet, instead of bouncing admins straight to the WorkOS admin portal. Once a connection exists, the buttons continue to open the WorkOS portal to manage it. The entitled-org configure buttons no longer emit `identity_provider_interest` PostHog tracking events; the non-entitled upsell button still does (it backs the "our team has been contacted" notification).
- d53899a: Fix the Agent filter on the MCP & Tools insights page not reloading data. Selecting an agent updated the filter chip but the tool usage summary ignored the hook source, so the graphs never changed.
- 5917df2: Enhance the org-level MCP Connections page: multi-select with select-all to revoke sessions in bulk, a search box in the filter bar, brand status badges, and a status filter (removed the OAuth Client filter).
- Updated dependencies [1ba5adb]
  - @gram-ai/elements@1.38.0

## 0.75.0

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

### Patch Changes

- 0b4e42b: Unify the search, filters, sort, and view-as controls on every list page into a single `Page.Toolbar` component, replacing the per-page mix of bespoke search boxes, filter sidebars, sort dropdowns, and view toggles.

  - One contained control bar per page (search + filters on the left, sort + count + view on the right), with uniform control heights.
  - Filter chips and sheet share a typed schema (`defineFilters`/`useFilterState`); adds Status + Source filters to the MCP page.
  - Folds in fixes: "Reset to default" now clears every filter atomically, the date picker opens inside the filter sheet, and empty filter labels are pluralized ("All servers").

## 0.74.0

### Minor Changes

- bf94bd2: Add a full-page Project Assistant chat as a second way into the assistant, alongside the docked composer.

  - New **Project Assistant** entry in the project sidebar (under Home) opening a `/chat` landing: a personalized "Ask your Project Assistant about your AI usage" composer with a cycling, crossfading placeholder, a `/` slash menu of starter prompts, your recent conversations grouped by time period ("Show all" to expand), and a set of enterprise-focused starter prompts.
  - A `/chat/:id` conversation view with a back/new-chat bar and the conversation's title in the header, which updates live as the backend generates it.
  - The project home page now embeds the same "Ask anything" widget.
  - The dock and the full-page chat share **one** assistant runtime, so an in-flight conversation continues seamlessly when you expand the dock into the page (no lost response). The docked pill offers "Continue chat" to reopen a recent conversation, and pages that host their own chat runtime (Playground, Elements, assistant onboarding) hide the dock to avoid duplicate composers.
  - A decorative rainbow "powder burst" header on the chat landing.

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

### Patch Changes

- 94f624d: Add Beta badges to the Project Assistant (sidebar nav item, footer resume button, and `/chat` landing page) and promote the Assistants surface from Preview to Beta. Both use the shared `ReleaseStageBadge` so the label stays consistent across every surface.
- 672d9bc: Add optional onboarding for configuring Cursor and Anthropic Compliance API integrations, and show Codex instrumentation plus OpenAI Compliance API configuration as coming soon.
- bcda11d: Upgrade the default assistant model to Claude Opus 4.7. The platform-managed Project Assistant, the assistant onboarding flow, and the onboarding system prompt's default recommendation now use `anthropic/claude-opus-4.7` instead of `anthropic/claude-sonnet-4.6`. Existing assistants are unaffected; only newly created assistants pick up the new default.
- 2adba2f: Add employee detail metric drilldowns for tool logs, agent sessions, and risk events.
- 94c551b: Fix the Project Assistant chat getting stuck "loading" after a tool-using turn, where the assistant had already replied but the composer stayed disabled.
- bd4261a: Use the Speakeasy marketing site favicon for the dashboard
- dfd4834: The MCP servers screen no longer crashes when a non-critical request fails to load. It now keeps showing the most recently loaded servers with a subtle "couldn't refresh" indicator instead of replacing the whole page with an error.
- e594e20: Add a step to user session migrations that port existing client registrations from oauth proxy to user sessions
- 04ddbd0: Rename and reorder project sidebar navigation groups.
- 88669aa: Update moonshine - design system dependency to latest
- 32c4165: Unify Tool Logs across hosted MCP servers, shadow MCP servers, local tools, and skills.
- Updated dependencies [bf94bd2]
  - @gram-ai/elements@1.37.1

## 0.73.1

### Patch Changes

- 2630d11: Fix the Cmd+K command palette's "Recently Visited" list showing an assistant's opaque id (e.g. "Assistant · 0190abcd") instead of its name. Visits are recorded centrally from the URL, which for the id-keyed assistant detail route fell back to the id. The assistant detail page now registers its name as the recents label, and `App` consults that override (re-recording when the name resolves asynchronously), so the palette shows the assistant name.
- 1e1e9b7: Pin the assistant ⌘/ shortcut hint to the nav bar's right edge on wide screens, and show it in the assistant dock.

## 0.73.0

### Minor Changes

- e81a134: feat(assistants): surface Triggers as a tab on the Assistants page and a filtered Triggers tab per assistant, and rename the Assistants "Audit log" tab to "Activity"
- 4f65d12: Make the Project Assistant dock a continuous experience across the dashboard. The dock stays expanded across page navigation and swaps in the new page's suggestions; every suggestion set is colocated in one route-keyed object with question-phrased titles and per-subject icons, and chips animate in on route change. The expanded composer gets a Granola-style grey tray with a bordered inner input, the Cmd+/ hint moves to the breadcrumb bar, and the chat panel opens as an extension of the pill — including a matching slim composer.

  Elements: add `theme.customCss` to `ElementsConfig` — extra CSS injected into the Elements shadow root after the built-in stylesheet, the supported escape hatch for embedders restyling the stable `aui-*` class hooks (host-page CSS cannot reach into the shadow DOM).

### Patch Changes

- 4f65d12: Fix the Project Assistant dock losing or duplicating the first message sent after a cold page load.

  Elements: the history-enabled runtime now mounts immediately instead of waiting for auth — the previous auth gate swapped the without-history runtime for the history one when the session resolved, replacing the runtime and wiping any message sent into the first. The thread-list adapter resolves request headers through an async `getHeaders` that awaits the session fetch, so its bind-time `chat.list` waits for auth instead of failing. The custom transport is also resolved in its own memo so churn in the default transport's dependencies (MCP tool discovery settling, auth, connection status) no longer changes the transport identity mid-turn, which rebuilt the per-thread runtimes and discarded in-flight optimistic messages.

  Dashboard: the dock's queued-prompt bridge appends exactly once — a throw from the placeholder thread core (before the real core binds) leaves the prompt queued for retry, while a successful append never re-fires, fixing both the dropped first message and the duplicate sends that minted a fresh chat per attempt. The server-assistant transport keeps at most one chat.load poll loop alive per dock: each send aborts the previous turn's poller, so a turn that never reaches a terminal row no longer leaves zombie polling loops behind.

- Updated dependencies [4f65d12]
- Updated dependencies [4f65d12]
  - @gram-ai/elements@1.37.0

## 0.72.0

### Minor Changes

- 0d51b12: Assistants UX: the detail panel is wider and split into Overview and Sessions tabs, system instructions open in an editable modal, the active/paused status toggle is available on the detail panel (shared with the index cards), and index cards show a mini activity sparkline derived from chat session activity.
- 0d51b12: Assistant tool-call audit events no longer appear in the platform audit logs feed or its facets. They are surfaced instead on a new "Audit log" tab on the Assistants page, filterable by assistant, backed by new `subject_type` / `subject_id` filters on `auditlogs.list`.

## 0.71.0

### Minor Changes

- 87cb734: The Project Assistant now cycles a set of whimsical "thinking" verbs while it works and types replies onto the screen token-by-token, emulating an SSE stream over the poll-based transport.
- 430deac: Add tokens under management (TUM) billing for enterprise organizations. The billing page now shows enterprise orgs their TUM consumption for the active billing cycle against the contracted monthly allowance, replacing the self-serve usage meters. TUM counts token usage only from agent sessions Gram has stored non-metrics data for (chats, tool calls), excluding OTEL-forwarded token metrics from uninstalled users. Platform admins get an admin-only section on the billing page to set the contracted monthly token limit, an alert email (alerting to follow), and the billing cycle anchor day, backed by the new `usage.getTokensUnderManagement` and `usage.setBillingMetadata` endpoints and a `billing_metadata` table. Contract changes emit `audit_log.billing_metadata_event_v1` audit events.
- 430deac: The tokens under management endpoint now returns usage history: the trailing 12 billing cycles, each with a per-UTC-day breakdown. Chat qualification is evaluated per cycle, so daily points sum exactly to each cycle's TUM. The enterprise billing page renders this as a bar chart with day and billing-cycle granularity toggles, including a contracted-limit line in the cycle view.

### Patch Changes

- 8759477: Auto-configure remote identity providers for custom remote MCP servers when OAuth
  metadata can be discovered, including reusing existing issuers and deriving a
  display name from the issuer URL.
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

- 850b430: Add a Distribute MCP servers step to onboarding. Teams can search the MCP catalog and distribute selected servers to their organization during setup, running the deploy → bundle → publish flow inline. Servers that require OAuth are auto-configured with Speakeasy OAuth, matching the catalog add-server flow. The onboarding list shows only servers Gram can fully auto-configure (OAuth with dynamic client registration); servers needing manual setup (OAuth without DCR, API keys) remain available in the catalog.
- 657f61c: Add a "Platform Status" link to the profile menu, pointing to the Speakeasy status page.
- 46374b8: Show which tool the Project Assistant is calling while it works. Tool calls
  and their results now render in the side panel as they happen, instead of the
  conversation sitting silent until the final reply lands.
- b5fc99c: Promoted Agent Sessions to top-level Observe navigation.
- b4e32a6: Promoted Employees and Costs to top-level navigation.
- d857151: Open prompt-based ("LLM-judge") risk policies to all message types.

  Previously the judge was hard-scoped to `tool_request` in both the realtime
  scanner and the batch analyzer, regardless of the policy's `message_types`. The
  judge now runs on whatever types a policy declares (`user_message`,
  `tool_request`, `tool_response`, `assistant_message`), and the policy form lets
  you choose them instead of locking to tool requests.

- 4207390: Moved Risk Events into the Secure section.
- 30cefe6: Add a "Source" filter to the Tools logs/insights pages so tool traces can be narrowed by originating agent (Claude Code, Cursor, Codex, …).
- 0c7373d: Added unified Tools insights for hosted MCP servers, shadow MCP servers, local tools, and skills.
- Updated dependencies [87cb734]
- Updated dependencies [9bc9a1d]
  - @gram-ai/elements@1.36.0

## 0.70.1

### Patch Changes

- ba8bdd4: Direct assistant MCP authentication prompts to the assistant's owner instead
  of whoever happened to trigger the assistant. Slack onboarding now records the
  owner's Slack identity in the assistant's instructions, runtime guidance
  delivers OAuth links to the owner (ephemeral or DM) and tells anyone else that
  the owner has to complete the connection, and prompts shown when the owner is
  unknown now say explicitly that authentication is for the owner — so an
  unexpected auth message is no longer mistaken for a failed setup.
- 2a7fbec: Fix a request storm of `GET /rpc/auth.info` 401s introduced by the command
  palette's Recently Visited feature. The user-id lookup issued an unconditional
  `auth.info` request from the always-mounted palette (including on the
  unauthenticated login page). The session lookup is now gated on the palette's
  open state, and the page-visit recorder reuses the session already fetched by
  the auth provider instead of issuing its own request.
- f18938f: Fix dark mode visibility in the enterprise onboarding flow. The onboarding stepper and the instrument-agents step now use theme-aware colors so text and controls remain legible in dark mode.
- 15bb129: Add a Configure policies step to onboarding. Teams can enable the Shadow MCP guardrail and per-category detection policies (secrets, PII, prompt injection) directly during setup, with state persisted via the risk policy API.
- cc9d8ee: Add optional `name` (display name) and `logo_asset_id` to remote session issuers across both the project-scoped (`remoteSessionIssuers`) and organization-scoped (`organizationRemoteSessionIssuers`) services. On create, `name` is trimmed and stored as NULL when empty; on update it follows the same three-state semantics as the nullable endpoint fields (omitted keeps, empty string clears). `logo_asset_id` is set-only for now (no clear path, no upload UI yet). The dashboard renders the display name as the primary label with the issuer URL as the secondary line, exposes an optional Display name input on the attach and modify sheets, and renders a logo when one is present. On the attach sheet the Display name auto-derives from the Issuer URL hostname until the operator edits it, matching the existing Slug behavior.

## 0.70.0

### Minor Changes

- 84240ec: `Cmd+K` — and a new magnifying-glass trigger beside the logo — now searches
  across resources (MCP servers, sources, deployments, environments, assistants,
  plugins, and admin-only risk policies, detection rules and approval requests),
  all app pages (auto-enumerated from the route map), and offers a free-form
  "Ask AI" hand-off to the Project Assistant.
- 489f7fe: Support publishing Remote MCP-backed `mcp_servers` to collections alongside toolset-backed servers. `collections.attachServer` / `collections.detachServer` accept either `toolset_id` or `mcp_server_id` (exactly one), `collections.create` accepts `mcp_server_ids` in addition to `toolset_ids`, `collections.listServers` returns both backends merged by publish time, and `ExternalMCPServer` exposes `mcp_server_id`. In the dashboard, the Publishing section, the create-collection form, and the collection detail edit-servers picker all offer Remote MCP-backed servers, and the Remote MCP server settings page gains a Publishing section.

### Patch Changes

- e99b6fc: Change the Project Assistant keyboard shortcut from Option+Shift+W to Cmd+/ (Ctrl+/ on PC), updating the trigger's hover hint and tooltip to match, and add a hover tooltip to the sidebar collapse button showing its Cmd+B shortcut.
- 327fc15: Fix "Server not found" on the catalog detail page for servers whose `registrySpecifier` was not in the first page of the unfiltered catalog list (e.g. `com.pulsemcp.mirror/datadog`). The backend `ListCatalog` aggregates across registries and caps results at 100, and only emits a `nextCursor` for single-registry queries, so the detail page could never reach deep entries. The lookup now passes the specifier's last path segment as a `search` filter so the upstream registry narrows the result set and the target server lands in the first page.
- de92585: Order and filter agent sessions by their latest persisted chat message instead of original session creation time, and show that activity time in the dashboard sessions list.
- 30a5bb6: Dim inactive access rules when no blocking Shadow MCP policy exists while keeping rule actions available
- a1038ec: Fix feature-flag gated UI (e.g. the Device Agent nav link) staying hidden even when the flag is on. PostHog loads feature flags asynchronously, but `useTelemetry()` consumers never re-rendered once flags resolved, so `isFeatureEnabled(...)` reads were stuck on their pre-load value. `useTelemetry` now subscribes to PostHog's `onFeatureFlags` event and re-renders consumers when flags resolve or change, making every `isFeatureEnabled` call site reactive.
- a0a0ebe: Align Risk Policies page title with design system
- ee1c922: Remove the `value_hash` field from environment entries. It was documented as a way to identify matching values across environments, but every code path computed it from the already-redacted display value (`val[:3] + "*****"`), so it collided for any two values sharing a 3-character prefix and never reliably identified matching values. The only dashboard consumer grouped by it, and because colliding values also render identical redacted strings, the grouping was never observable. Replaced the dashboard's value-hash grouping with direct per-environment value tracking and dropped the field from the API surface.
- cfd120a: Removed the deprecated standalone Slack app feature. The dedicated Slack app pages, their backend endpoints, and the associated event-handling workflow have been retired. Slack continues to work through assistants and triggers, which is the supported path.

## 0.69.0

### Minor Changes

- 5d59ae9: Support adding Remote MCP-backed `mcp_servers` to plugins alongside toolset-backed servers. `plugins.addPluginServer` accepts either `toolset_id` or `mcp_server_id` (exactly one), `PluginServer` exposes `mcp_server_id`, and `display_name` is now optional (defaulting to the backing toolset or mcp_server name). Plugin bundle generation resolves the preferred endpoint for mcp_server-backed servers (custom-domain over platform, then oldest) and emits them as OAuth HTTP servers with no static auth header. In the dashboard, the plugin add-server picker and server cards offer and render Remote MCP-backed servers (gated on the `gram-remote-mcp` feature flag).

### Patch Changes

- fb3f0ca: Two Project Assistant sidebar fixes:

  - Make the server-assistant transport poll adaptively for replies: poll quickly for the first few iterations (so short turns surface within a few hundred milliseconds instead of waiting a full fixed interval), then back off geometrically to the steady-state interval for long, tool-heavy turns. Reduces the perceived latency of the project assistant relative to the old streaming AI assistant.
  - Strip the backend's `<message-context>` framing (and drop framing-only turns) from the rendered transcript via Elements' new `history.transformChatMessage` hook, so reopening a historical thread no longer shows a raw block exposing internal event/MCP-auth metadata.

- fbd0398: Add right-click (context-menu) support to dashboard cards. Right-clicking a card opens a menu of the same actions shown by the card's visible `⋯` button, driven by one shared `Action[]` so the two never drift. Applied to every card that already exposes an action menu — Plugin, Source, Environment, Assistant, Custom Tool, Prompt, and Resource cards — via a reusable `CardContextMenu` primitive. The right-click menu honors the same RBAC gating as the `⋯` menu (e.g. the Environment "Clone" action stays hidden without `environment:write`).
- b23c7f3: Fix the catalog Add Server dialog rendering duplicate-looking endpoint checkboxes when a registry entry exposes the same streamable-http URL twice (e.g. Datadog publishes an OAuth variant and an API-key variant under one URL). Same-URL remotes are now collapsed in the UI, matching the backend's deploy-time behavior of picking the first URL match.
- 3fd496d: Redesign the app shell as a sidebar-only vertical split. The full-width top nav bar is removed and all of its functionality moves into the sidebar: a combined org/project workspace switcher and the logo in the header, and the account menu in the footer (inline theme toggle, Docs/Changelog/Get Support, and a new **Roadmap** link replacing the old bug/feature link). Light mode is now the default, the brand gradient line sits beneath the main-panel header, and pages gain a `Page.Header.Actions` toolbar slot. Switching projects now shows a nav skeleton instead of flashing empty while permissions reload.
- c6517cb: Redesign the Overview tab of the experimental MCP server details page (`/x/mcp` route) into status-driven cards. Server Address, Authentication, and Source/Tools each render as a consistent card with a Ready / Needs Setup signal: the Server Address card shows the connect URL plus the shareable `/install` page URL, and the Authentication card derives an explicit posture (Gram-only, Gram + remote, remote-only, or open to anyone) so a public server with no remote identity is correctly flagged as unsecured. Adds an early-access banner and an "Enhance your server" section, and wires the install-page Customize action to the Settings tab.
- 989d905: Gate the Device Agent pages and sidebar nav item behind the `gram-device-agent` feature flag (defaults off). The nav link is hidden entirely when the flag is off, and the page redirects away so it can't be reached by direct URL.
- Updated dependencies [fb3f0ca]
- Updated dependencies [7acfbee]
  - @gram-ai/elements@1.35.0

## 0.68.0

### Minor Changes

- 69d8cdb: Add read-only tool filtering visibility on the MCP details page Tools tab. New `mcp:read`-scoped `listToolFilters` methods on the `toolsets` and `mcpServers` services resolve the effective tool variations group (`mcp_servers` then `toolsets`) and return the available filter scopes (tags) with their member tools plus the tools excluded from all filters, mirroring the runtime `?tags=` behavior. The dashboard Tools tab renders a scopes panel above the tool list when filtering is enabled, with per-tag tool membership and a tag chip that filters the list below.
- 4feb400: Add the enterprise onboarding wizard at `/<org>/setup` that walks new orgs through five steps end-to-end: SSO setup via WorkOS, directory sync, publishing a private plugin marketplace to GitHub, instrumenting each agent platform (Claude Code, Claude Cowork, OpenAI Codex, Cursor) with the org's marketplace and observability plugin, and confirming traffic is flowing.

  Includes:

  - New `Create Plugin Marketplace` step that wraps the same GitHub publish flow as the Plugins page, with a typeahead-driven GitHub-user picker for collaborator access (replaces the old comma-separated input).
  - `Instrument Agents` step that surfaces per-platform setup instructions with auto-generated API keys, marketplace URL / repo URL / plugin slug substitution, eligibility gating (Claude Teams/Enterprise check), and platform-specific screenshots. Coming-soon entries for GitHub Copilot, Gemini, Glean, and AWS Bedrock are rendered as a half-width muted grid and excluded from the configured/total count.
  - Wizard resume logic backed by `organizations.getOnboardingStatus` and `plugins.getPublishStatus` — reloading lands on the deepest known-incomplete step instead of step 0.
  - `organizations.sendEnterpriseAdminOnboardingEmail` endpoint and a super-admin "Onboarding" tab for dispatching the enterprise-admin invite email (Loops template `cmpqyxnzl00hj0jwtkibhyjdz`), which deep-links recipients into the active org's wizard.
  - `organizations.verifyOnboardingHooksSetup` polling endpoint that surfaces recent hook events from ClickHouse for the `Confirm Traffic` step.
  - Wizard chrome: header with Docs / Get Support (Pylon) / Go to Dashboard buttons, footer with the moonshine ThemeSwitcher, and a project-slug query-string override on the SDK provider so the wizard can hit project-scoped endpoints from org-level routes (falls back to the `default` project when unset).

- 51fadba: Make the generated marketplace name configurable per-project. Adds `plugins.getMarketplaceSettings` and `plugins.updateMarketplaceSettings` on the management API plus a Marketplace settings dialog in the Plugins tab. The default is now `<org-slug>-speakeasy` (previously `<org-slug>-gram`); the org-slug prefix keeps defaults unique across customers so end users installing from two Gram marketplaces don't collide. Saving an override on a project that already has a published marketplace auto-republishes the new manifest to GitHub. References to "Gram" in the generated README, plugin descriptions, and hook scripts are rebranded to "Speakeasy"; URLs, env-var names, and HTTP header names are unchanged.

### Patch Changes

- f0b17a8: Auto-create a default MCP endpoint when importing a remote MCP server as a source. Previously the import created the server but required the user to add an endpoint by hand before it could serve traffic. The import flow and the detail-page re-link flow now pre-stage a default platform endpoint with slug `{orgSlug}-{random}`. Endpoint creation is best-effort: a failure leaves the source intact and surfaces a warning toast, and the server stays disabled so the endpoint begins serving once the user enables it.
- 9bdb3cd: Fix inconsistent results in the filter dropdowns on the Insights and Logs pages (e.g. "Filter by user email"). `MultiSelect` filtered its own option list while cmdk's built-in filter was also enabled, so the two raced as the user typed: valid matches could fall into the "No results found" empty state and it could stay stuck when characters were deleted. The component's own filter is now the single source of truth.
- d33c557: Fix dashboard release-stage badges so preview/beta labels render consistently in side nav and tab navigation.
- 1b2edcd: Insights now only counts workspace members across the Employees and Role views, excluding activity from impersonating superadmins that previously showed up as a raw UUID.
- f2072b1: Improve confirmation dialog for deleting Shadow MCP access rules
- Updated dependencies [e39ea7e]
  - @gram-ai/elements@1.34.0

## 0.67.0

### Minor Changes

- 55a25ac: Add management APIs and dashboard UI for enabling and configuring MCP server tool filtering via tool variation groups.
- 254158e: Add an org-level Device Agent page (`/<org>/device-agent`) under the Secure nav group: per-OS install instructions for the Speakeasy device agent, `managed.json` MDM configuration reference (schema, paths, example), and self-service `org_token` generation via the new `agent` API-key scope (mint/rotate from the page, with a ready-to-paste `managed.json` copied to the clipboard).

  Also patches the CLI loopback callback (`CliCallback.tsx`) to append `&email=<userEmail>` to the redirect, which the device agent's one-shot OAuth-loopback enrollment requires.

### Patch Changes

- 1078e46: Add an optional `user_id` filter to the risk events list. The Risk Events page now exposes a "User contains..." search box that filters findings by the chat's external user id (case-insensitive substring match), alongside the existing policy and rule filters.
- 1cb069e: Fix the audit logs page AI Insights setup so it no longer calls `toolsets.list` without a project slug from org-level routes.
- a86651b: Fix the shared `Link` component so external links render inline with surrounding text. The external-link icon was wrapped in a block-level flex container, which stretched inline links to full width and pushed trailing punctuation to a new line; the icon now sits inline on the text baseline.

## 0.66.0

### Minor Changes

- 0653bf4: Add `agent.getPlugins` management API method consumed by the Speakeasy device agent. The endpoint accepts an `email` query parameter, resolves plugin assignments for that email plus the `*` wildcard within the caller's org, and returns the published plugins as Claude Code marketplace + plugin references (drops directly into Claude Code's `extraKnownMarketplaces` and `enabledPlugins` settings). Authenticates with an org-scoped API key carrying the new `agent` scope.

  Adds `agent` as a selectable scope on the existing API Keys page so admins can mint these tokens from the same place every other scope is minted.

  Adds `email` as a first-class principal URN type (`urn.PrincipalTypeEmail`) so admins can assign plugins by email address. Existing `user:` and `role:` URNs are unchanged; the wildcard `*` is now exported as `urn.PrincipalWildcard`.

- 598d0d8: Re-target AI Insights sidebar at project toolsets instead of hardcoded Speakeasy MCP. The sidebar now connects to all toolsets configured in the current project.

### Patch Changes

- 91e166d: Add an employee data-flow graph endpoint and dashboard visualization for workforce observability.
- 9fde650: External MCP proxy servers (whose tools cannot be enumerated until a user authenticates) now appear in the "Specific MCP Servers" picker when creating an access role, and no longer display a misleading "No Tools" badge on the MCP list and table views.

## 0.65.1

### Patch Changes

- 3d2afcf: Added a risk-only toggle to trace panels and deep-linked Risk Events to open with risk filtering enabled

## 0.65.0

### Minor Changes

- d7c9904: Assistant onboarding: new Slack setup card lets you pick capabilities (send, read, react, etc.) and which events wake the assistant up, then provisions a dedicated per-assistant Slack toolset instead of reusing a shared one. The card warns about "always-on" event firehoses, and the onboarding agent now offers plain-English filter narrowing after Slack install.

## 0.64.0

### Minor Changes

- 67be909: add tool variations menu to source detail Tools tab
- 6039fe5: Add `risk.customRules.suggest` endpoint that calls OpenRouter to turn a one-line description ("what do you want to detect?") into a prefilled custom detection rule. The dashboard's New Custom Detection Rule sheet now opens on a single textarea, calls the new endpoint, and lands the operator in the editable review form with the suggested rule_id, title, description, regex, and severity.
- 6f176bb: Add dashboard UI for reviewing Shadow MCP requests and managing project-scoped access rules.
- 6039fe5: Add a rule playground: from the Detection Rules detail sheet, the operator pastes a sample into a textarea and the dashboard calls the new `risk.rules.test` endpoint which dispatches to the same scanner code (gitleaks, Presidio, prompt-injection, regex) the worker uses. The response is a list of `TestDetectionRuleMatch`es mirroring the runtime risk_result shape.

  Drop the severity-override UI from the rule detail sheet. The override edit / reset affordances will return in a follow-up PR; default severity continues to render as a row badge for context.

### Patch Changes

- e4c2bfb: Logs filter search bars can now be cleared with the Escape key or by emptying the box, not just the × button. This applies to the MCP Server Logs filter bar and the shared SearchBar (Agent Sessions, etc.). Escape only clears when there is text to clear, so an empty box lets the key bubble to close a surrounding popover.
- 649a7cb: Fixed sidebar nav hover highlight snapping back to active route when moving between items.
- 72ccf7b: Fixes login journey for allowed orgs
- Updated dependencies [faaab73]
  - @gram-ai/elements@1.33.2

## 0.63.0

### Minor Changes

- 37158f0: ingest tags declared on Gram Function tools (top-level `tags` on the manifest and `tags?: string[]` on the TS framework `ToolDefinition`) and expose them through the management API; the playground tool editor now opens for function tools the same way it does for HTTP tools
- 7e4501e: Metric cards now display abbreviated numbers (1.5K, 2.3M) instead of raw comma-separated values.
- 5875739: Added remote-based MCP server authentication UI
- 50ab453: Add SSO and SCIM feature flags with WorkOS event sync. Admin settings now includes product feature toggles for SSO and SCIM. The Identity page shows connection status and gates configure buttons on these flags. Team page invite button is disabled when SSO is active. WorkOS event processing now handles all SSO connection and SCIM directory sync lifecycle events.

### Patch Changes

- 1871808: Fix the triggers page failing to load whenever a wake trigger has fired or been cancelled. The triggers list response advertised a status enum of `active | paused`, but wake triggers transition through `fired` and `cancelled` too, so the dashboard's response validation rejected the payload and surfaced a generic "Response validation failed" error. The status enum now includes all four states, and the triggers page renders distinct badges for fired and cancelled triggers instead of mislabelling them as "Paused".

## 0.62.0

### Minor Changes

- 5b5dc6d: Replace Vaul drawers with Radix sheets for chat detail panels, restoring text selection in trace/log views and removing the unused drawer dependency.

### Patch Changes

- 5a9c1f4: expose the Destructive CLI Commands category in the risk policy form so customers can opt into `cli_destructive` scanning, and stop silently stripping the source when editing policies that had it set via the API
- 217b173: Fix the empty "Logs" panel on a source's Deployments tab. The embedded panel was comparing a kind string (e.g. `"function"`) against log events' `attachmentId` (a UUID) and was also sending the wrong type token (`"function"` instead of the backend's `"functions"`). Now filters on `event.attachmentType` with the correct backend constants (`functions`, `openapi`, `external_mcp`), so per-source deployment logs render as expected.

## 0.61.0

### Minor Changes

- 23d2150: expose tags on tool variations and add a tags row to the playground tool editor for HTTP tools, with chip input, base-source quick-add, override indicator, and reset-to-source affordance
- 46e00ac: Migrate dashboard tables to Moonshine Table with sortable insights columns and remove the legacy table wrapper.

### Patch Changes

- 821f4a2: Redesign the org home page with a two-column layout. Left rail shows compressed Recent challenges (color-coded deny pills, sidebar-friendly width) and Recent activity (sharing the audit-log action color scheme). Right column shows projects as a thin rectangular stack (list view) or a 1/2/3-column card grid (grid view, toggle persisted in localStorage). Each project row/card shows the most recent audit-log action with a hover tooltip for full UTC + local timestamps, a facepile of active contributors (up to 10, sourced from audit-log actors with a deterministic earliest-by-joinedAt fallback), a star to favorite/unfavorite (stored client-side per org), and a kebab menu (favorite toggle, project settings, view audit logs, copy slug). New "Add New" dropdown next to the search bar offers Project / Team member / Role, gated by `org:admin`. Favorites surface in their own section above an "All projects" divider when present.
- 5ded32c: Preserve theme and saved project favorites when logging out.

## 0.60.0

### Minor Changes

- b58bf0f: Adds an org-level AI Integrations product surface with Cursor as the first provider. Organization admins can connect a Cursor Admin API key from org settings, and an hourly Temporal workflow polls Cursor for token and cost usage events and writes them into ClickHouse `telemetry_logs` so the dashboard shows Cursor usage and cost alongside Claude Code data. The dashboard cost copy is updated to reflect Cursor and Claude Code coverage, and the employee detail page now shows cost beside total tokens.
- ed12a35: Add multiple role support to the RBAC system. Users can now be assigned multiple roles simultaneously, replacing the previous single-role assignment model.
- 3b8bfb4: Adds `risk.results.listForAgent` — a redacted variant of `risk.results.list` for AI assistant / MCP consumption. The new endpoint returns the same fields as `listRiskResults` but replaces the `match` field with `match_redacted`, an opaque token of the form `<redacted len=N sha=XXXXXXXX>` where `N` is the byte length and `XXXXXXXX` is the first 8 hex characters of `sha256(match)`. Identical secrets produce identical fingerprints so agents can dedupe leak counts without ever seeing secret content.

  `shadow_mcp` findings pass `match` through verbatim because the value is a server URL or stdio command identifier (already shown unmasked in the dashboard), and exact byte positions are coarsened to a single `position_known` boolean to remove reconstruction signals.

  The dashboard's AI Insights sidebar gains risk-aware suggestions on the Security Overview and Policy Center pages, plus a system-prompt rule that bars the assistant from echoing `match_redacted` values verbatim.

### Patch Changes

- 9d6ba7b: The Source Activity panel on the Remote MCP source overview now shows real telemetry for the last 7 days, scoped to that remote server via the new `remote_mcp_server_id` filter. `TelemetrySummaryRow` and `ToolBarList` are extracted into a shared `SourceActivityPanel` component consumed by both the OpenAPI and Remote MCP source overview tabs.
- 4b49beb: Expand the assistant onboarding personality picker with Brad and Walker, rebalance Quinn against Nolan and Daniel, and group team voices into a compact chip row above the generic preset cards (Friendly / Professional / Playful / Analytical / Teacher).
- 8e247f9: Chat loading is now paginated by generation, returning one generation per request. The chat detail panel fetches older generations in parallel until the full transcript is assembled, so long-running sessions no longer stall on the initial fetch.

## 0.59.0

### Minor Changes

- d755880: Assistants spec panel now has a "Sessions" quick link that opens Agent Sessions filtered to that assistant.

## 0.58.0

### Minor Changes

- 12a0fa3: Add risk overview summary metrics, charts, and trend data for recent policy findings.

### Patch Changes

- 3db9f30: Deleting a custom domain now soft-deletes every `mcp_endpoints` row registered under it across all projects in the org, emits one `mcp-endpoint:delete` audit event per cascaded row, and the dashboard delete-confirmation modal previews the impacted endpoints via the new `/rpc/domain.listMcpEndpoints` endpoint.
- 7b002eb: The Assistants spec panel now links each attached MCP server to its MCP details page, so you can jump straight from the draft view to inspect or configure the server.
- 85790f1: The active/paused indicator on each assistant card is now an interactive switch — you can pause or resume an assistant directly from the assistants list without opening it.
- 12a0fa3: Add risk overview summary metrics, charts, and trend data for recent policy findings
- 35a7938: Improved server names in hooks logs. Improved UI for inspecting indiivudal logs
- Updated dependencies [12a0fa3]
  - @gram-ai/elements@1.33.1

## 0.57.0

### Minor Changes

- 5e00422: Add the initial Remote MCP-backed MCP server management UI under the `gram-remote-mcp` feature flag.
- f9b43d9: Add deny rules (exceptions) to RBAC role editor, allowing admins to grant broad access then carve out specific resources or tools that a role should not access

### Patch Changes

- 2cdae0e: Fix token graph blanking when filtering by agent type on /insights/costs
- 8dcf760: update @speakeasy-api/moonshine dependency to 1.36.1

## 0.56.0

### Minor Changes

- 639952a: Add a beta Risk Events log under Logs for reviewing and managing policy-flagged or blocked findings.

### Patch Changes

- 93d7919: Keep the OAuth wizard auto-configure path on OAuth Proxy setup with DCR credentials, even when user-session onboarding is enabled for the organization.
- 3f928eb: Fixes agent log drawer accessibility warnings

## 0.55.0

### Minor Changes

- 129e642: Add "Install All" button to collection detail page for bulk server installation
- 4ea14f3: Enforce RBAC on the collections API. `List` and `ListServers` now require `org:read`; `Create`, `Update`, `Delete`, `AttachServer`, and `DetachServer` require `org:admin`. The dashboard's sidebar, collections list, and detail pages open up to `org:read` members, while create/edit/delete and server attach/detach controls stay behind `org:admin`.
- 4eadd44: Show assigned roles on pending organization invites and allow org admins to change the role before acceptance. Invite creation and invite role changes now emit audit log entries.
- b8ed614: Adds filtering and clearer presentation for trace entries

### Patch Changes

- 7a82597: Filter the plugin "Add Server" dialog to exclude toolsets already attached to the plugin, preventing duplicate entries.
- fb40041: Show a graceful message in AI Insights and the Playground when an organization runs out of chat credits. Previously the chat would silently stop streaming on a 402 from the gateway because the AI SDK masks stream errors by default. The thread now renders `You've reached the chat credit limit for this account. Click the "Get Support" button at the top of the page to reach out about upgrading.` and the new `describeStreamError` / `CREDITS_EXHAUSTED_MESSAGE` exports are available on `@gram-ai/elements` for downstream consumers that want to react to the same condition.
- Updated dependencies [fb40041]
  - @gram-ai/elements@1.33.0

## 0.54.0

### Minor Changes

- ea88bb7: Add `Preview` and `Beta` release-stage badges so pre-GA features are clearly labeled wherever their name appears. The same badge renders inline on the sidebar nav entry (with a hover tooltip explaining the stage), the page section title, sub-route tabs, and raw `<h1>` headings. Tagged in this release: **Assistants** as preview; **Risk Overview** and **Risk Policies** as beta; **Insights → Employees** and **Insights → Costs** as preview. Also: the Policy Center nav entry is renamed to **Risk Policies** to match its page heading and URL, the Risk Overview empty state now uses the same dashed-border box treatment as Risk Policies, and a handful of pages that were missing a top-level title (SDKs, Integrations, Roles & Permissions, Audit Logs) now have one.

### Patch Changes

- 0f52a3e: The playground's Connect button now drives the issuer-gated OAuth flow when a toolset is bound to a user-session issuer, so connecting to MCP servers like `speakeasy-team-github` lands an upstream session that the runtime can resolve. The connection-status badge and the 401 challenge on `/mcp/{slug}` both read from the issuer-gated session store for these toolsets, and the security-check fallback now always emits a non-empty `resource_metadata` URL.

## 0.53.1

### Patch Changes

- 3eaeb9b: Rename the plugins publish CTA from "Publish to GitHub" to "Publish Private Marketplace".
- 2b15965: Allow users to rename MCP servers from the server detail page without changing the server URL slug.
- f837455: Updated risk overview and policy page headers to match the shared page layout and Moonshine CTA patterns.

## 0.53.0

### Minor Changes

- ff01624: Onboarding now asks for the assistant's name and personality as two separate steps instead of one combined card. When the user has already named the assistant in chat ("call it X"), the agent skips the name picker and goes straight to the personality step.
- 3517705: add search bar to MCP servers listing page
- fa5ef43: Add Codex (OpenAI) hooks support. A new `/rpc/hooks.codex` endpoint accepts all six Codex hook events (SessionStart, PreToolUse, PermissionRequest, PostToolUse, UserPromptSubmit, Stop), enforces org-level risk policies on blocking events, and records telemetry to ClickHouse. The plugin generator now produces a downloadable Codex observability plugin (ZIP and install script) that registers the hooks with a Gram marketplace entry in `~/.codex/config.toml`. The install instructions dialog gains a Codex tab alongside Claude Code and Cursor.
- d807fe6: Rename the org "Security" tab to "Identity" and refresh the SSO / Directory Sync cards: drop the SAML-specific branding (Single Sign-On / SSO instead of SAML SSO), replace the hover popover with a tooltip on a fully clickable Configure button, and capture an `identity_provider_interest` PostHog event on click so the team is pinged in Slack when a customer expresses interest. Clicking now confirms with a success toast.
- bbfecc5: Allow adding multiple GitHub collaborators when publishing plugins to a marketplace. The publish dialog accepts a list of usernames as chips, and the `publishPlugins` API now takes `github_usernames` (array) instead of `github_username` (string).
- 1057ea9: Add OTEL forwarding: customers can configure a URL and headers on the Org Logs page, and a body-tee middleware mirrors every payload received on `/rpc/hooks.otel/v1/*` to that endpoint. Forwarding is org-wide, async (bounded worker pool, fire-and-forget on failure), capped at 4 MiB per request, and gated behind `org:admin` for writes / `org:read` for reads. Header values are encrypted at rest and never returned by the API.
- db033c9: Make the Project Overview page date range configurable via a `TimeRangePicker` in the header, matching the Insights and Logs pages. The selected range is URL-backed (`range`, `from`, `to`, `label`), the default is still the last 7 days, and the "Explore with AI" suggestions reflect the active range.
- a5e0990: Added support for configuring webhooks to deliver audit log events to external destinations.

### Patch Changes

- 491f3b8: add an opt-in L1 ML prompt-injection classifier (deberta-v3) that runs alongside the heuristic baseline. enable the new "ML classifier (deberta-v3)" rule under the Prompt Injection category in the policy editor to layer the classifier on top of L0 heuristics. detection runs in a sidecar service; configure with `PI_CLASSIFIER_URL` and `PI_CLASSIFIER_THRESHOLD` (default `0.9`)
- 055d650: Onboarding now asks for Slack credentials in the order users encounter them in Slack's UI: Signing Secret first (Basic Information), then Bot User OAuth Token, then User OAuth Token (both on the Install App / OAuth & Permissions page). No more pasting `xoxb-` into the signing-secret field.
- 7290607: Removed the 1-public-MCP-server cap on accounts without an active subscription. Users can now enable as many public MCP servers as they want on any plan.
- 86023a0: Prevent a transient "toolset not found" error from appearing immediately after deleting an MCP server's toolset.
- 476e7d2: Disable Create Assistant buttons in the dashboard when the user lacks the permissions needed to create one, with a tooltip explaining why.
- Updated dependencies [4069ffd]
  - @gram-ai/elements@1.32.1

## 0.52.0

### Minor Changes

- 35b4b51: The assistant onboarding chat now connects to every MCP server attached to the assistant, not just the first one, so the agent can call tools across all configured toolsets.
- c6944af: Repurpose the Agents insights tab into an employee token observability dashboard. The new view shows per-employee token consumption, estimated cost, tool usage breakdown, and platform/model distribution. Clicking an employee row opens a detail dialog with model-level usage, time-series charts, and tool breakdown. Results can be scoped to specific coding tools like Cursor or Claude Code, and the outdated Elements setup modal no longer appears on this page.

### Patch Changes

- b2012be: Fix expanding panel animation on the assistant page.
- Updated dependencies [35b4b51]
- Updated dependencies [35b4b51]
  - @gram-ai/elements@1.32.0

## 0.51.0

### Minor Changes

- 1f34b03: Unified the Observe filter bar for Insights Tools and Logs Tools views; server, email, and type filters are now multi-select dropdowns with OR semantics.
- 5d80d8c: Rebuild the assistant-onboarding Slack install step as two separate cards: an install card (workspace pick, install, Event Subscriptions Retry) followed by a tokens card. Copy rewritten for non-technical users, with the Event Subscriptions Retry step called out as the most common silent failure.

## 0.50.0

### Minor Changes

- 575c0ac: Overhauled MultiSelect component: new onValueChange/defaultValue API, option grouping, per-option styling, and singleLine fixed-height mode.

### Patch Changes

- 0ac8dba: Refactor Security Overview to use Page.Section wrappers, scrollable tables, and a lighter category label.

## 0.49.1

### Patch Changes

- 6b070cd: Assistant onboarding now installs a Slack app reliably end-to-end: the install card stays in view until you click "I've installed it", a single approval grants both the bot and user OAuth tokens, and the generated manifest can no longer be rejected by Slack. Slack-touching assistants now get a Slack trigger by default — additive with any cron or other trigger you asked for — so the bot is reachable bidirectionally and you can talk back to it.

## 0.49.0

### Minor Changes

- 5b1da59: Add an Employees tab to the AI Insights section that tracks Gram uptake and compliance across organization members. Shows per-member token usage, compliance status, and last activity over the last 30 days, paginated at 25 per page. Usage is attributed by matching the email reported by each AI coding tool (Claude Code, Cursor) to the member's Gram account.

### Patch Changes

- 79d57ad: Always grant the full Slack bot-scope superset in the assistant onboarding manifest builder, regardless of which platform tools are attached. Slack manifests are static post-install — adding a scope later forces the user to delete the app and re-OAuth — so per-tool scope gating only locked future capabilities behind a forced re-install.
- 2c84295: Surface `environment:read` / `environment:write` in the RBAC dev toolbar and the
  `access.listGrants` fallback so the env-clone permission picker works end-to-end.

## 0.48.0

### Minor Changes

- 5136b45: Add the initial Remote MCP source management UI under the gram-remote-mcp feature flag: a Custom remote server entry in the Add Source dropdown, a URL-only create form, and a detail page with Overview, MCP Servers, and Settings tabs covering URL edit and a delete flow that lists the linked MCP servers and endpoints. Also renames the existing Third party server entry to Registry server.
- 50433e1: Upgraded dashboard and elements Tailwind dependencies to 4.2.4

### Patch Changes

- d1bdd11: Fix a crash in the RBAC dev toolbar when toggling `environment:read` or
  `environment:write` for the first time. The toggle handler spread `undefined`
  into a new state entry, producing an object without the `resources` field;
  the next render then crashed reading `.length` on undefined. Hardened
  `toggleScope` and `setScopeResources` to materialize a known-good baseline
  before spreading, and added a defensive `!= null` at the render site so any
  legacy malformed localStorage state can't crash either.
- 0b356a5: Fix Claude Code plugins not loading after restart. The `git-subdir` source
  type used by the marketplace proxy does not persist the plugin cache path
  across Claude Code sessions, causing "not cached at (not recorded)" errors
  on every relaunch. The marketplace URL returned by `getPublishStatus` now
  points directly at the git proxy (`/marketplace/p/{token}.git`) and the
  install instructions emit `"source": "git"` in the `extraKnownMarketplaces`
  snippet, which Claude Code caches reliably between sessions. The
  URL-based manifest endpoint and its rewrite logic have been removed.
- Updated dependencies [50433e1]
  - @gram-ai/elements@1.31.0

## 0.47.0

### Minor Changes

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

### Patch Changes

- 188e614: Add a credit-balance gate on `/chat/completions` for **free-tier** orgs: pre-request check returns HTTP 402 `insufficient_credits` once the cached Polar Chat Credits balance is exhausted. Pro and enterprise stay bounded by the existing OpenRouter monthly key cap; unifying the two limit sources is tracked separately. Speakeasy-internal orgs (`specialLimitOrgs`) bypass; cache misses fail open. Self-serve top-up checkout (`usage.createTopUpCheckout`) opens a one-time Polar product configured via `POLAR_PRODUCT_IDS_TOPUP`.
- e9f4a92: Add a Clone action to environment cards in the Environments page. The clone
  dialog lets users pick a new name and choose whether to copy only the variable
  names (with empty placeholders) or duplicate the encrypted secret values from
  the source. Encrypted secret values are never decrypted during the clone —
  ciphertext is copied row-to-row inside Postgres. Clone is gated by a project-
  level `environment:write` scope plus a per-resource read check on the source
  environment (either an `environment:read` grant on that specific env or a
  `project:read` grant on the project).
- 8ce7444: scan risk policies for prompt injection. enable the new "Prompt Injection" category in the policy editor to flag or block instruction overrides, role hijacks, system-prompt leaks, encoded payloads, delimiter injection, and shell tool-abuse attempts
- a25df49: Filter the "Recent Challenges" widget on the org home page to only show
  denied, unresolved challenges (previously it listed any challenge in any
  status). When there are no denied challenges, the widget now renders the
  same empty state used on the Denied tab of the Challenges page.

## 0.46.0

### Minor Changes

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

- 0978641: Default-attach Slack reaction tools during assistant onboarding and inject reaction etiquette guidance into the assistant's `# Behavior` section. Slack manifest builder now maps the reaction tool handlers to the `reactions:write`, `reactions:read`, and `emoji:read` bot scopes.

### Patch Changes

- b27c6bd: Allow publishing to GitHub when the org has only the observability plugin (no custom plugins required)
- 504c815: Allow setting custom policy messages to be shown to end users

## 0.45.2

### Patch Changes

- 485e9fa: Tag chat sessions started from the Assistants page with `X-Gram-Source: assistant` (was `assistant-onboarding`). Agent session logs now show `assistant` as the source for these sessions instead of conflating ongoing assistant chats with the onboarding flow.
- abf9f59: fix certain agent session side panel failing to load conversation history
- 07819a8: Show function memory and instances on source overview
- 8701c12: Redesign the MCP servers list on the plugin detail page so each entry
  matches the card pattern from the MCP list page: the Network icon in
  the dot-pattern sidebar, name plus tool-count badge in the header, and
  the Public / Private / Disabled status indicator on the footer left.
  The footer right has a trash icon button that removes the server from
  the plugin, and servers whose toolset has been deleted are flagged
  inline. Also extracts the shared status indicator from MCPCard,
  MCPTableRow, and the new card into a reusable
  `MCPStatusIndicator` component.

## 0.45.1

### Patch Changes

- 02712dc: Teams installing Gram-published plugins now get observability automatically.
  Each org's published marketplace ships a `base` plugin containing the team's
  hooks with credentials embedded — no manual SessionStart configuration, no
  credential paste, no risk of forgetting the setup step. Install once per
  machine and tool events flow into the Gram dashboard for the org regardless
  of how many feature plugins a team member also installs.
- ceaf5a8: Switch the Plugins list from a table to a card grid that matches the Collections
  page. Each plugin card surfaces name, slug, description, server count, and last
  updated time, and the existing delete action moves into a per-card menu. The
  empty state is replaced by the shared "create resource" tile so the layout stays
  consistent with Collections.
- b0726b5: Normalized observe component filenames to (section)(feature) pattern

## 0.45.0

### Minor Changes

- cc00be4: Assistants v0: server-side service, Temporal workflows + reaper, Fly.io / local Firecracker runtime providers, per-thread token manager, and the dashboard create/edit/onboarding UI for assistants with model, instructions, toolset and environment bindings.
- fb726e1: Reorganized Observe into tabbed Insights and Logs sections

### Patch Changes

- c44959b: Handle missing deployment and MCP detail routes with a not-found state instead of surfacing raw errors
- 745d0b2: feat(access): reassign members to the default role on role deletion and surface the affected members in the dashboard delete dialog
- 04c2dbf: Improve automatic setup of OAuth Settings for Remote MCP servers
- f32d4e2: Edit log filter chips on click instead of deleting
- 7721e8e: Add a one-click "Auto-Configure" path on the OAuth wizard's path selection step for OAuth 2.1 MCP servers, and drop the requirement that custom OAuth proxy configurations supply scopes.
- 2fa84af: click-to-reveal for sensitive data in risk findings
- 7c3be05: Support for shadow mcp blocking (block unapproved MCP servers org-wide)
- Updated dependencies [cc00be4]
  - @gram-ai/elements@1.30.1

## 0.44.0

### Minor Changes

- 58b4498: Support tool-level RBAC for MCP servers. Grants now use typed selectors with `resource_kind`, `resource_id`, `disposition`, and `tool` fields instead of untyped string maps. The dashboard scope picker stores toolset UUIDs (not slugs) as resource identifiers, fixing a bug where grants created via the UI never matched backend authorization checks. Public MCP servers correctly skip per-tool RBAC enforcement.

### Patch Changes

- 9ff743e: fix(dashboard): factor impersonation banner into page height calc so the bottom of the page stays reachable when impersonating an organization
- 5efc8d4: dashboard navigation polish: collapse both project- and org-level sidebars to an icon rail, fade-and-slide nav labels on collapse, show a click-loading spinner on nav items, reorder Chat Elements below Plugins, and unify the MCP and Playground empty states to match the Sources card pattern

## 0.43.1

### Patch Changes

- 1b6f532: add skill usage time series and users-per-skill charts
- ac59dac: feat(plugins): replace the Claude-only download button on the plugin detail page with a Download Plugin dropdown offering both Claude and Cursor
- Updated dependencies [2b2d423]
  - @gram/client@0.33.6

## 0.43.0

### Minor Changes

- e8e2d81: deps: lucide-react from 0.554 to 1.8.0
- ea3e1aa: Add GitHub publishing for plugins. Admins can publish generated plugin
  packages to a GitHub repository via a configured GitHub App, enabling
  distribution through Claude Code and Cursor team marketplaces.

### Patch Changes

- a74a72b: fix(ai-insights): stop sidebar crash on rapid Explore-with-AI clicks, and render `chart` / `ui` widgets in the agent session pop-out
- c797e16: fix: resolve ResizeObserver loop errors on navigation hover
- e81699f: Show the published GitHub repo URL on the plugins page, and include it in the publish success toast.
- 00a8f2a: Cursor hooks native MCP support. Token use tracking support for Cursor sessions
- Updated dependencies [3c581aa]
- Updated dependencies [a74a72b]
- Updated dependencies [e8e2d81]
  - @gram-ai/elements@1.30.0

## 0.42.0

### Minor Changes

- d8c6ce1: add support for publishing external servers into collections.
- cd8d31f: charts on the Hooks analytics page can now be expanded to full-width for easier reading
- a20f7df: Add risk analysis system for detecting secrets and sensitive data in chat messages.
- 1ee9f95: Improved Hooks dashboard with new charts, refined visuals, and smarter default filters.
- 04c6c30: Add team invite flow with accept page, configurable expiry, and security hardening

### Patch Changes

- 8c5d6e9: - Add stable URL deep-links for agent sessions in Chat Logs — the selected
  session now syncs to a `chatId` search param so `/logs?chatId=<id>` is
  shareable and survives reload.
  - Upgrade the default AI Insights model from claude-sonnet-4.5 to
    claude-sonnet-4.6.
  - Insights sidebar now opts into tool-output byte capping (50KB per MCP tool
    call) and tighter auto-compaction (60% of the model's context ceiling) to
    avoid "prompt is too long" errors on long tool-heavy conversations.
- 0f687d7: fix: remove gradient from onboarding banner
- 78d4d2b: Fix project onboarding banner to support dark mode by using semantic
  background tokens instead of hardcoded white.
- e1f64de: Add a "blank MCP server" CTA on the empty-project MCP page (create empty server, add built-in tools, connect a data source later). Relabel `TriggerLogRow` counts from "N attempts" to "N events".
- 442223d: Warn users before flipping an MCP server to Public when the attached environment has system-provided values that would be shared with every caller.
- dc4b0f3: Add eight Slack platform tools: read channel messages, read thread messages, read user profile, search channels, search messages and files, search users, send message, and schedule message.
- 5c81e5f: Fix plugin toolset picker to show project-scoped toolsets instead of all
  org toolsets. Uses useListToolsets (project-scoped) instead of
  useListToolsetsForOrg.
- c05690d: Show skeleton loading state for toolset picker in plugin detail instead
  of incorrectly displaying "No toolsets available" while loading.
- 8ea73c8: Add info tooltips to every KPI and chart card on the Project Overview
  dashboard, plus an "Explore with AI" wand on each chart that opens the
  Insights sidebar and auto-submits a chart-specific question through the
  thread runtime. The nav-bar AI Insights trigger also gains a brand
  gradient border on hover.
- f0cf087: Trigger infrastructure additions: `App.RegisterDispatcher` for post-construction dispatcher wiring; short-circuit Slack `url_verification` in `AuthenticateWebhook`; drop the `thread_ts`→`ts` fallback so top-level DM/channel messages correlate on the channel alone; populate `Task.EventJSON` and surface `bot_id`/`app_id` on Slack trigger events.
- 8b698a3: Hooks dashboard bar charts now collapse to the top 6 rows with a "show more" link to expand the full dataset.
- Updated dependencies [8c5d6e9]
- Updated dependencies [d0356b5]
- Updated dependencies [d8c6ce1]
- Updated dependencies [1ee9f95]
  - @gram-ai/elements@1.29.0
  - @gram/client@0.34.0

## 0.41.1

### Patch Changes

- e56314e: Captures token and cost metrics for Claude agent sessions
- Updated dependencies [e56314e]
  - @gram/client@0.32.65

## 0.41.0

### Minor Changes

- 63317cc: feat: replace MCPEnableButton with 3-state status dropdown (Disabled/Private/Public)
- 91f7e0d: Improve OAuth configuration for external MCP servers with a new step-by-step wizard flow. Extracts OAuth setup into a dedicated wizard with state machine (useReducer), supports both proxy and external OAuth paradigms, and adds success/failure result steps.
- ea1e23d: Add organisational collections and the capability to publish MCP servers to share within the organisation.
- f749a53: Add plugins feature for distributing MCP server bundles to teams and allowing zip distribution
- 60fe6ee: feat: replace home placeholder with data-driven project dashboard
- ab0c415: Update for Hooks Dashboard. Hooks now has charts for server activity, source volume, user activity, error rates, and usage over time. A new metric cards row surfaces key KPIs at a glance. Includes a toggle to show/hide the raw trace log list alongside the charts.

### Patch Changes

- 7b34ae4: Add click-to-filter on attribute rows in the MCP logs detail sheet. Click any attribute to filter by equals, exclude, contains, or copy its value. Also fixes attribute filters returning too few results due to a hardcoded event_source filter that didn't account for attributes being spread across multiple log entries per trace.
- 1ea6dff: Adds a super-admin interface for enabling RBAC to organisations.
- dce4595: Surface AI Insights as a static button in the top breadcrumb bar across every project page. Pages that need a custom prompt or tool set declare it with `<InsightsConfig />`; everywhere else the global default applies.
- aa3c846: Redesign MCP logs with color-coded severity badges, left-edge status dots, tighter row density, sample-query popovers on the filter and date-range inputs, and React performance fixes (memoized trace rows and stable callbacks)
- 8e4fd98: Adds a better error handler for failed role resolution in the case that the user winds up with a corrupt session.
- 3a3850e: Restore the rich tool-call rendering in the playground. The MCP Apps integration had replaced Elements' default tool UI for every tool call; now the playground delegates to the default `ToolFallback` and only appends the MCP App iframe when the tool has a UI resource binding. Elements now exports `ToolFallback` from its public API so consumers can compose around it.
- be476e6: feat: use pre-aggregated summary endpoint for hooks analytics charts and KPIs
- Updated dependencies [d2bf604]
- Updated dependencies [f749a53]
- Updated dependencies [3a3850e]
- Updated dependencies [be476e6]
  - @gram/client@0.33.0
  - @gram-ai/elements@1.28.0

## 0.40.0

### Minor Changes

- 98d322b: Add support for triggers across Gram.

  This introduces webhook and scheduled triggers end to end, including server APIs, worker execution for trigger dispatch and cron processing, SDK support, and dashboard UI for managing trigger definitions and instances.

### Patch Changes

- 19fb17f: Add ability to soft-delete chat sessions from the dashboard with confirmation dialog, available from both the chat list table and detail panel
- cdf94a3: Redesign deployment logs with color-coded level badges, dot indicators, inline keyboard hints, and React performance fixes
- b20533b: fix: migrate globals.css to Tailwind CSS v4 syntax
- 4590453: Move oauth config to the "authentication" tab of mcp page and provide indications for type of Oauth connection per MCP.
- Updated dependencies [98d322b]
  - @gram/client@0.33.0

## 0.39.0

### Minor Changes

- 61cc193: Add team invite flow with accept page, configurable expiry, and security hardening

### Patch Changes

- 734c03d: Fix playground credential saving failing with "length of slug must be lesser or equal than 40" error. The environment slug format was shortened to stay within the server's 40-character limit.

## 0.38.0

### Minor Changes

- b328938: Add static platform tools to tool discovery and the built-in MCP logs server.

### Patch Changes

- 3a3acd3: Add editable OAuth proxy server configuration.

  Admins can now edit an existing OAuth proxy server's audience, authorization endpoint, token endpoint, scopes, token endpoint auth methods, and environment slug without having to unlink and recreate the configuration. The new `POST /rpc/toolsets.updateOAuthProxyServer` endpoint accepts partial updates with PATCH semantics (omit fields to leave them unchanged; pass an empty array to clear array fields). The dashboard's OAuth proxy details modal now exposes an Edit button that opens the existing OAuth modal in edit mode with the current values pre-filled.

  Slug and provider type remain immutable after creation. Gram-managed OAuth proxy servers stay view-only.

- Updated dependencies [3a3acd3]
- Updated dependencies [b328938]
  - @gram/client@0.33.0

## 0.37.2

### Patch Changes

- 494f76c: Adds support for tracking skills in hooks dashboard
- baa93c7: Store user-provided playground credentials in encrypted server-side environments instead of localStorage. Credentials are scoped per-user per-toolset and resolved server-side when proxying to MCP servers. Also shows the active environment name in the authentication section and adds a starter suggestion prompt.
- Updated dependencies [494f76c]
  - @gram/client@0.32.38

## 0.37.1

### Patch Changes

- fc19ac9: Rename Chat Sessions, Slack, and CLIs dashboard nav tabs to Agent Sessions, Assistants, and Skills
- 3af7f95: fix install instructions for cursor hooks
- d571001: Fix tool request/result JSON clipping in playground by adding `overflow-auto` to the details container
- 4531f8e: Performance tab for MCP page tool selection mode for static and dynamic toolsets.
- 8c488a2: Restore audit logs sidebar link alongside roles & permissions
- 7a685a7: Update playground models to latest OpenRouter offerings — add Claude Sonnet 4.6, GPT-5.4 Mini, o4-mini, o3, Gemini 3.1 Pro, DeepSeek R1/V3.2, Llama 4 Maverick, Grok 4, Qwen3 Coder and remove superseded models
- Updated dependencies [7a685a7]
  - @gram-ai/elements@1.27.6

## 0.37.0

### Minor Changes

- c28788e: Add MCP App support across the playground, local functions runner, and the functions SDK.

  Improve local runner lifecycle handling for proxied tool and resource responses, and only seed MCP App function assets when the functions backend is local.

### Patch Changes

- 0a3af53: Adds support for full session capture from Claude. Complete transcripts of prompts, responses, and tool calls
- ba10ce4: Add Cursor hooks support with authenticated endpoint, plugin, and setup
- Updated dependencies [0a3af53]
  - @gram/client@0.32.20

## 0.36.4

### Patch Changes

- 5d68b58: Replace browser confirm() with Dialog component for MCP server deletion
- bcc775c: adds feature flagged dashboard for assigning roles

## 0.36.3

### Patch Changes

- 3831ca8: Improve initial page load performance by prefetching key queries in parallel with auth, adding preconnect hints, and switching font-display to swap.
- 19fcd09: when searching mcp logs show available attribute keys

## 0.36.2

### Patch Changes

- b0f341b: Fix Pylon chat widget overlapping playground send button by hiding the default launcher and adding toggle support to the Get Support button.
- c54bf04: Clean up defunct observability seed tool logic

## 0.36.1

### Patch Changes

- 2b7754e: Align built-in MCP detail page header and install section with standard MCP detail page styling

## 0.36.0

### Minor Changes

- 7710d31: Introduced a diff viewer that highlights the changes in audit subjects for update events.

  This establishes a baseline UX for understanding the changes happening in orgs/projects. In future iterations, some of the changes will be promoted to natural language bullet points under each audit log message.

  Additionally this change adds a preprocessing step to rename toolset:_ audit events to mcp:_ since "toolsets" are no longer a visible primitive on the dashboard.

### Patch Changes

- ba94c5a: Make deployment interactions non-blocking by passing `nonBlocking: true` to create/evolve API calls. The UI now polls for deployment completion instead of blocking the request, preventing timeouts on long-running deployments. Added error handling for polling failures so the UI shows an error state instead of getting stuck on a permanent spinner.

## 0.35.0

### Minor Changes

- c4d9bdd: Introduced a new "Audit Logs" page to the organization dashboard, allowing Gram users to view a history of actions taken within the organization.

### Patch Changes

- 3d28f83: Fixes bug with server selection in logs page.

## 0.34.3

### Patch Changes

- 68177ef: Upgrade insights copilot to anthropic/claude-sonnet-4.5 and inject current date into system prompt
- 544fac2: Revamp login page with Speakeasy brand styling, distributed platform diagram, and updated copy.

  - Right pane: new copy, Build/Secure/Observe/Distribute badges, off-white background with moving dot pattern, RGB gradient bar, Terms of Service and Privacy Policy links
  - Left pane: distributed AI agents and product agents view, Control Plane and Tools Platform sections, pulse flow animations, hover-activated dot background, docs social link
  - Accessibility: prefers-reduced-motion support for all animations

- cbc16a9: Suppress skeleton flash on logout by skipping the loading shell on unauthenticated routes
- Updated dependencies [658bef4]
  - @gram/client@0.33.0

## 0.34.2

### Patch Changes

- 045f51a: Replace hardcoded org slugs in MCP URLs for the built in MCP logs server

## 0.34.1

### Patch Changes

- 558c158: Show coming soon placeholder on CLIs page
- 41d507c: Fixed `GET /rpc/chat.creditUsage` authentication so org-scoped credit usage works correctly for customers with multiple projects, requiring only session auth and no longer allowing chat-session access.
- Updated dependencies [7ef727b]
  - @gram/client@0.28.5

## 0.34.0

### Minor Changes

- 30036db: Add table view toggle for list pages (MCP, Sources, Catalog) with grid/table switching, animated dot-pattern rows, and localStorage persistence

### Patch Changes

- 17788a8: fix: MCP environments section shows wrong default when none attached
- b0120d4: Prevent double-back-button on detail pages

## 0.33.2

### Patch Changes

- 7aaeb96: Fix playground OAuth discovery to use toolset-level configuration instead of removed tool-definition fields.

  The frontend now detects OAuth requirements from `toolset.oauthProxyServer` and `toolset.externalOauthServer` instead of inspecting individual external MCP tool definitions (whose `requiresOauth` field was removed in a prior PR). The backend `getExternalOAuthConfig()` gains two new resolution paths — OAuth proxy providers with pre-configured client credentials (skipping DCR) and external OAuth server metadata — before falling back to the legacy tool-definition lookup for backward compatibility.

## 0.33.1

### Patch Changes

- 3b26329: Display audience field in OAuth proxy server details view

## 0.33.0

### Minor Changes

- 8c72d8c: Renames attribute_filters to filters in searchLogs, and introduces "in" operator.

### Patch Changes

- 110f5b1: Replace Claude Desktop mcpb download with Connections instructions on MCP install page
- d8133af: Suite of hooks improvements
- 5c7aa32: Rename MCP environment tab labels for clarity. `Project` tab renamed to `default` to match environment name.
- 76b411d: Update hooks UI to better accomodate many servers/users
- 686fee5: Add gpt-5.4 support in playground.
- Updated dependencies [d8133af]
- Updated dependencies [6108c5a]
- Updated dependencies [686fee5]
- Updated dependencies [8c72d8c]
  - @gram/client@0.28.0
  - @gram-ai/elements@1.27.5

## 0.32.1

### Patch Changes

- 1765931: Removes the logs enabled flag in the telemetry API responses.
- 1500853: Surface correct http status attribute references in MCP logs search
- e616da7: Add admin-only cache purging functionality
- Updated dependencies [1765931]
- Updated dependencies [e616da7]
  - @gram/client@0.27.20

## 0.32.0

### Minor Changes

- 63d10d0: ## Changeset

  External MCP servers now use the same OAuth configuration pathway as all other toolsets — no more special-cased token resolution.

  The "Configure OAuth" button is now enabled for external MCP servers that require OAuth. When discovered OAuth metadata is available, the configuration form can be auto-populated with a single click.

### Patch Changes

- 0c90e1e: Add hooks dashboard page
- Updated dependencies [0c90e1e]
  - @gram/client@0.27.24

## 0.31.0

### Minor Changes

- be6dcae: Upgrade zod to v4 across the monorepo. Bump @modelcontextprotocol/sdk
- f066870: Adds ability to telemetry logs page to filter by dynamic attributes.

### Patch Changes

- 907ea0b: Move server instructions to dedicated section with LLM generation with best practices for mcp server instructions based on [mcp release](https://blog.modelcontextprotocol.io/posts/2025-11-03-using-server-instructions/)
- 1821e46: Adds an initial pass "POC" implementation of Gram hooks for tool capture
- fb7439b: Improve settings page with tabs routing and logging API
- 998102f: Update telemetry search logs API response to sent unix nano timestamps as strings instead of int.
- Updated dependencies [ee711ab]
- Updated dependencies [1821e46]
- Updated dependencies [be6dcae]
- Updated dependencies [fb7439b]
- Updated dependencies [998102f]
  - @gram-ai/elements@1.27.4
  - @gram/client@0.28.0

## 0.30.0

### Minor Changes

- 125d6c9: adds the ability to install slack apps through the Gram UI

### Patch Changes

- 823e7ab: feat(cli): add `gram redeploy` command to clone and redeploy existing deployments

  fix(dashboard): show redeploy button on every deployment detail page and add visible Deployments navigation to sources page

- f293092: fix: tool call logs count shown but empty state

## 0.29.4

### Patch Changes

- Updated dependencies [f364cc0]
- Updated dependencies [e2c00cb]
  - @gram/client@0.28.0

## 0.29.3

### Patch Changes

- 3cae542: Improve logs page timestamp display (no wrapping, remove comma, hide duplicate child timestamps)
  Fix tree line alignment with parent chevron in expanded log rows
  Fix loading state layout shift in expanded logs
  Filter out chat completion logs (urn:uuid:) from tool calls list
  Fix breadcrumb scrolling issue on insights page
  Add click-outside-to-close for AI Insights sidebar
  Remove Beta labels from AI Insights
- Updated dependencies [3cae542]
  - @gram/client@0.28.1

## 0.29.2

### Patch Changes

- 833263c: Prevent source detail page crash when logs are not enabled. The telemetry query now uses useLogsEnabledErrorCheck and throwOnError: false to gracefully degrade without metrics instead of crashing the entire page.

## 0.29.1

### Patch Changes

- 6a585c5: Expose customer's mcp logs as built-in logs mcp server that comes pre deployed for a project. This enables customers to interact with their logs through their favorite LLM client just as they would with
  any MCP server created on the platform

## 0.29.0

### Minor Changes

- 0f4f5dd: Adds an opt-in toggle for recording tool call inputs/outputs in logs

### Patch Changes

- 2c8987d: Wire up add another button in environment variables sheet
- c4baf37: Redesign source detail page with two-panel deployments and invocation activity to give users a high level overview of a sources's utilisation in any MCP servers.
- Updated dependencies [0f4f5dd]
  - @gram/client@0.28.0

## 0.28.5

### Patch Changes

- Updated dependencies [7063e97]
  - @gram-ai/elements@1.27.3

## 0.28.4

### Patch Changes

- 987ce35: Reorder insights dashboard to show tool metrics first
- Updated dependencies [62c6784]
- Updated dependencies [c26afea]
  - @gram-ai/elements@1.27.2

## 0.28.3

### Patch Changes

- bb8f3d2: Add CLI commands tab to OpenAPI version update modal
- Updated dependencies [e5500f7]
  - @gram-ai/elements@1.27.1

## 0.28.2

### Patch Changes

- Updated dependencies [3d0ce56]
  - @gram-ai/elements@1.27.0

## 0.28.1

### Patch Changes

- 78f81f6: Bring back resources and prompts tabs to MCP details page
- d9506c5: Show tool annotation badges in tool list sidebar
- e87ada8:

## 0.28.0

### Minor Changes

- 514fce6: Improve observability chat logs with server-side sorting (sort_by/sort_order params), sticky pagination with page count, N/A score indicator with tooltip for unscored sessions, Shiki syntax highlighting for code blocks, character-based truncation with "Show more" button, System Prompt tab in chat detail panel, and Tool Result labeling for tool messages.
- 9df7d84: Add observability features including telemetry logs, traces, chat logs with AI-powered resolution analysis, and an overview dashboard with time-series metrics.
- ab5142f: fix UI bug where the openapi spec provided by URL upload is not fetched, leading to a blank preview.

### Patch Changes

- 292eab4: Add system prompt instruction to treat 4xx HTTP responses as errors in AI observability analysis.
- Updated dependencies [514fce6]
  - @gram/client@0.27.0

## 0.27.9

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

- Updated dependencies [f635e22]
  - @gram/client@0.27.4
  - @gram-ai/elements@1.26.1

## 0.27.8

### Patch Changes

- 6d195c5: Show date in deployment log line timestamps on the deployments page
- Updated dependencies [b2347fc]
- Updated dependencies [a34d18a]
  - @gram/client@0.27.3

## 0.27.7

### Patch Changes

- Updated dependencies [9cb2f0e]
  - @gram-ai/elements@1.26.0

## 0.27.6

### Patch Changes

- Updated dependencies [e08b45e]
  - @gram/client@0.27.1
  - @gram-ai/elements@1.25.2

## 0.27.5

### Patch Changes

- a7422f8: feat: add OAuth support for external MCP servers in the Playground
- a753172: feat: customize documentation button text on MCP install page
- 7505433: fix: allow creating MCP server when project has sources but no toolsets
- 1f74200: Fixes issue with loading of metrics when logs are disabled.
- Updated dependencies [a7422f8]
- Updated dependencies [a753172]
- Updated dependencies [6e29702]
- Updated dependencies [1f74200]
  - @gram/client@0.26.18

## 0.27.4

### Patch Changes

- a7cb2d9: reinstate deployments access in Sources page UI and make relevant deployment accessible from a source card
- Updated dependencies [63bb328]
  - @gram-ai/elements@1.25.1

## 0.27.3

### Patch Changes

- 85d64ad: Add support links to user dropdown menu (Get Support, Chat with Team, Bug or Feature Request)
- Updated dependencies [feea712]
- Updated dependencies [c9b74af]
  - @gram-ai/elements@1.25.0
  - @gram/client@0.26.13

## 0.27.2

### Patch Changes

- Updated dependencies [46004f8]
  - @gram-ai/elements@1.24.2

## 0.27.1

### Patch Changes

- Updated dependencies [ca387c6]
- Updated dependencies [6793e29]
  - @gram-ai/elements@1.24.1

## 0.27.0

### Minor Changes

- 0a550bc: Adds experimental metrics insights to dashboard.
- 567289d: Major UX overhaul with redesigned MCP cards, pattern-based illustrations, and improved environment variable management
- b85bfd5: Last accessed date is now available for Gram API keys and can be viewed via the
  API and dashboard settings page.

### Patch Changes

- 75eff56: Restored the organization override feature for admin users and ensures that both organization and project IDs are also displayed.
- 6a35424: Various UX improvements to the new dashboard
- 73304b3: UX improvements for Sources and MCP pages: tabbed interfaces, function tools table with runtime column, dynamic tab validation, softer delete warning styling
- 90ad1ba: Add support for install page redirect URLs
- Updated dependencies [659d955]
- Updated dependencies [c17b9f7]
- Updated dependencies [08e4fb5]
- Updated dependencies [438e1a7]
- Updated dependencies [2d520cb]
- Updated dependencies [afb9fbb]
- Updated dependencies [51b9f17]
- Updated dependencies [90ad1ba]
  - @gram/client@0.26.9
  - @gram-ai/elements@1.24.0

## 0.26.19

### Patch Changes

- e1f46b5: feat: add page titles to tab
- Updated dependencies [6744e5d]
  - @gram-ai/elements@1.23.0

## 0.26.18

### Patch Changes

- Updated dependencies [258b503]
  - @gram-ai/elements@1.22.5

## 0.26.17

### Patch Changes

- 156bc66: Fix logs page on dashboard and correct display issues in Elements library
- f8a3eae: Show all envirnoment variables for basic auth in mcp details and install page
- Updated dependencies [a57b307]
- Updated dependencies [156bc66]
- Updated dependencies [834a770]
  - @gram-ai/elements@1.22.4
  - @gram/client@0.27.0

## 0.26.16

### Patch Changes

- 484bbe0: Enable renaming of MCP authorization headers and with user friendly display names. These names are used as the default names of environment variables on the user facing MCP config.
- Updated dependencies [484bbe0]
  - @gram/client@0.25.16

## 0.26.15

### Patch Changes

- Updated dependencies [d733319]
  - @gram-ai/elements@1.22.3

## 0.26.14

### Patch Changes

- 9073203: Fix elements onboarding in dashboard which was broken by shadow DOM changes
- d6ae47c: Always connect to servers in playground through gram domain in order to avoid
  conflicting with CSP connect-src
- Updated dependencies [9073203]
  - @gram-ai/elements@1.22.2

## 0.26.13

### Patch Changes

- ff3ff3e: Restore chat history in the playground using Gram Elements

## 0.26.12

### Patch Changes

- 5c6f78a: Embed Elements chat in logs page
- Updated dependencies [5c6f78a]
  - @gram-ai/elements@1.22.1

## 0.26.11

### Patch Changes

- Updated dependencies [adac3f8]
  - @gram-ai/elements@1.22.0

## 0.26.10

### Patch Changes

- a0b7e13: feat: Use Gram Elements for the Playground UI
- Updated dependencies [a0b7e13]
- Updated dependencies [43500b3]
  - @gram-ai/elements@1.21.3

## 0.26.9

### Patch Changes

- Updated dependencies [0472997]
  - @gram-ai/elements@1.21.2

## 0.26.8

### Patch Changes

- Updated dependencies [ed50d35]
  - @gram-ai/elements@1.21.1

## 0.26.7

### Patch Changes

- 848e623: Fixed a couple of issues in the dashboard that were causing production errors: 1. Setup the monaco editor environment to properly load web workers for different languages 2. Add missing `Dialog.Title` elements in dialog headers to ensure accessibility compliance.
- Updated dependencies [03f7cbe]
- Updated dependencies [5d14e1a]
- Updated dependencies [0fd8d39]
- Updated dependencies [8b20bcf]
- Updated dependencies [3be7ac7]
  - @gram-ai/elements@1.21.0
  - @gram/client@0.25.12

## 0.26.6

### Patch Changes

- Updated dependencies [adc02ce]
  - @gram-ai/elements@1.20.2

## 0.26.5

### Patch Changes

- Updated dependencies [7506a42]
- Updated dependencies [b3ac308]
  - @gram-ai/elements@1.20.1

## 0.26.4

### Patch Changes

- 45dd841: Updated the dashboard and vite config so that monaco editor and various three.js dependencies are not included in the main app bundle. This was causing extreme bloat of that bundle which ultimately slows down loading times of the web app.
- bd81e47: Adds MCP server selection into elements configurator
- Updated dependencies [950419c]
- Updated dependencies [45eb983]
  - @gram-ai/elements@1.20.0

## 0.26.3

### Patch Changes

- 12e825c: Add hide/show toggle for environment variable inputs

## 0.26.2

### Patch Changes

- 81be736: Updates dashboard to only use telemetry API
- Updated dependencies [f2fa135]
  - @gram-ai/elements@1.19.1

## 0.26.1

### Patch Changes

- Updated dependencies [856576b]
- Updated dependencies [a1231be]
- Updated dependencies [748c52e]
  - @gram-ai/elements@1.19.0

## 0.26.0

### Minor Changes

- eefebf6: Add updated elements onboarding

### Patch Changes

- Updated dependencies [f744f2b]
  - @gram-ai/elements@1.18.8

## 0.25.2

### Patch Changes

- f0dad26: Adds support for UNSAFE_apiKey in Elements. This will be used during onboarding to allow users to quickly trial elements without needing to set up the sessions endpoint in their backend

## 0.25.1

### Patch Changes

- 8ad0455: Ensure delete source dialog closes after completion
- 0583dc0: Improves logs side panel to make it wider and more human-readable
- Updated dependencies [d972d1b]
- Updated dependencies [3a82c2e]
  - @gram/client@0.25.8

## 0.25.0

### Minor Changes

- 01932db: Removes legacy logs page, replaced with a new page for improved user experience

### Patch Changes

- c8c45b5: add a source detail page for imported mcp servers

## 0.24.0

### Minor Changes

- 0341739: Add a new telemetry page to view logs grouped by tool calls

### Patch Changes

- b73b92d: Added empty state component for catalog search results

## 0.23.1

### Patch Changes

- Updated dependencies [7e5e7c8]
  - @gram/client@0.24.2

## 0.23.0

### Minor Changes

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

- 6e84b55: Allow external mcp sources to be renamed in the Gram UI
- Updated dependencies [811989e]
- Updated dependencies [76beb93]
- Updated dependencies [8c865e1]
  - @gram/client@0.24.0

## 0.22.3

### Patch Changes

- ba502dc: fix playground tools list now updates immediately when adding/removing tools from a toolset
- abbb9a3: Don't brick page when certain dialogs are closed. Also improves the mcp config dialog to not overflow the entire screen

## 0.22.2

### Patch Changes

- 45bea6e: Pin to older mcp-remote@0.1.25 to avoid classic claude desktop issue with selecting the oldest node version on the machine. Versions pre v20 such as commonly available v18 make it not possible for people to load an mcp

## 0.22.1

### Patch Changes

- a5d6df2: fix playground tool parameters not rendering on initial load and add horizontal scroll to responses
- 013d15d: Restore chat history loading in playground after v5 AI SDK upgrade
- 2667ecf: Fixed radix warning about Dialog.Content not having a Dialog.Title child.
- 90a3b7b: Allow instances.get to return mcp server representations of a toolset. Remove unneeded environment for instances get
- c8a0376: - fix SSE streaming response truncation due to chunk boundary misalignment
  - `addToolResult()` was called following tool execution, the AI SDK v5 wasn't automatically triggering a follow-up LLM request with the tool results. This is a known limitation with custom transports (vercel/ai#9178).
- 1a63676: Replace Shiki with Monaco Editor for viewing large inline specs
- e9988d8: Ensure stable QueryClient is used for lifetime of web app especially during
  development mode hot reloads.

## 0.22.0

### Minor Changes

- 1c836a2: Proxy remote file uploads through gram server
- c213781: Upgrade to AI SDK 5 and improve playground functionality
  - Upgraded to AI SDK 5 with new chat transport and message handling
  - Fixed keyboard shortcuts in playground chat input - Enter now properly submits messages (Shift+Enter for newlines)
  - Fixed TextArea component to properly accept and forward event handlers (onKeyDown, onCompositionStart, onCompositionEnd, onPaste)
  - Fixed AI SDK 5 compatibility by changing maxTokens to maxOutputTokens in CustomChatTransport
  - Fixed Button variant types in EditToolDialog (destructive-secondary, secondary)
  - Fixed Input component onChange handler to use value parameter directly
  - Fixed type mismatches between ToolsetEntry and Toolset in Playground component
  - Added missing Tool type import

### Patch Changes

- Updated dependencies [1c836a2]
  - @gram/client@0.22.0

## 0.21.1

### Patch Changes

- 59f21eb: fix: AddSourceDialog continue button not closing dialog when clicked
- 5f6d646: Allow uploading OpenAPI specs via remote url
- Updated dependencies [949787b]
  - @gram/client@0.21.6

## 0.21.0

### Minor Changes

- a041994: Introduces a new page for each source added to a users project. Source page provides details on the source, which toolsets its used and the abilty to attach an environment to a source.

### Patch Changes

- 4228c3e: Implements passthrough oauth support for function tools via oauthTarget indicator. Also simplifies the oauth proxy redirect for more recent usecases
- Updated dependencies [4228c3e]
  - @gram/client@0.21.2

## 0.20.1

### Patch Changes

- bc147e0: Updated dependencies to address dependabot security alerts
- c2ea282: admin view for creating oauth proxies
- Updated dependencies [c2ea282]
  - @gram/client@0.20.1

## 0.20.0

### Minor Changes

- 6716410: Add the ability to attach gram environments at the toolset level for easier configuration set up

### Patch Changes

- 6716410: restructure MCP authentication form to hide attach environments in advanced section
- e34b505: updating of openrouter key limits for chat based usage
- Updated dependencies [6716410]
  - @gram/client@0.19.0

## 0.19.5

### Patch Changes

- 6b04cc2: Updates playground chat models to a more modern list. Add Claude 4.5 Opus and ChatGPT 5.1

## 0.19.4

### Patch Changes

- 5396fd8: Update login page animation with interactive Gram function demo
  - Redesigned the login page animation from a sequential upload/generate flow to an interactive two-window demo
  - Replaced the generic Pet Store OpenAPI example with a real Gram function showcasing Supabase integration and UK property data querying
  - Added draggable, focusable windows to create a more engaging and realistic demonstration
  - Implemented progressive tool generation animation with reset functionality

## 0.19.3

### Patch Changes

- 8a92350: Fixes automatic closing behavior for Source Dialogs

## 0.19.2

### Patch Changes

- 44d4dca: Update dashboard to fix a few ui issues
- 0d4c7c8: Fix shiki theme in dark mode
- 3210d73: Add annoucement modal for Gram Functions
- 8bf8710: Introduces v2 of Dynamic Toolsets, combining learnings from Progressive and Semantic searches into one unified feature. Extremely token efficient, especially for medium and large toolsets.

## 0.19.1

### Patch Changes

- Updated dependencies [cf3e81b]
  - @gram/client@0.18.1

## 0.19.0

### Minor Changes

- c249bb0: Adds the ability to attach an environment to a source such that all tool calls originating from that source will have those environment variables apply

## 0.18.7

### Patch Changes

- 3552ff0: modifies gram auth so it respects current project context on the initial auth and sets that as defaultProjectSlug
- d9f4980: Fix onboarding steps to use `npm run` prefix

## 0.18.6

### Patch Changes

- 900d4cc: Adds the option to select/deselect all during tool management, for example when adding tools to a toolset
- 4b5a511: fix: logs page dialog content warning

## 0.18.5

### Patch Changes

- faef164: opens up logs to free tier
- 29aee79: fixes potentially duplicate env vars from functions in the UX and MCP config

## 0.18.4

### Patch Changes

- 10140df: Makes tool type filterable on more than just http tools (functions, custom)
- 77446ee: fully connects server url tracking feature in opt in tool call logs
- Updated dependencies [77446ee]
  - @gram/client@0.17.3

## 0.18.3

### Patch Changes

- ff7615f: Fixed a bug where the download link for function assets was incorrect on the Deployment page's Assets tab.
- bb37fed: creates the concept of user controllable product features, opens up logs to self-service enable/disable control
- Updated dependencies [bb37fed]
  - @gram/client@0.17.2

## 0.18.2

### Patch Changes

- 403a2c8: Fixes delete asset confirmation modal visual discrepancy and css fixes

## 0.18.1

### Patch Changes

- 9dd1b7a: Unify code block components

## 0.18.0

### Minor Changes

- 613f10e: Upgrade @speakeasy-api/moonshine to integrate bundle size reduction changes

## 0.17.8

### Patch Changes

- 192d6cb: temporarily clarify node version for functions
- 145295a: Changes default install method for Cursor MCPs to HTTP streaming
- 9963bbd: fix: multiple react versions in dev causes rules of hooks error

## 0.17.7

### Patch Changes

- f79fd52: Open dashboard from gram-build, better completing the flow starting from pnpm create

## 0.17.6

### Patch Changes

- 2db3a23: Add filtering support to the tool call logs table
- Updated dependencies [2db3a23]
  - @gram/client@0.16.7

## 0.17.5

### Patch Changes

- 8df9e59: Polish onboarding wizard with improved animations and code quality. Fixed memory leaks in WebGL particle effects, improved window trail particle density during fast movement, added scrollable content with blur gradients, and removed dead code.

## 0.17.4

### Patch Changes

- bab05ce: Adds support to the Playground for any tool type, notably enabling function tools to be used there
- Updated dependencies [7afda6e]
  - @gram/client@0.16.3

## 0.17.3

### Patch Changes

- 69e766a: Adds a page for viewing tool call logs from ClickHouse with a searchable table interface displaying tool call history and infinite scroll pagination with cursor-based navigation for efficient data loading.

## 0.17.2

### Patch Changes

- 4ae6852: Adds an icon to the mcpb installation method that will render in Claude Desktop alongside your tool calls
- Updated dependencies [5038166]
  - @gram/client@0.15.3

## 0.17.1

### Patch Changes

- 3c00725: Set of improvements for functions onboarding UX, including better support for mixed OpenAPI / Functions projects
- Updated dependencies [3c00725]
  - @gram/client@0.14.17

## 0.17.0

### Minor Changes

- aaad92f: Show Gram Functions on deployment pages

### Patch Changes

- 0b51c20: Add WebGL ASCII shader effects to onboarding wizard with interactive star particles
- d6f5579: Adds a basic toolset UX for managing resources in the system adding/subtracting them per toolset
- 321699e: Function-based tools can now be used in Custom Tools
- 2fb24e6: Adds UI hints for custom tools, indicating which "subtools" are missing (if any), or just surfacing the list of subtools otherwise. Begins tracking the required subtools more powerfully in order to support Gram Functions.
- Updated dependencies [d6f5579]
- Updated dependencies [2fb24e6]
  - @gram/client@0.14.16

## 0.16.0

### Minor Changes

- 7cd9b62: Rename packages in changelogs, git tags and github releases

### Patch Changes

- b6b4ed0: Better custom domain model ordering

## 0.15.1

### Patch Changes

- Updated dependencies [f3cea34]
  - @gram/client@0.14.14

## 0.15.0

### Minor Changes

- f3ffd00: Preserve redirect URLs during log-in for unauthenticated browsers.

### Patch Changes

- 73a7ffc: chore: Make tools dialog is wider, tool name prefixes are muted for easier legibility and mo tools found in search message has been improved for clarity

## 0.14.2

### Patch Changes

- 660c110: Support variations on any tool type. Allows the names of Custom Tools to now be edited along with all fields of Functions.
- Updated dependencies [660c110]
  - @gram/client@0.14.11

## 0.14.1

### Patch Changes

- b53cefb: Ensure all pages have proper bottom padding
- 64b8fc7: feat: Claude 4.5 Haiku available in playground model switcher

## 0.14.0

### Minor Changes

- 9df917a: Adds the ability for users of private servers to load the install page for easy user install of MCPs.

### Patch Changes

- f7a157d: Fix to set srcToolUrn when updating variations
- 9df917a: fix: update to use mcpb instead of dxt nomenclature for MCP installation pages

## 0.13.0

### Minor Changes

- 3cb955a: Dashboard support for the CLI authentication flow.

### Patch Changes

- 8148897: makes gram functions environments variables now account for in the MCP and gram environments UX
- 0f75503: adds a very basic few for displaying gram functions sources

## 0.12.0

### Minor Changes

- e956b16: feat: temperature slider in the playground
- fbdc9bd: feat: add @ symbol tool tagging syntax to playground
- 0e83d56: add new mcp configuration section for setting up install pages

### Patch Changes

- 90daf89: fix: prevent asset names from being cut off in deployments overview
- f312721: fix: only capture cmd-f in logs when logs section is focused
- Updated dependencies [8972d1d]
  - @gram/client@0.14.7

## 0.11.0

### Minor Changes

- 87136d0: Rename deployment fields for asset/tool count to prefix with openapiv3 and make room for new tool types/sources.

### Patch Changes

- 33cdfa7: Repairs errant release of install page by actually including assets
- 5a2214e: add GPT-5 to playground
- 0397ead: Enable cross-origin access to static assets

## 0.10.0

### Minor Changes

- 25b5d18: Migrate buttons from shadcn to design system component

## 0.9.3

### Patch Changes

- a1b3aaa: Revert to zod v3

## 0.9.2

### Patch Changes

- 72978ba: Standardize home page width
- acf6726: Expose the kind of prompt templates, and do not count higher order tools as prompts in the dashboard.

## 0.9.1

### Patch Changes

- d5e7b22: Avoid nil dereference in tool name callbacks used in ChatWindow

## 0.9.0

### Minor Changes

- d4dbddd: Manage versioning and changelog with [changesets](https://github.com/changesets/changesets)
