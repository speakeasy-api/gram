import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { narrowingLabel, type AudienceLevel } from "./serverAudience";

/**
 * One row per principal, with a line for each of the three things it can be
 * given on a server. `mcp:connect`, `mcp:read` and `mcp:write` are separate
 * scopes, so "who can use this server" is three answers per principal, not
 * one — and a role reaching the server belongs in the same list as a person,
 * since an administrator reads them together.
 */

/** The scopes a row shows, weakest first. */
export type ScopeKey = Extract<AudienceLevel, "use" | "view" | "manage">;

export const SCOPE_ROWS: { key: ScopeKey; label: string; hint: string }[] = [
  { key: "use", label: "Connect", hint: "Call this server's tools" },
  { key: "view", label: "View", hint: "See it in the catalogue" },
  { key: "manage", label: "Manage", hint: "Change its settings and access" },
];

/**
 * Which stronger scopes already satisfy a check for this one. Mirrors
 * scopeExpansions in authz/scopes.go: read and write both satisfy connect, and
 * write satisfies read.
 */
const IMPLIED_BY: Record<ScopeKey, ScopeKey[]> = {
  use: ["view", "manage"],
  view: ["manage"],
  manage: [],
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

export interface ScopeCell {
  scope: ScopeKey;
  /** The rule naming this server, which this page owns. */
  direct?: ResourceAudienceEntry;
  /** The rule covering every server, which the role editor owns. */
  inherited?: ResourceAudienceEntry;
  /** A stronger scope on the same row that already grants this one. */
  impliedBy?: ScopeKey;
  /** The block naming this server that takes this scope away. */
  block?: ResourceAudienceEntry;
  /**
   * A narrowed block on another principal that still trims this person: a
   * block reaches people, so a role's exception applies to its members
   * whatever else grants them access.
   */
  trimmedBy?: ResourceAudienceEntry;
  /**
   * Another principal whose block cancels this scope for the person this row
   * names. A block reaches people, not principals: one written for a role
   * takes the scope from everyone in it, including someone who was granted
   * it here directly.
   */
  cancelledBy?: string;
  /**
   * A rule belonging to another principal — a role this person is in — that
   * already grants this scope. A row that ignored it would say "No access"
   * about someone who plainly has it.
   */
  via?: {
    entry: ResourceAudienceEntry;
    principalName: string;
    /** A narrowed block on that principal, which trims what it gives. */
    block?: ResourceAudienceEntry;
  };
}

export interface AccessRow {
  principalUrn: string;
  kind: ResourceAudienceEntry["kind"];
  displayName: string;
  description?: string;
  cells: Record<ScopeKey, ScopeCell>;
  /** Blocks naming this server, by the scope each was written for. */
  blocks: Partial<Record<ScopeKey, ResourceAudienceEntry>>;
  /** Organization members this principal reaches, when the API says. */
  memberIds: string[];
  /** True when no rule on this row names this server. */
  inheritedOnly: boolean;
}

/** A rule that grants rather than subtracts, and covers the whole server. */
export function isUnnarrowed(entry: ResourceAudienceEntry): boolean {
  return (
    (entry.tools ?? []).length === 0 && (entry.dispositions ?? []).length === 0
  );
}

/** The scope a block level was written for, or null if it is not a block. */
function blockedScope(level: AudienceLevel): ScopeKey | null {
  return SCOPE_ROWS.find((row) => BLOCK_LEVEL[row.key] === level)?.key ?? null;
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
          use: { scope: "use" },
          view: { scope: "view" },
          manage: { scope: "manage" },
        },
        blocks: {},
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

    const blocked = blockedScope(entry.level);
    if (blocked) {
      // Only a rule naming this server can be edited here; an organization-
      // wide block is the role editor's.
      if (entry.appliesTo === "resource") row.blocks[blocked] = entry;
      continue;
    }

    const cell = row.cells[entry.level as ScopeKey];
    if (entry.appliesTo === "resource") cell.direct = entry;
    else cell.inherited = entry;
  }

  // Which people a block takes each scope from, and the principal that did
  // it. A grant written on this page for someone a role's block reaches does
  // nothing, so the row has to say so rather than promise access the server
  // will refuse.
  const blockedMembers = new Map<string, Map<ScopeKey, string>>();
  // And which scopes a rule already gives them, so a person's row reads as
  // what they can do here rather than as the rules that happen to name them.
  const grantedMembers = new Map<
    string,
    Map<ScopeKey, ResourceAudienceEntry>
  >();
  const trimmedMembers = new Map<
    string,
    Map<ScopeKey, ResourceAudienceEntry>
  >();
  for (const entry of entries) {
    const scope = blockedScope(entry.level);
    for (const memberId of entry.memberIds ?? []) {
      if (scope && !isUnnarrowed(entry)) {
        // A narrowed block trims rather than closes, and it trims everyone
        // it reaches — including someone holding a grant of their own.
        const perScope =
          trimmedMembers.get(memberId) ??
          new Map<ScopeKey, ResourceAudienceEntry>();
        if (!perScope.has(scope)) perScope.set(scope, entry);
        trimmedMembers.set(memberId, perScope);
        continue;
      }
      if (scope) {
        const perScope =
          blockedMembers.get(memberId) ?? new Map<ScopeKey, string>();
        if (!perScope.has(scope)) perScope.set(scope, entry.principalUrn);
        blockedMembers.set(memberId, perScope);
        continue;
      }
      const perScope =
        grantedMembers.get(memberId) ??
        new Map<ScopeKey, ResourceAudienceEntry>();
      // An unnarrowed rule beats a narrowed one: the line says how far the
      // person reaches, not which rule said so.
      const held = perScope.get(entry.level as ScopeKey);
      if (!held || (isUnnarrowed(entry) && !isUnnarrowed(held))) {
        perScope.set(entry.level as ScopeKey, entry);
      }
      grantedMembers.set(memberId, perScope);
    }
  }
  const nameByPrincipal = new Map(
    entries.map((entry) => [entry.principalUrn, entry.displayName]),
  );

  for (const row of rows.values()) {
    const memberId =
      row.kind === "user" ? row.principalUrn.replace(/^user:/, "") : null;
    for (const { key } of SCOPE_ROWS) {
      row.cells[key].impliedBy = IMPLIED_BY[key].find(
        (stronger) =>
          row.cells[stronger].direct ?? row.cells[stronger].inherited,
      );
      row.cells[key].block = row.blocks[key];
      const trimming = memberId
        ? trimmedMembers.get(memberId)?.get(key)
        : undefined;
      if (trimming && trimming.principalUrn !== row.principalUrn) {
        row.cells[key].trimmedBy = trimming;
      }

      // Only another principal's block is worth naming: this row's own is
      // already the value the line shows.
      const cancelling = memberId
        ? blockedMembers.get(memberId)?.get(key)
        : undefined;
      if (cancelling && cancelling !== row.principalUrn) {
        row.cells[key].cancelledBy =
          nameByPrincipal.get(cancelling) ?? cancelling;
      }

      // Only when this row has no rule of its own at this scope: `via` is
      // where the line comes from, and a rule naming this principal is
      // nearer than one naming a role it belongs to.
      const cell = row.cells[key];
      const granting =
        memberId && !cell.direct && !cell.inherited
          ? grantedMembers.get(memberId)?.get(key)
          : undefined;
      if (granting && granting.principalUrn !== row.principalUrn) {
        row.cells[key].via = {
          entry: granting,
          principalName: granting.displayName,
          // A block trimming that principal trims what this person gets from
          // it, so the line cannot read the grant on its own.
          block: rows.get(granting.principalUrn)?.blocks[key],
        };
      }
    }
  }

  return [...rows.values()];
}

