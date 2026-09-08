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

export const LEVEL_DESCRIPTION: Record<AudienceLevel, string> = {
  use: "Connect to this server and call its tools.",
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

/**
 * How many people a set of rules reaches, when every rule says so. An
 * "everyone" rule has no count of its own, and neither does a rule naming a
 * principal the directory no longer knows, so the total is deliberately
 * absent rather than wrong.
 */
export function reachSummary(entries: ResourceAudienceEntry[]): string {
  if (entries.some((entry) => entry.kind === "everyone")) {
    return "Everyone in the organization";
  }
  const people = entries.filter((entry) => entry.kind === "user").length;
  const groups = entries.filter(
    (entry) => entry.kind !== "user" && entry.kind !== "everyone",
  ).length;
  const parts: string[] = [];
  if (people > 0) parts.push(`${people} ${people === 1 ? "person" : "people"}`);
  if (groups > 0) parts.push(`${groups} ${groups === 1 ? "group" : "groups"}`);
  if (parts.length === 0) return "Nobody yet";
  return parts.join(" and ");
}

/** Option groups for the picker, in the order they are offered. */
export const OPTION_GROUPS: {
  kind: AudienceOption["kind"];
  heading: string;
}[] = [
  { kind: "everyone", heading: "Everyone" },
  { kind: "user", heading: "People" },
  { kind: "directory_group", heading: "Directory groups" },
  { kind: "directory_attribute", heading: "Directory attributes" },
  { kind: "role", heading: "Roles" },
];
