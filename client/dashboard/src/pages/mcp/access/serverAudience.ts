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

export const GRANTABLE_LEVELS = ["use", "view", "manage"] as const;

export const LEVEL_LABEL: Record<AudienceLevel, string> = {
  use: "Use",
  view: "View",
  manage: "Manage",
  blocked: "No access",
};

/** The level as a verb, for rows that read "Can connect to all tools". */
export const LEVEL_VERB: Record<AudienceLevel, string> = {
  use: "connect",
  view: "view",
  manage: "manage",
  blocked: "never connect",
};

/** The same verbs, capitalized, for the menu that picks a level. */
export const LEVEL_MENU_LABEL: Record<AudienceLevel, string> = {
  use: "Connect",
  view: "View",
  manage: "Manage",
  blocked: "Never connect",
};

export const LEVEL_DESCRIPTION: Record<AudienceLevel, string> = {
  use: "Call this server's tools.",
  view: "See this server and its configuration in Gram.",
  manage: "Edit this server's configuration. Includes view and use.",
  blocked:
    "Subtracts access, whatever else grants it. Narrow it to take away only some tools.",
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

/** Rules owned by the organization-wide Access page, shown here read-only. */
export function inheritedRules(
  entries: ResourceAudienceEntry[],
): ResourceAudienceEntry[] {
  return entries.filter((entry) => entry.appliesTo === "all_resources");
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
  /** A narrower rule that changes nothing, and what already covers it. */
  ineffective?: { narrowing: string; because: string };
}

export function effectiveReach(
  reaching: ResourceAudienceEntry[],
): EffectiveReach | null {
  const granting = reaching.filter((entry) => entry.level !== "blocked");
  if (granting.length === 0) return null;

  const isNarrowed = (entry: ResourceAudienceEntry) =>
    (entry.tools ?? []).length > 0 || (entry.dispositions ?? []).length > 0;

  const unnarrowed = granting.find((entry) => !isNarrowed(entry));
  const toolsLabel = unnarrowed
    ? "All tools"
    : capitalize(
        narrowingLabel({
          tools: granting.flatMap((entry) => entry.tools ?? []),
          dispositions: granting.flatMap((entry) => entry.dispositions ?? []),
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
  ];
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
      id: `${entry.principalUrn}|${entry.level}`,
      label: `${LEVEL_MENU_LABEL[entry.level]} on ${narrowingLabel(entry)}`,
    }));

  // Grants add, so a narrow rule alongside an unnarrowed one takes nothing
  // away — worth saying, since the Access list shows it as a limit.
  const shadowed = granting.find(
    (entry) => isNarrowed(entry) && entry.appliesTo === "resource",
  );

  return {
    toolsLabel,
    capabilities,
    scopedLevels,
    excluded: reaching
      .filter((entry) => entry.level === "blocked")
      .map((entry) => ({
        id: entry.principalUrn,
        label: narrowingLabel(entry),
      })),
    // Two principals can share a display name, and dropping one would hide a
    // rule that is genuinely reaching this person.
    grantedBy: [
      ...new Map(
        granting.map((entry) => [entry.principalUrn, entry.displayName]),
      ).values(),
    ],
    ineffective:
      shadowed && unnarrowed
        ? {
            narrowing: narrowingLabel(shadowed),
            because: unnarrowed.displayName,
          }
        : undefined,
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