/** True when some rule grants this scope, ignoring anything blocking it. */
export function isGranted(row: AccessRow, scope: ScopeKey): boolean {
  const cell = row.cells[scope];
  return Boolean(cell.direct ?? cell.inherited ?? cell.impliedBy ?? cell.via);
}

/** What a scope line reads, and what it can be changed to. */
export interface ScopeState {
  /** The value shown in the sentence. */
  value: string;
  /** True when a rule grants this line and nothing takes it away. */
  granted: boolean;
  /**
   * True when narrowing this scope has to be written as a block: an
   * organization-wide rule already opens the whole server, and grants only
   * ever add.
   */
  subtracts: boolean;
  /** True when the line can be turned off here at all. */
  canRevoke: boolean;
  /** The principal whose block cancels this line, when one does. */
  cancelledBy?: string;
  /** The principal this line comes from, when it is not this row's own rule. */
  via?: string;
  /** True when another principal's block caps how far this line can reach. */
  capped?: boolean;
}

export function scopeState(
  row: AccessRow,
  scope: ScopeKey,
  catalog: ToolSelectionTool[] = [],
): ScopeState {
  const cell = row.cells[scope];
  const blockedOutright = Boolean(cell.block && isUnnarrowed(cell.block));

  if (blockedOutright || cell.cancelledBy || !isGranted(row, scope)) {
    return {
      value: "No access",
      granted: false,
      subtracts: false,
      canRevoke: false,
      cancelledBy: cell.cancelledBy,
    };
  }

  // Only connect reaches individual tools. View and manage are about the
  // server itself, so there is nothing inside one to narrow.
  const via = !cell.direct && !cell.inherited ? cell.via : undefined;

  if (scope !== "use") {
    return {
      value: "Allowed",
      granted: true,
      subtracts: false,
      canRevoke: true,
      via: via?.principalName,
    };
  }

  return {
    value: connectLabel(cell, catalog),
    granted: true,
    // Narrowing has to be written as a block whenever the grant behind the
    // line belongs to someone else — a rule covering every server, or a role
    // this person is in.
    subtracts: Boolean(
      (cell.inherited && isUnnarrowed(cell.inherited)) ||
      (via && isUnnarrowed(via.entry)),
    ),
    canRevoke: true,
    via: via?.principalName,
    // A block above this line already trims what reaches it, and lifting
    // that block is not this row's to do, so the line cannot be widened to
    // every tool from here.
    capped: Boolean(cell.via?.block ?? cell.trimmedBy),
  };
}

