import { Dimension } from "@gram/client/models/components";

// Shared cost-taxonomy config + helpers, used by both the CostsExplorer
// controller and the EntityProfile view. Kept in a non-component module so the
// view file can satisfy the react-refresh "only export components" rule.

// Individual chat sessions aren't a taxonomy dimension (a chat id isn't a
// filterable attribute), so they ride as a sentinel breakdown axis: selecting
// it swaps the table to a per-session list (telemetry.listSessions) without
// deepening the drill path. Lives alongside the real Dimension values in `?by=`.
export const SESSIONS_AXIS = "sessions" as const;
// The breakdown axis stored in `?by=`: a real Dimension or the sessions sentinel.
export type Axis = Dimension | typeof SESSIONS_AXIS;

export function isSessionsAxis(
  value: string | null | undefined,
): value is typeof SESSIONS_AXIS {
  return value === SESSIONS_AXIS;
}

// A single drill axis: API dimension key + human label.
export type DimMeta = { dim: Dimension; label: string };
// One ancestor selection in the drill path; becomes an ANDed query filter.
export type Crumb = { dim: Dimension; value: string };
// The four headline measures, summed over an entity's children.
export type Measures = {
  cost: number;
  sessions: number;
  tools: number;
  tokens: number;
};

// The suggested top-down chain an admin walks. "Team" maps to WorkOS groups[]
// (no dedicated team dimension yet); "User" is email; "Agent" is hook_source.
const CHAIN: DimMeta[] = [
  { dim: Dimension.DivisionName, label: "Division" },
  { dim: Dimension.DepartmentName, label: "Department" },
  { dim: Dimension.Group, label: "Team" },
  { dim: Dimension.Email, label: "User" },
  { dim: Dimension.HookSource, label: "Agent" },
];

// Every axis the user can pivot to at any level (dynamic taxonomy).
export const PIVOTS: DimMeta[] = [
  ...CHAIN,
  { dim: Dimension.JobTitle, label: "Job Title" },
  { dim: Dimension.EmployeeType, label: "Employment Type" },
  { dim: Dimension.CostCenterName, label: "Cost Center" },
  { dim: Dimension.Model, label: "Model" },
  { dim: Dimension.Role, label: "Role" },
];

export const LABELS: Record<string, string> = {
  ...Object.fromEntries(PIVOTS.map((p) => [p.dim, p.label])),
  [SESSIONS_AXIS]: "Sessions",
};

// The most granular grouping axes — an Agent or a Model is an endpoint, not
// something you break down further. Drilling a row here jumps straight to that
// slice's individual sessions (the SESSIONS_AXIS list) instead of another
// dimension breakdown.
export const SESSION_LEAF_DIMS = new Set<Dimension>([
  Dimension.HookSource,
  Dimension.Model,
]);
export function isSessionLeaf(dim: Dimension): boolean {
  return SESSION_LEAF_DIMS.has(dim);
}

// Levels that surface the "Most costly sessions" widget: the org root and the
// org-structure rollups down to the individual user. Agent/Model already render
// the full session table, so they don't repeat it as a widget.
const SESSION_WIDGET_DIMS = new Set<Dimension>([
  Dimension.DivisionName,
  Dimension.DepartmentName,
  Dimension.Group,
  Dimension.Email,
]);
export function showsTopSessionsWidget(entity: Crumb | null): boolean {
  return entity == null || SESSION_WIDGET_DIMS.has(entity.dim);
}

// The next axis to drill into after `dim`, following the suggested chain and
// falling back to User → Agent for off-chain pivots. null = leaf.
function nextDimension(dim: Dimension): Dimension | null {
  const i = CHAIN.findIndex((c) => c.dim === dim);
  if (i >= 0 && i < CHAIN.length - 1) return CHAIN[i + 1]!.dim;
  if (dim === Dimension.HookSource) return null;
  if (dim === Dimension.Email) return Dimension.HookSource;
  return Dimension.Email;
}

// ── URL persistence ─────────────────────────────────────────────────────────
// The drill path is encoded in the pathname so the breadcrumb bar tracks it and
// the view survives refresh/share/back. Each level is one segment `dim~value`,
// the value percent-encoded (so it can't introduce a literal `/`). The leaf
// breakdown axis lives in `?by=` instead (it filters nothing — re-pivoting at a
// level doesn't deepen the path), defaulting to the natural child axis when
// absent (e.g. after a breadcrumb link drops the query string).
export const BREAKDOWN_PARAM = "by";
const DRILL_SEP = "~";

