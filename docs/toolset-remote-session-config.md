# Handover: RSI/RSC configuration for toolset-based MCP servers

Branch: `walker/toolset-remote-session-config` (off `origin/main`, uncommitted working tree).
Status: implementation complete, all checks green, manual browser verification done except one edge (see "Remaining work").

## Goal

Bring the remote-MCP-server identity-provider configuration (user session issuer + remote session issuer/client, the "sheet" flow) to toolset/catalog-based MCP servers on the old toolset detail page. Motivation: USI/RSI/RSC is the target auth direction for all server kinds, and today a toolset that gets a USI wired via the one-shot `WireUserSessionIssuerModal` has no UI to tweak or manage it afterwards.

## Agreed design decisions (from spec interview, 2026-07-08)

1. Frontend-only, single PR. Backend already type-agnostic: the serve path gates on `toolsets.user_session_issuer_id`, and when it is set the issuer gate wins and the legacy OAuth chain is skipped entirely (`server/internal/mcp/impl.go`, ~line 642, `runInToolsetGate`).
2. Shared components parameterized by an `AuthTarget` interface (state-context composition pattern). Remote target links via `mcpServers.update` (keeps its visibility-private side effect); toolset target links via `toolsets.setUserSessionIssuer` and touches nothing else (no `mcpEnabled`/`mcpIsPublic` changes).
3. State matrix on the toolset Authentication tab, USI-first tiebreak:
   - Flag off: today's UI, byte-for-byte (`legacy-only`).
   - USI wired: shared manage surface only, even if inert legacy config remains (`manage`).
   - Legacy OAuth configured, unwired: legacy UI plus a convert path (`legacy`). Proxy/gram paradigms convert via the existing wire modal; external OAuth converts via the attach sheet with the issuer URL prefilled from the stored RFC 8414 metadata.
   - Nothing configured: shared attach surface only, legacy wizard unreachable (`attach`).
4. No full USI unlink anywhere. Once wired, the link is permanent; only the RSI/RSC layer is tweakable (add/edit/remove identity providers).
5. Convert leaves legacy config in the DB inert (wire-modal precedent). No `removeOAuthServer` call.
6. Whole matrix gated behind the existing `ONBOARD_EXTERNAL_MCP_TO_USER_SESSIONS_FLAG` PostHog flag. In local dev, `devTelemetry` returns true for every flag, so the new surfaces always show locally.

## What changed (all in `client/dashboard/src/pages/mcp/`)

New files:

- `x/tabs/settings/sections/authentication/authTarget.ts`: `AuthTarget` type plus `useMcpServerAuthTarget` and `useToolsetAuthTarget` hooks. Each supplies `slug`, `userSessionIssuerId`, optional `remoteMcpServerId` (probe seed, undefined for toolsets so the RFC 9728 probe stays idle), `linkUserSessionIssuer`, and `invalidate`.
- `toolsetAuthSurface.ts`: pure matrix logic. `getOAuthParadigm` (moved out of MCPEnvironmentSettings), `toolsetAuthSurface`, `toolsetConvertAction`, `externalOauthIssuerUrl`. Note: `toolsetConvertAction` uses `case null` because oxlint enforces switch exhaustiveness.
- `toolsetAuthSurface.test.ts`: 13 unit tests, all passing.
- `ToolsetAuthenticationSection.tsx`: toolset-page chrome. `ToolsetAuthenticationSection` renders a PageSection "Authentication" wrapping `AuthenticationSectionBody`, plus a PageSection "User sessions" wrapping `UserSessionsList` when wired. `ConvertToUserSessionsButton` is the external-OAuth convert entry (opens the attach sheet with `userSessionIssuer={null}` and all project issuers selectable).

Modified files:

