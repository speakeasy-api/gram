import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { narrowingLabel, type AudienceLevel } from "./serverAudience";

/**
 * One row per principal, with a line for each of the three things it can be
 * given on a server. `mcp:connect`, `mcp:read` and `mcp:write` are separate
 * scopes, so "who can use this server" is three answers per principal, not
 * one — and a role reaching the server belongs in the same list as a person,
 * since an administrator reads them together.
 *
 * Every line is resolved the way the server resolves it: gather every rule
 * that reaches this principal at this scope, gather every block that takes it
 * away, and answer from the two sets. Grants add and blocks subtract, so
 * picking one contributing rule — the nearest, the first, the row's own —
 * gives an answer that is right only by luck. What the line reads, what its
 * menu offers, and what a click writes all come from the same resolution.
 */

/** The scopes a row shows, weakest first. */
export type ScopeKey = Extract<AudienceLevel, "use" | "view" | "manage">;

export const SCOPE_ROWS: { key: ScopeKey; label: string; hint: string }[] = [
  { key: "use", label: "Connect", hint: "Call this server's tools" },
  { key: "view", label: "View", hint: "See it in the catalogue" },
  { key: "manage", label: "Manage", hint: "Change its settings and access" },
];

/**
 * The scopes whose grant satisfies a check for this one, itself included.
 * Mirrors scopeExpansions in authz/scopes.go: read and write both satisfy
 * connect, and write satisfies read.
 */
const SATISFIED_BY: Record<ScopeKey, ScopeKey[]> = {
  use: ["use", "view", "manage"],
  view: ["view", "manage"],
  manage: ["manage"],
};

/**
 * The level that takes one scope away. Each block stands on its own — see
 * scopeExpansions in authz/scopes.go, where the mcp:blocked_* scopes do not
 * satisfy one another — so closing connect leaves view and manage as they
 * were, and taking a principal off the server writes all three.
 */
export const BLOCK_LEVEL: Record<ScopeKey, AudienceLevel> = {
  use: "blocked",
  view: "blocked_view",
  manage: "blocked_manage",
};

/** A rule that grants rather than subtracts, and covers the whole server. */
export function isUnnarrowed(entry: {
  tools?: string[];
  dispositions?: string[];
}): boolean {
  return (
    (entry.tools ?? []).length === 0 && (entry.dispositions ?? []).length === 0
  );
}

/** The scope a block level was written for, or null if it is not a block. */
function blockedScope(level: AudienceLevel): ScopeKey | null {
  return SCOPE_ROWS.find((row) => BLOCK_LEVEL[row.key] === level)?.key ?? null;
}

/** Whether a rule names this tool, by name or by annotation. */
function covers(
  entry: { tools?: string[]; dispositions?: string[] },
  tool: ToolSelectionTool,
): boolean {
  if (isUnnarrowed(entry)) return true;
  const tools = new Set(entry.tools ?? []);
  const dispositions = new Set<string>(entry.dispositions ?? []);
  return (
    tools.has(tool.name) ||
    tool.annotations.some((annotation) => dispositions.has(annotation))
  );
}

/** Everything that decides one scope for one principal. */
export interface ScopeCell {
  scope: ScopeKey;
  /**
   * Every rule granting this scope to this principal: its own, the ones
   * covering every server, and the ones belonging to a role it is in. A
   * stronger scope counts, since it satisfies the same check.
   */
  grants: ResourceAudienceEntry[];
  /** Every block taking this scope away from this principal. */
  blocks: ResourceAudienceEntry[];
  /** This row's own rule naming this server, the one this page rewrites. */
  own?: ResourceAudienceEntry;
  /** This row's own block naming this server, the one this page can lift. */
  ownBlock?: ResourceAudienceEntry;
}

export interface AccessRow {
  principalUrn: string;
  kind: ResourceAudienceEntry["kind"];
  displayName: string;
  description?: string;
  cells: Record<ScopeKey, ScopeCell>;
  /** Organization members this principal reaches, when the API says. */
  memberIds: string[];
  /** True when no rule on this row names this server. */
  inheritedOnly: boolean;
}

