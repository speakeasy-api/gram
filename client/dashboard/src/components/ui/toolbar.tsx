// oxlint-disable react/only-export-components -- compound component (Object.assign) pattern
import React, { Fragment, useEffect, useState, type ReactNode } from "react";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  RefreshCw,
  SearchIcon,
  SlidersHorizontal,
  X,
} from "lucide-react";
import { Badge, Button } from "@/components/ui/moonshine";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ViewToggle } from "@/components/ui/view-toggle";
import type { ViewMode } from "@/components/ui/use-view-mode";
import { cn } from "@/lib/utils";
import type { Operator } from "@gram/client/models/components/logfilter";
import type { ActiveLogFilter } from "@/pages/logs/log-filter-types";
import { FilterChip, CustomFilterChip } from "@/components/filters/FilterChip";
import { FilterSheet } from "@/components/filters/FilterSheet";
import {
  chipLabel,
  isDimensionActive,
  isDimensionAtDefault,
  type FilterDimension,
  type FilterValue,
  type OptionsById,
} from "@/components/filters/filter-schema";

/**
 * The page Toolbar: the single control strip that sits on its own row below a
 * page title and lets the user shape the collection below it — search, filters,
 * sort, and view. Compose it from its pieces:
 *
 *   <Page.Toolbar>
 *     <Page.Toolbar.Search value={q} onChange={setQ} />
 *     <Page.Toolbar.Filters schema={…} values={…} … />
 *     <Page.Toolbar.SortBy value={sort} options={…} onChange={setSort} />
 *     <Page.Toolbar.ViewAs value={view} onChange={setView} />
 *     <Page.Toolbar.Refresh onRefresh={refetch} isRefreshing={isFetching} />
 *   </Page.Toolbar>
 *
 * Every control is the same height ({@link CONTROL_HEIGHT}) so the row reads
 * flush. The toolbar groups its children: Search and Filters stay left; Sort,
 * ViewAs, Count, Refresh, and Actions anchor the right edge, in source order.
 * Child order otherwise doesn't matter.
 */

// Shared height for every control in the toolbar (40px).
const CONTROL_HEIGHT = "h-10";

function ToolbarRoot({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}): JSX.Element {
  // Two clusters, spaced apart (justify-between): the left holds the controls
  // that narrow the data (search + filters), the right holds how it's ordered
  // and shown (sort + count + view + custom actions). Children are sorted by
  // type, so they can be written in any order.
  let search: ReactNode = null;
  let filters: ReactNode = null;
  let sort: ReactNode = null;
  const trailing: ReactNode[] = [];
  React.Children.forEach(children, (child) => {
    if (!React.isValidElement(child)) return;
    if (child.type === ToolbarSearch) search = child;
    else if (child.type === ToolbarFilters) filters = child;
    else if (child.type === ToolbarSortBy) sort = child;
    else trailing.push(child);
  });

  const hasLeft = search != null || filters != null;
  const right: ReactNode[] = [sort, ...trailing].filter((n) => n != null);

  return (
    <div
      className={cn(
        "border-border bg-muted/40 flex w-full flex-wrap items-center justify-between gap-3 rounded-lg border p-2",
        className,
      )}
    >
      {hasLeft && (
        <div className="flex flex-wrap items-center gap-3">
          {search}
          {search != null && filters != null && (
            <div className="bg-border h-6 w-px shrink-0" />
          )}
          {filters}
        </div>
      )}
      {right.length > 0 && (
        <div className="flex shrink-0 items-center gap-2">
          {right.map((node, index) => (
            <Fragment key={index}>{node}</Fragment>
          ))}
        </div>
      )}
    </div>
  );
}

/** Search box (debounced, white, shared height). */
interface ToolbarSearchProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  /** Debounce before firing onChange, in ms. 0 / omitted = immediate. */
  debounceMs?: number;
  className?: string;
}

