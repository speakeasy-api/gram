# Component structure and reuse

**The core rule: every UI pattern that appears in more than two places must be centralized so it can be changed in a single location.**

## Check `components/` before writing anything

Before writing any JSX for a UI element, check `client/dashboard/src/components/` for an existing component. This includes layout wrappers, table headers, empty states, filter pill groups, search inputs, badges, cards — anything. Reuse what exists. Never create a one-off `<div className="...">` when a named component already exists for that purpose.

If no component exists and you expect the pattern to appear in more than a few places across the app, **create one** in `client/dashboard/src/components/` before using it. Name it for what it _is_, not where it happens to appear first (e.g., `PageTabsTrigger`, not `SourceDetailTabTrigger`).

## No duplicated className strings

If the same Tailwind className string for a coherent visual pattern (a card shell, a header row, a pill) appears on 3+ elements, extract it to the options below. Short incidental utilities such as `flex items-center gap-2` stay local.

- A component's built-in styling
- A `cva` variant
- A named `const` used in `cn()`

The symptom to watch for: copy-pasting a whole `className` prop that defines how a thing looks.

## No duplicated JSX blocks

If you find yourself copy-pasting a JSX structure — even with minor variations — stop and extract a parameterized component. Three near-identical blocks is the threshold.

## No IIFEs in JSX

Never use immediately-invoked function expressions inside JSX (`{(() => { ... })()} `). Extract to a named sub-component or a variable above the return statement.

## No multi-line or nested ternaries

A ternary that wraps onto multiple lines, or chains a second `?:` inside a branch, is unreadable. Reach for one of these instead:

- A `switch` statement when mapping a discriminator (e.g. a `kind` / `type` string) to one of several values.
- A small **named helper function**, hoisted to module scope when the mapping is pure (e.g. converting a frontend enum to a backend constant). The helper labels the intent at the call site and the JSX stays flat.
- An object lookup when the keys are statically known and the values are simple constants.

```tsx
// ❌ wrong — nested multi-line ternary
attachmentType={
  sourceKind === "function"
    ? "functions"
    : sourceKind === "externalmcp" || sourceKind === "remotemcp"
      ? "external_mcp"
      : "openapi"
}

// ✅ right — hoisted helper with a switch
function attachmentTypeForSourceKind(sourceKind: string | undefined): string {
  switch (sourceKind) {
    case "function":
      return "functions";
    case "externalmcp":
    case "remotemcp":
      return "external_mcp";
    default:
      return "openapi";
  }
}

attachmentType={attachmentTypeForSourceKind(sourceKind)}
```

Single-line, single-branch ternaries (`isOpen ? "x" : "y"`) are fine.

## Keep components focused

A component that has grown past ~150 lines of JSX is doing too much. Break it up. If a page has multiple tabs, each tab's content is its own component.

## Page headers and subtext are a common duplication trap

Many pages render the same `<h1>` + `<p>` header block in 2–3 conditional render paths (loading skeleton, empty state, populated state). Remaining examples: `InsightsTools.tsx`, `InsightsAgents.tsx`. Symptoms: a copy change touches the same string in 3 places; `Edit` with `replace_all` fails because indentation differs between the duplicates.

When adding or editing page headers, lift the title and subtitle into one small header component or a shared shell, not into each render branch. `LoggingPageHeader` (used by `LogsTools.tsx` and `LogsAgents.tsx`) and `RiskOverviewShell` in `SecurityOverview.tsx` are the reference. Do not name it `PageHeader`: that name is taken by the compound in `components/page-header.tsx`. When editing existing duplicated copy, target a unique trailing fragment of the string (e.g. `"in chat messages."`) so a single `replace_all` covers every copy regardless of indentation — and file a follow-up to extract a shared header.

## Shared empty states: props with defaults, not forks

When the same empty-state component is reused across pages but needs different copy per caller (e.g. `HooksEmptyState` rendered from both `InsightsTools.tsx` and `LogsTools.tsx`), add optional `title` / `subtitle` props with sensible defaults rather than forking the component:

```tsx
export function HooksEmptyState({
  title = "No logs captured",
  subtitle = "Install Observability plugin in your AI agent to start capturing tool execution logs",
}: { title?: string; subtitle?: string } = {}) {
  /* … */
}
```

Backwards-compatible callers stay `<HooksEmptyState />`; only the variant caller passes overrides. Avoids divergent copies of the surrounding scaffolding (provider cards, setup dialogs, etc.).
