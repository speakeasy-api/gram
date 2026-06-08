# Right-click card context menus — design spec

**Date:** 2026-06-07
**Status:** Approved direction; spec under review
**Scope:** Dashboard frontend (`client/dashboard`) only. No backend changes.

## Summary

Add right-click (context-menu) support to cards in the dashboard. Right-clicking a card
opens a menu of that card's actions — the **same** actions already shown by the card's
visible `⋯` (`MoreActions`) button. Right-click is an _enhancement_ layered on the
existing visible affordance, never the only path to an action.

v1 wires every card that already defines an actions array — `PluginCard`, `SourceCard`,
`EnvironmentCard`, `AssistantCard`, `CustomToolCard`, `PromptTemplateCard`, and
`ResourceCard` — and ships a reusable `<CardContextMenu>` primitive so any other card can
opt in later (one line) once its actions are defined. The right-click menu honors the same
RBAC gating as the card's `⋯` (scope-gated actions are excluded from both).

> Scope note: the initial draft scoped v1 to just `PluginCard`/`SourceCard`. It was widened
> during implementation to cover all cards that already had an action menu, since that's the
> same rule ("cards with existing actions") with no new product decisions required.

## Decisions captured during brainstorming

| Topic             | Decision                                                                                                                                                                                                             |
| ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Card scope (v1)   | Every card that already defines a `MoreActions` actions array — Plugin, Source, Environment, Assistant, Custom Tool, Prompt, Resource — plus a reusable primitive. No new actions invented for cards that lack them. |
| Affordance model  | A single `Action[]` drives BOTH the visible `⋯` menu and the right-click menu, so they never drift. The visible `⋯` stays (discoverability / touch).                                                                 |
| Out of scope (v1) | MCP / Collections / Catalog cards (they have no actions today — adopting them is a later, per-card product decision). Backend. New action types.                                                                     |

## Current state

- `@radix-ui/react-context-menu@2.2.16` is already installed (transitive dep). No
  context-menu UI wrapper exists yet, and there is **no** existing `onContextMenu` /
  context-menu usage anywhere in the app.
- A shared action menu already exists: `components/ui/more-actions.tsx` exports
  `MoreActions` and the `Action` type:
  ```ts
  export type Action = {
    icon?: IconName; // Moonshine IconName
    label: string;
    onClick: () => void;
    disabled?: boolean;
    destructive?: boolean;
  };
  ```
  `MoreActions` renders each action as a `DropdownMenuItem`, wrapping `onClick` with
  `e.stopPropagation(); e.preventDefault();` so a menu action never triggers the card's
  own click handler.
- Cards already using `MoreActions` with an `Action[]`:
  - `pages/plugins/PluginCard.tsx` — actions: **Delete** (destructive). Card root has an
    `onClick` that navigates to the plugin detail.
  - `components/sources/SourceCard.tsx` — actions: **View asset**, **Change document**,
    **View deployment** (conditional), **Delete** (destructive). Card navigates on click.

## Architecture

```
Action[] (existing type, ui/more-actions.tsx)
        │
        ├──────────────► <MoreActions actions={actions} />        // existing visible ⋯ button
        │
        └──────────────► <CardContextMenu actions={actions}>      // NEW right-click wrapper
                              {card markup}
                          </CardContextMenu>
```

Both consumers read the **same** `actions` reference defined once in the card, so the
visible menu and the right-click menu are always identical.

### Component 1 — `components/ui/context-menu.tsx` (NEW)

A shadcn-style thin wrapper around `@radix-ui/react-context-menu`, mirroring the existing
`components/ui/dropdown-menu.tsx` (styling, `data-slot` attributes, `cn` usage). Exports the
subset we need:

- `ContextMenu` (Root)
- `ContextMenuTrigger`
- `ContextMenuContent` (portalled, same surface styling as `DropdownMenuContent`)
- `ContextMenuItem` (same item styling as `DropdownMenuItem`, incl. `disabled`)
- `ContextMenuSeparator` (exported for completeness; not required by v1)