/**
 * The tools connect actually reaches: what the grant opens, minus what a
 * block takes away. Needs the catalogue, since a grant may be unnarrowed and
 * a block names only what it removes. Null when the server publishes none.
 */
export function reachableTools(
  cell: ScopeCell,
  catalog: ToolSelectionTool[],
): string[] | null {
  if (catalog.length === 0) return null;
  const granting = cell.direct ?? cell.inherited ?? cell.via?.entry;
  if (!granting) return null;

  const matches = (entry: ResourceAudienceEntry, tool: ToolSelectionTool) => {
    const tools = new Set(entry.tools ?? []);
    const dispositions = new Set<string>(entry.dispositions ?? []);
    return (
      tools.has(tool.name) ||
      tool.annotations.some((annotation) => dispositions.has(annotation))
    );
  };

  // A stronger scope satisfies a connect check over the whole server, so an
  // implied line reaches every tool whatever the connect rule says.
  let reachable =
    isUnnarrowed(granting) || cell.impliedBy
      ? catalog
      : catalog.filter((tool) => matches(granting, tool));

  const block = cell.block ?? cell.via?.block ?? cell.trimmedBy;
  if (block) reachable = reachable.filter((tool) => !matches(block, tool));

  return reachable.map((tool) => tool.name);
}

/**
 * How far connect reaches, said as what it opens rather than what it takes
 * away. Nobody reads "5 tools except 4 tools" and pictures the one that is
 * left, so the count is resolved against the catalogue wherever there is one.
 */
function connectLabel(cell: ScopeCell, catalog: ToolSelectionTool[]): string {
  const reachable = reachableTools(cell, catalog);
  if (reachable) {
    if (reachable.length === 0) return "No access";
    if (reachable.length === catalog.length) return "All tools";
    return reachable.length === 1 ? "1 tool" : `${reachable.length} tools`;
  }

  // Without a catalogue there is nothing to count against, so the rule and
  // the block it carries are all there is to say.
  const granting = cell.direct ?? cell.inherited ?? cell.via?.entry;
  const base =
    !granting || cell.impliedBy
      ? "All tools"
      : capitalize(narrowingLabel(granting));
  const block = cell.block ?? cell.via?.block ?? cell.trimmedBy;
  return block ? `${base} except ${narrowingLabel(block)}` : base;
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

/** Every rule on this row that reaches the server from outside it. */
export function inheritedGrants(row: AccessRow): ResourceAudienceEntry[] {
  return SCOPE_ROWS.map(({ key }) => row.cells[key].inherited).filter(
    (entry): entry is ResourceAudienceEntry => Boolean(entry),
  );
}

/**
 * The tools a block has to name so that only `selection` is left reachable.
 * Narrowing an organization-wide rule is a subtraction, and a subtraction has
 * to name what it takes away, so this needs the server's whole catalogue.
 */
export function complementTools(
  catalog: ToolSelectionTool[],
  selection: { tools: string[]; dispositions: string[] },
): string[] {
  const tools = new Set(selection.tools);
  const dispositions = new Set(selection.dispositions);
  return catalog
    .filter(
      (tool) =>
        !tools.has(tool.name) &&
        !tool.annotations.some((annotation) => dispositions.has(annotation)),
    )
    .map((tool) => tool.name);
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
    .join(" \u00b7 ");
}
