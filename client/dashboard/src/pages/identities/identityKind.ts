import {
  isUnattributedEmployee,
  type Employee,
} from "@/components/observe/insightsEmployeesData";

/**
 * What sort of subject a row names. The index lists everyone the org has seen,
 * and these three read very differently: a directory member is a colleague, an
 * unclaimed address is a person we cannot yet name, and a bare agent id may not
 * be a person at all.
 */
export type IdentityKind = "person" | "unknown" | "agent";

export const IDENTITY_KIND_LABELS: Record<IdentityKind, string> = {
  person: "Person",
  unknown: "Unattributed",
  agent: "Agent",
};

export function identityKindOf(employee: Employee): IdentityKind {
  if (!isUnattributedEmployee(employee)) return "person";
  // An address is a person we failed to attribute; anything else is an
  // identifier the agent made up for itself.
  return employee.email || employee.name.includes("@") ? "unknown" : "agent";
}

/** The URN the resolver expects for a row, by what the row actually holds. */
export function identityUrnForEmployee(employee: Employee): string {
  if (!isUnattributedEmployee(employee)) return `user:${employee.id}`;
  const usageId = employee.id.slice("usage:".length);
  return employee.email ? `email:${employee.email}` : `external:${usageId}`;
}