- `x/tabs/settings/sections/authentication/AttachRemoteIdentityProviderSheet.tsx`: prop `mcpServer: McpServer` replaced by `target: AuthTarget`. Slug seeding uses `target.slug`, step 4 first-add link is `target.linkUserSessionIssuer(issuerId)`, and target-specific invalidations go through `target.invalidate(queryClient)` alongside the common USI/RSI/RSC ones.
- `x/tabs/settings/sections/authentication/AuthenticationSection.tsx`: split into chrome-free exported `AuthenticationSectionBody({ target })` (all queries, fields, and the attach/modify/delete overlays) and the thin `AuthenticationSection({ mcpServer })` wrapper that keeps the SettingsSection chrome and sessions panel. Remote page output unchanged. Gotcha: body converts `target.userSessionIssuerId` (string | null) to undefined for the SDK hooks.
- `x/tabs/settings/sections/authentication/McpServerSessionsPanel.tsx`: inner list extracted as exported chrome-free `UserSessionsList({ issuerId })`; the panel wrapper is unchanged.
- `MCPDetails.tsx`: `MCPStatusDropdown` now blocks the public→private flip while legacy OAuth is configured unwired. Backend (`UpdateToolset`, `server/internal/toolsets/impl.go` ~line 413) silently clears external OAuth / OAuth proxy config on any `mcp_is_public` flip; the new `mustConvertOAuthBeforePrivate` guard (in `toolsetAuthSurface.ts`, unit tested) shows a blocking inform-only dialog ("The existing OAuth configuration must be converted to a session issuer before this MCP server can be made private") instead of applying the update. Flag-gated like the rest of the matrix (flag off keeps today's silent clear); once a USI is wired the flip is allowed since leftover config is inert. Guards on `toolset.mcpIsPublic`, not the dropdown's current status, so a disabled-but-still-public server is covered too.
- `MCPEnvironmentSettings.tsx`: `OAuthSection` is now a small dispatcher (computes paradigm + surface, routes to `ToolsetAuthenticationSection` or legacy). The old body is renamed `LegacyOAuthSection` and takes `convertAction: ToolsetConvertAction | null`; `showWireUserSessionIssuer` is now just `convertAction === "wire-modal"`, and an `attach-sheet` convertAction renders `ConvertToUserSessionsButton` next to it. Everything else in the legacy component (modals, status display, badges) is untouched.

Untouched on purpose: `WireUserSessionIssuerModal` and its machine, `ModifyRemoteIdentityProviderSheet`, `DeleteRemoteIdentityProviderDialog` (both already target-agnostic, keyed by USI/RSI ids only), all backend code.

## Verification done

- `pnpm -F dashboard type-check`, `pnpm -F dashboard lint`: green.
- `pnpm -F dashboard test toolsetAuthSurface`: 13/13 pass. The full suite has 53 failures in 7 files, but the identical failures exist on clean main (verified by stashing and rerunning), so they are pre-existing.
- Browser against local stack (dashboard at https://localhost:5173, note HTTPS):
  - Remote server regression: `/mcp/x/r-linear-9e27/settings#authentication` renders exactly as before (session duration, issuer row with Edit/Delete, user sessions panel).
  - Manage surface: `/mcp/linear#authentication` (toolset with USI wired) shows Authentication + User sessions sections, attach sheet opens.
  - Attach surface: `/mcp/jamf#authentication` (unconfigured toolset) shows the "No authentication configured" empty state with Configure Manually (Use Discovered disabled since no probe), no sessions section, no legacy wizard.
  - No console errors.

## Remaining work

1. Not manually verified: the `legacy` surface (legacy OAuth configured, unwired) with its convert buttons. No local toolset has an OAuth proxy or external OAuth configured. Options: seed one locally for a visual check, or rely on unit tests plus the untouched wire modal. Walker had not chosen when this doc was written.
2. Commit + PR (use the `pr` skill; remember the `env -u GH_TOKEN -u GITHUB_TOKEN` prefix for gh writes). Suggested demo: pr-demo-gif of the toolset attach flow.
3. Possible reviewer question: `ConvertToUserSessionsButton` mounts its own `useRemoteSessionIssuers()`; fine per React Query dedup.

## Related context

- Memory file: `project_toolset_rsi_rsc_sheet.md` in the project memory dir holds the same decisions in short form.
- Related future work: remove-oauth-proxy spec (`docs/remove-oauth-proxy-for-user-session-issuer.md`) eventually kills the legacy wizard; AGE-1902 unifies toolset servers into `/mcp/x`, at which point the same `AuthenticationSectionBody` remounts there.
