import { dateTimeFormatters } from "@/lib/dates";
import type {
  AccessMember,
  Role,
  UserSummary,
} from "@gram/client/models/components";

export type EmployeeStatus = "enrolled" | "not_enrolled";

// One linked AI account for an employee. Identity is (provider, email): the same
// email on two providers is two distinct accounts, so provider is always shown.
type EmployeeAccount = {
  email: string;
  provider: string;
  // "team" | "personal" | "" (unclassified).
  accountType: string;
};

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
  // All of this user's linked AI accounts (team + personal, across providers),
  // from the user_accounts directory.
  accounts: EmployeeAccount[];
  // Convenience flag: any account is personal. Drives the account-type filter.
  hasPersonalAccount: boolean;
};

// Maps a user summary's linked accounts (from the directory) into the display
// shape. Tolerant of the field being absent on older payloads.
function accountsFromSummary(
  summary: UserSummary | undefined,
): EmployeeAccount[] {
  return (summary?.accounts ?? []).map((a) => ({
    email: a.email ?? "",
    provider: a.provider,
    accountType: a.accountType ?? "",
  }));
}

// Unattributed identities are usage rows that matched no org member; they are
// marked with a synthetic "usage:"-prefixed id by buildEmployees().
export function isUnattributedEmployee(employee: Employee): boolean {
  return employee.id.startsWith("usage:");
}

// The email a summary should be matched to a member on: the explicit user email
// when present, otherwise the user id when it looks like an email (older payloads
// that grouped opaque and email identities under the same key).
function summaryEmail(summary: UserSummary): string {
  return (
    summary.userEmail || (summary.userId.includes("@") ? summary.userId : "")
  );
}

// Unions the linked accounts across a member's summaries, deduped on the account
// identity (provider, email) so the same account seen on two summaries collapses.
function mergeAccounts(summaries: UserSummary[]): EmployeeAccount[] {
  const byIdentity = new Map<string, EmployeeAccount>();
  for (const summary of summaries) {
    for (const account of accountsFromSummary(summary)) {
      const key = `${account.provider}\u0000${account.email}`;
      if (!byIdentity.has(key)) byIdentity.set(key, account);
    }
  }
  return [...byIdentity.values()];
}

export function buildEmployees(
  members: AccessMember[],
  roles: Role[],
  summaries: UserSummary[],
): Employee[] {
  const roleNameById = new Map(roles.map((role) => [role.id, role.name]));
  const memberById = new Map(members.map((member) => [member.id, member]));
  const memberByEmail = new Map<string, AccessMember>();
  for (const member of members) {
    const key = member.email.toLowerCase();
    // First member wins so ownership stays deterministic on the (rare) case of
    // two members sharing an email.
    if (key && !memberByEmail.has(key)) memberByEmail.set(key, member);
  }

  // Assign every summary to at most one owning member. A single person's usage
  // can be split across multiple summaries — the backend groups by work email
  // when present and by resolved user id otherwise, so sessions that carry an
  // email land in a different bucket from those that don't. Both buckets belong
  // to the same member, so the member must claim all of them; otherwise the
  // leftover surfaces as a phantom "unknown user" for someone already listed as
  // an employee (DNO-380). A user-id match wins over an email match so
  // opaque-id usage keeps its member id.
  const claimedByMember = new Map<string, UserSummary[]>();
  const unmatched: UserSummary[] = [];
  for (const summary of summaries) {
    const email = summaryEmail(summary);
    const owner =
      memberById.get(summary.userId) ??
      (email ? memberByEmail.get(email.toLowerCase()) : undefined);
    if (!owner) {
      unmatched.push(summary);
      continue;
    }
    const claimed = claimedByMember.get(owner.id);
    if (claimed) claimed.push(summary);
    else claimedByMember.set(owner.id, [summary]);
  }

  const employees = members.map((member) => {
    const claimed = claimedByMember.get(member.id) ?? [];
    const status: EmployeeStatus =
      claimed.length > 0 ? "enrolled" : "not_enrolled";
    const tokenCount = claimed.reduce(
      (sum, summary) =>
        sum + summary.totalInputTokens + summary.totalOutputTokens,
      0,
    );
    const latest = claimed.reduce<UserSummary | null>(
      (acc, summary) =>
        acc == null ||
        BigInt(summary.lastSeenUnixNano) > BigInt(acc.lastSeenUnixNano)
          ? summary
          : acc,
      null,
    );
    const accounts = mergeAccounts(claimed);
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
      lastActivityTimestamp: latest
        ? Number(BigInt(latest.lastSeenUnixNano) / 1_000_000n)
        : null,
      lastActivity: latest
        ? formatUnixNano(latest.lastSeenUnixNano)
        : "No activity found",
      accounts,
      hasPersonalAccount: accounts.some((a) => a.accountType === "personal"),
    };
  });

  const unmatchedUsage = unmatched.map((summary) => {
    const tokenCount = summary.totalInputTokens + summary.totalOutputTokens;
    const email = summaryEmail(summary);
    const accounts = accountsFromSummary(summary);
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
      accounts,
      hasPersonalAccount: accounts.some((a) => a.accountType === "personal"),
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
