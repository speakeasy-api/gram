import type { AudienceOption } from "@gram/client/models/components/audienceoption.js";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import {
  Globe,
  Shield,
  Tag,
  User,
  UsersRound,
  type LucideIcon,
} from "lucide-react";

/**
 * The per-server slice of access control, read the way an administrator thinks
 * about it: who can use this server. A rule names a principal — everyone, a
 * role, a person, a directory group, or a directory attribute value — and the
 * level it gives them. Rules that name this server are edited here; rules that
 * cover every server are inherited and shown read-only.
 */

export type AudienceLevel = ResourceAudienceEntry["level"];

/** The verbs capitalized, for a row that names a level rather than uses it. */
const LEVEL_MENU_LABEL: Record<AudienceLevel, string> = {
  use: "Connect",
  view: "View",
  manage: "Manage",
  blocked: "Never connect",
  blocked_view: "Never view",
  blocked_manage: "Never manage",
};

/** The level as a verb, for rows that read "Can connect to all tools". */
export const LEVEL_VERB: Record<AudienceLevel, string> = {
  use: "connect",
  view: "view",
  manage: "manage",
  blocked: "never connect",
  blocked_view: "never view",
  blocked_manage: "never manage",
};

const KIND_ICON: Record<string, LucideIcon> = {
  everyone: Globe,
  role: Shield,
  user: User,
  directory_group: UsersRound,
  directory_attribute: Tag,
  unknown: User,
};

export function audienceIcon(kind: string): LucideIcon {
  return KIND_ICON[kind] ?? User;
}

/** Rules edited on this page: the ones whose selector names this server. */
export function ownRules(
  entries: ResourceAudienceEntry[],
): ResourceAudienceEntry[] {
  return entries.filter((entry) => entry.appliesTo === "resource");
}

/** Option groups for the picker, in the order they are offered. */
export const OPTION_GROUPS: {
  kind: AudienceOption["kind"];
  heading: string;
}[] = [
  { kind: "everyone", heading: "Everyone" },
  { kind: "user", heading: "People" },
  { kind: "role", heading: "Roles" },
];

export interface EffectiveReach {
  /**
   * How much of the server this person can call. Only connect reaches
   * individual tools; view and manage are about the server itself, and both
   * satisfy a connect check, so an unnarrowed rule at any level opens every
   * tool.
   */
  toolsLabel: string;
  /** Everything the person can do here, weakest first. */
  capabilities: AudienceLevel[];
  /** Stronger levels held only over some tools, e.g. "Manage on 2 tools". */
  scopedLevels: { id: string; label: string }[];
  /** Narrowed blocks, which subtract from the reach above. */
  excluded: { id: string; label: string }[];
  /** Every rule that reaches them. */
  grantedBy: string[];
  /**
   * The tools the label counts, so "3 tools" can name them on hover. Empty
   * when the count came from somewhere other than the catalogue.
   */
  reachableTools: string[];
}

/** The capability a block level takes away. */
const BLOCKED_CAPABILITY: Partial<Record<AudienceLevel, AudienceLevel>> = {
  blocked: "use",
  blocked_view: "view",
  blocked_manage: "manage",
};

function isBlock(entry: { level: AudienceLevel }): boolean {
  return BLOCKED_CAPABILITY[entry.level] !== undefined;
}

