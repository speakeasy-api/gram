# Dashboard Row Context Menus Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every table row, card, and list entry with per-entry actions gets the same actions on right-click.

**Architecture:** A new `TableRowContextMenu` (the `asChild` sibling of `CardContextMenu`) wraps row elements in a Radix context menu fed by the same `Action[]` as the visible "⋯" menu. Raw `DropdownMenu` kebabs are refactored to build one `Action[]` and render the dropdown from it, so both menus share a single source of truth. Moonshine `<Table>` pages use the new moonshine `renderRow` prop (see companion plan `2026-07-14-moonshine-render-row.md`).

**Tech Stack:** React 19 + TypeScript, Radix (`@radix-ui/react-context-menu` via `src/components/ui/context-menu.tsx`), moonshine, vitest.

## Global Constraints

- Repo worktree: `/Users/sagar/go/src/gram/.claude/worktrees/table-context-menu`, branch `feat/table-row-context-menus`. All paths below relative to `client/dashboard/`.
- Commands (from repo root): `pnpm -F dashboard type-check`, `pnpm -F dashboard test` (verify exact script name in `client/dashboard/package.json` before first use), `pnpm -F dashboard build`.
- The shared `Action` type is `import type { Action } from "@/components/ui/more-actions"` — `{ icon?: IconName; label: string; onClick: () => void; disabled?: boolean; destructive?: boolean }`.
- Do not change what actions do; only where they can be triggered from. Existing confirm dialogs stay.
- Permission gating must match the kebab: where a `RequireScope` hides/disables the kebab, the context menu gets an empty `actions` array (renders unwrapped). Use `RequireScope`'s render-prop form (`{({ disabled }) => ...}`, as in `src/pages/access/RolesTab.tsx:50`) or the same `useRBAC().hasAnyScope([...])` check the file already uses.
- Tasks 2–9 do NOT depend on the moonshine release. Tasks 10–12 use `renderRow` and type-check only against the local moonshine build (see Task 13).
- Commit after each task with a `feat(dashboard): ...` conventional message.

### Out of scope (deliberate, discovered during extraction)

- `Sources.tsx` grid cards — `SourceCard` already uses `CardContextMenu`.
- `Triggers.tsx` — no per-row menu exists (row-click edit + a logs link); adding one would invent actions.
- Team invites table — actions are two visible buttons (Resend/Revoke), not hidden options.
- User session rows (SessionRow/SessionTableRow) — already hand-wired before this work; distinct from the remote-identity SessionsTab in Task 3.

---

### Task 1: TableRowContextMenu + shared menu content

**Files:**

- Modify: `src/components/card-context-menu.tsx` (extract shared content)
- Create: `src/components/table-row-context-menu.tsx`
- Test: `src/components/table-row-context-menu.test.tsx`

**Interfaces:**

- Produces: `ActionContextMenuContent({ actions }: { actions: Action[] })` exported from `card-context-menu.tsx`; `TableRowContextMenu({ actions, children }: { actions: Action[]; children: React.ReactElement })`.

- [ ] **Step 1:** In `card-context-menu.tsx`, extract the `<ContextMenuContent>` block (lines 41–55) into an exported component in the same file, and use it in `CardContextMenu`:

```tsx
export function ActionContextMenuContent({
  actions,
}: {
  actions: Action[];
}): React.JSX.Element {
  return (
    <ContextMenuContent className="min-w-[10rem]">
      {actions.map((action, index) => (
        <ContextMenuItem
          key={index}
          disabled={action.disabled}
          variant={action.destructive ? "destructive" : "default"}
          onSelect={() => action.onClick()}
        >
          {action.label}
          {action.icon && (
            <Icon name={action.icon} className="size-3 shrink-0" />
          )}
        </ContextMenuItem>
      ))}
    </ContextMenuContent>
  );
}
```

- [ ] **Step 2:** Create `src/components/table-row-context-menu.tsx`:

```tsx
import { ContextMenu, ContextMenuTrigger } from "./ui/context-menu";
import { ActionContextMenuContent } from "./card-context-menu";
import type { Action } from "./ui/more-actions";

/**
 * Row variant of CardContextMenu. Wraps a single row element (`<tr>`, list
 * row, row button) in a right-click menu of the same `Action[]` the row
 * feeds its visible "⋯" menu. Uses `asChild`, so `children` must be one
 * element that forwards refs and props (DotRow, moonshine Table rows via
 * `renderRow`, native elements). Renders children unwrapped when `actions`
 * is empty.
 */
export function TableRowContextMenu({
  actions,
  children,
}: {
  actions: Action[];
  children: React.ReactElement;
}): React.JSX.Element {
  if (actions.length === 0) {
    return <>{children}</>;
  }

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ActionContextMenuContent actions={actions} />
    </ContextMenu>
  );
}
```

