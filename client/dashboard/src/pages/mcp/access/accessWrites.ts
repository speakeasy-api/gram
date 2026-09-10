import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
import type {
  SetResourceAudienceEntry,
  SetResourceAudienceEntryDispositions,
} from "@gram/client/models/components/setresourceaudienceentry.js";
import {
  BLOCK_LEVEL,
  complementTools,
  inheritedGrants,
  isUnnarrowed,
  reachableTools,
  scopeState,
  SCOPE_ROWS,
  type AccessRow,
  type ScopeKey,
} from "./accessRows";
import {
  ruleId,
  withAdded,
  withRule,
  withoutPrincipal,
  withoutRules,
  type AudienceRule,
} from "./manageAccessState";
import { LEVEL_VERB, narrowingLabel } from "./serverAudience";

/**
 * What one click on the Access list writes. The endpoint replaces every rule
 * naming this server at once, so each decision is "the whole list, changed" —
 * pure enough to test on its own, which is the point of keeping it out of the
 * component.
 *
 * A rule this page does not own — one covering every server, or one belonging
 * to a role this person is in — is never rewritten here. Changing it for this
 * one server is a subtraction instead: a block naming this server, at the
 * level for the scope being taken away. The grant stays where it was, and
 * every other server is untouched.
 */
export interface AudienceWrite {
  entries: SetResourceAudienceEntry[];
  /** What the toast says once the write lands. */
  message: string;
}

/** Tools and annotations the narrowing dialog came back with. */
export interface Narrowing {
  tools: string[];
  dispositions: string[];
}

function serverLabel(resourceName?: string): string {
  return resourceName ?? "this server";
}

/** The rules with this row's own block on `scope` lifted. */
function withoutOwnBlock(
  direct: AudienceRule[],
  row: AccessRow,
  scope: ScopeKey,
): AudienceRule[] {
  if (!row.cells[scope].ownBlock) return direct;
  return withoutRules(direct, [
    ruleId({ principalUrn: row.principalUrn, level: BLOCK_LEVEL[scope] }),
  ]);
}

/**
 * What a write did, said as the change an administrator just made. The rule
 * it was stored as — a grant here, a block against a role's own rule — is an
 * implementation detail of this server's access, so the sentence names the
 * server and leaves the mechanism out.
 */
function reachMessage(row: AccessRow, server: string, reach: string): string {
  return `${row.displayName} can now ${reach} ${server}.`;
}

/** Open one scope over the whole server. */
export function allowWrite(
  direct: AudienceRule[],
  row: AccessRow,
  scope: ScopeKey,
  resourceName?: string,
): AudienceWrite {
  const cell = row.cells[scope];
  const base = withoutOwnBlock(direct, row, scope);
  // Something else may already open this whole. Lifting the block is then
  // the entire edit, and this row's own narrower rule goes with it: a grant
  // alongside an unnarrowed one takes nothing away and only muddies the line.
  const openedElsewhere = cell.grants.some(
    (grant) => grant !== cell.own && isUnnarrowed(grant),
  );
  const entries = openedElsewhere
    ? withoutRules(base, [
        ruleId({ principalUrn: row.principalUrn, level: scope }),
      ])
    : withRule(base, row.principalUrn, scope);

  return {
    entries,
    message: reachMessage(
      row,
      serverLabel(resourceName),
      scope === "use" ? "call every tool on" : LEVEL_VERB[scope],
    ),
  };
}

/**
 * Close one scope. This row's own rule is dropped; anything still granting
 * the scope afterwards — a rule covering every server, a role this person is
 * in, or a stronger scope that satisfies the same check — is subtracted with
 * a block naming this server.
 */
export function revokeScopeWrite(
  direct: AudienceRule[],
  row: AccessRow,
  scope: ScopeKey,
  resourceName?: string,
): AudienceWrite {
  const cell = row.cells[scope];
  const cleared = withoutRules(direct, [
    ruleId({ principalUrn: row.principalUrn, level: scope }),
  ]);
  const server = serverLabel(resourceName);
  const said = `${row.displayName} can no longer ${LEVEL_VERB[scope]} ${server}.`;

  // Everything granting this line that dropping the row's own rule leaves
  // standing. Without a block, the line would still be open.
  const remaining = cell.grants.filter((grant) => grant !== cell.own);
  if (remaining.length === 0) return { entries: cleared, message: said };

  return {
    entries: withRule(cleared, row.principalUrn, BLOCK_LEVEL[scope]),
    message: `${said} Other servers are unchanged.`,
  };
}

