import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import { Globe, Mail, Shield, User } from "lucide-react";
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

export type PrincipalKind = "everyone" | "email" | "role" | "user" | "unknown";

const principalKindIcon: Record<
  PrincipalKind,
  React.ComponentType<{ className?: string }>
> = {
  everyone: Globe,
  email: Mail,
  role: Shield,
  user: User,
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
): { kind: PrincipalKind; label: string } {
  if (urn === WILDCARD_PRINCIPAL)
    return { kind: "everyone", label: "Everyone" };
  if (urn === "user:all") return { kind: "everyone", label: "All users" };
  if (urn.startsWith(EMAIL_PREFIX)) {
    return { kind: "email", label: urn.slice(EMAIL_PREFIX.length) };
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

export function roleMapByUrn(roles: Role[]): Map<string, Role> {
  return new Map(roles.map((r) => [r.principalUrn, r]));
}

export function memberMapByUrn(
  members: AccessMember[],
): Map<string, AccessMember> {
  return new Map(members.map((m) => [m.principalUrn, m]));
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
    (trimmed.startsWith(USER_PREFIX) && trimmed.length > USER_PREFIX.length)
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
