# MCP public-visibility warning for system variables

## Problem

When a user toggles an MCP server to **Public**, any variable currently marked `state: "system"` on the toolset's attached environment will be injected server-side into every public caller's requests. In effect, those secrets become shared credentials for everyone using the public MCP URL. Today the UI does not surface this risk before the visibility change is applied.

Concretely, in `client/dashboard/src/pages/mcp/MCPDetails.tsx`, the `MCPStatusDropdown.handleSelect` helper only triggers a confirmation dialog when the transition involves `disabled` (line 472). Direct **private → public** fires the mutation immediately, with no user prompt.

## Goal

Before the visibility change is applied, show a modal listing the system-provided variables that will be exposed to all public callers, and require explicit confirmation to proceed. Offer a one-click escape hatch to the environment's detail page so the user can revoke or reconfigure the variables before publishing.

## Triggers

The warning modal MUST appear on **any transition whose target is `public`** when the toolset's attached environment contains ≥1 variable — required or custom — that resolves to `state: "system"` with a value present:

- `private` → `public`
- `disabled` → `public`

If no system-provided variables exist, the current behavior is preserved:

- `private` → `public` fires immediately without confirmation.
- `disabled` → `public` shows the existing `ServerEnableDialog` unchanged.

## User-facing design

The dialog is styled to the Speakeasy brand voice and typography system (the dashboard already self-hosts the licensed fonts — see `client/dashboard/src/App.css:8–49`). Marketing-only brand elements — the full RGB gradient bar, ASCII pattern illustrations — are intentionally omitted; they don't scale to a product-UI dialog and would clash with the moonshine component system.

**Brand accent:** A 2px top border inside the dialog in Swift red `#C83228` — the first stop of the brand's "Product / Features" gradient. Signals "product warning" without pulling in the full 9-color bar.

**Title (Tobias Thin, ~24px, -4% tracking, ends with period):**
"Share system secrets with public callers."

**Body (ABC Diatype Light, 14–15px, short declarative sentences, brand voice):**

> Anyone with this URL will call with values from **{environment name}**. System values are shared. Treat them as team credentials, not user credentials.

**Variable list:**

- Header label in ABC Diatype Mono 11px uppercase, muted grey (`#8B8684`): `USED BY EVERY PUBLIC CALLER`.
- Variable keys in ABC Diatype Mono Light, one per line, left-aligned.
- List scrolls vertically inside the modal if more than ~8 entries.
- No values shown — names only.

**Quick link (ABC Diatype Light, 13px, underlined on hover):** "Review in {environment name} →" → `/environments/{environmentSlug}`, opens in a new tab. Dismissing is the user's choice; the modal stays open so they can tab back and confirm.

**Actions (moonshine `Button`):**

- `Cancel` (tertiary) — closes the modal, no mutation.
- `Make public anyway.` (destructive-primary, period on the label for voice consistency) — fires the visibility mutation and closes the modal.

**Font tokens (already defined in the dashboard):**

- Display: `"Tobias", serif` via `@font-face` at `App.css:36`
- Body: `"Diatype", sans-serif` via `App.css:8`
- Mono: `"Diatype Mono", monospace` via `App.css:22`

## Implementation plan

### New helper

Add `getSystemProvidedVariables(envVars: EnvironmentVariable[], attachedEnvironmentSlug: string): string[]` to `client/dashboard/src/pages/mcp/environmentVariableUtils.ts`. It consumes the output of `useEnvironmentVariables()` (so required and custom vars are handled uniformly) and returns the keys of variables where `state === "system"` and `environmentHasValue(envVar, attachedEnvironmentSlug)` is true.

This extraction is small and keeps the new dialog from reimplementing state logic that will drift from the table view.

### New dialog component

Create `client/dashboard/src/components/public-mcp-warning-dialog.tsx`, patterned after `server-enable-dialog.tsx`. Props:

```ts
interface PublicMcpWarningDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  isLoading?: boolean;
  environmentName: string;
  environmentSlug: string;
  variableNames: string[]; // non-empty when dialog is open
}
```

The dialog is only rendered when `variableNames.length > 0`; callers check the helper's output before opening it. Uses the same `Dialog` primitive as `ServerEnableDialog`.

### Wire-up in `MCPStatusDropdown`

In `client/dashboard/src/pages/mcp/MCPDetails.tsx`:

1. Pull in the attached environment + entries (the component already has `toolset` and can call the hook / query used by `MCPEnvironmentSettings`).
2. Compute `systemVars = getSystemProvidedVariables(...)` memoized on the toolset/env inputs.
3. Extend `handleSelect`:
   - If `status === "public"` and `systemVars.length > 0`, open the new `PublicMcpWarningDialog` and stop.
   - If `status === "public"` and `systemVars.length === 0`, keep today's behavior (no dialog on private→public; existing `ServerEnableDialog` on disabled→public).
   - All non-public transitions unchanged.
4. When the warning dialog confirms, call `applyStatus("public")`.

### Disabled → public composition

When the target is `public` **and** currently disabled **and** there are system vars, we need both confirmations. The simpler composition: show the system-variables warning **first**; on confirm, immediately open the existing `ServerEnableDialog`. This keeps each dialog focused on one risk (security vs. billing/enablement) and preserves the existing billing-gate logic (`hasAdditionalIncludedServers`) without duplicating it into the new dialog.

### Telemetry

Extend the existing `mcp_event` capture in `applyStatus` so `mcp_made_public` includes a boolean `system_vars_warned: true|false` — useful to observe how often users hit the warning vs. proceed.

## Risk / out-of-scope

- Does not change any server-side behavior. Values are still injected as before; we only add a speed bump in the UI.
- Does not re-warn if the user re-publishes later without having changed any system vars. The warning fires on every transition-to-public, by design — this is a cheap check and the risk is the same every time.
- No changes to the environments page itself; the "Review" link points at an existing route.

## Testing

- Vitest: unit-test `getSystemProvidedVariables` with toolsets that have (a) no env, (b) env with no system vars, (c) env with mixed system + user vars.
- Vitest: render `PublicMcpWarningDialog` with a representative variable list; assert names render, link points to the right slug, confirm/cancel fire callbacks.
- Manual: in `mise start:server --dev-single-process`, create a toolset with an environment containing a STRIPE_API_KEY-like system var, switch to public, verify the warning appears; switch back to private, then to public again — warning still appears.
