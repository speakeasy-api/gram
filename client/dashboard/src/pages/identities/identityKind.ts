import {
  isUnattributedEmployee,
  type Employee,
} from "@/components/observe/insightsEmployeesData";

/**
 * What sort of subject a row names.
 *
 * A person is a person whether or not this platform has an account for them:
 * an address we cannot match to a member is still someone's address, and
 * showing it as a third species reads as a judgement on them rather than as a
 * gap in our records. Whether they hold an account is a separate fact — see
 * {@link identityHasAccount} — shown as its own affordance.
 */
export type IdentityKind = "person" | "agent" | "unknown";

export const IDENTITY_KIND_LABELS: Record<IdentityKind, string> = {
  person: "Person",
  agent: "Agent",
  unknown: "Unknown",
};

export function identityKindOf(employee: Employee): IdentityKind {
  if (employee.registeredAgentId) return "agent";
  if (!isUnattributedEmployee(employee)) return "person";
  // A bare telemetry identifier is not evidence of a registered agent.
  return employee.email || employee.name.includes("@") ? "person" : "unknown";
}

/**
 * Whether this identity resolves to a member of the organization. False for a
 * person whose activity we see but who holds no account here — a colleague not
 * yet synced, a contractor, or an address that does not match their member
 * record.
 */
export function identityHasAccount(employee: Employee): boolean {
  return !employee.registeredAgentId && !isUnattributedEmployee(employee);
}

/** The URN the resolver expects for a row, by what the row actually holds. */
export function identityUrnForEmployee(employee: Employee): string {
  if (!isUnattributedEmployee(employee)) return `user:${employee.id}`;
  const usageId = employee.id.slice("usage:".length);
  return employee.email ? `email:${employee.email}` : `external:${usageId}`;
}
