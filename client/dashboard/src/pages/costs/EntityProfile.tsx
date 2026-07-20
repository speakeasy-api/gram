import { Badge } from "@speakeasy-api/moonshine";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { type QueryRow } from "@gram/client/models/components/queryrow.js";
import type { ReactNode } from "react";
import { ChevronLeft, Download, Home } from "lucide-react";
import { CostMeasureLabel } from "@/components/estimated-cost";
import { BreakdownBar } from "./BreakdownBar";
import { breakdownCaption, breakdownTitle } from "./breakdownCopy";
import { CostTable } from "./CostTable";
import { downloadCsv, slugify, toCsv } from "./csv";
import {
  type Crumb,
  entityBadgeVariant,
  friendlyName,
  isAttributionDim,
  LABELS,
  type Measures,
  pluralLabel,
  unsetLabel,
} from "./taxonomy";

// ── Formatting helpers ──────────────────────────────────────────────────────

function formatCost(value: number): string {
  return `$${value.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

function displayValue(dim: Dimension, groupValue: string): string {
  return groupValue === "" ? unsetLabel(dim) : groupValue;
}

// ── CSV export ──────────────────────────────────────────────────────────────

// Serialize the current table rows to CSV — same columns the table shows
// (minus the Trend sparkline), with raw numbers so the file is spreadsheet-ready.
// Attribution breakdowns swap "Tool Calls" for "Tokens Added" to mirror the table.
function buildCostCsv(
  rows: QueryRow[],
  groupLabel: string,
  groupBy: Dimension,
): string {
  const total = rows.reduce((sum, r) => sum + (r.measures.totalCost ?? 0), 0);
  const cacheMetric = isAttributionDim(groupBy);
  const header = [
    groupLabel,
    "Total Cost",
    "% Share",
    "Cost / Session",
    "Sessions",
    cacheMetric ? "Tokens Added" : "Tool Calls",
    "Tokens",
  ];
  const body = rows.map((r) => {
    const cost = r.measures.totalCost ?? 0;
    const chats = r.measures.totalChats ?? 0;
    return [
      displayValue(groupBy, r.groupValue),
      cost.toFixed(2),
      total > 0 ? ((cost / total) * 100).toFixed(1) : "0.0",
      chats > 0 ? (cost / chats).toFixed(2) : "0.00",
      chats,
      cacheMetric
        ? (r.measures.cacheCreationInputTokens ?? 0)
        : (r.measures.totalToolCalls ?? 0),
      r.measures.totalTokens ?? 0,
    ];
  });
  return toCsv(header, body);
}

// The search placeholder's noun: the axis plural sentence-cased — words
// lowercase except acronyms ("Users" → "users", "MCP Servers" → "MCP servers").
function searchNoun(label: string): string {
  return label
    .split(" ")
    .map((word) => (word === word.toUpperCase() ? word : word.toLowerCase()))
    .join(" ");
}

// A unique, deterministic colour identity for an entity, derived from its name
// (FNV-1a → related hues), rendered as a faint blurred mesh wash behind the hero.
function entityPalette(name: string): { mesh: string } {
  let hash = 2166136261;
  for (let i = 0; i < name.length; i++) {
    hash ^= name.charCodeAt(i);
    hash +=
      (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
  }
  hash >>>= 0;
  // Pick from an on-brand hue set only (sky, blue, indigo, violet, purple,
  // fuchsia, rose, teal) — no lime/yellow-green or other off-brand hues. The
  // companion hues stay within the same family via small offsets.
  const ON_BRAND_HUES = [192, 210, 226, 244, 262, 284, 322, 340];
  const h1 = ON_BRAND_HUES[hash % ON_BRAND_HUES.length]!;
  const h2 = h1 + 16;
  const h3 = h1 - 12;
  return {
    // Faint, low-saturation wash spread across the full width; masked + blurred
    // in the markup so it fades downward.
    mesh: [
      `radial-gradient(52% 72% at 38% 10%, hsl(${h1} 70% 80% / 0.36) 0%, transparent 72%)`,
      `radial-gradient(56% 76% at 62% 6%, hsl(${h2} 66% 78% / 0.34) 0%, transparent 72%)`,
      `radial-gradient(56% 76% at 86% 16%, hsl(${h3} 68% 80% / 0.34) 0%, transparent 72%)`,
      `radial-gradient(50% 70% at 100% 24%, hsl(${h1} 68% 82% / 0.30) 0%, transparent 72%)`,
    ].join(", "),
  };
}

// ── Small presentational pieces ─────────────────────────────────────────────

// A headline metric in the profile header (Cost / Sessions / …), echoing the
// big Followers/Following/Likes numbers in the reference design.
function HeaderStat({
  label,
  value,
  onClick,
}: {
  label: ReactNode;
  value: string;
  // When set, the stat becomes a button — used to turn "Agent sessions" into the
  // header entry point for the per-session list.
  onClick?: () => void;
}): JSX.Element {
  const inner = (
    <>
      <span className="text-2xl font-semibold tabular-nums">{value}</span>
      <span className="text-muted-foreground text-xs">{label}</span>
    </>
  );
  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        className="hover:bg-muted -mx-2 -my-1 flex flex-col rounded-md px-2 py-1 text-left transition-colors"
      >
        {inner}
      </button>
    );
  }
  return <div className="flex flex-col">{inner}</div>;
}

// ── EntityProfile ───────────────────────────────────────────────────────────

export type EntityProfileProps = {
  // The entity this profile represents; null = the org root (bird's-eye).
  entity: Crumb | null;
  // At the root, an attribution breakdown presents as a collection (e.g. "MCP
  // Servers") instead of the project — supplies the hero title + icon. Null
  // otherwise (project root or a drilled entity).
  collection: { dim: Dimension; label: string } | null;
  // Whether this is an attribution lens: swaps the "Tool calls" hero stat for
  // "Tokens added" (cache-creation tokens), the meaningful measure for these cuts.
  cacheMetric: boolean;
  // Navigate up one ancestor. No-op at the root.
  onBack: () => void;
  // Jump straight back to the org root.
  onHome: () => void;
  // The current project's name — the dashboard is project-scoped, so the root
  // node represents this project rather than the whole organization.
  projectName: string;
  // The immediate parent's value, for the "Back to …" control.
  parentValue: string | null;
  // The full drill path (root → the entity in view), which the breakdown
  // caption names so it describes the slice actually on screen.
  path: Crumb[];
  // Headline measures summed over this entity's children.
  stats: Measures;
  // The dimension the child table is grouped by (drives labels + CostTable).
  groupBy: Dimension;
  // Whether rows can be drilled — false when no populated level exists below.
  canDrill: boolean;
  // The current breakdown axis value (a Dimension or the sessions sentinel) and
  // its selectable options, plus the change handler.
  axisValue: string;
  axisOptions: { value: string; label: string }[];
  // Optional caveat for the current breakdown axis, shown as an info tooltip
  // beside the select (e.g. the root Skill cut excludes subagent-run skills).
  axisHint?: string;
  onAxisChange: (value: string) => void;
  // Free-text filter over the visible table rows (dimension rows or sessions),
  // owned by the explorer so it can filter both row sources consistently.
  searchValue: string;
  onSearchChange: (value: string) => void;
  // The child rows + drill handler.
  rows: QueryRow[];
  // The view's resolved billing mode; "metered" shows real cost instead of the
  // API-rate estimate on the cost columns.
  billingMode?: string;
  onDrill: (row: QueryRow) => void;
  // When set, replaces the dimension CostTable (the per-session list in sessions
  // mode). The override owns its own loading/empty/error states.
  tableOverride?: ReactNode;
  // CSV export for a `tableOverride`'s rows. Supplied alongside the override so
  // the export control keeps working — and keeps its place in the header row —
  // on the sessions breakdown instead of unmounting and reflowing the row.
  overrideCsv?: { rowCount: number; build: () => string };
  // Switch the breakdown to the per-session list — wired to the clickable
  // "Agent sessions" header stat. Omitted when already in sessions mode.
  onViewSessions?: () => void;
  // Per-group daily cost series for the row sparklines.
  seriesByGroup: Map<string, number[]>;
  // The active dataset (spend slice) and its options, rendered as a selector at
  // the top-right beside the date picker. `all` is the full project spend; the
  // others narrow to a Claude attribution lens (MCP / Subagents / Skills).
  datasetValue: string;
  datasetOptions: { value: string; label: string }[];
  onDatasetChange: (value: string) => void;
  // The date-range picker control, rendered in the header above the stats.
  rangePicker: ReactNode;
  // Human date-range label (e.g. "June 15–19") for the CSV export filename.
  rangeLabel: string;
  // The summary widgets row (trend chart, mix, KPIs), rendered above the table.
  widgets: ReactNode;
  isLoading: boolean;
  isError: boolean;
};

/**
 * Generalized profile page for one node of the cost taxonomy. The same layout
 * renders the org root, a Division, a Department, a User, an Agent — driven entirely
 * by props: a bold header (avatar + name + headline stats), the entity's own
 * attribute grid, and the grouped table of its children.
 */
export function EntityProfile({
  entity,
  collection,
  cacheMetric,
  onBack,
  onHome,
  projectName,
  parentValue,
  path,
  stats,
  groupBy,
  canDrill,
  axisValue,
  axisOptions,
  axisHint,
  onAxisChange,
  searchValue,
  onSearchChange,
  rows,
  billingMode,
  onDrill,
  tableOverride,
  overrideCsv,
  onViewSessions,
  seriesByGroup,
  datasetValue,
  datasetOptions,
  onDatasetChange,
  rangePicker,
  rangeLabel,
  widgets,
  isLoading,
  isError,
}: EntityProfileProps): JSX.Element {
  const groupLabel = LABELS[groupBy] ?? "Group";

  const title = entity
    ? friendlyName(entity.dim, entity.value)
    : (collection?.label ?? projectName ?? "All costs");
  const typeLabel = entity
    ? (LABELS[entity.dim] ?? "Group")
    : collection
      ? "Breakdown"
      : "Project";
  // `title` title-cases a user's address into a name ("Olivia Novak"), which is
  // friendlier but ambiguous between two people — keep the address it came from
  // alongside it. Only users have one; every other value is already its label.
  const emailSuffix = entity?.dim === Dimension.Email ? entity.value : null;
  const badgeVariant = entityBadgeVariant(
    entity?.dim ?? collection?.dim ?? null,
  );
  const palette = entityPalette(title);

  const caption = breakdownCaption({
    axisValue,
    groupBy,
    path,
    costLabel: formatCost(stats.cost),
    groupCount: isError ? 0 : rows.length,
  });

  // The "Back to …" label names the immediate parent with its own dimension's
  // labeling (the parent crumb is second-to-last on the path; the last crumb is
  // the entity in view), falling back to the project at the root.
  const parentDim = path[path.length - 2]?.dim;
  const backLabel =
    parentValue !== null && parentDim !== undefined
      ? displayValue(parentDim, parentValue)
      : projectName || "All costs";

  // Whichever table is on screen owns the export: the dimension rows by default,
  // the override's rows (sessions) when it has supplied a builder. The control
  // renders either way and only disables on an empty table, so switching the
  // breakdown never reflows the header row.
  const csvExport = overrideCsv
    ? {
        rowCount: overrideCsv.rowCount,
        run: () =>
          downloadCsv(
            `${slugify(title)}-sessions-${slugify(rangeLabel)}.csv`,
            overrideCsv.build(),
          ),
      }
    : {
        rowCount: rows.length,
        run: () =>
          downloadCsv(
            `${slugify(title)}-by-${slugify(groupLabel)}-${slugify(rangeLabel)}.csv`,
            buildCostCsv(rows, groupLabel, groupBy),
          ),
      };

  // Placeholder names what the search box narrows: the sessions list when the
  // override table is on screen, otherwise the current axis's plural.
  const searchPlaceholder = tableOverride
    ? "Search sessions..."
    : `Search ${searchNoun(pluralLabel(groupBy))}...`;
  const searchActive = searchValue.trim().length > 0;

  // The default dimension table; replaced by `tableOverride` (the session list)
  // when one is supplied.
  const dimensionTable = isError ? (
    <Type className="text-muted-foreground">Failed to load cost data.</Type>
  ) : (
    <CostTable
      rows={rows}
      groupLabel={groupLabel}
      groupBy={groupBy}
      canDrill={canDrill}
      onDrill={onDrill}
      seriesByGroup={seriesByGroup}
      isLoading={isLoading}
      billingMode={billingMode}
      emptyMessage={searchActive ? "No matches for your search." : undefined}
    />
  );

  return (
    <div className="flex w-full flex-col">
      {/* Full-bleed hero: a soft, name-deterministic mesh fading downward so it
          curves around the avatar, flush to the top of the page body. */}
      <div className="relative w-full">
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-x-0 top-0 h-60 overflow-hidden [mask-image:linear-gradient(to_bottom,black_18%,transparent_92%)]"
        >
          <div
            className="absolute inset-0 opacity-80 blur-2xl dark:opacity-45"
            style={{ background: palette.mesh }}
          />
        </div>
        <div className="relative mx-auto w-full max-w-7xl px-8 pt-24 pb-6">
          {/* Cost Home (jump to root) + Back (one level up). Always mounted so
              they animate in/out across drills — conditional rendering would
              pop. The EntityProfile instance persists across drills, so the
              class swap triggers a real transition. */}
          <div
            aria-hidden={!entity}
            className={cn(
              "absolute top-5 left-8 flex items-center gap-2 transition-all duration-200 ease-out",
              entity
                ? "translate-x-0 opacity-100"
                : "pointer-events-none -translate-x-1 opacity-0",
            )}
          >
            {/* Only useful below depth 1 — at the root's immediate child,
                "Back to All costs" already jumps home. */}
            {parentValue !== null && (
              <button
                type="button"
                onClick={onHome}
                tabIndex={entity ? 0 : -1}
                className="text-muted-foreground hover:text-foreground border-border hover:bg-muted inline-flex items-center gap-1 rounded-md border bg-transparent py-1.5 pr-3 pl-2.5 text-sm transition-colors"
              >
                <Home className="size-3.5 shrink-0" />
                <span>Cost Overview</span>
              </button>
            )}
            <button
              type="button"
              onClick={onBack}
              tabIndex={entity ? 0 : -1}
              className="text-muted-foreground hover:text-foreground border-border hover:bg-muted inline-flex items-center gap-1 rounded-md border bg-transparent py-1.5 pr-3 pl-2.5 text-sm transition-colors"
            >
              <ChevronLeft className="size-3.5 shrink-0" />
              <span className="max-w-[220px] truncate">
                Back to{" "}
                <span className="text-foreground font-semibold">
                  {backLabel}
                </span>
              </span>
            </button>
          </div>
          {/* Dataset selector + date-range picker pinned to the top-right of the
              header, in line with the back controls on the left. The dataset
              narrows to a spend slice; the range scopes every number below. */}
          <div className="absolute top-5 right-8 z-10 flex items-stretch gap-2">
            {/* Grey "Dataset" label box wrapping the selector; stretches to the
                same height as the date picker via the row's items-stretch. */}
            <div className="border-border bg-muted flex items-stretch overflow-hidden rounded-md border text-sm">
              <span className="text-muted-foreground flex items-center pr-2 pl-3 font-medium">
                Dataset
              </span>
              <Select value={datasetValue} onValueChange={onDatasetChange}>
                <SelectTrigger className="border-border bg-background hover:bg-muted data-[state=open]:bg-muted !h-full w-auto cursor-pointer gap-1.5 rounded-none border-0 border-l py-1 pr-2.5 pl-3 font-medium shadow-none transition-colors">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent align="end">
                  {datasetOptions.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {rangePicker}
          </div>
          <div className="flex flex-col gap-6 sm:flex-row sm:items-start sm:justify-between">
            <div className="flex min-w-0 items-start gap-4">
              <div className="min-w-0">
                {/* The name leads; the type chip trails it, colour-coded by
                    entity family (see entityBadgeVariant). `min-w-0` on the
                    heading keeps the truncation on the name, so the chip stays
                    legible however long the value is. */}
                <div className="flex items-center gap-3">
                  <h1 className="min-w-0 truncate text-2xl font-semibold tracking-tight">
                    {title}
                    {emailSuffix && (
                      <span className="text-muted-foreground ml-2 text-xl font-normal">
                        ({emailSuffix})
                      </span>
                    )}
                  </h1>
                  <Badge
                    size="md"
                    variant={badgeVariant}
                    background
                    className="shrink-0"
                  >
                    <Badge.Text>{typeLabel}</Badge.Text>
                  </Badge>
                </div>
              </div>
            </div>
            <div className="flex shrink-0 gap-8">
              <HeaderStat
                label={<CostMeasureLabel billingMode={billingMode} />}
                value={formatCost(stats.cost)}
              />
              <HeaderStat
                label="Agent sessions"
                value={stats.sessions.toLocaleString()}
                onClick={onViewSessions}
              />
              {cacheMetric ? (
                <HeaderStat
                  label="Tokens added"
                  value={stats.cacheCreation.toLocaleString()}
                />
              ) : (
                <HeaderStat
                  label="Tool calls"
                  value={stats.tools.toLocaleString()}
                />
              )}
              <HeaderStat
                label="Tokens"
                value={stats.tokens.toLocaleString()}
              />
            </div>
          </div>
        </div>
      </div>

      <div className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-8 pt-2 pb-24">
        {widgets}
        {/* The breakdown is its own section under the summary widgets, so it
            opens on a rule rather than floating off the last widget. */}
        <div className="border-border flex flex-col gap-3 border-t pt-6">
          <BreakdownBar
            title={breakdownTitle(axisValue, groupBy)}
            caption={caption}
            axisValue={axisValue}
            axisOptions={axisOptions}
            axisHint={axisHint}
            onAxisChange={onAxisChange}
            searchValue={searchValue}
            onSearchChange={onSearchChange}
            searchPlaceholder={searchPlaceholder}
            actions={
              <button
                type="button"
                onClick={csvExport.run}
                disabled={csvExport.rowCount === 0}
                className="text-muted-foreground hover:text-foreground border-border hover:bg-muted inline-flex h-10 shrink-0 items-center gap-1.5 rounded-md border bg-transparent px-3 text-sm font-medium transition-colors disabled:pointer-events-none disabled:opacity-40"
              >
                <Download className="size-3.5 shrink-0" />
                Export CSV
              </button>
            }
          />
          {tableOverride ?? dimensionTable}
        </div>
      </div>
    </div>
  );
}