/** Whether a rule reaches this principal, directly or through a role. */
function reaches(entry: ResourceAudienceEntry, row: AccessRow): boolean {
  if (entry.principalUrn === row.principalUrn) return true;
  // A rule naming a role reaches the people in it. It does not reach another
  // role, so only person rows widen this way.
  if (row.kind !== "user") return false;
  const userId = row.principalUrn.replace(/^user:/, "");
  return (entry.memberIds ?? []).includes(userId);
}

/**
 * Group the rules deciding access to one server into a row per principal. The
 * API returns one entry per principal and level, widest principal first, and
 * that order is what the list keeps.
 */
export function buildAccessRows(entries: ResourceAudienceEntry[]): AccessRow[] {
  const rows = new Map<string, AccessRow>();

  for (const entry of entries) {
    let row = rows.get(entry.principalUrn);
    if (!row) {
      row = {
        principalUrn: entry.principalUrn,
        kind: entry.kind,
        displayName: entry.displayName,
        description: entry.description,
        cells: {
          use: { scope: "use", grants: [], blocks: [] },
          view: { scope: "view", grants: [], blocks: [] },
          manage: { scope: "manage", grants: [], blocks: [] },
        },
        memberIds: [],
        inheritedOnly: true,
      };
      rows.set(entry.principalUrn, row);
    }
    if (entry.appliesTo === "resource") row.inheritedOnly = false;
    // Every rule for a principal reports the same membership; the first one
    // that carries it is as good as any.
    if (row.memberIds.length === 0 && entry.memberIds) {
      row.memberIds = entry.memberIds;
    }
  }

  for (const row of rows.values()) {
    for (const entry of entries) {
      if (!reaches(entry, row)) continue;
      const own = entry.principalUrn === row.principalUrn;
      const blocked = blockedScope(entry.level);

      if (blocked) {
        // A block reaches the scope it names and nothing else.
        row.cells[blocked].blocks.push(entry);
        if (own && entry.appliesTo === "resource") {
          row.cells[blocked].ownBlock = entry;
        }
        continue;
      }

      const level = entry.level as ScopeKey;
      for (const { key } of SCOPE_ROWS) {
        if (SATISFIED_BY[key].includes(level))
          row.cells[key].grants.push(entry);
      }
      if (own && entry.appliesTo === "resource") row.cells[level].own = entry;
    }
  }

  return [...rows.values()];
}

/**
 * The tools this scope actually reaches: everything its grants open, minus
 * everything its blocks take away. Null when the server publishes no
 * catalogue, since there is then nothing to resolve names against.
 */
export function reachableTools(
  cell: ScopeCell,
  catalog: ToolSelectionTool[],
): string[] | null {
  if (catalog.length === 0 || cell.grants.length === 0) return null;
  return catalog
    .filter((tool) => cell.grants.some((grant) => covers(grant, tool)))
    .filter((tool) => !cell.blocks.some((block) => covers(block, tool)))
    .map((tool) => tool.name);
}

/** What a scope line reads, and what it can be changed to. */
export interface ScopeState {
  /** The value shown in the sentence. */
  value: string;
  /** True when a rule grants this line and nothing takes it away. */
  granted: boolean;
  /**
   * True when closing or narrowing this line has to be written as a block:
   * something other than this row's own rule on this server is granting it,
   * and grants only ever add.
   */
  subtracts: boolean;
  /** True when the line can be turned off here at all. */
  canRevoke: boolean;
  /** The principal this line comes from, when it is not this row's own rule. */
  via?: string;
  /** True when a block this row cannot lift caps how far the line reaches. */
  capped?: boolean;
}

/** The blocks on this line that this page cannot lift from this row. */
function foreignBlocks(
  row: AccessRow,
  cell: ScopeCell,
): ResourceAudienceEntry[] {
  return cell.blocks.filter(
    (block) =>
      block.principalUrn !== row.principalUrn || block.appliesTo !== "resource",
  );
}

