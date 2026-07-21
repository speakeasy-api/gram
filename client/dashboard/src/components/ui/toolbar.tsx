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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
 * The page Toolbar: the control strip that sits on its own row below a page
 * title and lets the user shape the collection below it — search, filters,
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
 * flush. The toolbar groups its children: Search, Filters, and Leading stay
 * left; Sort, ViewAs, Count, Refresh, and Actions anchor the right edge, in
 * source order. Child order otherwise doesn't matter.
 *
 * A bar that carries too many controls for one line composes explicit rows
 * instead — each row is the same left/right cluster layout inside the one
 * shared shell:
 *
 *   <Page.Toolbar>
 *     <Page.Toolbar.Row>
 *       <Page.Toolbar.Search … />
 *       <Page.Toolbar.Actions>{…}</Page.Toolbar.Actions>
 *     </Page.Toolbar.Row>
 *     <Page.Toolbar.Row>
 *       <Page.Toolbar.Leading>{…}</Page.Toolbar.Leading>
 *       <Page.Toolbar.Actions>{…}</Page.Toolbar.Actions>
 *     </Page.Toolbar.Row>
 *   </Page.Toolbar>
 */

// Shared height for every control in the toolbar (40px).
const CONTROL_HEIGHT = "h-10";

// The toolbar's shell (the grey rounded bar) — one definition whether the bar
// lays out a single row or composes Toolbar.Row children.
const TOOLBAR_SHELL = "border-border bg-muted/40 w-full rounded-lg border p-2";

// One row of clusters: the left holds the controls that narrow the data
// (search + filters + leading), spaced apart (justify-between) from the right,
// which holds how it's ordered and shown (sort + count + view + custom
// actions). Children are sorted by type, so they can be written in any order.
function ToolbarClusters({ children }: { children: ReactNode }): JSX.Element {
  let search: ReactNode = null;
  let filters: ReactNode = null;
  let leading: ReactNode = null;
  let sort: ReactNode = null;
  const trailing: ReactNode[] = [];
  React.Children.forEach(children, (child) => {
    if (!React.isValidElement(child)) return;
    if (child.type === ToolbarSearch) search = child;
    else if (child.type === ToolbarFilters) filters = child;
    else if (child.type === ToolbarLeading) leading = child;
    else if (child.type === ToolbarSortBy) sort = child;
    else trailing.push(child);
  });

  const hasLeft = search != null || filters != null || leading != null;
  const right: ReactNode[] = [sort, ...trailing].filter((n) => n != null);

  return (
    <>
      {hasLeft && (
        <div className="flex flex-wrap items-center gap-3">
          {search}
          {search != null && filters != null && (
            <div className="bg-border h-6 w-px shrink-0" />
          )}
          {filters}
          {leading}
        </div>
      )}
      {right.length > 0 && (
        // The cluster wraps right-aligned rather than clipping when it grows
        // wider than the bar; ml-auto keeps it anchored to the right edge
        // even when the row's flex-wrap drops the whole cluster onto its own
        // line below the left cluster. Single-line layouts are unaffected.
        <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-2">
          {right.map((node, index) => (
            <Fragment key={index}>{node}</Fragment>
          ))}
        </div>
      )}
    </>
  );
}

function ToolbarRoot({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}): JSX.Element {
  // Explicit rows, when present, each render the standard cluster layout on
  // their own line inside the one shell; otherwise the children form a single
  // row directly on the shell (the common case, unchanged).
  const rows = React.Children.toArray(children).filter(
    (child): child is React.ReactElement<{ children: ReactNode }> =>
      React.isValidElement(child) && child.type === ToolbarRow,
  );

  if (rows.length > 0) {
    return (
      <div className={cn(TOOLBAR_SHELL, "flex flex-col gap-2", className)}>
        {rows.map((row, index) => (
          <div
            key={index}
            className="flex w-full flex-wrap items-center justify-between gap-3"
          >
            <ToolbarClusters>{row.props.children}</ToolbarClusters>
          </div>
        ))}
      </div>
    );
  }

  return (
    <div
      className={cn(
        TOOLBAR_SHELL,
        "flex flex-wrap items-center justify-between gap-3",
        className,
      )}
    >
      <ToolbarClusters>{children}</ToolbarClusters>
    </div>
  );
}

/**
 * One explicit row of a multi-row toolbar. Only meaningful as a direct child
 * of Page.Toolbar; its children are the same pieces a single-row toolbar
 * takes, sorted into the same left/right clusters.
 */
function ToolbarRow({ children }: { children: ReactNode }): JSX.Element {
  return <>{children}</>;
}

/**
 * Custom controls anchored to the LEFT cluster beside Search/Filters — for
 * page-specific pieces that narrow or re-cut the collection (a segmented axis
 * track, a scope selector). Right-aligned extras belong in Actions.
 */
function ToolbarLeading({ children }: { children: ReactNode }): JSX.Element {
  return <>{children}</>;
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
        variant="outline"
        onClick={() => setSheetOpen(true)}
        className={cn(CONTROL_HEIGHT, "gap-2")}
      >
        <SlidersHorizontal className="size-4" />
        More filters
        {sheetCount > 0 && (
          <Badge variant="secondary" className="ml-1 px-1.5">
            {sheetCount}
          </Badge>
        )}
      </Button>

      {hasClearable && (
        <Button
          variant="ghost"
          onClick={onClearAll}
          className={cn(CONTROL_HEIGHT, "text-muted-foreground gap-1")}
        >
          <X className="size-3.5" />
          Reset to default
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
      variant="outline"
      onClick={() => {
        setMinDurationActive(true);
        onRefresh();
      }}
      disabled={showRefreshing}
      aria-label="Refresh"
      className={cn(CONTROL_HEIGHT, "gap-2", className)}
    >
      <RefreshCw className={cn("size-4", showRefreshing && "animate-spin")} />
      Refresh
    </Button>
  );
}

export const Toolbar = Object.assign(ToolbarRoot, {
  Row: ToolbarRow,
  Search: ToolbarSearch,
  Filters: ToolbarFilters,
  Leading: ToolbarLeading,
  SortBy: ToolbarSortBy,
  ViewAs: ToolbarViewAs,
  Count: ToolbarCount,
  Actions: ToolbarActions,
  Refresh: ToolbarRefresh,
});
