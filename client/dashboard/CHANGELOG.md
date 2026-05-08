# dashboard

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
