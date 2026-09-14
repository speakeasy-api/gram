import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
import type {
  SetResourceAudienceEntry,
  SetResourceAudienceEntryDispositions,
} from "@gram/client/models/components/setresourceaudienceentry.js";
import {
  BLOCK_LEVEL,
  complementTools,
  inheritedGrants,
  foreignGrants,
  isUnnarrowed,
  reachableTools,
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

/**
 * Every annotation a rule can name, mirroring validDispositions in
 * authz/selector.go. The set is closed, which is what makes "only these" and
 * "everything but these" two ways of writing the same restriction.
 */
const DISPOSITIONS: SetResourceAudienceEntryDispositions[] = [
  "read_only",
  "destructive",
  "idempotent",
  "open_world",
];

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
 * The rules a narrowing is written onto, with this row's own connect block
 * lifted only when the choice can speak for it. A choice naming tools says
 * nothing about annotations, so a block naming annotations has to survive it —
 * lifting it would hand back every tool that block was subtracting, which is
 * a widening nobody asked for.
 */
function narrowingBase(
  direct: AudienceRule[],
  row: AccessRow,
  next: Narrowing,
): AudienceRule[] {
  const ownBlock = row.cells.use.ownBlock;
  if (
    ownBlock &&
    (ownBlock.dispositions ?? []).length > 0 &&
    next.tools.length > 0
  ) {
    return direct;
  }
  return withoutOwnBlock(direct, row, "use");
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

/** The catalogue's tools carrying any of these annotations. */
function toolsCarrying(
  catalog: ToolSelectionTool[],
  dispositions: string[],
): string[] {
  const wanted = new Set(dispositions);
  return catalog
    .filter((tool) => tool.annotations.some((a) => wanted.has(a)))
    .map((tool) => tool.name);
}

function sameNames(left: string[], right: string[]): boolean {
  if (left.length !== right.length) return false;
  const seen = new Set(left);
  return right.every((name) => seen.has(name));
}

/**
 * A write that changes nothing, for the choices this form cannot store without
 * losing something already written. A principal holds one rule per level and a
 * rule names tools or annotations but never both, so the two vocabularies
 * cannot be merged — and replacing one with the other is a change nobody asked
 * for. Leaving the rules alone is the honest answer.
 */
function unchangedWrite(direct: AudienceRule[], server: string): AudienceWrite {
  return {
    entries: withoutRules(direct, []),
    message: `Access to ${server} is unchanged.`,
  };
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
  const base = narrowingBase(direct, row, next);
  const server = serverLabel(resourceName);
  const label = narrowingLabel(next).toLowerCase();

  // Nothing chosen: the line reaches nothing, which is the revoke this page
  // already knows how to write. Falling through would store a rule that names
  // nothing, which reads as the opposite.
  if (next.tools.length === 0 && next.dispositions.length === 0) {
    return revokeScopeWrite(direct, row, "use", resourceName);
  }

  // An annotation choice is always stored as a block on the annotations left
  // out, never as a grant naming the ones kept. The two are not the same: a
  // grant reaches only the tools carrying one of its annotations, so a tool
  // annotated with nothing at all would silently stop being reachable, while
  // the block leaves it alone. The block also keeps covering tools added
  // later, which is the reason to restrict by annotation rather than by name,
  // and it needs no catalogue — the form gateways and remote servers depend
  // on, since they resolve their tools per caller and publish none.
  if (next.tools.length === 0) {
    const blockedDispositions = DISPOSITIONS.filter(
      (disposition) => !next.dispositions.includes(disposition),
    );
    // Every annotation chosen is "all tools" said the long way.
    if (blockedDispositions.length === 0) {
      return allowWrite(direct, row, "use", resourceName);
    }
    return {
      entries: withRule(base, row.principalUrn, BLOCK_LEVEL.use, {
        dispositions: blockedDispositions,
      }),
      message: `${reachMessage(row, server, `call ${label} on`)} Other servers are unchanged.`,
    };
  }

  if (foreignGrants(row, "use").length === 0) {
    return {
      // A rule stores tools or annotations, never both — the endpoint refuses
      // the pair — and an annotation choice never reaches here, so this is
      // always the list of names.
      entries: withRule(base, row.principalUrn, "use", {
        tools: next.tools,
        dispositions: [],
      }),
      message: reachMessage(row, server, `call ${label} on`),
    };
  }

  // Subtracting by name needs the catalogue to say what to take away, and
  // without one the complement is unknowable — an empty one would read as
  // "block nothing" and fall through to an allow, lifting whatever block is
  // already standing. So the block is left exactly as it is and only this
  // row's own grant is rewritten, which cannot widen anything.
  if ((toolCatalog ?? []).length === 0) {
    // This row's own rule may be written in annotations while another grant
    // names tools. With no catalogue there is nothing to translate between the
    // two, so rewriting this rule as names would drop the annotations it holds
    // without saying so.
    if ((row.cells.use.own?.dispositions ?? []).length > 0) {
      return unchangedWrite(direct, server);
    }
    return {
      // `direct`, not `base`: without a catalogue the subtraction cannot be
      // recomputed, so no block on this line may be lifted, whatever it names.
      entries: withRule(direct, row.principalUrn, "use", {
        tools: next.tools,
        dispositions: [],
      }),
      message: reachMessage(row, server, `call ${label} on`),
    };
  }

  // Subtracting by name. A block with nothing named would take the whole
  // server away, so a choice covering everything reachable is the same as
  // asking for all tools.
  const blocked = complementTools(toolCatalog ?? [], next);
  if (blocked.length === 0) return allowWrite(direct, row, "use", resourceName);

  // The subtraction is stored at the block level, so writing it replaces any
  // block already there. Replacing an annotation block with the names it
  // happens to cover today would quietly stop it covering the ones added
  // tomorrow, which is the whole reason it was written as an annotation. When
  // the choice takes away exactly what the annotation already takes away — an
  // untouched dialog, saved — the annotation stays.
  const ownAnnotations = row.cells.use.ownBlock?.dispositions ?? [];
  if (
    ownAnnotations.length > 0 &&
    sameNames(blocked, toolsCarrying(toolCatalog ?? [], ownAnnotations))
  ) {
    return unchangedWrite(direct, server);
  }

  return {
    entries: withRule(base, row.principalUrn, BLOCK_LEVEL.use, {
      tools: blocked,
    }),
    message: `${reachMessage(row, server, `call ${label} on`)} Other servers are unchanged.`,
  };
}

/**
 * Keep a principal off this server's destructive tools, and let it back on.
 *
 * Stored as a block naming the annotation rather than the tools carrying it
 * today: a name list stops covering a destructive tool added next week, which
 * is the whole reason to restrict by annotation. It also needs no catalogue, so
 * it works on gateways and remote servers, which resolve their tools per
 * caller and publish none.
 */
export function blockDestructiveWrite(
  direct: AudienceRule[],
  row: AccessRow,
  resourceName?: string,
): AudienceWrite {
  return {
    entries: withRule(direct, row.principalUrn, BLOCK_LEVEL.use, {
      dispositions: ["destructive"],
    }),
    message: `${row.displayName} can no longer call destructive tools on ${serverLabel(resourceName)}.`,
  };
}

export function allowDestructiveWrite(
  direct: AudienceRule[],
  row: AccessRow,
  resourceName?: string,
): AudienceWrite {
  return {
    entries: withoutRules(direct, [
      ruleId({ principalUrn: row.principalUrn, level: BLOCK_LEVEL.use }),
    ]),
    message: `${row.displayName} can call destructive tools on ${serverLabel(resourceName)} again.`,
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
  // Named by what was actually added: the picker grants people and agents
  // separately, and "1 person" is wrong for either an agent or a mixed set.
  const agents = principalUrns.filter((principalUrn) =>
    principalUrn.startsWith("agent:"),
  ).length;
  const noun =
    agents === principalUrns.length
      ? principalUrns.length === 1
        ? "1 agent"
        : `${principalUrns.length} agents`
      : agents === 0
        ? principalUrns.length === 1
          ? "1 person"
          : `${principalUrns.length} people`
        : `${principalUrns.length} principals`;
  return {
    entries: withAdded(direct, principalUrns),
    message: `${noun} can now connect to ${server}.`,
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

  // Names come from every grant reaching this row, not only the rule it owns
  // here: a rule covering every server can be narrowed to names too, and it is
  // just as much what this row reaches. Seeding from the own rule alone left
  // an inherited name list with nothing chosen, and saving that revoked it.
  // The names a block takes away come off, or saving an untouched dialog would
  // hand them straight back.
  const blockedTools = new Set(
    cell.blocks.flatMap((block) => block.tools ?? []),
  );
  const grantedTools = [
    ...new Set(cell.grants.flatMap((grant) => grant.tools ?? [])),
  ].filter((tool) => !blockedTools.has(tool));
  if (grantedTools.length > 0) return { tools: grantedTools, dispositions: [] };

  // No catalogue to resolve against, so the seed is said in the vocabulary the
  // blocks are written in: everything the grants open, minus every annotation a
  // block takes away. Reading the grant alone would open the dialog with
  // nothing chosen on a row whose line plainly reads "all tools except
  // destructive tools" — and saving that would revoke the line.
  const blocked = new Set(
    cell.blocks.flatMap((block) => block.dispositions ?? []),
  );
  const granted = cell.grants.some(isUnnarrowed)
    ? DISPOSITIONS
    : [...new Set(cell.grants.flatMap((grant) => grant.dispositions ?? []))];
  return {
    tools: [],
    dispositions: granted.filter(
      (disposition) => !blocked.has(disposition),
    ) as string[],
  };
}
