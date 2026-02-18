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

If no component exists and you expect the pattern to appear in more than a few places across the app, **create one** in `client/dashboard/src/components/` before using it. Name it for what it *is*, not where it happens to appear first (e.g., `PageTabsTrigger`, not `SourceDetailTabTrigger`).

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

### Styling and Design System

- **ALWAYS use Moonshine design system utilities** from `@speakeasy-api/moonshine` instead of hardcoded Tailwind color values
- **NEVER use hardcoded Tailwind colors** like `bg-neutral-100`, `border-gray-200`, `text-gray-500`, etc.
