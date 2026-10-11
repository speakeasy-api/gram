# Data fetching and React performance

## Telemetry & observability data fetching

Observability surfaces — Tool Logs, insights/analytics, summary cards, counts, and filter dropdowns — are served from **pre-aggregated ClickHouse summary views** behind endpoints like `telemetry.getToolUsageSummary`, `telemetry.listToolUsageTraces`, and `telemetry.query`. **Default to these summary-backed endpoints; they're fast and are the right choice for almost every view.**

Per-log detail is the rare exception. Free-text search over log bodies, arbitrary custom-attribute filters (e.g. `@user.region`), and inspecting an individual log/trace fall back to scanning the raw `telemetry_logs` table, which is slow. Only reach for those when detailed telemetry is genuinely what the user needs — not for default lists, summaries, or filter options.

When you add a control that triggers the raw-log path (a free-text search box, a custom-attribute filter), tell the user it may be slower, with a notice shown only while such a filter is active — and keep the structured filters (server, user, agent, type, date) on the fast summary path. Don't build a default-on view whose first paint requires a raw-log scan.

## React performance patterns

These patterns were established in the audit log (#2140) and deployment log (#2167) redesigns. Apply them whenever building search, filtering, or keyboard navigation.

### Hoist RegExp creation

Never create `new RegExp()` inside a render callback (e.g., `highlightMatch`). Extract it to a `useMemo` keyed on the search query:

```typescript
const searchRegex = useMemo(() => {
  if (!searchQuery) return null;
  const escaped = searchQuery.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(${escaped})`, "gi");
}, [searchQuery]);
```

Then use `searchRegex` inside `useCallback`-wrapped functions.

### Defer expensive search computations

Wrap search queries with `useDeferredValue` before passing them to expensive `useMemo` computations (e.g., filtering all logs). This keeps the input responsive while React defers the downstream recomputation:

```typescript
const deferredSearchQuery = useDeferredValue(searchQuery);
const filteredIndices = useMemo(() => { /* expensive filter */ }, [deferredSearchQuery, ...]);
```

### Derive state during render, not via useEffect

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

### Reset navigation state on data changes

When a component has keyboard navigation (j/k/g/G) with a `currentIndex` state, reset it when the underlying data changes (filters, pagination, data refresh):

```typescript
useEffect(() => {
  setCurrentLogIndex(null);
}, [logs]); // or parsedLogs, depending on the component
```
