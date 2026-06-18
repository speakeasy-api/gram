import { SkeletonTable } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Dimension, type QueryRow } from "@gram/client/models/components";
import {
  Box,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronsUpDown,
  ChevronUp,
  Info,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  ClaudeCodeIcon,
  CodexIcon,
  GeminiIcon,
  HookSourceIcon,
} from "../hooks/HookSourceIcon";
import { Sparkline, trendDirection, trendOf } from "./Sparkline";

function formatCost(value: number): string {
  return `$${value.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

// Bucket the cost into three bands by its position in the column's range:
// lowest third → emerald, middle → neutral (default text), highest → rose.
function costColor(t: number): string | undefined {
  if (t >= 2 / 3) return "#e11d48"; // rose-600 — high cost
  if (t <= 1 / 3) return "#059669"; // emerald-600 — low cost
  return undefined; // neutral
}

function displayValue(groupValue: string): string {
  return groupValue === "" ? "(unset)" : groupValue;
}

function isDrillableValue(groupValue: string): boolean {
  return groupValue !== "" && groupValue !== "Other";
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

// Shared grid template so the header and every row align.
const GRID =
  "grid grid-cols-[minmax(140px,20rem)_112px_116px_92px_104px_128px_1fr] items-center gap-3 px-6";

const PAGE_SIZE = 10;

type SortKey = "name" | "cost" | "chats" | "tools" | "tokens" | "trend";
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
      return row.measures.totalCost ?? 0;
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
  const active = sort.key === sortKey;
  let arrow = (
    <ChevronsUpDown className="text-muted-foreground/40 group-hover:text-foreground size-3.5" />
  );
  if (active) {
    arrow =
      sort.dir === "asc" ? (
        <ChevronUp className="size-3.5" />
      ) : (
        <ChevronDown className="size-3.5" />
      );
  }
  return (
    <button
      type="button"
      onClick={() => onSort(sortKey)}
      className={cn(
        "group hover:text-foreground inline-flex items-center gap-1 transition-colors",
        active && "text-foreground",
      )}
    >
      {label}
      {arrow}
    </button>
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
};

export function CostTable({
  rows,
  groupLabel,
  groupBy,
  canDrill,
  onDrill,
  seriesByGroup,
  isLoading,
}: CostTableProps): JSX.Element {
  const [sort, setSort] = useState<Sort>({ key: "cost", dir: "desc" });
  const [page, setPage] = useState(0);

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

  return (
    <div className="border-border divide-border divide-y overflow-hidden rounded-lg border">
      <div
        className={cn(GRID, "text-muted-foreground py-3.5 text-sm font-medium")}
      >
        <span className="flex">
          <HeaderButton
            label={groupLabel}
            sortKey="name"
            sort={sort}
            onSort={onSort}
          />
        </span>
        <span className="flex justify-end">
          <HeaderButton
            label="Total Cost"
            sortKey="cost"
            sort={sort}
            onSort={onSort}
          />
        </span>
        <span className="flex justify-end">
          <HeaderButton
            label="Chat sessions"
            sortKey="chats"
            sort={sort}
            onSort={onSort}
          />
        </span>
        <span className="flex justify-end">
          <HeaderButton
            label="Tool calls"
            sortKey="tools"
            sort={sort}
            onSort={onSort}
          />
        </span>
        <span className="flex justify-end">
          <HeaderButton
            label="Tokens"
            sortKey="tokens"
            sort={sort}
            onSort={onSort}
          />
        </span>
        <span className="flex items-center justify-end gap-1">
          <HeaderButton
            label="Cost trend"
            sortKey="trend"
            sort={sort}
            onSort={onSort}
          />
          <SimpleTooltip tooltip="Daily cost over 30 days — green if falling, red if rising.">
            <Info className="size-3.5 cursor-help" />
          </SimpleTooltip>
        </span>
      </div>

      {sorted.length === 0 ? (
        <div className="px-4 py-10 text-center">
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
              onClick={() => drillable && onDrill(row)}
              className={cn(
                GRID,
                "w-full py-4 text-left text-sm transition-colors",
                (safePage * PAGE_SIZE + i) % 2 === 1 && "bg-muted/25",
                drillable ? "hover:bg-muted cursor-pointer" : "cursor-default",
              )}
            >
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
                className="text-right font-medium tabular-nums"
                style={isOther ? undefined : { color: costColor(costT) }}
              >
                {formatCost(cost)}
              </span>
              <span className="text-right tabular-nums">
                {(row.measures.totalChats ?? 0).toLocaleString()}
              </span>
              <span className="text-right tabular-nums">
                {(row.measures.totalToolCalls ?? 0).toLocaleString()}
              </span>
              <span className="text-right tabular-nums">
                {(row.measures.totalTokens ?? 0).toLocaleString()}
              </span>
              <span className="flex justify-end">
                <Sparkline values={seriesByGroup.get(row.groupValue) ?? []} />
              </span>
            </button>
          );
        })
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-between px-6 py-3">
          <p className="text-muted-foreground text-sm">
            {safePage * PAGE_SIZE + 1}–
            {Math.min((safePage + 1) * PAGE_SIZE, sorted.length)} of{" "}
            {sorted.length.toLocaleString()}
          </p>
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => setPage((p) => p - 1)}
              disabled={safePage === 0}
              className="hover:bg-muted inline-flex size-8 items-center justify-center rounded-md transition-colors disabled:pointer-events-none disabled:opacity-40"
            >
              <ChevronLeft className="size-4" />
            </button>
            <button
              type="button"
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