- [ ] **Step 3:** Write `src/components/table-row-context-menu.test.tsx` following the mocking idiom of `card-context-menu.test.tsx` (mock `./ui/context-menu` so `ContextMenuItem` renders a button firing `onSelect`; mock moonshine `Icon`): three cases — renders an item per action and invokes `onClick`; destructive action gets `variant="destructive"`; empty `actions` renders the child element unwrapped with no menu items.
- [ ] **Step 4:** Run the two test files + `pnpm -F dashboard type-check` → PASS. Verify `CardContextMenu`'s existing tests still pass after the extraction.
- [ ] **Step 5:** Commit `feat(dashboard): add TableRowContextMenu and shared action menu content`.

### Task 2: Sources table rows

**Files:** Modify `src/components/sources/SourceTableRow.tsx`

- [ ] Wrap the returned `<DotRow>` (lines 127–193) with the existing `actions` array (built at lines 62–98):

```tsx
return (
  <TableRowContextMenu actions={actions}>
    <DotRow /* existing props unchanged */>
      {/* existing children unchanged */}
    </DotRow>
  </TableRowContextMenu>
);
```

`DotRow` forwards refs/props (built for this). `actions` is already `[]` for remotemcp/tunneledmcp, so those rows no-op. Type-check, then commit `feat(dashboard): right-click menu on sources table rows`.

### Task 3: Remote-identity tabs (3 files)

**Files:** Modify `src/pages/remote-identity-providers/tabs/client/McpServersTab.tsx`, `.../client/SessionsTab.tsx`, `.../issuer/ClientsTab.tsx`

Each renders `DotRow`s with a raw `DropdownMenu` kebab inside `RequireScope scope="org:admin" level="section"`. For each row, inside the map:

- [ ] Build the `Action[]` (per file):

```tsx
// McpServersTab
const actions: Action[] = [
  {
    label: "Remove from server",
    destructive: true,
    disabled: remove.isPending,
    onClick: () =>
      remove.mutate({
        request: {
          removeClientFromMcpServerRequestBody: {
            clientId,
            mcpServerId: server.id,
          },
        },
      }),
  },
];

// SessionsTab
const actions: Action[] = [
  ...(session.hasRefreshToken
    ? [
        {
          label: "Refresh now",
          disabled: refresh.isPending,
          onClick: () => refresh.mutate({ request: { id: session.id } }),
        },
      ]
    : []),
  {
    label: "Revoke session",
    destructive: true,
    onClick: () => revoke.mutate({ request: { id: session.id } }),
  },
];

// ClientsTab
const actions: Action[] = [
  {
    label: "Delete client",
    destructive: true,
    onClick: () => setDeleteTarget(item),
  },
];
```

