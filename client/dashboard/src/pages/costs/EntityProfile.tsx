import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
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
  type LucideIcon,
  Network,
  Shield,
  UserRound,
  Users,
  Wallet,
} from "lucide-react";
import { CostTable } from "./CostTable";
import {
  type Crumb,
  type DimMeta,
  LABELS,
  type Measures,
  nextDimension,
} from "./taxonomy";

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

function entitySubtitle(
  entity: Crumb | null,
  parentValue: string | null,
): string {
  // The type already shows in the badge, so the subtitle carries context only:
  // the email for users, otherwise the parent it belongs to.
  if (!entity) return "Across all projects";
  if (entity.dim === Dimension.Email && entity.value.includes("@")) {
    return entity.value;
  }
  return parentValue ? `in ${displayValue(parentValue)}` : "";
}

// A unique, deterministic colour identity for an entity, derived from its name
// (FNV-1a → related hues). The avatar is a soft pastel "macOS app icon" squircle
// (light tinted gradient + a deeper icon tone); the mesh is the same hues as a
// faint blurred wash behind the hero.
function entityPalette(name: string): {
  iconBg: string;
  iconColor: string;
  mesh: string;
} {
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
    iconBg: `hsl(${h1} 30% 95%)`,
    iconColor: `hsl(${h1} 45% 45%)`,
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
  [Dimension.Group]: Users,
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
}: {
  label: string;
  value: string;
}): JSX.Element {
  return (
    <div className="flex flex-col">
      <span className="text-2xl font-semibold tabular-nums">{value}</span>
      <span className="text-muted-foreground text-xs">{label}</span>
    </div>
  );
}

// ── EntityProfile ───────────────────────────────────────────────────────────

export type EntityProfileProps = {
  // The entity this profile represents; null = the org root (bird's-eye).
  entity: Crumb | null;
  // Navigate up one ancestor. No-op at the root.
  onBack: () => void;
  // The immediate parent's value, for the subtitle (e.g. Team · Engineering).
  parentValue: string | null;
  // Headline measures summed over this entity's children.
  stats: Measures;
  // The axis the child table is grouped by, and the re-pivot options.
  groupBy: Dimension;
  pivotOptions: DimMeta[];
  onGroupByChange: (dim: Dimension) => void;
  // The child rows + drill handler.
  rows: QueryRow[];
  onDrill: (row: QueryRow) => void;
  // Per-group daily cost series for the row sparklines.
  seriesByGroup: Map<string, number[]>;
  // Per-group total cost in the previous period, for the % change column.
  prevCostByGroup: Map<string, number>;
  // The date-range picker control, rendered next to the row count.
  rangePicker: ReactNode;
  // The summary widgets row (trend chart, mix, KPIs), rendered above the table.
  widgets: ReactNode;
  isLoading: boolean;
  isError: boolean;
};

/**
 * Generalized profile page for one node of the cost taxonomy. The same layout
 * renders the org root, a Division, a Team, a User, an Agent — driven entirely
 * by props: a bold header (avatar + name + headline stats), the entity's own
 * attribute grid, and the grouped table of its children.
 */
export function EntityProfile({
  entity,
  onBack,
  parentValue,
  stats,
  groupBy,
  pivotOptions,
  onGroupByChange,
  rows,
  onDrill,
  seriesByGroup,
  prevCostByGroup,
  rangePicker,
  widgets,
  isLoading,
  isError,
}: EntityProfileProps): JSX.Element {
  const canDrill = nextDimension(groupBy) !== null;
  const groupLabel = LABELS[groupBy] ?? "Group";

  const title = entity ? prettyName(entity.value, entity.dim) : "All costs";
  const typeLabel = entity ? (LABELS[entity.dim] ?? "Group") : "Organization";
  const subtitle = entitySubtitle(entity, parentValue);
  const palette = entityPalette(title);
  const Icon = entityIcon(entity);

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
        <div className="relative mx-auto w-full max-w-7xl px-8 pt-24 pb-10">
          {entity && (
            <button
              type="button"
              onClick={onBack}
              className="text-muted-foreground hover:text-foreground border-border hover:bg-muted absolute top-5 left-8 inline-flex items-center gap-1 rounded-md border bg-transparent py-1.5 pr-3 pl-2.5 text-sm transition-colors"
            >
              <ChevronLeft className="size-3.5 shrink-0" />
              <span className="max-w-[220px] truncate">
                Back to{" "}
                <span className="text-foreground font-semibold">
                  {parentValue ? displayValue(parentValue) : "All costs"}
                </span>
              </span>
            </button>
          )}
          <div className="flex flex-col gap-6 sm:flex-row sm:items-start sm:justify-between">
            <div className="flex min-w-0 items-start gap-4">
              <div className="border-border bg-background flex size-16 shrink-0 items-center justify-center rounded-2xl border">
                <Icon className="text-foreground size-7" strokeWidth={1.5} />
              </div>
              <div className="min-w-0">
                <h1 className="truncate text-2xl font-semibold tracking-tight">
                  {title}
                </h1>
                <div className="mt-1.5 flex items-center gap-2">
                  <Badge variant="secondary">{typeLabel}</Badge>
                  {subtitle && (
                    <span className="text-muted-foreground truncate text-sm">
                      {subtitle}
                    </span>
                  )}
                </div>
              </div>
            </div>
            <div className="flex shrink-0 gap-8">
              <HeaderStat label="Cost" value={formatCost(stats.cost)} />
              <HeaderStat
                label="Chat sessions"
                value={stats.sessions.toLocaleString()}
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

      <div className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-8 pt-6 pb-24">
        {widgets}
        <div className="flex flex-col gap-3">
          <div className="mb-3 flex items-center justify-between gap-4">
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              Breakdown by
              <Select
                value={groupBy}
                onValueChange={(value) => onGroupByChange(value as Dimension)}
              >
                <SelectTrigger className="border-border hover:bg-muted data-[state=open]:bg-muted !h-auto w-auto -my-1 cursor-pointer gap-1.5 rounded-md border bg-transparent py-1.5 pr-2.5 pl-3 text-sm font-semibold shadow-none transition-colors focus-visible:ring-0">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {pivotOptions.map((p) => (
                    <SelectItem key={p.dim} value={p.dim}>
                      {p.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </h2>
            {rangePicker}
          </div>
          {isError ? (
            <Type className="text-muted-foreground">
              Failed to load cost data.
            </Type>
          ) : (
            <CostTable
              rows={rows}
              groupLabel={groupLabel}
              groupBy={groupBy}
              canDrill={canDrill}
              onDrill={onDrill}
              seriesByGroup={seriesByGroup}
              prevCostByGroup={prevCostByGroup}
              isLoading={isLoading}
            />
          )}
        </div>
      </div>
    </div>
  );
}
