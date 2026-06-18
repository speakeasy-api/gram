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
