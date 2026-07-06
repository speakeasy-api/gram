import { Dimension } from "@gram/client/models/components";
import { providerLabel } from "@/components/observe/account-display-utils";

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
// The headline measures, summed over an entity's children. `cacheCreation` is
// the cache-creation input tokens — the context weight an attribution cut (MCP
// server/tool, skill, subagent) writes to the prompt cache, surfaced in place of
// tool calls on those breakdowns.
export type Measures = {
  cost: number;
  sessions: number;
  tools: number;
  tokens: number;
  cacheCreation: number;
};

// The suggested top-down chain an admin walks. "User" is email; "Agent" is
// hook_source.
const CHAIN: DimMeta[] = [
  { dim: Dimension.DivisionName, label: "Division" },
  { dim: Dimension.DepartmentName, label: "Department" },
  { dim: Dimension.Email, label: "User" },
  { dim: Dimension.HookSource, label: "Agent" },
];

// Every axis the user can pivot to at any level (dynamic taxonomy). The Claude
// attribution cuts (MCP Server/Tool, Skill, Subagent) are appended last so they
// never preempt the org-hierarchy default at the root (see defaultGroupBy).
export const PIVOTS: DimMeta[] = [
  ...CHAIN,
  { dim: Dimension.JobTitle, label: "Job Title" },
  { dim: Dimension.EmployeeType, label: "Employment Type" },
  { dim: Dimension.CostCenterName, label: "Cost Center" },
  { dim: Dimension.Model, label: "Model" },
  { dim: Dimension.AccountType, label: "Account Type" },
  { dim: Dimension.Provider, label: "Provider" },
  { dim: Dimension.Role, label: "Role" },
  { dim: Dimension.McpServerName, label: "MCP Server" },
  { dim: Dimension.McpToolName, label: "MCP Tool" },
  { dim: Dimension.SkillName, label: "Skill" },
  // agent_name is the Claude subagent (e.g. generalPurpose); "Agent" is already
  // taken by hook_source (the consuming surface), so label this "Subagent".
  { dim: Dimension.AgentName, label: "Subagent" },
];

export const LABELS: Record<string, string> = {
  ...Object.fromEntries(PIVOTS.map((p) => [p.dim, p.label])),
  [SESSIONS_AXIS]: "Sessions",
};

// Plural labels for the "collection" hero shown at the root when the breakdown
// axis is an attribution cut (e.g. the root grouped by MCP Server presents as
// "MCP Servers" rather than the project). Only attribution dims get one.
const COLLECTION_LABELS: Partial<Record<Dimension, string>> = {
  [Dimension.McpServerName]: "MCP Servers",
  [Dimension.McpToolName]: "MCP Tools",
  [Dimension.SkillName]: "Skills",
  [Dimension.AgentName]: "Subagents",
};
export function collectionLabel(dim: Dimension): string | null {
  return COLLECTION_LABELS[dim] ?? null;
}

// The most granular grouping axes — an Agent or a Model is an endpoint, not
// something you break down further. Drilling a row here jumps straight to that
// slice's individual sessions (the SESSIONS_AXIS list) instead of another
// dimension breakdown.
const SESSION_LEAF_DIMS = new Set<Dimension>([
  Dimension.HookSource,
  Dimension.Model,
  // Claude attribution leaves: an MCP *tool* or a *skill* is an endpoint —
  // drilling one lists the sessions that touched it. Their parents (MCP Server,
  // Subagent) are NOT leaves: they drill one level deeper first (Server → Tool,
  // Subagent → Skill) before bottoming out at sessions (see nextDimension).
  Dimension.McpToolName,
  Dimension.SkillName,
]);
export function isSessionLeaf(dim: Dimension): boolean {
  return SESSION_LEAF_DIMS.has(dim);
}

// The Claude api_request attribution cuts. On these dims an empty "" group is
// spend where the attribute is *not applicable* (a turn with no skill/subagent/
// MCP call), not missing data — so breakdowns drop it instead of rendering an
// "(unset)" row (see CostsExplorer). Two independent drill trees live here:
// MCP Server → MCP Tool and Subagent → Skill.
const ATTRIBUTION_DIMS = new Set<Dimension>([
  Dimension.McpServerName,
  Dimension.McpToolName,
  Dimension.SkillName,
  Dimension.AgentName,
]);
export function isAttributionDim(dim: Dimension): boolean {
  return ATTRIBUTION_DIMS.has(dim);
}

// A nested attribution cut and the parent it must sit under. MCP Tool only makes
// sense scoped to an MCP Server (a tool call always belongs to a server), so it
// is offered as a breakdown axis only once its parent is pinned in the drill
// path. Skill, by contrast, is a valid root cut ("skills run outside a
// subagent") so it has no required parent.
const PIVOT_PARENT: Partial<Record<Dimension, Dimension>> = {
  [Dimension.McpToolName]: Dimension.McpServerName,
};
export function pivotParent(dim: Dimension): Dimension | null {
  return PIVOT_PARENT[dim] ?? null;
}

// Levels that surface the "Most costly sessions" widget: the org root and the
// org-structure rollups down to the individual user. Agent/Model already render
// the full session table, so they don't repeat it as a widget.
const SESSION_WIDGET_DIMS = new Set<Dimension>([
  Dimension.DivisionName,
  Dimension.DepartmentName,
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
  // Attribution trees: each parent drills into its child cut; the leaves
  // (MCP Tool, Skill) bottom out at sessions (isSessionLeaf), so no child here.
  if (dim === Dimension.McpServerName) return Dimension.McpToolName;
  if (dim === Dimension.AgentName) return Dimension.SkillName;
  if (dim === Dimension.McpToolName || dim === Dimension.SkillName) return null;
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
  [Dimension.Role]: "user.roles",
  [Dimension.Model]: "gen_ai.response.model",
  [Dimension.HookSource]: "gram.hook.source",
  [Dimension.AccountType]: "gram.account_type",
  [Dimension.Provider]: "gram.provider",
  // Claude attribution keys are stamped at the top level of `attributes` on
  // api_request rows (see attribute_metrics_summaries_mv), so JSONAllPaths emits
  // them verbatim. Present only when the org has Claude attribution data, so the
  // pivots auto-hide otherwise.
  [Dimension.McpServerName]: "mcp_server.name",
  [Dimension.McpToolName]: "mcp_tool.name",
  [Dimension.SkillName]: "skill.name",
  [Dimension.AgentName]: "agent.name",
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
  if (dim === Dimension.Provider) return providerLabel(value);
  if (dim === Dimension.AccountType) {
    return value.charAt(0).toUpperCase() + value.slice(1);
  }
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