export function scopeState(
  row: AccessRow,
  scope: ScopeKey,
  catalog: ToolSelectionTool[] = [],
): ScopeState {
  const cell = row.cells[scope];
  const reachable = reachableTools(cell, catalog);
  // With a catalogue the answer is exact; without one, only an unnarrowed
  // block is known to close the line.
  const closed = reachable
    ? reachable.length === 0
    : cell.blocks.some(isUnnarrowed);

  if (cell.grants.length === 0 || closed) {
    // A block this row cannot lift is why the line is closed, and it is not
    // this row's to reopen.
    const cancelling = foreignBlocks(row, cell).find(isUnnarrowed);
    return {
      value: "No access",
      granted: false,
      subtracts: false,
      canRevoke: false,
      via: cancelling?.displayName,
      capped: Boolean(cancelling),
    };
  }

  // Anything granting this line other than the rule this page owns. It is
  // what a revoke has to subtract from, and what the line comes "via".
  const foreign = cell.grants.filter((grant) => grant !== cell.own);

  return {
    value: scope === "use" ? connectLabel(cell, catalog, reachable) : "Allowed",
    granted: true,
    subtracts: foreign.length > 0,
    canRevoke: true,
    via: cell.own ? undefined : foreign[0]?.displayName,
    capped: foreignBlocks(row, cell).length > 0,
  };
}

/**
 * How far connect reaches, said as what it opens rather than what it takes
 * away. Nobody reads "5 tools except 4 tools" and pictures the one that is
 * left, so the count is resolved against the catalogue wherever there is one.
 */
function connectLabel(
  cell: ScopeCell,
  catalog: ToolSelectionTool[],
  reachable: string[] | null,
): string {
  if (reachable) {
    if (reachable.length === catalog.length) return "All tools";
    return reachable.length === 1 ? "1 tool" : `${reachable.length} tools`;
  }

  // Without a catalogue there is nothing to count against, so the rules and
  // the blocks trimming them are all there is to say.
  const base = cell.grants.some(isUnnarrowed)
    ? "All tools"
    : capitalize(
        narrowingLabel({
          tools: cell.grants.flatMap((grant) => grant.tools ?? []),
          dispositions: cell.grants.flatMap(
            (grant) => grant.dispositions ?? [],
          ),
        }),
      );
  if (cell.blocks.length === 0) return base;
  return `${base} except ${narrowingLabel({
    tools: cell.blocks.flatMap((block) => block.tools ?? []),
    dispositions: cell.blocks.flatMap((block) => block.dispositions ?? []),
  })}`;
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

/**
 * The row read at a glance, for a collapsed row: what this principal can do
 * on this server, weakest first. Connect carries how far it reaches inside
 * the server; view and manage are the server itself, so their name is the
 * whole answer.
 */
export function accessSummary(
  row: AccessRow,
  catalog: ToolSelectionTool[] = [],
): string {
  const granted = SCOPE_ROWS.filter(
    ({ key }) => scopeState(row, key, catalog).granted,
  );
  if (granted.length === 0) return "No access";
  return granted
    .map(({ key, label }) =>
      key === "use" ? scopeState(row, key, catalog).value : label,
    )
    .join(" · ");
}

/**
 * Every rule reaching this row other than the ones it owns on this server.
 * Removing the row has to subtract from these rather than delete them.
 */
export function inheritedGrants(row: AccessRow): ResourceAudienceEntry[] {
  const foreign = new Set<ResourceAudienceEntry>();
  for (const { key } of SCOPE_ROWS) {
    for (const grant of row.cells[key].grants) {
      // Removing the row drops every rule it owns here, whatever scope they
      // sit at, so ownership is the test — not whether this particular cell
      // is the one that rule was written for.
      const owned =
        grant.principalUrn === row.principalUrn &&
        grant.appliesTo === "resource";
      if (!owned) foreign.add(grant);
    }
  }
  return [...foreign];
}

/**
 * The tools a block has to name so that only `selection` is left reachable.
 * Narrowing a rule this page does not own is a subtraction, and a subtraction
 * has to name what it takes away, so this needs the server's whole catalogue.
 */
export function complementTools(
  catalog: ToolSelectionTool[],
  selection: { tools: string[]; dispositions: string[] },
): string[] {
  return catalog
    .filter((tool) => !covers(selection, tool))
    .map((tool) => tool.name);
}
