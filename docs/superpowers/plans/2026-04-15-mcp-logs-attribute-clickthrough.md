# MCP Logs Attribute Clickthrough Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users click attribute rows in the MCP logs detail sheet to add filters (`=`, `!=`, `~`) to the filter bar, or copy the value.

**Architecture:** Extract shared `applyFilterAdd` helper from `LogFilterBar` dedup logic. Enrich `flattenObject` return type to carry filterability. Add `DropdownMenu` from `@speakeasy-api/moonshine` to each `AttributesSection` row. Prop-drill `onAddFilter` callback from `Logs.tsx` through `LogDetailSheet`.

**Tech Stack:** React, TypeScript, `@speakeasy-api/moonshine` (DropdownMenu), Vitest, `@testing-library/react`

**Skills to activate:** `frontend`, `vercel-react-best-practices`

---

### Task 1: Extract `applyFilterAdd` helper

**Files:**

- Modify: `client/dashboard/src/pages/logs/log-filter-types.ts`
- Modify: `client/dashboard/src/pages/logs/LogFilterBar.tsx:124-139`
- Create: `client/dashboard/src/pages/logs/log-filter-types.test.ts`

- [ ] **Step 1: Write failing tests for `applyFilterAdd`**

Create `client/dashboard/src/pages/logs/log-filter-types.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { Operator } from "@gram/client/models/components/logfilter";
import type { ActiveLogFilter } from "./log-filter-types";
import { applyFilterAdd } from "./log-filter-types";

function makeFilter(
  overrides: Partial<ActiveLogFilter> & { path: string; op: Operator },
): ActiveLogFilter {
  return { id: "test-id", ...overrides };
}

describe("applyFilterAdd", () => {
  it("appends to empty list", () => {
    const result = applyFilterAdd([], {
      path: "http.status",
      op: Operator.Eq,
      value: "200",
    });
    expect(result).toHaveLength(1);
    expect(result[0].path).toBe("http.status");
    expect(result[0].op).toBe(Operator.Eq);
    expect(result[0].value).toBe("200");
    expect(result[0].id).toBeDefined();
  });

  it("replaces eq with eq on same path", () => {
    const existing = [makeFilter({ path: "x", op: Operator.Eq, value: "1" })];
    const result = applyFilterAdd(existing, {
      path: "x",
      op: Operator.Eq,
      value: "2",
    });
    expect(result).toHaveLength(1);
    expect(result[0].value).toBe("2");
  });

  it("replaces in with in on same path", () => {
    const existing = [makeFilter({ path: "x", op: Operator.In, value: "a,b" })];
    const result = applyFilterAdd(existing, {
      path: "x",
      op: Operator.In,
      value: "c,d",
    });
    expect(result).toHaveLength(1);
    expect(result[0].value).toBe("c,d");
  });

  it("does not replace not_eq when adding eq on same path", () => {
    const existing = [
      makeFilter({ path: "x", op: Operator.NotEq, value: "1" }),
    ];
    const result = applyFilterAdd(existing, {
      path: "x",
      op: Operator.Eq,
      value: "2",
    });
    expect(result).toHaveLength(2);
  });

  it("stacks not_eq on same path", () => {
    const existing = [
      makeFilter({ path: "x", op: Operator.NotEq, value: "A" }),
    ];
    const result = applyFilterAdd(existing, {
      path: "x",
      op: Operator.NotEq,
      value: "B",
    });
    expect(result).toHaveLength(2);
  });

  it("stacks contains on same path", () => {
    const existing = [
      makeFilter({ path: "x", op: Operator.Contains, value: "foo" }),
    ];
    const result = applyFilterAdd(existing, {
      path: "x",
      op: Operator.Contains,
      value: "bar",
    });
    expect(result).toHaveLength(2);
  });

  it("does not interfere across different paths", () => {
    const existing = [makeFilter({ path: "x", op: Operator.Eq, value: "1" })];
    const result = applyFilterAdd(existing, {
      path: "y",
      op: Operator.Eq,
      value: "1",
    });
    expect(result).toHaveLength(2);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd client/dashboard && pnpm vitest run src/pages/logs/log-filter-types.test.ts`
Expected: FAIL — `applyFilterAdd` is not exported from `log-filter-types.ts`

- [ ] **Step 3: Add `applyFilterAdd` to `log-filter-types.ts`**

Add at the bottom of `client/dashboard/src/pages/logs/log-filter-types.ts`:

