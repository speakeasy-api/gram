import { dateTimeFormatters } from "@/lib/dates";
import type {
  AccessMember,
  Role,
  UserSummary,
} from "@gram/client/models/components";

export type EmployeeStatus = "enrolled" | "not_enrolled";

export type Employee = {
  id: string;
  name: string;
  email: string;
  role: string;
  status: EmployeeStatus;
  tokenCount: number;
  lastActivity: string;
  lastActivityTimestamp: number | null;
  photoUrl?: string | null;
};

// Unattributed identities are usage rows that matched no org member; they are
// marked with a synthetic "usage:"-prefixed id by buildEmployees().
export function isUnattributedEmployee(employee: Employee): boolean {
  return employee.id.startsWith("usage:");
}

export function buildEmployees(
  members: AccessMember[],
  roles: Role[],
  summaries: UserSummary[],
): Employee[] {
  const roleNameById = new Map(roles.map((role) => [role.id, role.name]));
  const summaryByUserId = new Map(
    summaries.map((summary) => [summary.userId, summary]),
  );
  const summaryByEmail = new Map(
    summaries.flatMap((summary) => {
      const emails = [summary.userEmail, summary.userId].filter((value) =>
        value.includes("@"),
      );
      return emails.map((email) => [email.toLowerCase(), summary] as const);
    }),
  );
  const matchedSummaryIds = new Set<string>();

  const employees = members.map((member) => {
    const summary =
      summaryByUserId.get(member.id) ??
      summaryByEmail.get(member.email.toLowerCase());
    if (summary) {
      matchedSummaryIds.add(summary.userId);
    }
    const tokenCount =
      (summary?.totalInputTokens ?? 0) + (summary?.totalOutputTokens ?? 0);
    const status: EmployeeStatus =
      summary != null ? "enrolled" : "not_enrolled";
    const role =
      member.roleIds
        .map((id) => roleNameById.get(id))
        .filter(Boolean)
        .join(", ") || "Unknown";

    return {
      id: member.id,
      name: member.name,
      email: member.email,
      role,
      status,
      tokenCount,
      photoUrl: member.photoUrl,
      lastActivityTimestamp: summary
        ? Number(BigInt(summary.lastSeenUnixNano) / 1_000_000n)
        : null,
      lastActivity: summary
        ? formatUnixNano(summary.lastSeenUnixNano)
        : "No activity found",
    };
  });

  const unmatchedUsage = summaries
    .filter((summary) => !matchedSummaryIds.has(summary.userId))
    .map((summary) => {
      const tokenCount = summary.totalInputTokens + summary.totalOutputTokens;
      const email =
        summary.userEmail ||
        (summary.userId.includes("@") ? summary.userId : "");
      return {
        id: `usage:${summary.userId}`,
        name: email || summary.userId,
        email,
        role: "-",
        status: "not_enrolled" as const,
        tokenCount,
        photoUrl: null,
        lastActivityTimestamp: Number(
          BigInt(summary.lastSeenUnixNano) / 1_000_000n,
        ),
        lastActivity: formatUnixNano(summary.lastSeenUnixNano),
      };
    });

  return [...employees, ...unmatchedUsage].sort((a, b) => {
    if (a.status !== b.status) {
      return a.status === "not_enrolled" ? -1 : 1;
    }

    return a.name.localeCompare(b.name);
  });
}

function formatUnixNano(value: string) {
  const nanos = BigInt(value);
  const millis = Number(nanos / 1_000_000n);

  return dateTimeFormatters.humanize(new Date(millis));
}
