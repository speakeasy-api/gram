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
  level: AudienceLevel;
  /** The rule that decides the level shown. */
  grantedBy: string;
  /** How far the reach goes, worded for a table cell. */
  toolsLabel: string;
  /**
   * A direct rule narrower than what the person already holds. Grants add, so
   * such a rule changes nothing, and saying so beats implying it does.
   */
  ineffective?: { narrowing: string; because: string };
}

/**
 * What a person can actually do here, as the union of every rule that reaches
 * them: the strongest level, and the widest reach any of those rules gives.
 * Reporting only the first matching rule understated one and overstated the
 * other, depending on which arrived first.
 */
export function effectiveReach(
  reaching: ResourceAudienceEntry[],
): EffectiveReach | null {
  const granting = reaching.filter((entry) => entry.level !== "blocked");
  if (granting.length === 0) return null;

  const widest = [...granting].sort(
    (a, b) => levelRank(a.level) - levelRank(b.level),
  )[0]!;

  // One unnarrowed rule opens the whole server; otherwise the reach is the
  // union of what the narrowed ones name.
  const unnarrowed = granting.find(
    (entry) =>
      (entry.tools ?? []).length === 0 &&
      (entry.dispositions ?? []).length === 0,
  );
  const toolsLabel = unnarrowed
    ? "All tools"
    : capitalize(
        narrowingLabel({
          tools: granting.flatMap((entry) => entry.tools ?? []),
          dispositions: granting.flatMap((entry) => entry.dispositions ?? []),
        }),
      );

  const shadowed = granting.find(
    (entry) =>
      entry.appliesTo === "resource" &&
      ((entry.tools ?? []).length > 0 ||
        (entry.dispositions ?? []).length > 0) &&
      unnarrowed !== undefined,
  );

  return {
    level: widest.level,
    grantedBy: widest.displayName,
    toolsLabel,
    ineffective:
      shadowed && unnarrowed
        ? {
            narrowing: narrowingLabel(shadowed),
            because: unnarrowed.displayName,
          }
        : undefined,
  };
}

function levelRank(level: AudienceLevel): number {
  return ["blocked", "manage", "view", "use"].indexOf(level);
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

/** How far a rule reaches inside a server, worded for the row's sentence. */
export function narrowingLabel(entry: {
  tools?: string[];
  dispositions?: string[];
}): string {
  const dispositions = entry.dispositions ?? [];
  if (dispositions.length > 0) {
    const labels = dispositions.map(
      (disposition) => DISPOSITION_LABEL[disposition] ?? disposition,
    );
    return labels.length === 1
      ? `${labels[0]} tools`
      : `${labels.join(", ")} tools`;
  }
  const tools = entry.tools ?? [];
  if (tools.length === 1) return tools[0]!;
  if (tools.length > 1) return `${tools.length} tools`;
  return "all tools";
}

const DISPOSITION_LABEL: Record<string, string> = {
  read_only: "read-only",
  destructive: "destructive",
  idempotent: "idempotent",
  open_world: "open-world",
};