```ts
/**
 * Add a filter to a list, applying dedup rules:
 * - For eq/in: replaces any existing filter on the same path+op
 * - For not_eq/contains: appends (stacking is valid)
 */
export function applyFilterAdd(
  current: ActiveLogFilter[],
  next: { path: string; op: Op; value?: string },
): ActiveLogFilter[] {
  const rest =
    next.op === Operator.Eq || next.op === Operator.In
      ? current.filter((f) => !(f.path === next.path && f.op === next.op))
      : current;
  return [
    ...rest,
    {
      id: crypto.randomUUID(),
      path: next.path,
      op: next.op,
      value: next.value,
    },
  ];
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd client/dashboard && pnpm vitest run src/pages/logs/log-filter-types.test.ts`
Expected: All 7 tests PASS

- [ ] **Step 5: Refactor `LogFilterBar` to use the shared helper**

In `client/dashboard/src/pages/logs/LogFilterBar.tsx`, update the import at line 20-23:

```ts
import {
  type ActiveLogFilter,
  OP_LABELS,
  applyFilterAdd,
  parseOperatorSymbol,
  tryParseFilterExpression,
} from "./log-filter-types";
```

Replace the `addFilter` callback (lines 124-139) with:

```ts
const addFilter = useCallback(
  (path: string, op: Op, value?: string) => {
    onChange(applyFilterAdd(filters, { path, op, value }));
    onSearchInputChange("");
    resetFlow();
  },
  [filters, onChange, onSearchInputChange, resetFlow],
);
```

- [ ] **Step 6: Type-check**

Run: `cd client/dashboard && pnpm tsc --noEmit`
Expected: No errors

- [ ] **Step 7: Commit**

```bash
git add client/dashboard/src/pages/logs/log-filter-types.ts \
       client/dashboard/src/pages/logs/log-filter-types.test.ts \
       client/dashboard/src/pages/logs/LogFilterBar.tsx
git commit -m "refactor: extract applyFilterAdd helper from LogFilterBar"
```

---

### Task 2: Enrich `flattenObject` return type

**Files:**

- Modify: `client/dashboard/src/pages/logs/LogDetailSheet.tsx:319-399`

- [ ] **Step 1: Define `AttributeEntry` type and update `flattenObject`**

In `client/dashboard/src/pages/logs/LogDetailSheet.tsx`, replace the `flattenObject` function (lines 362-399) and add the type above it:

```ts
interface AttributeEntry {
  key: string;
  displayValue: string;
  filterValue: string | null;
}

/**
 * Flatten a nested object into dot-notation keys with filterability metadata.
 * e.g. { http: { request: { method: "POST" } } } =>
 *   [{ key: "http.request.method", displayValue: "POST", filterValue: "POST" }]
 */
function flattenObject(
  obj: Record<string, unknown>,
  prefix = "",
): AttributeEntry[] {
  const result: AttributeEntry[] = [];

  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;

    if (value === null || value === undefined) {
      result.push({ key: fullKey, displayValue: "\u2014", filterValue: null });
      continue;
    }

    switch (typeof value) {
      case "object":
        if (Array.isArray(value)) {
          result.push({
            key: fullKey,
            displayValue: JSON.stringify(value),
            filterValue: null,
          });
        } else if (Object.keys(value).length > 0) {
          result.push(
            ...flattenObject(value as Record<string, unknown>, fullKey),
          );
        }
        break;
      case "string":
        result.push({
          key: fullKey,
          displayValue: value || "\u2014",
          filterValue: value || null,
        });
        break;
      case "number":
      case "boolean":
        result.push({
          key: fullKey,
          displayValue: String(value),
          filterValue: String(value),
        });
        break;
      default:
        result.push({
          key: fullKey,
          displayValue: JSON.stringify(value),
          filterValue: JSON.stringify(value),
        });
    }
  }

  return result;
}
```

- [ ] **Step 2: Update `AttributesSection` to use `AttributeEntry`**

Replace the `AttributesSection` component (lines 319-356) to consume the new type:

