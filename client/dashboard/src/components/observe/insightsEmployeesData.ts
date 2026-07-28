import { dateTimeFormatters } from "@/lib/dates";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";

export type EmployeeStatus = "enrolled" | "not_enrolled";

// One linked AI account for an employee. Identity is (provider, email): the same
// email on two providers is two distinct accounts, so provider is always shown.
export type EmployeeAccount = {
  email: string;
  provider: string;
  // "team" | "personal" | "" (unclassified).
  accountType: string;
  // Latest activity for this account in Unix nanoseconds (string for JS int64
  // precision); null when the directory has no last-seen recorded for it.
  lastSeenUnixNano: string | null;
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
  // The linked account with the latest activity — identifies the workspace the
  // employee was last working in. Null when no account has a last-seen.
  mostRecentAccount: EmployeeAccount | null;
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
    lastSeenUnixNano: a.lastSeenUnixNano ?? null,
  }));
}

// The account with the latest recorded activity, compared at full nanosecond
// precision. Accounts the directory has no last-seen for can't be ranked and
// are skipped.
function mostRecentAccount(
  accounts: EmployeeAccount[],
): EmployeeAccount | null {
  let latest: EmployeeAccount | null = null;
  for (const account of accounts) {
    if (account.lastSeenUnixNano == null) continue;
    if (
      latest?.lastSeenUnixNano == null ||
      BigInt(account.lastSeenUnixNano) > BigInt(latest.lastSeenUnixNano)
    ) {
      latest = account;
    }
  }
  return latest;
}

// dedupeSummaries returns the distinct, present summaries among the candidates.
// A member can match the same summary by both id and email, and can match two
// different summaries when their telemetry splits across identity keys.
function dedupeSummaries(
  candidates: (UserSummary | undefined)[],
): UserSummary[] {
  const out: UserSummary[] = [];
  for (const summary of candidates) {
    if (summary && !out.includes(summary)) out.push(summary);
  }
  return out;
}

// mostRecentSummary picks the matched summary with the latest activity, used for
// a member's displayed last-activity when their usage spans multiple summaries.
function mostRecentSummary(summaries: UserSummary[]): UserSummary | undefined {
  let latest: UserSummary | undefined;
  for (const summary of summaries) {
    if (
      !latest ||
      BigInt(summary.lastSeenUnixNano) > BigInt(latest.lastSeenUnixNano)
    ) {
      latest = summary;
    }
  }
  return latest;
}

// mergeAccounts unions the linked accounts across a member's matched summaries,
// deduping by (provider, email) and keeping the most-recently-active instance.
function mergeAccounts(summaries: UserSummary[]): EmployeeAccount[] {
  const byKey = new Map<string, EmployeeAccount>();
  for (const summary of summaries) {
    for (const account of accountsFromSummary(summary)) {
      const key = JSON.stringify([
        account.provider,
        account.email.toLowerCase(),
      ]);
      const existing = byKey.get(key);
      if (
        !existing ||
        (account.lastSeenUnixNano != null &&
          (existing.lastSeenUnixNano == null ||
            BigInt(account.lastSeenUnixNano) >
              BigInt(existing.lastSeenUnixNano)))
      ) {
        byKey.set(key, account);
      }
    }
  }
  return [...byKey.values()];
}

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
    summaries
      .map((summary) => {
        const email =
          summary.userEmail ||
          (summary.userId.includes("@") ? summary.userId : "");
        return email ? ([email.toLowerCase(), summary] as const) : null;
      })
      .filter(
        (entry): entry is readonly [string, UserSummary] => entry != null,
      ),
  );
  const matchedSummaryIds = new Set<string>();

  const employees = members.map((member) => {
    // A member's telemetry can split across identity keys: their opaque user_id
    // (e.g. Gram MCP tool calls that carry no email) and their email
    // (Claude/Cursor usage). Match BOTH and merge — otherwise a token-less,
    // email-less id summary shadows the member's token-bearing email summary,
    // showing them enrolled with 0 tokens while their real usage is orphaned
    // into the unattributed list (DNO-618; regression of the DNO-468 merge).
    const matched = dedupeSummaries([
      summaryByUserId.get(member.id),
      summaryByEmail.get(member.email.toLowerCase()),
    ]);
    for (const summary of matched) {
      matchedSummaryIds.add(summary.userId);
    }
    const status: EmployeeStatus =
      matched.length > 0 ? "enrolled" : "not_enrolled";
    const tokenCount = matched.reduce(
      (sum, summary) =>
        sum +
        (summary.totalInputTokens ?? 0) +
        (summary.totalOutputTokens ?? 0),
      0,
    );
    // Display fields (last activity) come from the most-recent matched summary.
    const primary = mostRecentSummary(matched);
    const role =
      member.roleIds
        .map((id) => roleNameById.get(id))
        .filter(Boolean)
        .join(", ") || "Unknown";
    const accounts = mergeAccounts(matched);

    return {
      id: member.id,
      name: member.name,
      email: member.email,
      role,
      status,
      tokenCount,
      photoUrl: member.photoUrl,
      lastActivityTimestamp: primary
        ? Number(BigInt(primary.lastSeenUnixNano) / 1_000_000n)
        : null,
      lastActivity: primary
        ? formatUnixNano(primary.lastSeenUnixNano)
        : "No activity found",
      accounts,
      mostRecentAccount: mostRecentAccount(accounts),
      hasPersonalAccount: accounts.some((a) => a.accountType === "personal"),
    };
  });

  const unmatchedUsage = summaries
    .filter((summary) => !matchedSummaryIds.has(summary.userId))
    .map((summary) => {
      const tokenCount = summary.totalInputTokens + summary.totalOutputTokens;
      const email =
        summary.userEmail ||
        (summary.userId.includes("@") ? summary.userId : "");
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
        mostRecentAccount: mostRecentAccount(accounts),
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
