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
  blocked: "Cannot reach this server, whatever else grants them access.",
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