export function effectiveReach(
  reaching: ResourceAudienceEntry[],
  /** The server's tool names, when it publishes a catalogue. */
  catalog: string[] = [],
): EffectiveReach | null {
  const granting = reaching.filter((entry) => !isBlock(entry));
  if (granting.length === 0) return null;

  const isNarrowed = (entry: ResourceAudienceEntry) =>
    (entry.tools ?? []).length > 0 || (entry.dispositions ?? []).length > 0;

  // Blocks are independent of one another, so each takes away just the
  // capability it names — and only an unnarrowed one takes it away whole. A
  // block narrowed to some tools leaves the rest reachable.
  const blocked = new Set(
    reaching
      .filter((entry) => isBlock(entry) && !isNarrowed(entry))
      .map((entry) => BLOCKED_CAPABILITY[entry.level]!),
  );

  const unnarrowed = granting.find((entry) => !isNarrowed(entry));
  // Tools taken away by a narrowed block on connect. This page narrows a
  // rule it does not own by subtracting from it, so the reachable set is the
  // catalogue minus these — and that is what a reader wants named, not the
  // subtraction itself.
  const trimmed = [
    ...new Set(
      reaching
        .filter(
          (entry) =>
            BLOCKED_CAPABILITY[entry.level] === "use" && isNarrowed(entry),
        )
        .flatMap((entry) => entry.tools ?? []),
    ),
  ];
  const removed = new Set(trimmed);
  const reachable = catalog.filter((tool) => !removed.has(tool));
  const countable =
    Boolean(unnarrowed) && catalog.length > 0 && trimmed.length > 0;
  const remaining = reachable.length;

  const toolsLabel = blocked.has("use")
    ? // Tool access is about connecting; without it there are no tools to
      // reach, whatever view and manage still allow.
      "None"
    : countable
      ? remaining <= 0
        ? "None"
        : remaining === 1
          ? "1 tool"
          : `${remaining} tools`
      : unnarrowed
        ? "All tools"
        : capitalize(
            narrowingLabel({
              tools: granting.flatMap((entry) => entry.tools ?? []),
              dispositions: granting.flatMap(
                (entry) => entry.dispositions ?? [],
              ),
            }),
          );

  // A capability held over every tool, versus one held over a few: the second
  // is worth naming separately rather than implying it everywhere.
  const wholeServer = granting.filter((entry) => !isNarrowed(entry));
  const capabilities = [
    ...new Set(
      (wholeServer.length > 0 ? wholeServer : granting).flatMap((entry) =>
        capabilitiesOf(entry.level),
      ),
    ),
  ].filter((capability) => !blocked.has(capability));
  // Everything this server offered them has been taken away, so they do not
  // reach it at all.
  if (capabilities.length === 0) return null;
  // Only a narrowed rule that adds something the unrestricted rules do not
  // already give: "Manage on 2 tools" says nothing to someone who manages the
  // whole server already.
  const alreadyHeld = new Set(
    wholeServer.flatMap((entry) => capabilitiesOf(entry.level)),
  );
  const scopedLevels = granting
    .filter(
      (entry) =>
        isNarrowed(entry) &&
        entry.level !== "use" &&
        !alreadyHeld.has(entry.level),
    )
    .map((entry) => ({
      id: `${entry.principalUrn}|${entry.appliesTo}|${entry.level}`,
      label: `${LEVEL_MENU_LABEL[entry.level]} on ${narrowingLabel(entry)}`,
    }));

  return {
    toolsLabel,
    reachableTools: countable && !blocked.has("use") ? reachable : [],
    capabilities,
    scopedLevels,
    // Only the blocks that trim tools. One that removes a capability whole
    // is already absent from `capabilities` and from `toolsLabel`, so naming
    // it again would say the same thing twice in worse words.
    // Once the remaining tools can be counted, the label says the whole
    // answer and the subtraction behind it is noise.
    excluded: (countable ? [] : reaching)
      .filter((entry) => isBlock(entry) && isNarrowed(entry))
      .map((entry) => ({
        // A principal can hold a block here and another covering every
        // server; both belong on the row and need to be told apart.
        id: `${entry.principalUrn}|${entry.appliesTo}|${entry.level}`,
        label: narrowingLabel(entry),
      })),
    // Two principals can share a display name, and dropping one would hide a
    // rule that is genuinely reaching this person.
    grantedBy: [
      ...new Map(
        granting.map((entry) => [entry.principalUrn, entry.displayName]),
      ).values(),
    ],
  };
}

/** Weakest first, so a row reads "connect, view" rather than "view, connect". */
function capabilitiesOf(level: AudienceLevel): AudienceLevel[] {
  switch (level) {
    case "manage":
      return ["use", "view", "manage"];
    case "view":
      return ["use", "view"];
    case "use":
      return ["use"];
    case "blocked":
    case "blocked_view":
    case "blocked_manage":
      // A block permits nothing; it only subtracts.
      return [];
  }
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

/** How far a rule reaches inside a server, worded for the row's sentence. */
export function narrowingLabel(entry: {
  tools?: string[];
  dispositions?: string[];
}): string {
  const dispositions = (entry.dispositions ?? []).map(
    (disposition) => DISPOSITION_LABEL[disposition] ?? disposition,
  );
  const tools = entry.tools ?? [];

  // A rule stores one or the other, but a summary can hold both — from two
  // rules — and naming only the annotations would hide the tools.
  const parts: string[] = [];
  if (dispositions.length > 0) parts.push(`${dispositions.join(", ")} tools`);
  if (tools.length === 1) parts.push(tools[0]!);
  else if (tools.length > 1) parts.push(`${tools.length} tools`);

  if (parts.length === 0) return "all tools";
  return parts.join(" and ");
}

const DISPOSITION_LABEL: Record<string, string> = {
  read_only: "read-only",
  destructive: "destructive",
  idempotent: "idempotent",
  open_world: "open-world",
};
