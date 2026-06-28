import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Dimension, type QueryRow } from "@gram/client/models/components";
import type { ReactNode } from "react";
import {
  BadgeCheck,
  Bot,
  Briefcase,
  Building,
  Building2,
  ChevronLeft,
  Cpu,
  Download,
  Home,
  type LucideIcon,
  Network,
  Shield,
  UserRound,
  Wallet,
} from "lucide-react";
import { CostTable } from "./CostTable";
import { type Crumb, LABELS, type Measures } from "./taxonomy";

// ── Formatting helpers ──────────────────────────────────────────────────────

function formatCost(value: number): string {
  return `$${value.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

function displayValue(groupValue: string): string {
  return groupValue === "" ? "(unset)" : groupValue;
}

// ── CSV export ──────────────────────────────────────────────────────────────

// Serialize one CSV field. Two concerns:
//   1. Formula injection (CWE-1236): a cell starting with = + - @ (or a control
//      char) is treated as a formula by Excel/Sheets. Directory-sync values
//      (names, emails) are attacker-influenced, so neutralize with a leading
//      apostrophe before quoting.
//   2. RFC 4180 quoting: wrap in quotes (doubling internal quotes) when the
//      value contains a comma, quote, or newline.
function csvField(value: string | number): string {
  let s = String(value);
  if (/^[=+\-@\t\r]/.test(s)) s = `'${s}`;
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

// Serialize the current table rows to CSV — same columns the table shows
// (minus the Trend sparkline), with raw numbers so the file is spreadsheet-ready.
function buildCostCsv(rows: QueryRow[], groupLabel: string): string {
  const total = rows.reduce((sum, r) => sum + (r.measures.totalCost ?? 0), 0);
  const header = [
    groupLabel,
    "Total Cost",
    "% Share",
    "Cost / Session",
    "Sessions",
    "Tool Calls",
    "Tokens",
  ];
  const body = rows.map((r) => {
    const cost = r.measures.totalCost ?? 0;
    const chats = r.measures.totalChats ?? 0;
    return [
      displayValue(r.groupValue),
      cost.toFixed(2),
      total > 0 ? ((cost / total) * 100).toFixed(1) : "0.0",
      chats > 0 ? (cost / chats).toFixed(2) : "0.00",
      chats,
      r.measures.totalToolCalls ?? 0,
      r.measures.totalTokens ?? 0,
    ];
  });
  return [header, ...body]
    .map((cols) => cols.map(csvField).join(","))
    .join("\n");
}

// Trigger a client-side download of a CSV string.
function downloadCsv(filename: string, csv: string): void {
  const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  URL.revokeObjectURL(url);
}

function slugify(value: string): string {
  return (
    value
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/(^-|-$)/g, "") || "all-costs"
  );
}

