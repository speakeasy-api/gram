import { Dimension } from "@gram/client/models/components";

// Shared cost-taxonomy config + helpers, used by both the CostsExplorer
// controller and the EntityProfile view. Kept in a non-component module so the
// view file can satisfy the react-refresh "only export components" rule.

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
export const CHAIN: DimMeta[] = [
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

export const LABELS: Record<string, string> = Object.fromEntries(
  PIVOTS.map((p) => [p.dim, p.label]),
);

// The next axis to drill into after `dim`, following the suggested chain and
// falling back to User → Agent for off-chain pivots. null = leaf.
export function nextDimension(dim: Dimension): Dimension | null {
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

// The breakdown axis a node defaults to: the next chain step below the deepest
// filter, or the top of the chain at the org root.
export function defaultGroupBy(path: Crumb[]): Dimension {
  const last = path[path.length - 1];
  if (!last) return CHAIN[0]!.dim;
  return nextDimension(last.dim) ?? last.dim;
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
