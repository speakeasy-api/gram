import type { UserSummary } from "@gram/client/models/components/usersummary.js";

// One person's rows can key several summaries (work email, personal account
// emails, bare user id); the server widens the userIds filter to all of them,
// and this folds the returned summaries back into the single view the employee
// detail page renders. Mirrors the per-member merge the employees list does
// client-side.
export function mergeUserSummaries(users: UserSummary[]): UserSummary | null {
  const [base, ...rest] = users;
  if (base == null) return null;
  if (rest.length === 0) return base;

  const toolsByUrn = new Map(base.tools.map((t) => [t.urn, { ...t }]));
  const hooksBySource = new Map(
    base.hookSources.map((h) => [h.source, { ...h }]),
  );
  const accountsByKey = new Map(
    (base.accounts ?? []).map((a) => [
      `${a.provider}:${(a.email ?? "").toLowerCase()}`,
      a,
    ]),
  );
  const accountTypes = new Set(base.accountTypes ?? []);

  // sort=desc puts the latest-active summary first; it seeds identity fields.
  // Chat counts are per-key uniq counts, so a session spanning two identity
  // keys sums twice here; the canonical fold collapses the keys upstream.
  const merged: UserSummary = { ...base };
  for (const summary of rest) {
    merged.totalChats += summary.totalChats;
    merged.totalChatRequests += summary.totalChatRequests;
    merged.totalInputTokens += summary.totalInputTokens;
    merged.totalOutputTokens += summary.totalOutputTokens;
    merged.totalTokens += summary.totalTokens;
    merged.cacheReadInputTokens += summary.cacheReadInputTokens;
    merged.cacheCreationInputTokens += summary.cacheCreationInputTokens;
    merged.totalCost += summary.totalCost;
    merged.totalToolCalls += summary.totalToolCalls;
    merged.toolCallSuccess += summary.toolCallSuccess;
    merged.toolCallFailure += summary.toolCallFailure;
    if (BigInt(summary.firstSeenUnixNano) < BigInt(merged.firstSeenUnixNano)) {
      merged.firstSeenUnixNano = summary.firstSeenUnixNano;
    }
    if (BigInt(summary.lastSeenUnixNano) > BigInt(merged.lastSeenUnixNano)) {
      merged.lastSeenUnixNano = summary.lastSeenUnixNano;
    }
    if (!merged.userEmail && summary.userEmail) {
      merged.userEmail = summary.userEmail;
    }
    for (const tool of summary.tools) {
      const existing = toolsByUrn.get(tool.urn);
      if (existing) {
        existing.count += tool.count;
        existing.successCount += tool.successCount;
        existing.failureCount += tool.failureCount;
      } else {
        toolsByUrn.set(tool.urn, { ...tool });
      }
    }
    for (const hook of summary.hookSources) {
      const existing = hooksBySource.get(hook.source);
      if (existing) {
        existing.eventCount += hook.eventCount;
      } else {
        hooksBySource.set(hook.source, { ...hook });
      }
    }
    for (const account of summary.accounts ?? []) {
      const key = `${account.provider}:${(account.email ?? "").toLowerCase()}`;
      if (!accountsByKey.has(key)) accountsByKey.set(key, account);
    }
    for (const accountType of summary.accountTypes ?? []) {
      accountTypes.add(accountType);
    }
  }

  merged.tools = [...toolsByUrn.values()];
  merged.hookSources = [...hooksBySource.values()];
  merged.accounts = [...accountsByKey.values()];
  merged.accountTypes = [...accountTypes];
  merged.avgTokensPerRequest =
    merged.totalChatRequests > 0
      ? merged.totalTokens / merged.totalChatRequests
      : 0;

  return merged;
}
