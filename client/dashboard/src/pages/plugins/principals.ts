import type { FacepileMember } from "@/components/member-facepile";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { PluginAudience } from "@gram/client/models/components/pluginaudience.js";
import type { PluginAssignment } from "@gram/client/models/components/pluginassignment.js";
import type { Role } from "@gram/client/models/components/role.js";
import { Globe, Mail, Shield, Tag, User, UsersRound } from "lucide-react";
import { z } from "zod";

// A plugin assignment targets a principal identified by a URN. The agent's
// getPlugins resolves these to decide which plugins reach a device:
//   *                    — everyone in the org (wildcard)
//   user:all             — every org member (subject-set)
//   email:<addr>         — a specific email
//   user:<id>            — a specific member
//   role:<kind>:<id>     — every member of a role
export const WILDCARD_PRINCIPAL = "*";

const EMAIL_PREFIX = "email:";
const ROLE_PREFIX = "role:";
const USER_PREFIX = "user:";
const DIRECTORY_GROUP_PREFIX = "directory_group:";
const DIRECTORY_ATTRIBUTE_PREFIX = "directory_attribute:";

export type PrincipalKind =
  | "everyone"
  | "email"
  | "role"
  | "user"
  | "directory_group"
  | "directory_attribute"
  | "unknown";

export function isEveryoneAssignmentPrincipal(urn: string): boolean {
  return urn === WILDCARD_PRINCIPAL || urn === "user:all";
}

// The wildcard reaches every synced identity and therefore subsumes every
// targeted audience. `user:all` is intentionally excluded: it reaches only
// synced identities that resolve to organization members, so it can coexist
// with email assignments for non-members.
export function selectMutuallyExclusivePluginAudiences(
  previous: string[],
  next: string[],
): string[] {
  const previousValues = new Set(previous);
  const addedValues = next.filter((value) => !previousValues.has(value));
  const addedWildcard = addedValues.find(
    (value) => value === WILDCARD_PRINCIPAL,
  );

  if (addedWildcard) return [addedWildcard];
  if (addedValues.some((value) => value !== WILDCARD_PRINCIPAL)) {
    return next.filter((value) => value !== WILDCARD_PRINCIPAL);
  }
  return next;
}

const principalKindIcon: Record<
  PrincipalKind,
  React.ComponentType<{ className?: string }>
> = {
  everyone: Globe,
  email: Mail,
  role: Shield,
  user: User,
  directory_group: UsersRound,
  directory_attribute: Tag,
  unknown: User,
};

export function principalIcon(
  kind: PrincipalKind,
): React.ComponentType<{ className?: string }> {
  return principalKindIcon[kind];
}

// describePrincipal resolves a principal URN to a display kind + label, using
// the role/member lookups so user:/role: URNs render as human names rather than
// opaque ids. Unresolvable URNs fall back to the raw URN so nothing is hidden.
export function describePrincipal(
  urn: string,
  roleByUrn: Map<string, Role>,
  memberByUrn: Map<string, AccessMember>,
  audienceByUrn: Map<string, PluginAudience> = new Map(),
): { kind: PrincipalKind; label: string } {
  if (urn === WILDCARD_PRINCIPAL)
    return { kind: "everyone", label: "Everyone" };
  if (urn === "user:all") return { kind: "everyone", label: "All users" };
  if (urn.startsWith(EMAIL_PREFIX)) {
    return { kind: "email", label: urn.slice(EMAIL_PREFIX.length) };
  }
  const audience = audienceByUrn.get(urn);
  if (audience) {
    return { kind: audience.kind, label: audience.displayName };
  }
  if (urn.startsWith(DIRECTORY_GROUP_PREFIX)) {
    return {
      kind: "directory_group",
      label: `Unavailable directory group (${urn})`,
    };
  }
  if (urn.startsWith(DIRECTORY_ATTRIBUTE_PREFIX)) {
    return {
      kind: "directory_attribute",
      label: `Unavailable directory attribute (${urn})`,
    };
  }
  if (urn.startsWith("role:")) {
    return { kind: "role", label: roleByUrn.get(urn)?.name ?? urn };
  }
  if (urn.startsWith("user:")) {
    const member = memberByUrn.get(urn);
    return { kind: "user", label: member?.name || member?.email || urn };
  }
  return { kind: "unknown", label: urn };
}

// An individually-assigned member is a "user:<id>" principal — but not the
// "user:all" subject-set, which describes everyone rather than a single person.
export function isIndividualMemberPrincipal(urn: string): boolean {
  return urn.startsWith("user:") && urn !== "user:all";
}