function ToolbarSearch({
  value,
  onChange,
  placeholder = "Search",
  debounceMs = 0,
  className,
}: ToolbarSearchProps): JSX.Element {
  const [local, setLocal] = useState(value);

  // Sync when value changes externally (back/forward, clear-all).
  useEffect(() => setLocal(value), [value]);

  useEffect(() => {
    if (local === value) return;
    const timer = setTimeout(() => onChange(local), debounceMs);
    return () => clearTimeout(timer);
  }, [local, value, debounceMs, onChange]);

  return (
    <div
      className={cn(
        "border-border bg-card focus-within:border-ring flex shrink-0 items-center gap-2 rounded-md border px-3",
        CONTROL_HEIGHT,
        className ?? "w-64",
      )}
    >
      <SearchIcon className="size-4 shrink-0 opacity-50" />
      <input
        value={local}
        onChange={(e) => setLocal(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape" && local) {
            e.preventDefault();
            e.stopPropagation();
            setLocal("");
          }
        }}
        placeholder={placeholder}
        className="min-w-0 flex-1 bg-transparent text-sm outline-none"
      />
      {local && (
        <button
          type="button"
          onClick={() => setLocal("")}
          aria-label="Clear search"
          className="opacity-50 transition-opacity hover:opacity-100"
        >
          <X className="size-4" />
        </button>
      )}
    </div>
  );
}

/** Filter chips + "More filters" sheet trigger + "Clear all". */
interface ToolbarFiltersProps {
  schema: readonly FilterDimension[];
  values: Record<string, FilterValue>;
  optionsById: OptionsById;
  onChange: (id: string, value: FilterValue) => void;
  onClear: (id: string) => void;
  onClearAll: () => void;
  customFilters?: ActiveLogFilter[];
  onEditCustomFilter?: (id: string, op: Operator, value?: string) => void;
  onRemoveCustomFilter?: (id: string) => void;
  /** Page-supplied arbitrary-attribute builder, rendered inside the sheet. */
  customBuilder?: ReactNode;
  projectSlug?: string;
}

function ToolbarFilters({
  schema,
  values,
  optionsById,
  onChange,
  onClear,
  onClearAll,
  customFilters = [],
  onEditCustomFilter,
  onRemoveCustomFilter,
  customBuilder,
  projectSlug,
}: ToolbarFiltersProps): JSX.Element {
  const [sheetOpen, setSheetOpen] = useState(false);

  // Pinned dims always pill (with an "All …" default); non-pinned pill once
  // active. hideChip dims never pill.
  const pillDims = schema.filter(
    (d) => !d.hideChip && (d.pinned || isDimensionActive(d, values[d.id]!)),
  );

  // Badge counts only filters hidden in the sheet (active non-pinned dims +
  // custom attributes) — pinned dims are already visible.
  const sheetCount =
    schema.filter(
      (d) => !d.pinned && !d.hideChip && isDimensionActive(d, values[d.id]!),
    ).length + customFilters.length;

  const hasClearable =
    customFilters.length > 0 ||
    schema.some(
      (d) =>
        d.kind !== "daterange" &&
        !d.hideChip &&
        isDimensionActive(d, values[d.id]!),
    );

  return (
    <div className="flex flex-wrap items-center gap-2">
      {pillDims.map((dim) => (
        <FilterChip
          key={dim.id}
          label={chipLabel(dim, values[dim.id]!, optionsById[dim.id])}
          onClick={() => setSheetOpen(true)}
          // A default pinned chip ("All …", default daterange) has nothing to
          // clear — omit onRemove so the × is hidden instead of a no-op.
          onRemove={
            isDimensionAtDefault(dim, values[dim.id]!)
              ? undefined
              : () => onClear(dim.id)
          }
        />
      ))}

      {customFilters.map((filter) => (
        <CustomFilterChip
          key={filter.id}
          filter={filter}
          onEdit={onEditCustomFilter ?? (() => {})}
          onRemove={onRemoveCustomFilter ?? (() => {})}
        />
      ))}

      <Button
        variant="secondary"
        onClick={() => setSheetOpen(true)}
        className={cn(CONTROL_HEIGHT, "gap-2")}
      >
        <Button.LeftIcon>
          <SlidersHorizontal className="size-4" />
        </Button.LeftIcon>
        <Button.Text>More filters</Button.Text>
        {sheetCount > 0 && (
          <Badge variant="neutral" size="sm" className="ml-1">
            {sheetCount}
          </Badge>
        )}
      </Button>

      {hasClearable && (
        <Button
          variant="tertiary"
          onClick={onClearAll}
          className={cn(CONTROL_HEIGHT, "text-muted-foreground gap-1")}
        >
          <Button.LeftIcon>
            <X className="size-3.5" />
          </Button.LeftIcon>
          <Button.Text>Reset to default</Button.Text>
        </Button>
      )}

      <FilterSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        schema={schema}
        values={values}
        optionsById={optionsById}
        onChange={onChange}
        onClearAll={onClearAll}
        projectSlug={projectSlug}
        customFilters={customFilters}
        onEditCustomFilter={onEditCustomFilter}
        onRemoveCustomFilter={onRemoveCustomFilter}
        customBuilder={customBuilder}
      />
    </div>
  );
}