This is a primitive: no app logic, no tests (consistent with `dropdown-menu.tsx`, which has
none).

### Component 2 — `components/card-context-menu.tsx` (NEW)

```tsx
export function CardContextMenu({
  actions,
  children,
  className,
}: {
  actions: Action[]; // reuse Action from ui/more-actions
  children: React.ReactNode;
  className?: string; // applied to the trigger wrapper
}): React.ReactNode;
```

Behavior:

- If `actions.length === 0`, render `children` unwrapped (no context menu, no extra DOM).
- Otherwise wrap `children` in:
  ```
  <ContextMenu>
    <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
    <ContextMenuContent>
      {actions.map(action => (
        <ContextMenuItem
          disabled={action.disabled}
          onSelect={() => action.onClick()}   // Radix onSelect; no need to stopPropagation
          className={destructive styling matching MoreActions}
        >
          {action.label}{action.icon && <Icon .../>}
        </ContextMenuItem>
      ))}
    </ContextMenuContent>
  </ContextMenu>
  ```
- Destructive items get the same red treatment string `MoreActions` uses
  (`text-destructive hover:bg-destructive! hover:text-background!`).
- Icon rendering mirrors `MoreActions` (trailing `Icon name={action.icon}`).

> **Click isolation:** Radix `ContextMenu` opens on `contextmenu` (right-click) and does
> not fire the card's `onClick`. `ContextMenuItem`'s `onSelect` fires on selection only.
> Unlike `MoreActions` (a left-click dropdown nested inside a clickable card, which needs
> `stopPropagation`), the context menu doesn't need manual propagation guards — but each
> `action.onClick` is the card's existing handler, already safe to call directly.

### Wiring (v1)

- **PluginCard:** ensure the `Delete` action array is a named `const actions: Action[]`
  (it is effectively inline today). Pass it to the existing `<MoreActions actions={actions}/>`
  AND wrap the card's outer element with `<CardContextMenu actions={actions}>`.
- **SourceCard:** it already builds a `const actions` for `MoreActions`. Wrap the card root
  with `<CardContextMenu actions={actions}>` using the same const.

No change to what the actions do, their gating, or the visible `⋯`.

## Edge cases

- **Right-clicking inner links/buttons:** the whole card subtree is the trigger, so
  right-click anywhere on the card opens the card menu and suppresses the native browser
  context menu within the card. This is intended.
- **Empty/zero actions:** `CardContextMenu` renders children unwrapped (guard above) — no
  dangling trigger, no behavior change.
- **Touch / no mouse:** right-click doesn't exist; the visible `⋯` remains the path. No
  regression.
- **Keyboard:** Radix handles the context-menu key / Shift+F10 and focus management.

## Testing

- `context-menu.tsx`: none (thin primitive, matches `dropdown-menu.tsx`).
- `card-context-menu.test.tsx` (vitest + @testing-library/react):
  - renders one `ContextMenuItem` per action (labels present) when opened;
  - selecting an item invokes that action's `onClick`;
  - `actions={[]}` renders children with no context-menu wrapper.
  - Radix-in-happy-dom note: if dispatching the real `contextmenu` event doesn't open the
    Radix menu in happy-dom, stub the `@radix-ui/react-context-menu` family (or the local
    `ui/context-menu` wrapper) the way `sidebar-user-menu.test.tsx` stubs the dropdown
    family, and assert on the rendered items + `onClick`.
- Manual QA: right-click a Plugin card and a Source card → menu matches the `⋯`; Delete
  works and does not navigate; native browser menu is replaced on the card; left-click on
  the card still navigates.

## Verification

- `pnpm -F dashboard lint` (eslint + oxfmt), `type-check`, `test`, `build` all pass.
- No hardcoded colors (use Moonshine tokens / existing destructive classes); no nested
  `TooltipProvider`; reuse the existing `Action` type rather than redefining it.

## Out of scope

- MCP, Collections, and Catalog cards (need actions designed first).
- Any new card actions, bulk selection, or submenus.
- Backend / generated code / migrations.