// Initials for the avatar: email local-part tokens (olivia.novak → ON) or the
// first letters of the first two words (Engineering → EN, R&D → RD).
// Title-case an email local part into a name; pass other values through.
function prettyName(value: string, dim: Dimension): string {
  if (dim === Dimension.Email && value.includes("@")) {
    const local = value.split("@")[0] ?? value;
    return local
      .split(/[._-]+/)
      .filter(Boolean)
      .map((w) => w[0]!.toUpperCase() + w.slice(1))
      .join(" ");
  }
  return displayValue(value);
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

// A Lucide icon representing the entity's type (Division → org chart, User →
// person, Agent → bot, …). Falls back to the org building at the root.
const ENTITY_ICONS: Partial<Record<Dimension, LucideIcon>> = {
  [Dimension.DivisionName]: Network,
  [Dimension.DepartmentName]: Building,
  [Dimension.Email]: UserRound,
  [Dimension.HookSource]: Bot,
  [Dimension.JobTitle]: Briefcase,
  [Dimension.EmployeeType]: BadgeCheck,
  [Dimension.CostCenterName]: Wallet,
  [Dimension.Model]: Cpu,
  [Dimension.Role]: Shield,
};

function entityIcon(entity: Crumb | null): LucideIcon {
  if (!entity) return Building2;
  return ENTITY_ICONS[entity.dim] ?? Building2;
}

// ── Small presentational pieces ─────────────────────────────────────────────

// A headline metric in the profile header (Cost / Sessions / …), echoing the
// big Followers/Following/Likes numbers in the reference design.
function HeaderStat({
  label,
  value,
  onClick,
}: {
  label: string;
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
  // Navigate up one ancestor. No-op at the root.
  onBack: () => void;
  // Jump straight back to the org root.
  onHome: () => void;
  // The current project's name — the dashboard is project-scoped, so the root
  // node represents this project rather than the whole organization.
  projectName: string;
  // The immediate parent's value, for the "Back to …" control.
  parentValue: string | null;
  // The ancestor chain above this entity (root → immediate parent), rendered as
  // the typed breadcrumb trail under the title so deep nesting stays legible.
  ancestors: Crumb[];
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
  onAxisChange: (value: string) => void;
  // The child rows + drill handler.
  rows: QueryRow[];
  onDrill: (row: QueryRow) => void;
  // When set, replaces the dimension CostTable (the per-session list in sessions
  // mode). The override owns its own loading/empty/error states.
  tableOverride?: ReactNode;
  // Switch the breakdown to the per-session list — wired to the clickable
  // "Agent sessions" header stat. Omitted when already in sessions mode.
  onViewSessions?: () => void;
  // Per-group daily cost series for the row sparklines.
  seriesByGroup: Map<string, number[]>;
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
  onBack,
  onHome,
  projectName,
  parentValue,
  ancestors,
  stats,
  groupBy,
  canDrill,
  axisValue,
  axisOptions,
  onAxisChange,
  rows,
  onDrill,
  tableOverride,
  onViewSessions,
  seriesByGroup,
  rangePicker,
  rangeLabel,
  widgets,
  isLoading,
  isError,
}: EntityProfileProps): JSX.Element {
  const groupLabel = LABELS[groupBy] ?? "Group";

  const title = entity
    ? prettyName(entity.value, entity.dim)
    : projectName || "All costs";
  const typeLabel = entity ? (LABELS[entity.dim] ?? "Group") : "Project";
  // Raw ancestor values joined by chevrons (e.g. "R&D › Engineering › elena@…").
  // Values stay raw — the title already shows the entity's pretty name.
  const ancestryTrail = ancestors
    .map((c) => displayValue(c.value))
    .join("  ›  ");
  const palette = entityPalette(title);
  const Icon = entityIcon(entity);

  const handleExportCsv = () =>
    downloadCsv(
      `${slugify(title)}-by-${slugify(groupLabel)}-${slugify(rangeLabel)}.csv`,
      buildCostCsv(rows, groupLabel),
    );

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
                  {parentValue
                    ? displayValue(parentValue)
                    : projectName || "All costs"}
                </span>
              </span>
            </button>
          </div>
          {/* Date-range picker pinned to the top-right of the header, in line
              with the back controls on the left — it scopes every number below. */}
          <div className="absolute top-5 right-8 z-10">{rangePicker}</div>
          <div className="flex flex-col gap-6 sm:flex-row sm:items-start sm:justify-between">
            <div className="flex min-w-0 items-start gap-4">
              <div className="border-border bg-background flex size-16 shrink-0 items-center justify-center rounded-2xl border">
                <Icon className="text-foreground size-7" strokeWidth={1.5} />
              </div>
              <div className="min-w-0">
                {ancestryTrail && (
                  <div className="text-muted-foreground mb-1.5 truncate text-sm">
                    {ancestryTrail}
                  </div>
                )}
                <div className="flex items-center gap-2">
                  <h1 className="truncate text-2xl font-semibold tracking-tight">
                    {title}
                  </h1>
                  <Badge variant="secondary" className="shrink-0">
                    {typeLabel}
                  </Badge>
                </div>
              </div>
            </div>
            <div className="flex shrink-0 gap-8">
              <HeaderStat label="Cost" value={formatCost(stats.cost)} />
              <HeaderStat
                label="Agent sessions"
                value={stats.sessions.toLocaleString()}
                onClick={onViewSessions}
              />
              <HeaderStat
                label="Tool calls"
                value={stats.tools.toLocaleString()}
              />
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
        <div className="flex flex-col gap-3">
          <div className="mb-3 flex items-center gap-3">
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              Breakdown by
              <Select value={axisValue} onValueChange={onAxisChange}>
                <SelectTrigger className="border-border hover:bg-muted data-[state=open]:bg-muted !h-auto w-auto -my-1 cursor-pointer gap-1.5 rounded-md border bg-transparent py-1.5 pr-2.5 pl-3 text-sm font-semibold shadow-none transition-colors focus-visible:ring-0">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {axisOptions.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </h2>
            {/* CSV export covers the dimension table only; the session list owns
                its own affordances. */}
            {!tableOverride && (
              <button
                type="button"
                onClick={handleExportCsv}
                disabled={rows.length === 0}
                className="text-muted-foreground hover:text-foreground border-border hover:bg-muted inline-flex items-center gap-1.5 rounded-md border bg-transparent px-2.5 py-1.5 text-sm transition-colors disabled:pointer-events-none disabled:opacity-40"
              >
                <Download className="size-3.5 shrink-0" />
                Export CSV
              </button>
            )}
          </div>
          {tableOverride ?? dimensionTable}
        </div>
      </div>
    </div>
  );
}