/** Sort dropdown, with an optional asc/desc direction toggle. */
interface ToolbarSortByProps {
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: string }[];
  direction?: "asc" | "desc";
  onDirectionChange?: (direction: "asc" | "desc") => void;
  className?: string;
}

function ToolbarSortBy({
  value,
  onChange,
  options,
  direction,
  onDirectionChange,
  className,
}: ToolbarSortByProps): JSX.Element {
  // The dropdown and the direction toggle read as one control: a single
  // bordered box holding a borderless Select, then (optionally) a divider and a
  // direction button — rather than two detached boxes.
  return (
    <div
      className={cn(
        "border-border bg-card flex shrink-0 items-center rounded-md border",
        CONTROL_HEIGHT,
        className,
      )}
    >
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger
          aria-label="Sort by"
          className="h-full w-auto min-w-[120px] gap-2 border-0 bg-transparent whitespace-nowrap shadow-none focus-visible:ring-0 [&>span]:overflow-visible"
        >
          <SelectValue placeholder="Sort by" />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {direction && onDirectionChange && (
        <>
          <div className="bg-border h-5 w-px shrink-0" />
          <button
            type="button"
            aria-label="Toggle sort direction"
            onClick={() =>
              onDirectionChange(direction === "desc" ? "asc" : "desc")
            }
            className="text-muted-foreground hover:text-foreground flex h-full items-center px-3 transition-colors"
          >
            {direction === "desc" ? (
              <ArrowDownIcon className="size-4" />
            ) : (
              <ArrowUpIcon className="size-4" />
            )}
          </button>
        </>
      )}
    </div>
  );
}

/** Grid/table view toggle (right-aligned). */
interface ToolbarViewAsProps {
  value: ViewMode;
  onChange: (value: ViewMode) => void;
}

function ToolbarViewAs({ value, onChange }: ToolbarViewAsProps): JSX.Element {
  return (
    <ViewToggle
      value={value}
      onChange={onChange}
      itemClassName={CONTROL_HEIGHT}
    />
  );
}

/** Result count (or similar), right-aligned, left of the view toggle. */
function ToolbarCount({ children }: { children: ReactNode }): JSX.Element {
  return <span className="text-muted-foreground text-sm">{children}</span>;
}

/**
 * Right-aligned slot for page-specific controls that aren't search/sort/view —
 * e.g. a Tokens/Cost segmented toggle or an Employees/Unknown-users scope
 * toggle. Renders its children as-is; size them to {@link CONTROL_HEIGHT}.
 */
function ToolbarActions({ children }: { children: ReactNode }): JSX.Element {
  return <>{children}</>;
}

// Minimum time the refresh button stays in its spinning state, so a
// fast/cached refetch still reads as having done something.
const MIN_REFRESH_MS = 2000;

/** Refresh button (right-aligned). Spins/disables while refreshing. */
interface ToolbarRefreshProps {
  onRefresh: () => void;
  isRefreshing?: boolean;
  className?: string;
}

function ToolbarRefresh({
  onRefresh,
  isRefreshing = false,
  className,
}: ToolbarRefreshProps): JSX.Element {
  const [minDurationActive, setMinDurationActive] = useState(false);

  useEffect(() => {
    if (!minDurationActive) return;
    const timer = setTimeout(() => setMinDurationActive(false), MIN_REFRESH_MS);
    return () => clearTimeout(timer);
  }, [minDurationActive]);

  const showRefreshing = isRefreshing || minDurationActive;

  return (
    <Button
      variant="secondary"
      onClick={() => {
        setMinDurationActive(true);
        onRefresh();
      }}
      disabled={showRefreshing}
      aria-label="Refresh"
      className={cn(CONTROL_HEIGHT, "gap-2", className)}
    >
      <Button.LeftIcon>
        <RefreshCw className={cn("size-4", showRefreshing && "animate-spin")} />
      </Button.LeftIcon>
      <Button.Text>Refresh</Button.Text>
    </Button>
  );
}

export const Toolbar = Object.assign(ToolbarRoot, {
  Search: ToolbarSearch,
  Filters: ToolbarFilters,
  SortBy: ToolbarSortBy,
  ViewAs: ToolbarViewAs,
  Count: ToolbarCount,
  Actions: ToolbarActions,
  Refresh: ToolbarRefresh,
});
