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

#### Page headers and subtext are a common duplication trap

Many pages render the same `<h1>` + `<p>` header block in 2–3 conditional render paths (loading skeleton, empty state, populated state). Examples observed: `InsightsTools.tsx`, `LogsTools.tsx`, `LogsAgents.tsx`, `SecurityOverview.tsx`, `PolicyCenter.tsx`. Symptoms: a copy change touches the same string in 3 places; `Edit` with `replace_all` fails because indentation differs between the duplicates.

When adding or editing page headers, lift the title and subtitle into a small `<PageHeader title="…" subtitle="…" />` (or pass them as props to a shared shell), not into each render branch. When editing existing duplicated copy, target a unique trailing fragment of the string (e.g. `"in chat messages."`) so a single `replace_all` covers every copy regardless of indentation — and file a follow-up to extract a shared header.

#### Shared empty states: props with defaults, not forks

When the same empty-state component is reused across pages but needs different copy per caller (e.g. `HooksEmptyState` rendered from both `/insights/tools` and `/logs/tools`), add optional `title` / `subtitle` props with sensible defaults rather than forking the component:

```tsx
export function HooksEmptyState({
  title = "No logs captured",
  subtitle = "Install Observability plugin in your AI agent to start capturing tool execution logs",
}: { title?: string; subtitle?: string } = {}) {
  /* … */
}
```

Backwards-compatible callers stay `<HooksEmptyState />`; only the variant caller passes overrides. Avoids divergent copies of the surrounding scaffolding (provider cards, setup dialogs, etc.).

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

### Navigation and Links

Use the right primitive for the link type — mixing them causes full-page reloads, broken multi-tenancy, or missing security headers.

**Internal navigation (any URL inside the dashboard):** Use the route helpers from `client/dashboard/src/routes.tsx`. Top-level routes _and_ subpages expose `.Link`, `.href()`, and `.goTo()`:

```tsx
// Wrap a child node with .Link
<routes.sources.Link>
  <Button>Connect a Source</Button>
</routes.sources.Link>

// Subpages get .Link too — use it instead of building strings
<routes.insights.tools.Link>
  <Button>Track AI usage</Button>
</routes.insights.tools.Link>

// Plain react-router Link with .href() when you need a className or are inside <p>
<Link to={routes.plugins.href()} className="underline underline-offset-2">
  Observability plugin
</Link>
```

Never hardcode org/project slugs in an `href` (e.g. `https://app.getgram.ai/speakeasy-team/projects/default/plugins`). The route helpers resolve the current `:orgSlug` / `:projectSlug` from the URL, so the same call works for every tenant.

**External links (anywhere outside the dashboard):** Use a plain `<a>` with `target="_blank"` and `rel="noopener noreferrer"`. This matches the existing pattern (`AddServerDialog.tsx:1162`, `CatalogDetail.tsx:229`) and the security attributes are mandatory — `noopener` blocks `window.opener` access; `noreferrer` strips the Referer header.

```tsx
<a
  href="https://www.speakeasy.com/product/mcp-gateway/catalog"
  target="_blank"
  rel="noopener noreferrer"
  className="underline underline-offset-2 hover:text-foreground"
>
  MCP Registry
</a>
```

The `@/components/ui/link` wrapper sets `target="_blank"` when `external` is true but also injects an icon — fine for nav rows, too heavy for inline links inside subtext. Reach for the plain `<a>` for inline external links.

### Editing copy

- **Preserve dynamic tokens.** Page subtext often interpolates state like `{rangeLabel}`, `{periodUsage.credits}`, or `{projectName}`. When rewording copy that contains a token, keep the token in place — replace it with the literal current value (e.g. "the last 30 days") only when the data fetch itself is locked to that value. Otherwise the copy starts lying as soon as the user changes a filter.
- **Don't fight the Tailwind class sorter.** Prettier's `prettier-plugin-tailwindcss` reorders classes on save. Write classes in any order; the formatter will normalize them and the diff stays clean across the codebase.
- **AI context strings shadow user-visible names.** When renaming a chart or card (e.g. "Most Used LLM Clients" → "Most Used Agents"), search for the old name in nearby `contextInfo=` / `suggestions=` props passed to `ExploreWithAI` / `InsightsConfig`. Those strings are sent to the LLM as analytical context; if they drift from the visible label, the AI assistant talks about a card the user can't see.

### Styling and Design System

- **ALWAYS use Moonshine design system utilities** from `@speakeasy-api/moonshine` instead of hardcoded Tailwind color values
- **NEVER use hardcoded Tailwind colors** like `bg-neutral-100`, `border-gray-200`, `text-gray-500`, etc.
- `@tailwindcss/typography` must remain in `devDependencies` — the dashboard uses `prose` and `not-prose` classes directly (e.g. `CatalogDetail.tsx`, `tool.tsx`) which are provided by this plugin.

### Release Stage Badges (Preview / Beta)

Pre-GA features get a `Preview` or `Beta` badge wherever the user would otherwise mistake the feature for being GA. The same `ReleaseStageBadge` component renders on every surface, so labels never drift.

**Source of truth:** `client/dashboard/src/components/release-stage-badge.tsx` — exports `ReleaseStageBadge` and the `ReleaseStage = "preview" | "beta"` type.

**Underlying primitive:** Moonshine's `<Badge>` component (`@speakeasy-api/moonshine`). `ReleaseStageBadge` composes Moonshine's Badge with `background` enabled — this is the source of truth for shape (mono, uppercase, tracked, bordered, `rounded-xs`, `h-5`, `text-[12px]`). Do **not** override these classes; the design system owns them. The wrapper just picks a semantic variant and adds a tooltip.

**Variant → stage mapping** (variant names are hooks, not literal semantics):

- `preview` → Moonshine `warning` variant (amber).
- `beta` → Moonshine `information` variant (Speakeasy brand blue).

> Moonshine's badge variants (`neutral | destructive | information | success | warning`) are tuned for alert/feedback contexts, but the names are just hooks — `warning` here means "experimental, use with caution," not "alert." That's the intended way to reuse the palettes; don't invent new variants without design buy-in.

**Never hardcode Tailwind colors** (no `bg-violet-500`, no raw `bg-warning-softest` spans). If you find yourself reaching for raw classes for a new badge use case, that's a signal to either pick an existing Moonshine variant or add one upstream.

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
