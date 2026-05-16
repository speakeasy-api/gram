---
name: frontend
description: Rules and best practices when working on the dashboard and elements React frontend codebases
metadata:
  relevant_files:
    - "client/dashboard/**"
    - "elements/**"
---

## React & Frontend Coding Guidelines

### General Guidelines

- Use the `pnpm` package manager
- When interacting with the server, prefer the `@gram/sdk` package (sourced from workspace at `./client/sdk`)
- The document `client/sdk/REACT_QUERY.md` is very helpful for understanding how to use React Query hooks that come with the SDK.
- For data fetching and server state, use `@tanstack/react-query` instead of manual `useEffect`/`useState` patterns
- When invalidating React Query caches after mutations, invalidate ALL relevant query keys — not just the most specific one. Different hooks may use different query key prefixes for the same data (e.g., `queryKeyInstance` vs `toolsets.getBySlug`). Use broad invalidation helpers like `invalidateAllToolset(queryClient)` to ensure all consumers refresh.

### Component Structure and Reuse

**The core rule: every UI pattern that appears in more than two places must be centralized so it can be changed in a single location.**

#### Check `components/` before writing anything

Before writing any JSX for a UI element, check `client/dashboard/src/components/` for an existing component. This includes layout wrappers, table headers, empty states, filter pill groups, search inputs, badges, cards — anything. Reuse what exists. Never create a one-off `<div className="...">` when a named component already exists for that purpose.

If no component exists and you expect the pattern to appear in more than a few places across the app, **create one** in `client/dashboard/src/components/` before using it. Name it for what it _is_, not where it happens to appear first (e.g., `PageTabsTrigger`, not `SourceDetailTabTrigger`).

#### No duplicated className strings

If the same Tailwind className string (or any meaningful substring of one) appears on 3+ elements anywhere in the codebase, extract it to:

- A component's built-in styling
- A `cva` variant
- A named `const` used in `cn()`

The symptom to watch for: copy-pasting a `className` prop. That is always wrong.

#### No duplicated JSX blocks

If you find yourself copy-pasting a JSX structure — even with minor variations — stop and extract a parameterized component. Three near-identical blocks is the threshold.

#### No IIFEs in JSX

Never use immediately-invoked function expressions inside JSX (`{(() => { ... })()} `). Extract to a named sub-component or a variable above the return statement.

#### Keep components focused

A component that has grown past ~150 lines of JSX is doing too much. Break it up. If a page has multiple tabs, each tab's content is its own component.

### React Performance Patterns

These patterns were established in the audit log (#2140) and deployment log (#2167) redesigns. Apply them whenever building search, filtering, or keyboard navigation.

#### Hoist RegExp creation

Never create `new RegExp()` inside a render callback (e.g., `highlightMatch`). Extract it to a `useMemo` keyed on the search query:

```typescript
const searchRegex = useMemo(() => {
  if (!searchQuery) return null;
  const escaped = searchQuery.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(${escaped})`, "gi");
}, [searchQuery]);
```

Then use `searchRegex` inside `useCallback`-wrapped functions.

#### Defer expensive search computations

Wrap search queries with `useDeferredValue` before passing them to expensive `useMemo` computations (e.g., filtering all logs). This keeps the input responsive while React defers the downstream recomputation:

```typescript
const deferredSearchQuery = useDeferredValue(searchQuery);
const filteredIndices = useMemo(() => { /* expensive filter */ }, [deferredSearchQuery, ...]);
```

#### Derive state during render, not via useEffect

If a value can be computed from current state, derive it inline — don't sync it via `useEffect`. This prevents a flash of stale values between renders:

```typescript
// DO: derive during render
const effectiveSearchIndex =
  searchMatchIndices.length > 0
    ? Math.min(currentSearchIndex, searchMatchIndices.length - 1)
    : 0;

// DON'T: clamp via useEffect (causes stale render flash)
useEffect(() => {
  if (currentSearchIndex >= searchMatchIndices.length) setCurrentSearchIndex(0);
}, [searchMatchIndices.length]);
```

#### Reset navigation state on data changes

When a component has keyboard navigation (j/k/g/G) with a `currentIndex` state, reset it when the underlying data changes (filters, pagination, data refresh):

```typescript
useEffect(() => {
  setCurrentLogIndex(null);
}, [logs]); // or parsedLogs, depending on the component
```

### Tooltip Usage

`App.tsx` wraps the entire app in a global `TooltipProvider`. **Never add another `TooltipProvider` inside a component** — doing so creates a redundant Radix context per instance and contributes to `ResizeObserver loop completed with undelivered notifications` errors in the browser.

Use `<Tooltip>`, `<TooltipTrigger>`, and `<TooltipContent>` directly — they inherit the global provider automatically. For simple cases use the existing `<SimpleTooltip tooltip="...">` wrapper from `@/components/ui/tooltip`.

```tsx
// ✅ correct
<Tooltip>
  <TooltipTrigger asChild>{button}</TooltipTrigger>
  <TooltipContent>Hello</TooltipContent>
</Tooltip>

// ❌ wrong — TooltipProvider already exists at the app root
<TooltipProvider>
  <Tooltip>
    <TooltipTrigger asChild>{button}</TooltipTrigger>
    <TooltipContent>Hello</TooltipContent>
  </Tooltip>