```tsx
function AttributesSection({
  title,
  data,
}: {
  title: string;
  data: Record<string, unknown>;
}) {
  const flatEntries = flattenObject(data);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <div className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
          {title}
        </div>
        <button
          className="hover:bg-muted rounded p-1.5"
          onClick={() => {
            void navigator.clipboard.writeText(JSON.stringify(data, null, 2));
          }}
        >
          <Copy className="size-4" />
        </button>
      </div>
      <div className="bg-muted border-border divide-border divide-y rounded-lg border">
        {flatEntries.map((entry) => (
          <div
            key={entry.key}
            className="hover:bg-muted/50 flex flex-col gap-1 px-4 py-2.5 transition-colors"
          >
            <span className="text-muted-foreground text-xs">{entry.key}</span>
            <span className="font-mono text-sm break-all">
              {entry.displayValue}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Type-check**

Run: `cd client/dashboard && pnpm tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add client/dashboard/src/pages/logs/LogDetailSheet.tsx
git commit -m "refactor: enrich flattenObject with filterability metadata"
```

---

### Task 3: Add DropdownMenu to `AttributesSection` rows

**Files:**

- Modify: `client/dashboard/src/pages/logs/LogDetailSheet.tsx`

- [ ] **Step 1: Add imports**

At the top of `LogDetailSheet.tsx`, add to existing imports:

```ts
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@speakeasy-api/moonshine";
import { Operator } from "@gram/client/models/components/logfilter";
```

- [ ] **Step 2: Add `truncateValue` helper**

Add this small helper near the top of the file, below the existing `TOOL_IO_ATTR_KEYS`:

```ts
function truncateValue(value: string, maxLen = 24): string {
  return value.length > maxLen ? `${value.slice(0, maxLen)}\u2026` : value;
}
```

- [ ] **Step 3: Update `AttributesSection` props and add menu**

Update `AttributesSection` to accept `onAddFilter` and render a `DropdownMenu` per row:

```tsx
function AttributesSection({
  title,
  data,
  onAddFilter,
}: {
  title: string;
  data: Record<string, unknown>;
  onAddFilter?: (path: string, op: Operator, value: string) => void;
}) {
  const flatEntries = flattenObject(data);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <div className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
          {title}
        </div>
        <button
          className="hover:bg-muted rounded p-1.5"
          onClick={() => {
            void navigator.clipboard.writeText(JSON.stringify(data, null, 2));
          }}
        >
          <Copy className="size-4" />
        </button>
      </div>
      <div className="bg-muted border-border divide-border divide-y rounded-lg border">
        {flatEntries.map((entry) => {
          const isFilterable = entry.filterValue !== null && !!onAddFilter;

          const rowContent = (
            <>
              <span className="text-muted-foreground text-xs">{entry.key}</span>
              <span className="font-mono text-sm break-all">
                {entry.displayValue}
              </span>
            </>
          );

          if (!onAddFilter) {
            return (
              <div
                key={entry.key}
                className="hover:bg-muted/50 flex flex-col gap-1 px-4 py-2.5 transition-colors"
              >
                {rowContent}
              </div>
            );
          }

          return (
            <DropdownMenu key={entry.key}>
              <DropdownMenuTrigger asChild>
                <button
                  className="hover:bg-muted/50 flex w-full cursor-pointer flex-col gap-1 px-4 py-2.5 text-left transition-colors"
                  aria-label={`Filter by ${entry.key}`}
                >
                  {rowContent}
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                <DropdownMenuItem
                  disabled={!isFilterable}
                  onClick={() =>
                    onAddFilter(entry.key, Operator.Eq, entry.filterValue!)
                  }
                >
                  <span>
                    Filter by{" "}
                    <span className="font-mono text-xs">
                      {entry.key} = {truncateValue(entry.filterValue ?? "")}
                    </span>
                  </span>
                </DropdownMenuItem>
                <DropdownMenuItem
                  disabled={!isFilterable}
                  onClick={() =>
                    onAddFilter(entry.key, Operator.NotEq, entry.filterValue!)
                  }
                >
                  <span>
                    Exclude{" "}
                    <span className="font-mono text-xs">
                      {entry.key} != {truncateValue(entry.filterValue ?? "")}
                    </span>
                  </span>
                </DropdownMenuItem>
                <DropdownMenuItem
                  disabled={!isFilterable}
                  onClick={() =>
                    onAddFilter(
                      entry.key,
                      Operator.Contains,
                      entry.filterValue!,
                    )
                  }
                >
                  <span>
                    Contains{" "}
                    <span className="font-mono text-xs">
                      {truncateValue(entry.filterValue ?? "")}
                    </span>
                  </span>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={() => {
                    void navigator.clipboard.writeText(entry.displayValue);
                  }}
                >
                  Copy value
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          );
        })}
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Type-check**

Run: `cd client/dashboard && pnpm tsc --noEmit`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add client/dashboard/src/pages/logs/LogDetailSheet.tsx
git commit -m "feat: add dropdown menu to attribute rows in log detail sheet"
```

---

### Task 4: Wire `onAddFilter` from `Logs.tsx` through `LogDetailSheet`

**Files:**

- Modify: `client/dashboard/src/pages/logs/LogDetailSheet.tsx:9-13, 15-29`
- Modify: `client/dashboard/src/pages/logs/Logs.tsx:51, 345-363, 857-861`

- [ ] **Step 1: Update `LogDetailSheet` props**

In `LogDetailSheet.tsx`, update the props interface (lines 9-13) and the component to thread the callback:

```tsx
interface LogDetailSheetProps {
  log: TelemetryLogRecord | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAddFilter?: (path: string, op: Operator, value: string) => void;
}

export function LogDetailSheet({
  log,
  open,
  onOpenChange,
  onAddFilter,
}: LogDetailSheetProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className="h-full max-h-screen overflow-y-auto"
        style={{ width: "33vw", minWidth: 500, maxWidth: "none" }}
      >
        {log && <LogDetailContent log={log} onAddFilter={onAddFilter} />}
      </SheetContent>
    </Sheet>
  );
}
```

- [ ] **Step 2: Update `LogDetailContent` to pass `onAddFilter` to both `AttributesSection` instances**

Update the `LogDetailContent` function signature to accept and forward `onAddFilter`:

```tsx
function LogDetailContent({
  log,
  onAddFilter,
}: {
  log: TelemetryLogRecord;
  onAddFilter?: (path: string, op: Operator, value: string) => void;
}) {
```

Then update both `AttributesSection` usages inside `LogDetailContent` to pass the prop:

For the Attributes block (~line 193):

```tsx
<AttributesSection
  title="Attributes"
  data={filteredAttrs}
  onAddFilter={onAddFilter}
/>
```

For the Resource block (~line 200):

```tsx
<AttributesSection
  title="Resource"
  data={log.resourceAttributes as Record<string, unknown>}
  onAddFilter={onAddFilter}
/>
```

- [ ] **Step 3: Add `handleAddFilterFromLog` in `Logs.tsx`**

In `client/dashboard/src/pages/logs/Logs.tsx`, add the import for `applyFilterAdd` (update line 54):

```ts
import { type ActiveLogFilter, applyFilterAdd } from "./log-filter-types";
```

Add the `Operator` type alias for the callback signature. Add this handler after `handleLogFiltersChange` (~line 363):

```ts
const handleAddFilterFromLog = useCallback(
  (path: string, op: Operator, value: string) => {
    handleLogFiltersChange(applyFilterAdd(logFilters, { path, op, value }));
  },
  [logFilters, handleLogFiltersChange],
);
```

- [ ] **Step 4: Pass callback to `LogDetailSheet`**

Update the `<LogDetailSheet>` usage (~line 857):

```tsx
<LogDetailSheet
  log={selectedLog}
  open={!!selectedLog}
  onOpenChange={(open) => !open && setSelectedLog(null)}
  onAddFilter={handleAddFilterFromLog}
/>
```

- [ ] **Step 5: Type-check**

Run: `cd client/dashboard && pnpm tsc --noEmit`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add client/dashboard/src/pages/logs/LogDetailSheet.tsx \
       client/dashboard/src/pages/logs/Logs.tsx
git commit -m "feat: wire onAddFilter from Logs page through LogDetailSheet"
```

---

### Task 5: Manual verification

**Files:** None (testing only)

- [ ] **Step 1: Start dev server**

Run: `cd client/dashboard && pnpm dev`

- [ ] **Step 2: Test happy path**

1. Navigate to MCP Logs page
2. Click a log row to open the detail sheet
3. Hover over an attribute row — confirm `cursor: pointer`
4. Click an attribute row — confirm dropdown menu appears
5. Click "Filter by `key = value`" — confirm chip appears in filter bar, URL `?af=` updates, log list re-queries
6. Verify sheet stays open after filter add

- [ ] **Step 3: Test `!=` and `~`**

1. Open a log, click attribute row
2. Click "Exclude `key != value`" — confirm chip with `!=` appears
3. Open another log, click attribute row, click "Contains `value`" — confirm chip with `~` appears

- [ ] **Step 4: Test non-filterable rows**

1. Find a log with a null attribute (displays `—`) or an array attribute
2. Click the row — confirm menu opens
3. Confirm `=`, `!=`, `~` items are greyed out / disabled
4. Confirm "Copy value" is clickable and copies to clipboard

- [ ] **Step 5: Test Resource section**

1. Scroll to the "Resource" section in the detail sheet
2. Click a resource attribute row — confirm same menu behavior as Attributes section

- [ ] **Step 6: Test dedup**

1. Add a filter `http.response.status_code = 200` via the detail sheet
2. Open a different log with `http.response.status_code = 500`
3. Click "Filter by `= 500`" — confirm it replaces the existing `= 200` chip (not stacks)

- [ ] **Step 7: Test existing filter bar**

1. Clear all filters
2. Type a filter expression in the filter bar manually (e.g. `http.response.status_code != 200`)
3. Confirm existing filter bar flow still works (no regression from `applyFilterAdd` refactor)

- [ ] **Step 8: Test URL round-trip**

1. Add a filter via the detail sheet
2. Copy the URL
3. Refresh the page
4. Confirm the filter chip reappears from the URL

- [ ] **Step 9: Commit (if any final adjustments were needed)**

```bash
git add -u
git commit -m "fix: address issues found during manual verification"
```

Skip this step if no adjustments were needed.
