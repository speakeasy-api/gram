# MCP Logs Attribute Clickthrough

## Summary

Add click-to-filter on attribute rows in the MCP logs detail sheet. Clicking an attribute row opens a dropdown menu with filter actions (`=`, `!=`, `~`, Copy value). Filters are added to the existing `LogFilterBar` and persisted to the URL `?af=` param.

## Scope

- **In scope:** `Attributes` and `Resource` rows inside `LogDetailSheet` (both rendered via `AttributesSection`).
- **Out of scope:** Header metadata badges (Service, Trace ID, etc.), tool I/O content blocks, and text-search-within-payloads.

## UX Decisions

| Decision                                  | Choice                                           | Rationale                                                                                                                       |
| ----------------------------------------- | ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| Interaction model                         | Popover menu on row click                        | Surfaces `!=` (exclude) alongside `=` without separate UI; one extra click is cheap when user is already paused reading the row |
| Menu items                                | `=`, `!=`, `~` (contains), separator, Copy value | Covers the three advertised filter operators plus per-row copy (a gap today)                                                    |
| Non-filterable rows (null `"--"`, arrays) | Menu opens; filter items disabled, Copy enabled  | Teaches affordance boundary without hiding Copy                                                                                 |
| Sheet behavior after filter add           | Stays open                                       | User may stack multiple filters from the same log; list re-queries behind the sheet                                             |

## Architecture

### State ownership (unchanged)

`Logs.tsx` owns `logFilters: ActiveLogFilter[]`, persisted to URL param `af` via `serializeFilters`/`parseFilters` in `log-filter-url.ts`. No new state stores or contexts.

### New shared helper

Extract dedup logic from `LogFilterBar.addFilter` into a shared function in `log-filter-types.ts`:

```ts
export function applyFilterAdd(
  current: ActiveLogFilter[],
  next: { path: string; op: Op; value?: string },
): ActiveLogFilter[] {
  const rest =
    next.op === Operator.Eq || next.op === Operator.In
      ? current.filter((f) => !(f.path === next.path && f.op === next.op))
      : current;
  return [...rest, { id: crypto.randomUUID(), ...next }];
}
```

`LogFilterBar` is refactored to call this helper instead of its private inline logic.

### Prop flow

```
Logs.tsx                         // creates handleAddFilterFromLog callback
  -> LogDetailSheet              // new optional prop: onAddFilter
    -> LogDetailContent          // passes through
      -> AttributesSection (x2)  // wraps rows in DropdownMenu
```

`onAddFilter` is optional; when absent, rows render as today (static, no menu).

### Data shape enrichment

`flattenObject` return type changes from `[string, string][]` to `AttributeEntry[]`:

```ts
type AttributeEntry = {
  key: string; // dot-notation path (e.g. "http.response.status_code")
  displayValue: string; // UI display (may be "--" or JSON array literal)
  filterValue: string | null; // null = not filterable
};
```

Mapping rules:

- `null`/`undefined` -> `{ displayValue: "--", filterValue: null }`
- Array -> `{ displayValue: JSON.stringify(value), filterValue: null }`
- String/number/boolean -> `{ displayValue: String(value), filterValue: String(value) }`

### Menu component

Uses `@/components/ui/dropdown-menu` (shadcn/Radix). Each attribute row wraps in `<DropdownMenu>`:

```
+-------------------------------+
| Filter by  key = value        |   disabled if filterValue === null
| Exclude    key != value       |   disabled if filterValue === null
| Contains   "value"            |   disabled if filterValue === null
| ----------------------------- |
| Copy value                    |   always enabled
+-------------------------------+
```

Menu item labels truncate the echoed value at ~24 chars with ellipsis. The actual filter uses the full untruncated value.

## Edge Cases

- **eq replaces eq on same path** -- existing dedup rule, unchanged.
- **not_eq stacks** -- two `!=` on same path = AND exclusion. Correct.
- **Contradictory filter** (`= A` then `!= A`) -- both chips persist, query returns empty. User removes one to recover.
- **Arbitrary key paths** -- backend accepts free-form paths; no client-side validation needed.
- **Copy clipboard failure** -- fire-and-forget (matches existing pattern throughout LogDetailSheet).
- **Long values in filter chip** -- existing filter bar behavior; not this feature's concern.

## Accessibility

- Radix DropdownMenu provides keyboard navigation (Enter/Space to open, arrows, Esc, typeahead).
- Row trigger gets `aria-label={`Filter by ${key}`}` for screen readers.
- Focus returns to the row after menu dismissal (Radix default).

## Stacking/Portals

DropdownMenu content portals to body, stacks above the Sheet via Radix's internal z-index layering. No manual z-index needed (consistent with existing `QuerySamplesPopover` usage in the filter bar).

## Testing

### Unit tests (`log-filter-types.test.ts`)

Test `applyFilterAdd`:

- Append to empty list
- eq replaces eq on same path
- in replaces in on same path
- eq does not replace not_eq on same path
- not_eq stacks with not_eq
- contains stacks with contains
- Different paths never interfere

### Component tests (`LogDetailSheet.test.tsx`)

- Mixed attribute types (string, number, boolean, null, array) render expected rows
- Without `onAddFilter`: no menu appears on click
- With `onAddFilter`: click opens menu; each filter item calls with correct `(path, op, value)`
- Null/array rows: filter items disabled, Copy enabled
- Copy calls `navigator.clipboard.writeText` with full display value

### Manual verification

1. Hover shows pointer on attribute rows
2. Click opens dropdown at row position
3. Each filter action adds correct chip; URL `?af=` updates
4. List re-queries behind open sheet
5. Null and array rows have disabled filter items, working Copy
6. Existing LogFilterBar flows still work (no regression from applyFilterAdd refactor)
7. URL round-trip on refresh preserves chips

## Files to modify

| File                                                             | Change                                                                                                      |
| ---------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `client/dashboard/src/pages/logs/log-filter-types.ts`            | Add `applyFilterAdd` helper                                                                                 |
| `client/dashboard/src/pages/logs/LogFilterBar.tsx`               | Refactor `addFilter` to use `applyFilterAdd`                                                                |
| `client/dashboard/src/pages/logs/LogDetailSheet.tsx`             | Enrich `flattenObject` return type; add DropdownMenu to `AttributesSection` rows; accept `onAddFilter` prop |
| `client/dashboard/src/pages/logs/Logs.tsx`                       | Create `handleAddFilterFromLog`; pass to `LogDetailSheet`                                                   |
| `client/dashboard/src/pages/logs/log-filter-types.test.ts` (new) | Unit tests for `applyFilterAdd`                                                                             |
| `client/dashboard/src/pages/logs/LogDetailSheet.test.tsx` (new)  | Component tests for menu behavior                                                                           |

## Skills to activate during implementation

- `frontend` -- React frontend best practices
- `vercel-react-best-practices` -- React performance patterns