</TooltipProvider>
```

### Styling and Design System

- **ALWAYS use Moonshine design system utilities** from `@speakeasy-api/moonshine` instead of hardcoded Tailwind color values
- **NEVER use hardcoded Tailwind colors** like `bg-neutral-100`, `border-gray-200`, `text-gray-500`, etc.
- `@tailwindcss/typography` must remain in `devDependencies` — the dashboard uses `prose` and `not-prose` classes directly (e.g. `CatalogDetail.tsx`, `tool.tsx`) which are provided by this plugin.

### Release Stage Badges (Preview / Beta)

Pre-GA features get a `Preview` or `Beta` badge wherever the user would otherwise mistake the feature for being GA. The same `ReleaseStageBadge` component renders on every surface, so labels never drift.

**Source of truth:** `client/dashboard/src/components/release-stage-badge.tsx` — exports `ReleaseStageBadge` and the `ReleaseStage = "preview" | "beta"` type.

**Sibling component:** `ProductTierBadge` (the `Enterprise` pill on the Billing nav entry). `ReleaseStageBadge` deliberately mirrors its shape (`rounded-sm`, `text-xs`, `px-1 py-0.5`, no border, title-case label, default font weight). If you change one, change both — they need to read as one badge family in the sidebar.

**Semantic palettes** (don't repaint without product/design buy-in):

- `preview` — early / experimental, shape may change. Moonshine `warning` palette: `bg-warning-softest text-default-warning` (amber).
- `beta` — stable enough for production but still evolving. Moonshine `information` palette: `bg-information-softest text-default-information` (Speakeasy brand blue — the same family that backs `--feature`).

**Never hardcode Tailwind colors** (no `bg-violet-500`, no `dark:` overrides). Both palettes are Moonshine semantic tokens and will retune automatically if brand/theme changes.

#### Surface 1 — sidebar nav (route-driven)

Set `stage` on the route declaration. `app-sidebar.tsx` forwards `item.stage` through `ScopeGatedNavItem → NavButton`, which renders the badge with a hover tooltip explaining what the stage means. The badge auto-hides in collapsed-icon mode.

```tsx
// client/dashboard/src/routes.tsx
assistants: {
  title: "Assistants",
  url: "assistants",
  icon: "bot",
  stage: "preview", // ← sidebar pill appears automatically
  component: AssistantsRoot,
},
```

> **Gotcha**: if you ever introduce another sidebar wrapper that calls `NavButton` directly (instead of going through `NavMenuButton`), you must forward `stage={item.stage}` explicitly. `app-sidebar.tsx`'s `ScopeGatedNavItem` does this — copy that pattern.

#### Surface 2 — page section title

Pass `stage` on the **primary** `Page.Section.Title` for the page (usually the first `Page.Section` under `Page.Body`). Don't put it on secondary section titles like "Recent Chats" — the badge labels the whole feature, not individual sections.

```tsx
<Page.Section>
  <Page.Section.Title stage="beta">Risk Overview</Page.Section.Title>
  <Page.Section.Description>…</Page.Section.Description>
</Page.Section>
```

> **Gotcha**: pages with multiple render branches (loading / empty / populated) must include the title — and therefore the `stage` — in **every** branch. `PolicyCenter.tsx` and `SecurityOverview.tsx` are the reference for this pattern.

#### Surface 3 — tab nav (sub-route tabs)

For `ObserveTabNav`-style tab strips, add `stage` to the local tab descriptor and render `<ReleaseStageBadge size="xs" noTooltip>` inline. Use `inline-flex items-center gap-2` on the tab `<Link>` so the badge tracks the label without disrupting the active-tab underline.

```tsx
// client/dashboard/src/components/observe/ObserveTabNav.tsx
type Tab = { label: string; href: string; stage?: ReleaseStage };
const tabs: Tab[] = [
  { label: "Employees", href: `${baseSlug}/employees`, stage: "preview" },
];
```

#### Surface 4 — raw `<h1>` headings (pages that don't use Page.Section.Title)

A handful of pages render their own `<h1 className="text-xl font-semibold">…</h1>` instead of `Page.Section.Title` (e.g., `InsightsEmployees`, `InsightsAgents`). Wrap the heading and the badge in a `flex items-center gap-2` div:

```tsx
<div className="flex items-center gap-2">
  <h1 className="text-xl font-semibold">AI Agent Costs</h1>
  <ReleaseStageBadge stage="preview" />
</div>
```

> Consider migrating these pages to `Page.Section.Title` in a follow-up — but don't bundle that refactor with the badge addition.

#### Which surfaces does a given feature need?

- **Default**: every surface where the user encounters the feature's name. If it has a sidebar entry **and** a page heading, badge both.
- **Tab-only feature**: badge the tab. The parent route's nav entry stays clean since the parent isn't itself pre-GA.
- **Hidden behind a feature flag**: still badge the visible surfaces. The flag controls visibility; the badge communicates stage to users who can see it.

#### Removing a badge (feature ships GA)

Grep `stage="preview"` and `stage="beta"` and delete every match:

- `routes.tsx` — remove the `stage:` field on the route entry
- `Page.Section.Title stage="…"` — drop the prop
- `ObserveTabNav` (or similar) tab descriptors — drop the `stage` field
- Inline `<ReleaseStageBadge>` usages — delete the element and unwrap the `flex items-center gap-2` div

There's no other cleanup. The component itself stays in place for the next pre-GA feature.