- [ ] Render the existing `DropdownMenuContent` items by mapping over `actions` (keep the current trigger button; `disabled`/`destructive` map to the item props/classes already used in the file).
- [ ] Wrap each `<DotRow>` in `<TableRowContextMenu actions={canManage ? actions : []}>` where `canManage` comes from the same RBAC check the section `RequireScope` enforces (use `useRBAC().hasAnyScope(["org:admin"])`, matching `RolesTab.tsx`'s `canManageRoles`).
- [ ] Type-check; commit `feat(dashboard): right-click menus on remote identity provider tabs`.

### Task 4: Tool list rows

**Files:** Modify `src/components/tool-list/ToolList.tsx`

- [ ] In `ToolRow`, wrap the row `<div className="group border-neutral-softest hover:bg-muted relative flex items-center justify-between ...">` (lines 447–499) with `<TableRowContextMenu actions={readOnly ? [] : actions}>` using the existing `actions` (lines 384–438). Check the second `actions` array around line 946 — if it backs another `MoreActions` row (group-level), wrap that row the same way.
- [ ] Type-check; commit `feat(dashboard): right-click menu on tool list rows`.

### Task 5: Source tool rows

**Files:** Modify `src/pages/sources/SourceToolActions.tsx` + its parent row component (find with `grep -rn "SourceToolActions" client/dashboard/src`)

- [ ] Refactor `SourceToolActions` into a hook + thin component: export `useSourceToolActions(props)` returning `{ actions, dialog }` (the `Action[]` from lines 152–178 and the edit `Dialog` element); keep `SourceToolActions` rendering `<><MoreActions actions={actions} />{dialog}</>` via the hook.
- [ ] In the parent, call the hook once, wrap its row element in `<TableRowContextMenu actions={actions}>`, render `<MoreActions actions={actions} />` where `SourceToolActions` sat, and render `{dialog}` beside the row.
- [ ] Type-check; commit `feat(dashboard): right-click menu on source tool rows`.

### Task 6: Chat log rows

**Files:** Modify `src/pages/chatLogs/ChatLogsTable.tsx`

- [ ] Wrap each row `<button key={chat.id} ...>` (lines 215–323), moving the `key` to the wrapper:

```tsx
<TableRowContextMenu
  key={chat.id}
  actions={[
    {
      label: "Delete chat",
      destructive: true,
      onClick: () => setDeleteConfirmId(chat.id),
    },
  ]}
>
  <button onClick={() => onSelectChat(chat)} /* rest unchanged */>...</button>
</TableRowContextMenu>
```

- [ ] Type-check; commit `feat(dashboard): right-click delete on chat log rows`.

### Task 7: Org home project cards and rows

**Files:** Modify `src/pages/org/OrgHome.tsx`

- [ ] Extract the four dropdown items in `ProjectRowActions` (lines 723–753: toggle favorite, project settings, view audit logs, copy slug) into a hook in the same file, `useProjectActions(project, { isFavorite, onToggleFavorite })`, returning `Action[]` (moonshine icon names: `"star"`, `"settings"`, `"history"`, `"copy"`); `ProjectRowActions` keeps its trigger and renders its `DropdownMenuItem`s by mapping over the array (drop `closeAnd` by using `DropdownMenuItem onSelect` semantics or keep the open-state close in the map).
- [ ] In `ProjectCard` (outer div at line 617) and `ProjectRow` (line 545), call the hook and wrap the outer element in `<CardContextMenu actions={actions}>` (cards are div layouts — the existing `CardContextMenu` div wrapper is fine here, as in `SourceCard`).
- [ ] Type-check; commit `feat(dashboard): right-click menus on project cards`.

### Task 8: Plugin cards

**Files:** Modify `src/pages/plugins/PluginCard.tsx`

- [ ] Build an `Action[]`: GitHub installation (disabled unless `installTarget`; opens `InstallInstructionsDialog` via the existing `setTimeout` handler), Download as zip — Claude / Cursor / Codex (`handleDownload(...)`), View (`navigate(detailHref)`). Keep the visible Install split-button dropdown exactly as is (it's a CTA with a separator; do not restructure it).
- [ ] Wrap the card's outer `<div>` (line 64) in `<CardContextMenu actions={actions}>`.
- [ ] Type-check; commit `feat(dashboard): right-click menu on plugin cards`.

### Task 9: Roles rows (CSS-grid table)

**Files:** Modify `src/pages/access/RolesTab.tsx`

- [ ] In `RoleRow`, wrap the row's root element with `TableRowContextMenu` (asChild lands on the root div directly — no extra DOM, subgrid layout intact):

```tsx
const actions: Action[] = canManageRoles
  ? [
      { label: "Edit", onClick: onEdit },
      ...(!role.isSystem
        ? [{ label: "Delete", destructive: true as const, onClick: onDelete }]
        : []),
    ]
  : [];

return (
  <TableRowContextMenu actions={actions}>
    <div /* existing root row element, unchanged */>...</div>
  </TableRowContextMenu>
);
```

`canManageRoles` already exists in the file (`hasAnyScope(["org:admin"])`); thread it into `RoleRow` if not already a prop. Keep `RoleActionsMenu` unchanged (its `setTimeout`-deferred `onEdit`/`onDelete` stay for the dropdown; direct calls are fine from the context menu).

- [ ] Type-check; commit `feat(dashboard): right-click menu on role rows`.

### Task 10: Flat moonshine tables — Deployments, Exclusions, Policy Center

**Files:** Modify `src/pages/deployments/Deployments.tsx`, `src/pages/security/ExclusionsTab.tsx`, `src/pages/security/PolicyCenter.tsx`
**Depends on:** local moonshine build with `renderRow` (Task 13 loop).

- [ ] **Deployments:** extract the redeploy logic from `DeploymentActionsDropdown` into `useDeploymentActions(deployment, latest): Action[]` (returns `[]` when `!latest && !isCompletedDeployment`; single entry `{ icon: "refresh-cw", label: buttonText, disabled: redeployMutation.isPending, onClick: handleRedeploy }`). `DeploymentActionsDropdown` renders from it; add a small `DeploymentRowContextMenu({ deployment, latest, children })` component (hooks need a component, not the `renderRow` closure) that gates on `RequireScope scope="project:write" level="component"` render-prop and renders `TableRowContextMenu`. Wire:

```tsx
<Table<DeploymentSummary>
  /* existing props */
  renderRow={(row, rowElement) => (
    <DeploymentRowContextMenu
      key={row.id}
      deployment={row}
      latest={deployments[0] === row}
    >
      {rowElement}
    </DeploymentRowContextMenu>
  )}
/>
```

- [ ] **ExclusionsTab:** define `const exclusionActions = (exclusion: Exclusion): Action[] => [{ label: "Edit", onClick: () => onSheetChange({ mode: "edit", exclusion }) }, { label: "Delete", destructive: true, onClick: () => deleteMutation.mutate({ request: { id: exclusion.id } }) }]` in the component; render `ExclusionActionsMenu`'s two items from it (keep its `setTimeout` deferral for the dropdown) and add `renderRow={(exclusion, el) => <TableRowContextMenu actions={exclusionActions(exclusion)}>{el}</TableRowContextMenu>}` to the `<Table>`.
- [ ] **PolicyCenter:** same shape — `const policyActions = (row: PolicyRow): Action[] => [Edit, ...(row.kind === "risk" ? [View Progress] : []), Delete(destructive)]` using the exact handlers from lines 878–905; dropdown maps over it; `renderRow` wraps with `TableRowContextMenu`.
- [ ] Type-check against linked moonshine; commit `feat(dashboard): right-click menus on deployments, exclusions, and policy tables`.

### Task 11: Shadow MCP inventory (composed Table.Body)

**Files:** Modify `src/components/shadow-mcp/ShadowMCPInventoryTable.tsx`
**Depends on:** moonshine `renderRow` on `Table.Body`.

- [ ] Extract the item state machine from `InventoryActionMenu` (lines 290–327) into `inventoryActions(server, { disabled, onOpenAction }): Action[]`: `hasRequest → [Review Request]`; `!hasRequest && !hasAllowDecision → [Add Allow Rule]`; `hasAllowDecision → [Edit Rule, Delete Rule (destructive)]`; every entry `disabled` when the mutation is pending, `onClick` keeping the existing `window.setTimeout(() => onOpenAction(mode, server), 0)` deferral.
- [ ] `InventoryActionMenu` renders its `DropdownMenuContent` by mapping over the same array; add to `<Table.Body ... renderRow={(row, el) => <TableRowContextMenu actions={inventoryActions(row, { disabled: isActionPending, onOpenAction: (mode, server) => setActiveAction({ mode, server }) })}>{el}</TableRowContextMenu>} />`.
- [ ] Type-check; commit `feat(dashboard): right-click menu on shadow MCP inventory rows`.

### Task 12: Team members (complex, hand-wired)

**Files:** Modify `src/pages/team/Team.tsx`
**Depends on:** moonshine `renderRow`.

- [ ] Extract the menu _model_ from the members actions column (lines 493–583) into `useMemberMenuModel(member)` in the same file, returning the booleans and callbacks both renderers need: `{ scimManaged, accessMember, showChallenges: isRbacEnabled, canRemove: !isSelf && !isLastAdmin, openManageRoles, openChallenges, openRemove }`.
- [ ] The dropdown render fn consumes the model (same JSX, conditions now from the model). Add a `MemberRowContextMenu({ member, children })` component rendering `ContextMenu`/`ContextMenuTrigger asChild`/`ContextMenuContent` with the mirrored items: Manage roles (disabled when `scimManaged`, wrapped in `RequireScope scope="org:admin" level="component"` when not), View challenges (when `isRbacEnabled`), separator + destructive Remove member (when `canRemove`, scope-gated). Import `ContextMenuSeparator` from `@/components/ui/context-menu`. If no item is renderable, return children unwrapped.
- [ ] Wire `renderRow={(row, el) => <MemberRowContextMenu key={row.userId} member={row}>{el}</MemberRowContextMenu>}` on the members `<Table>` only (invites table out of scope).
- [ ] Type-check; commit `feat(dashboard): right-click menu on team member rows`.

### Task 13: Verification + PR

- [ ] Link the local moonshine build for tasks 10–12: in the moonshine worktree run `pnpm build`, then temporarily point the dashboard's `@speakeasy-api/moonshine` at it (`pnpm link` or a local `file:` override). Do NOT commit link/lockfile artifacts; revert after verification.
- [ ] Run `pnpm -F dashboard type-check`, the dashboard test suite, and `pnpm -F dashboard build`.
- [ ] Push `feat/table-row-context-menus`; open a **draft** PR noting: "Tasks 10–12 require the moonshine `renderRow` release (companion PR in speakeasy-api/moonshine); bump `@speakeasy-api/moonshine` here once it ships, then CI goes green."
