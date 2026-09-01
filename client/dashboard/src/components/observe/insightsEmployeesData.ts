import { dateTimeFormatters } from "@/lib/dates";
import { mergeUserSummaries } from "@/components/observe/mergeUserSummaries";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";

type EmployeeStatus = "enrolled" | "not_enrolled";

// One linked AI account for an employee. Identity is (provider, email): the same
// email on two providers is two distinct accounts, so provider is always shown.
type EmployeeAccount = {
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

// summaryEmail is the email a summary is keyed under, when it has one.
function summaryEmail(summary: UserSummary): string {
  return (
    summary.userEmail || (summary.userId.includes("@") ? summary.userId : "")
  );
}

// groupSummariesByMember routes each usage summary to the org member it
// belongs to. A member's telemetry can split across identity keys: their
// opaque user_id (e.g. Gram MCP tool calls that carry no email), their
// directory email (Claude/Cursor usage), and linked provider-account emails
// (personal-account usage imports). Match all three — otherwise a token-less,
// email-less id summary shadows the member's token-bearing email summaries,
// understating them while their real usage is orphaned into the unattributed
// list (DNO-618, and the account-email leg for personal accounts).
function groupSummariesByMember(
  members: AccessMember[],
  summaries: UserSummary[],
): {
  groups: { member: AccessMember; matched: UserSummary[] }[];
  unmatched: UserSummary[];
} {
  // Cursor pagination can re-serve the page-boundary group; keep the first
  // instance of each key so duplicates cannot double into any bucket.
  const seenKeys = new Set<string>();
  summaries = summaries.filter((summary) => {
    if (seenKeys.has(summary.userId)) return false;
    seenKeys.add(summary.userId);
    return true;
  });
  const summaryByUserId = new Map(
    summaries.map((summary) => [summary.userId, summary]),
  );
  // All summaries per lowercased email: case-variant keys are the same person.
  const summariesByEmail = new Map<string, UserSummary[]>();
  for (const summary of summaries) {
    const email = summaryEmail(summary).toLowerCase();
    if (!email) continue;
    const list = summariesByEmail.get(email);
    if (list) {
      list.push(summary);
    } else {
      summariesByEmail.set(email, [summary]);
    }
  }

  const matchedSummaryIds = new Set<string>();
  const groups = members.map((member) => {
    const matched = dedupeSummaries([
      summaryByUserId.get(member.id),
      ...(summariesByEmail.get(member.email.toLowerCase()) ?? []),
    ]);
    for (const summary of matched) {
      matchedSummaryIds.add(summary.userId);
    }
    return { member, matched };
  });

  // Second pass: usage under a linked provider-account email keys its own
  // summary and matches no member id or directory email. Every attached
  // account carries its directory owner's user id, so route each leftover
  // summary to that owner's member. An email whose account rows name two
  // different owners is ambiguous — leave its summary unattributed rather
  // than credit one person with another's usage (same refusal as the
  // server's single-owner rule, DNO-509).
  const ownerByAccountEmail = new Map<string, string | null>();
  for (const summary of summaries) {
    for (const account of summary.accounts ?? []) {
      const email = (account.email ?? "").toLowerCase();
      const owner = account.userId ?? "";
      if (!email || !owner) continue;
      const claimed = ownerByAccountEmail.get(email);
      if (claimed === undefined) {
        ownerByAccountEmail.set(email, owner);
      } else if (claimed !== owner) {
        ownerByAccountEmail.set(email, null);
      }
    }
  }
  const groupByMemberId = new Map(groups.map((g) => [g.member.id, g]));
  for (const summary of summaries) {
    if (matchedSummaryIds.has(summary.userId)) continue;
    const email = summaryEmail(summary).toLowerCase();
    const owner = email ? ownerByAccountEmail.get(email) : undefined;
    const group = owner ? groupByMemberId.get(owner) : undefined;
    if (group) {
      group.matched.push(summary);
      matchedSummaryIds.add(summary.userId);
    }
  }

  return {
    groups,
    unmatched: summaries.filter(
      (summary) => !matchedSummaryIds.has(summary.userId),
    ),
  };
}

// foldSummariesForMembers collapses the raw searchUsers result to one summary
// per person for pages that render summaries directly (the agents page): each
// member's matched summaries merge into one keyed by the member id, and
// unmatched summaries pass through as-is.
export function foldSummariesForMembers(
  members: AccessMember[],
  summaries: UserSummary[],
): UserSummary[] {
  const { groups, unmatched } = groupSummariesByMember(members, summaries);
  const folded: UserSummary[] = [];
  for (const { member, matched } of groups) {
    const merged = mergeUserSummaries(matched);
    if (merged) {
      folded.push({
        ...merged,
        userId: member.id,
        userEmail: merged.userEmail || member.email,
      });
    }
  }
  return [...folded, ...unmatched];
}

export function buildEmployees(
  members: AccessMember[],
  roles: Role[],
  summaries: UserSummary[],
): Employee[] {
  const roleNameById = new Map(roles.map((role) => [role.id, role.name]));
  const { groups, unmatched } = groupSummariesByMember(members, summaries);

  const employees = groups.map(({ member, matched }) => {
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
