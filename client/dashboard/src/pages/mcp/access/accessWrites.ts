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
 * A rule covering every server belongs to the role editor and is never
 * rewritten here. Editing it for this one server is a subtraction instead: a
 * block naming this server, at the level for the scope being taken away. The
 * grant stays where it was, and every other server is untouched.
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

/** The rules with the block on `scope` lifted. Each block stands alone. */
function withoutBlocks(
  direct: AudienceRule[],
  row: AccessRow,
  scope: ScopeKey,
): AudienceRule[] {
  if (!row.blocks[scope]) return direct;
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

/**
 * Whether some other rule already opens this scope over the whole server: an
 * organization-wide rule at this scope, or an unnarrowed grant at a stronger
 * one, which satisfies the same check. The row's own rule at `scope` is left
 * out, since that is the rule being widened.
 */
function alreadyOpen(row: AccessRow, scope: ScopeKey): boolean {
  // A block trimming this line subtracts from every grant that would
  // otherwise satisfy it, including one held at a stronger scope, so nothing
  // opens it whole while that block stands.
  if (row.cells[scope].trimmedBy ?? row.cells[scope].via?.block) return false;
  return satisfying(scope).some((key) => {
    const cell = row.cells[key];
    // A grant reaching through a role opens the whole server only if that
    // role is not itself trimmed by a block on this server.
    const via = cell.via?.block ? undefined : cell.via?.entry;
    const rules =
      key === scope
        ? [cell.inherited, via]
        : [cell.direct, cell.inherited, via];
    return rules.some((rule) => rule && isUnnarrowed(rule));
  });
}

/** The scopes whose grant satisfies a check for `scope`, `scope` included. */
function satisfying(scope: ScopeKey): ScopeKey[] {
  const from = SCOPE_ROWS.findIndex((row) => row.key === scope);
  return SCOPE_ROWS.slice(from).map((row) => row.key);
}

/** Open one scope over the whole server. */
export function allowWrite(
  direct: AudienceRule[],
  row: AccessRow,
  scope: ScopeKey,
  resourceName?: string,
): AudienceWrite {
  const base = withoutBlocks(direct, row, scope);
  // When something already opens the whole server, lifting the block is the
  // whole edit — and this row's own narrower rule goes with it, since a grant
  // alongside an unnarrowed one takes nothing away and only muddies the line.
  const entries = alreadyOpen(row, scope)
    ? withoutRules(base, [
        ruleId({ principalUrn: row.principalUrn, level: scope }),
      ])
    : withRule(base, row.principalUrn, scope);
  return {
    entries,
    message: reachMessage(
      row,
      serverLabel(resourceName),
      scope === "use" ? "call every tool on" : `${LEVEL_VERB[scope]}`,
    ),
  };
}

/**
 * Close one scope. A rule naming this server is dropped; anything still
 * reaching afterwards — an organization-wide rule, or a stronger scope that
 * satisfies this check — is subtracted with a block naming this server.
 */
export function revokeScopeWrite(
  direct: AudienceRule[],
  row: AccessRow,
  scope: ScopeKey,
  resourceName?: string,
): AudienceWrite {
  const cleared = withoutRules(direct, [
    ruleId({ principalUrn: row.principalUrn, level: scope }),
  ]);
  const server = serverLabel(resourceName);
  const said = `${row.displayName} can no longer ${LEVEL_VERB[scope]} ${server}.`;
  if (!stillReaches(row, scope)) {
    return { entries: cleared, message: said };
  }

  // Written as a block, because the grant belongs to a rule covering every
  // server — which is worth saying, since it is the surprising half.
  return {
    entries: withRule(cleared, row.principalUrn, BLOCK_LEVEL[scope]),
    message: `${said} Other servers are unchanged.`,
  };
}

/**
 * Whether dropping this row's own rule at `scope` would leave the scope open
 * anyway: an organization-wide rule at that scope, or any stronger scope this
 * page is not clearing.
 */
function stillReaches(row: AccessRow, scope: ScopeKey): boolean {
  const cell = row.cells[scope];
  // `via` counts: a role this person is in opens the scope for them, so
  // dropping their own rule leaves it open and a block is what closes it.
  return Boolean(cell.inherited ?? cell.impliedBy ?? cell.via);
}

/**
 * Limit connect to some tools. A rule naming this server is rewritten
 * narrower; an organization-wide one can only be subtracted from, so it is
 * written as a block naming everything the choice leaves out.
 */
export function narrowWrite(
  direct: AudienceRule[],
  row: AccessRow,
  next: Narrowing,
  toolCatalog: ToolSelectionTool[] | undefined,
  resourceName?: string,
): AudienceWrite {
  if (!scopeState(row, "use").subtracts) {
    return {
      entries: withRule(
        withoutBlocks(direct, row, "use"),
        row.principalUrn,
        "use",
        {
          tools: next.tools,
          // The annotation values and the stored dispositions are the same
          // strings; the generated union just types them more tightly.
          dispositions:
            next.dispositions as SetResourceAudienceEntryDispositions[],
        },
      ),
      message: reachMessage(
        row,
        serverLabel(resourceName),
        `call ${narrowingLabel(next).toLowerCase()} on`,
      ),
    };
  }

  // Subtracting: the rule reaching this server covers every server, so the
  // only per-server edit is a block naming the tools left out. A block with
  // nothing named would take the whole server away, so an empty complement is
  // the same as asking for all tools.
  const blocked = complementTools(toolCatalog ?? [], next);
  if (blocked.length === 0) return allowWrite(direct, row, "use", resourceName);

  return {
    entries: withRule(
      withoutBlocks(direct, row, "use"),
      row.principalUrn,
      BLOCK_LEVEL.use,
      { tools: blocked },
    ),
    message: `${reachMessage(
      row,
      serverLabel(resourceName),
      `call ${narrowingLabel(next).toLowerCase()} on`,
    )} Other servers are unchanged.`,
  };
}

/**
 * Take a principal off this server. The rules naming it go, and anything
 * still reaching from an organization-wide rule is subtracted. Blocks are
 * independent, so "no access at all" is all three of them.
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