// Email principals target one person too, but they are not necessarily current
// organization members. They remain separately identifiable so existing email
// assignments can be preserved without converting their delivery semantics.
export function isIndividualUserAssignmentPrincipal(urn: string): boolean {
  return isIndividualMemberPrincipal(urn) || urn.startsWith(EMAIL_PREFIX);
}

// Resolve a plugin's individually-assigned members to facepile entries. Role,
// email, and everyone principals are excluded — only "user:<id>" assignments
// map to a specific person's avatar.
export function individualMemberFacepile(
  assignments: PluginAssignment[],
  memberByUrn: Map<string, AccessMember>,
): FacepileMember[] {
  return individualMemberFacepileForUrns(
    assignments.map((assignment) => assignment.principalUrn),
    memberByUrn,
  );
}

export function individualMemberFacepileForUrns(
  principalUrns: string[],
  memberByUrn: Map<string, AccessMember>,
): FacepileMember[] {
  return principalUrns
    .filter(isIndividualMemberPrincipal)
    .map((principalUrn) => {
      const member = memberByUrn.get(principalUrn);
      return {
        id: member?.id ?? principalUrn,
        name: member?.name || member?.email || "Unknown member",
        email: member?.email ?? "",
        photoUrl: member?.photoUrl,
      };
    });
}

export function roleMapByUrn(roles: Role[]): Map<string, Role> {
  return new Map(roles.map((r) => [r.principalUrn, r]));
}

export function memberMapByUrn(
  members: AccessMember[],
): Map<string, AccessMember> {
  return new Map(members.map((m) => [m.principalUrn, m]));
}

export function audienceMapByUrn(
  audiences: PluginAudience[],
): Map<string, PluginAudience> {
  return new Map(
    audiences.map((audience) => [audience.principalUrn, audience]),
  );
}

export function memberCountDescription(
  memberCount: number | undefined,
): string | undefined {
  if (memberCount === undefined) return undefined;
  return `${memberCount} ${memberCount === 1 ? "member" : "members"}`;
}

export function audienceKindForPrincipal(
  urn: string,
  audienceByUrn: Map<string, PluginAudience>,
): PluginAudience["kind"] | undefined {
  const audience = audienceByUrn.get(urn);
  if (audience) return audience.kind;
  if (isEveryoneAssignmentPrincipal(urn)) return "everyone";
  if (urn.startsWith(ROLE_PREFIX)) return "role";
  if (urn.startsWith(DIRECTORY_GROUP_PREFIX)) return "directory_group";
  if (urn.startsWith(DIRECTORY_ATTRIBUTE_PREFIX)) return "directory_attribute";
  return undefined;
}

const emailSchema = z.string().email();

// normalizeToPrincipalUrn canonicalizes a raw picker value into a principal URN
// suitable for setPluginAssignments, or null when it is neither a known URN nor
// a valid email. Bare values typed into the picker are treated as emails.
export function normalizeToPrincipalUrn(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return null;
  if (trimmed === WILDCARD_PRINCIPAL) return trimmed;
  // role:/user: URNs are type:id — require a non-empty id after the prefix so a
  // malformed "role:" or "user:" fails local validation instead of being sent
  // to setPluginAssignments, where the server rejects the empty id with only a
  // generic mutation error.
  if (
    (trimmed.startsWith(ROLE_PREFIX) && trimmed.length > ROLE_PREFIX.length) ||
    (trimmed.startsWith(USER_PREFIX) && trimmed.length > USER_PREFIX.length) ||
    (trimmed.startsWith(DIRECTORY_GROUP_PREFIX) &&
      trimmed.length > DIRECTORY_GROUP_PREFIX.length) ||
    (trimmed.startsWith(DIRECTORY_ATTRIBUTE_PREFIX) &&
      trimmed.length > DIRECTORY_ATTRIBUTE_PREFIX.length)
  ) {
    return trimmed;
  }
  // Validate the address whether or not the email: prefix is already present, so
  // a typo like "email:not-an-address" can't be saved as a dead assignment.
  const bare = trimmed.startsWith(EMAIL_PREFIX)
    ? trimmed.slice(EMAIL_PREFIX.length)
    : trimmed;
  const email = bare.toLowerCase();
  return emailSchema.safeParse(email).success
    ? `${EMAIL_PREFIX}${email}`
    : null;
}
