import { SkeletonTable } from "@/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Dimension, type QueryRow } from "@gram/client/models/components";
import { Box, ChevronLeft, ChevronRight, Info } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  ClaudeCodeIcon,
  CodexIcon,
  GeminiIcon,
  HookSourceIcon,
} from "../hooks/HookSourceIcon";
import { Gutter, SortHeader, SUBGRID_ROW_CLASS } from "./gridTable";
import { Sparkline } from "./Sparkline";
import { trendDirection, trendOf } from "./sparkline-math";
import {
  costMeasureLabel,
  ESTIMATED_COST_TOOLTIP,
  isMeteredBilling,
} from "@/components/estimated-cost-utils";

function formatCost(value: number): string {
  return `$${value.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

// Average cost per chat session for a row; 0 when there are no sessions.
function costPerSession(row: QueryRow): number {
  const chats = row.measures.totalChats ?? 0;
  return chats > 0 ? (row.measures.totalCost ?? 0) / chats : 0;
}

// Bucket the cost into three bands by its position in the column's range:
// lowest third → emerald, middle → neutral (default text), highest → rose.
function costColor(t: number): string | undefined {
  if (t >= 2 / 3) return "#e11d48"; // rose-600 — high cost
  if (t <= 1 / 3) return "#059669"; // emerald-600 — low cost
  return undefined; // neutral
}

type LegendItem = { key: string; label: string; color: string };

// An info icon whose tooltip is a colour legend: bold (coloured) key + a plain
// description per line.
function LegendTooltip({
  intro,
  items,
}: {
  intro: string;
  items: LegendItem[];
}): JSX.Element {
  return (
    <Tooltip>
      <TooltipTrigger
        aria-label="Trend colour legend"
        className="text-muted-foreground inline-flex cursor-help"
      >
        <Info className="size-3.5" />
      </TooltipTrigger>
      <TooltipContent>
        <p className="text-primary-foreground/70 mb-1">{intro}</p>
        <div className="space-y-0.5">
          {items.map((it) => (
            <div key={it.key}>
              <span className="font-semibold" style={{ color: it.color }}>
                {it.key}
              </span>{" "}
              {it.label}
            </div>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

// A plain info icon + tooltip for explaining a column header.
function InfoTooltip({ text }: { text: string }): JSX.Element {
  return (
    <Tooltip>
      <TooltipTrigger
        aria-label={text}
        className="text-muted-foreground inline-flex cursor-help"
      >
        <Info className="size-3.5" />
      </TooltipTrigger>
      <TooltipContent className="max-w-56">{text}</TooltipContent>
    </Tooltip>
  );
}

const GREEN = "#10b981";
const RED = "#f43f5e";
const GREY = "#94a3b8";

function displayValue(groupValue: string): string {
  return groupValue === "" ? "(unset)" : groupValue;
}

// "" is the "(unset)" bucket — a real slice (everyone missing this attribute),
// so it stays drillable. Only "Other" — the synthetic top-N overflow rollup of
// many distinct values — can't map back to a single filter, so it's inert.
function isDrillableValue(groupValue: string): boolean {
  return groupValue !== "Other";
}

// The provider logo for a model value (claude-* → Claude, gpt-* → OpenAI, …).
function ModelIcon({
  model,
  className,
}: {
  model: string;
  className?: string;
}): JSX.Element {
  const m = model.toLowerCase();
  if (m.includes("claude")) return <ClaudeCodeIcon className={className} />;
  if (m.includes("gpt") || m.includes("openai") || /\bo[1-4]\b/.test(m)) {
    return <CodexIcon className={className} />;
  }
  if (m.includes("gemini")) return <GeminiIcon className={className} />;
  return <Box className={className} />;
}

// Parent grid track template (see gridTable.tsx for the subgrid mechanism).
// Track order: gutter | name | Total Cost | % Share | Cost/session | Chats |
// Tool calls | Tokens | Trend | gutter. Name sizes to its content (min 120px so
// short names aren't cramped, capped at 24rem so long emails truncate rather
// than dominate). The 7 numeric/trend columns are `minmax(max-content,1fr)`:
// never narrower than their content, but they grow equally to fill the row, so
// leftover width on wide viewports spreads across the columns instead of pooling
// in a dead right gutter. Fixed 8px gutters keep row hover + dividers full-bleed.
const COLUMNS = "8px minmax(120px,24rem) repeat(7,minmax(max-content,1fr)) 8px";

const PAGE_SIZE = 10;

type SortKey =
  | "name"
  | "cost"
  | "share"
  | "perSession"
  | "chats"
  | "tools"
  | "tokens"
  | "trend";
type SortDir = "asc" | "desc";
type Sort = { key: SortKey; dir: SortDir };

function sortValue(
  row: QueryRow,
  key: SortKey,
  seriesByGroup: Map<string, number[]>,
): number | string {
  switch (key) {
    case "name":
      return displayValue(row.groupValue).toLowerCase();
    case "cost":
    // Share is cost ÷ a constant total, so it sorts identically to cost.
    case "share":
      return row.measures.totalCost ?? 0;
    case "perSession":
      return costPerSession(row);
    case "chats":
      return row.measures.totalChats ?? 0;
    case "tools":
      return row.measures.totalToolCalls ?? 0;
    case "tokens":
      return row.measures.totalTokens ?? 0;
    case "trend": {
      // Group by colour first (up → flat → down), then by net change within a
      // group, so a sort never mixes a grey "flat" line among the red risers.
      const s = seriesByGroup.get(row.groupValue) ?? [];
      const dir = trendDirection(s);
      const rank = dir === "up" ? 1 : dir === "down" ? -1 : 0;
      return rank * 1e9 + trendOf(s);
    }
  }
}

function HeaderButton({
  label,
  sortKey,
  sort,
  onSort,
}: {
  label: string;
  sortKey: SortKey;
  sort: Sort;
  onSort: (key: SortKey) => void;
}): JSX.Element {
  return (
    <SortHeader
      label={label}
      active={sort.key === sortKey}
      dir={sort.dir}
      onClick={() => onSort(sortKey)}
    />
  );
}

export type CostTableProps = {
  rows: QueryRow[];
  groupLabel: string;
  groupBy: Dimension;
  canDrill: boolean;
  onDrill: (row: QueryRow) => void;
  // Per-group daily cost series for the trend sparkline, keyed by group value.
  seriesByGroup: Map<string, number[]>;
  isLoading: boolean;
  // The view's resolved billing mode; "metered" shows real cost rather than the
  // API-rate estimate on the cost headers.
  billingMode?: string;
};

export function CostTable({
  rows,
  groupLabel,
  groupBy,
  canDrill,
  onDrill,
  seriesByGroup,
  isLoading,
  billingMode,
}: CostTableProps): JSX.Element {
  const [sort, setSort] = useState<Sort>({ key: "cost", dir: "desc" });
  const [page, setPage] = useState(0);
  // A confidently metered view shows real cost, so the estimate caveat is hidden.
  const showCostEstimate = !isMeteredBilling(billingMode);

  const onSort = (key: SortKey) => {
    setPage(0);
    setSort((s) =>
      s.key === key
        ? { key, dir: s.dir === "asc" ? "desc" : "asc" }
        : { key, dir: key === "name" ? "asc" : "desc" },
    );
  };

  // Reset to the first page whenever the underlying data changes (drill, range).
  useEffect(() => setPage(0), [rows]);

  // Sort client-side, keeping the "Other" rollup pinned to the bottom.
  const sorted = useMemo(() => {
    const main = rows.filter((r) => r.groupValue !== "Other");
    const other = rows.filter((r) => r.groupValue === "Other");
    main.sort((a, b) => {
      const av = sortValue(a, sort.key, seriesByGroup);
      const bv = sortValue(b, sort.key, seriesByGroup);
      const cmp =
        typeof av === "string"
          ? av.localeCompare(bv as string)
          : (av as number) - (bv as number);
      return sort.dir === "asc" ? cmp : -cmp;
    });
    return [...main, ...other];
  }, [rows, sort, seriesByGroup]);

  if (isLoading) return <SkeletonTable />;

  const showModelIcon = groupBy === Dimension.Model;
  const showAgentIcon = groupBy === Dimension.HookSource;

  const totalPages = Math.ceil(sorted.length / PAGE_SIZE);
  const safePage = Math.min(page, Math.max(totalPages - 1, 0));
  const pageRows = sorted.slice(
    safePage * PAGE_SIZE,
    (safePage + 1) * PAGE_SIZE,
  );

  // Cost heat scale spans the real rows (excluding the "Other" rollup).
  const realCosts = sorted
    .filter((r) => r.groupValue !== "Other")
    .map((r) => r.measures.totalCost ?? 0);
  const minCost = realCosts.length ? Math.min(...realCosts) : 0;
  const maxCost = realCosts.length ? Math.max(...realCosts) : 0;
  // Denominator for % share — all rows incl. the "Other" rollup, so shares sum
  // to ~100% of the slice's cost.
  const totalCost = sorted.reduce(
    (sum, r) => sum + (r.measures.totalCost ?? 0),
    0,
  );

  return (
    <div
      className="border-border divide-border grid gap-x-3 gap-y-0 divide-y overflow-x-auto rounded-lg border"
      style={{ gridTemplateColumns: COLUMNS }}
    >
      <div
        className={cn(
          "text-muted-foreground grid items-center py-3.5 text-sm font-medium",
          SUBGRID_ROW_CLASS,
        )}
      >
        <Gutter />
        <span className="flex">
          <HeaderButton
            label={groupLabel}
            sortKey="name"
            sort={sort}
            onSort={onSort}
          />
        </span>
        <span className="flex items-center gap-1">
          <HeaderButton
            label={costMeasureLabel(billingMode)}
            sortKey="cost"
            sort={sort}
            onSort={onSort}
          />
          {showCostEstimate && <InfoTooltip text={ESTIMATED_COST_TOOLTIP} />}
        </span>
        <span className="flex items-center gap-1">
          <HeaderButton
            label="% Share"
            sortKey="share"
            sort={sort}
            onSort={onSort}
          />
          <InfoTooltip
            text={`Share of total cost across all ${groupLabel.toLowerCase()}s in this view.`}
          />
        </span>
        <span className="flex items-center gap-1">
          <HeaderButton
            label={costMeasureLabel(billingMode) + " / session"}
            sortKey="perSession"
            sort={sort}
            onSort={onSort}
          />
          {showCostEstimate && <InfoTooltip text={ESTIMATED_COST_TOOLTIP} />}
        </span>
        <span className="flex">
          <HeaderButton
            label="Sessions"
            sortKey="chats"
            sort={sort}
            onSort={onSort}
          />
        </span>
        <span className="flex">
          <HeaderButton
            label="Tool calls"
            sortKey="tools"
            sort={sort}
            onSort={onSort}
          />
        </span>
        <span className="flex">
          <HeaderButton
            label="Tokens"
            sortKey="tokens"
            sort={sort}
            onSort={onSort}
          />
        </span>
        <span className="flex items-center gap-1">
          <HeaderButton
            label="Trend This Period"
            sortKey="trend"
            sort={sort}
            onSort={onSort}
          />
          <LegendTooltip
            intro="over the selected range"
            items={[
              { key: "Green", label: "trending down", color: GREEN },
              { key: "Red", label: "trending up", color: RED },
              { key: "Grey", label: "no clear trend", color: GREY },
            ]}
          />
        </span>
        <Gutter />
      </div>

      {sorted.length === 0 ? (
        <div
          className="px-5 py-10 text-center"
          style={{ gridColumn: "1 / -1" }}
        >
          <Type className="text-muted-foreground">
            No cost data for this slice.
          </Type>
        </div>
      ) : (
        pageRows.map((row, i) => {
          const drillable = canDrill && isDrillableValue(row.groupValue);
          const cost = row.measures.totalCost ?? 0;
          const isOther = row.groupValue === "Other";
          const costT =
            maxCost > minCost ? (cost - minCost) / (maxCost - minCost) : 0.5;
          return (
            <button
              key={row.groupValue}
              type="button"
              disabled={!drillable}
              onClick={() => {
                if (drillable) onDrill(row);
              }}
              className={cn(
                "grid w-full items-center py-4 text-left text-sm transition-colors",
                SUBGRID_ROW_CLASS,
                (safePage * PAGE_SIZE + i) % 2 === 1 && "bg-muted/25",
                drillable ? "hover:bg-muted cursor-pointer" : "cursor-default",
              )}
            >
              <Gutter />
              <div className="flex min-w-0 items-center gap-2">
                {showModelIcon && (
                  <ModelIcon
                    model={row.groupValue}
                    className="size-4 shrink-0"
                  />
                )}
                {showAgentIcon && (
                  <HookSourceIcon
                    source={row.groupValue}
                    className="size-4 shrink-0"
                  />
                )}
                <span className="truncate font-medium">
                  {displayValue(row.groupValue)}
                </span>
                {drillable && (
                  <ChevronRight className="text-muted-foreground size-4 shrink-0" />
                )}
              </div>
              <span
                className="text-left font-medium tabular-nums whitespace-nowrap"
                style={isOther ? undefined : { color: costColor(costT) }}
              >
                {formatCost(cost)}
              </span>
              <span
                className="text-left tabular-nums whitespace-nowrap"
                style={isOther ? undefined : { color: costColor(costT) }}
              >
                {totalCost > 0
                  ? `${((cost / totalCost) * 100).toFixed(1)}%`
                  : "—"}
              </span>
              <span className="text-muted-foreground text-left tabular-nums whitespace-nowrap">
                {(row.measures.totalChats ?? 0) > 0
                  ? formatCost(costPerSession(row))
                  : "—"}
              </span>
              <span className="text-left tabular-nums whitespace-nowrap">
                {(row.measures.totalChats ?? 0).toLocaleString()}
              </span>
              <span className="text-left tabular-nums whitespace-nowrap">
                {(row.measures.totalToolCalls ?? 0).toLocaleString()}
              </span>
              <span className="text-left tabular-nums whitespace-nowrap">
                {(row.measures.totalTokens ?? 0).toLocaleString()}
              </span>
              <span className="flex">
                <Sparkline values={seriesByGroup.get(row.groupValue) ?? []} />
              </span>
              <Gutter />
            </button>
          );
        })
      )}

      {totalPages > 1 && (
        <div
          className="flex items-center justify-between px-5 py-3"
          style={{ gridColumn: "1 / -1" }}
        >
          <p className="text-muted-foreground text-sm">
            {safePage * PAGE_SIZE + 1}–
            {Math.min((safePage + 1) * PAGE_SIZE, sorted.length)} of{" "}
            {sorted.length.toLocaleString()}
          </p>
          <div className="flex items-center gap-1">
            <button
              type="button"
              aria-label="Previous page"
              onClick={() => setPage((p) => p - 1)}
              disabled={safePage === 0}
              className="hover:bg-muted inline-flex size-8 items-center justify-center rounded-md transition-colors disabled:pointer-events-none disabled:opacity-40"
            >
              <ChevronLeft className="size-4" />
            </button>
            <button
              type="button"
              aria-label="Next page"
              onClick={() => setPage((p) => p + 1)}
              disabled={safePage >= totalPages - 1}
              className="hover:bg-muted inline-flex size-8 items-center justify-center rounded-md transition-colors disabled:pointer-events-none disabled:opacity-40"
            >
              <ChevronRight className="size-4" />
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