/**
 * Limit connect to some tools. When this row's own rule is the only thing
 * granting it, that rule is rewritten narrower. When anything else grants it
 * too, narrowing has to be a subtraction: a block naming everything the
 * choice leaves out, since the other grants would otherwise keep opening it.
 */
export function narrowWrite(
  direct: AudienceRule[],
  row: AccessRow,
  next: Narrowing,
  toolCatalog: ToolSelectionTool[] | undefined,
  resourceName?: string,
): AudienceWrite {
  const base = withoutOwnBlock(direct, row, "use");
  const server = serverLabel(resourceName);
  const label = narrowingLabel(next).toLowerCase();

  if (!scopeState(row, "use", toolCatalog ?? []).subtracts) {
    return {
      // A rule stores tools or annotations, never both — the endpoint
      // refuses the pair — so an explicit list of names wins over the
      // annotations that would have covered them.
      entries: withRule(base, row.principalUrn, "use", {
        tools: next.tools,
        // The annotation values and the stored dispositions are the same
        // strings; the generated union just types them more tightly.
        dispositions: next.tools.length
          ? []
          : (next.dispositions as SetResourceAudienceEntryDispositions[]),
      }),
      message: reachMessage(row, server, `call ${label} on`),
    };
  }

  // Subtracting. A block with nothing named would take the whole server
  // away, so a choice covering everything reachable is the same as asking
  // for all tools.
  const blocked = complementTools(toolCatalog ?? [], next);
  if (blocked.length === 0) return allowWrite(direct, row, "use", resourceName);

  return {
    entries: withRule(base, row.principalUrn, BLOCK_LEVEL.use, {
      tools: blocked,
    }),
    message: `${reachMessage(row, server, `call ${label} on`)} Other servers are unchanged.`,
  };
}

/**
 * Take a principal off this server. The rules it owns here go, and anything
 * still reaching it is subtracted. Blocks are independent, so "no access at
 * all" is all three of them.
 */
export function revokeRowWrite(
  direct: AudienceRule[],
  row: AccessRow,
  resourceName?: string,
): AudienceWrite {
  const cleared = withoutPrincipal(direct, row.principalUrn);
  const server = serverLabel(resourceName);
  if (inheritedGrants(row).length === 0) {
    return {
      entries: cleared,
      message: `${row.displayName} no longer has access to ${server}.`,
    };
  }
  return {
    entries: SCOPE_ROWS.reduce<AudienceRule[]>(
      (entries, { key }) =>
        withRule(entries, row.principalUrn, BLOCK_LEVEL[key]),
      cleared,
    ),
    message: `${row.displayName} no longer has access to ${server}. Other servers are unchanged.`,
  };
}

/** Give people connect access to this server. */
export function addPrincipalsWrite(
  direct: AudienceRule[],
  principalUrns: string[],
  resourceName?: string,
): AudienceWrite {
  const server = serverLabel(resourceName);
  return {
    entries: withAdded(direct, principalUrns),
    message:
      principalUrns.length === 1
        ? `1 person can now connect to ${server}.`
        : `${principalUrns.length} people can now connect to ${server}.`,
  };
}

/**
 * The tools the connect dialog should open on: what this row already reaches,
 * so saving without touching anything cannot widen access past the blocks
 * already trimming it.
 */
export function narrowingSeed(
  row: AccessRow,
  toolCatalog: ToolSelectionTool[] | undefined,
): Narrowing {
  const cell = row.cells.use;
  const reachable = reachableTools(cell, toolCatalog ?? []);
  if (reachable) return { tools: reachable, dispositions: [] };
  // No catalogue to resolve against: the row's own rule is all there is.
  return {
    tools: cell.own?.tools ?? [],
    dispositions: cell.own?.dispositions ?? [],
  };
}