const VALID_DIMS = new Set<string>(PIVOTS.map((p) => p.dim));
export function isDimension(
  value: string | null | undefined,
): value is Dimension {
  return value != null && VALID_DIMS.has(value);
}

// One drill level → its `dim~encodedValue` path segment.
export function encodeCrumb(crumb: Crumb): string {
  return `${crumb.dim}${DRILL_SEP}${encodeURIComponent(crumb.value)}`;
}

// Parse the drill path out of the part of the pathname after the costs base.
// `tail` is taken from the *raw* (still-encoded) pathname so splitting on `/` is
// safe; each segment's value is decoded individually.
export function parseDrillPath(tail: string): Crumb[] {
  return tail
    .split("/")
    .filter(Boolean)
    .flatMap((segment) => {
      const i = segment.indexOf(DRILL_SEP);
      if (i < 0) return [];
      const dim = segment.slice(0, i);
      if (!isDimension(dim)) return [];
      let value = segment.slice(i + 1);
      try {
        value = decodeURIComponent(value);
      } catch {
        // keep the raw value on a malformed encoding
      }
      return [{ dim, value }];
    });
}

// ── Data availability ───────────────────────────────────────────────────────
// telemetry.listAttributeKeys returns the raw OTel attribute paths present in a
// time range. Directory-sync user attributes pass through as `user.attributes.*`
// (only `app.*` custom attrs get the `@` rename), so each taxonomy dimension
// maps to a fixed key. A dimension whose key is absent has no data for the org —
// we hide it from the breakdown dropdown and skip it when picking a default.
const DIM_ATTRIBUTE_KEY: Partial<Record<Dimension, string>> = {
  [Dimension.DivisionName]: "user.attributes.division_name",
  [Dimension.DepartmentName]: "user.attributes.department_name",
  [Dimension.JobTitle]: "user.attributes.job_title",
  [Dimension.EmployeeType]: "user.attributes.employee_type",
  [Dimension.CostCenterName]: "user.attributes.cost_center_name",
  [Dimension.Email]: "user.email",
  [Dimension.Group]: "user.groups",
  [Dimension.Role]: "user.roles",
  [Dimension.Model]: "gen_ai.response.model",
  [Dimension.HookSource]: "gram.hook.source",
};

// Build the set of dimensions that actually have data from the attribute keys.
// Returns undefined when keys are unavailable (loading/empty/errored) so callers
// fail open — never hide a breakdown we're unsure about.
export function availableDimensions(
  keys: string[] | undefined,
): Set<Dimension> | undefined {
  if (!keys || keys.length === 0) return undefined;
  const present = new Set(keys);
  const out = new Set<Dimension>();
  for (const p of PIVOTS) {
    const key = DIM_ATTRIBUTE_KEY[p.dim];
    if (key && present.has(key)) out.add(p.dim);
  }
  return out;
}

// The next chain step below `dim` that actually has data, skipping any empty
// links (e.g. an org with divisions and users but no departments). Falls back to
// the raw next dimension when availability is unknown.
export function nextAvailableDimension(
  dim: Dimension,
  available?: Set<Dimension>,
): Dimension | null {
  let next = nextDimension(dim);
  if (!available) return next;
  while (next !== null && !available.has(next)) {
    next = nextDimension(next);
  }
  return next;
}

// The breakdown axis a node defaults to: the next chain step below the deepest
// filter, or — at the org root — the first dimension in pivot order that has
// data (so a customer whose IDP omits the default chain still lands on a
// populated breakdown instead of an empty Division view).
export function defaultGroupBy(
  path: Crumb[],
  available?: Set<Dimension>,
): Dimension {
  const last = path[path.length - 1];
  if (last) return nextDimension(last.dim) ?? last.dim;
  if (available && available.size > 0) {
    const hit = PIVOTS.find((p) => available.has(p.dim));
    if (hit) return hit.dim;
  }
  return CHAIN[0]!.dim;
}

// Human label for an entity value: title-cased name for emails, the raw value
// otherwise. Shared by the profile header and the breadcrumb substitutions.
export function displayName(dim: Dimension, value: string): string {
  if (value === "") return "(unset)";
  if (dim === Dimension.Email && value.includes("@")) {
    const local = value.split("@")[0] ?? value;
    return local
      .split(/[._-]+/)
      .filter(Boolean)
      .map((w) => w[0]!.toUpperCase() + w.slice(1))
      .join(" ");
  }
  return value;
}
