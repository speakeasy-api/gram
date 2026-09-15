import { describe, expect, it } from "vitest";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";
import { mergeUserSummaries } from "./mergeUserSummaries";

function makeSummary(
  overrides: Partial<UserSummary> & Pick<UserSummary, "userId">,
): UserSummary {
  return {
    avgTokensPerRequest: 0,
    cacheCreationInputTokens: 0,
    cacheReadInputTokens: 0,
    firstSeenUnixNano: "1750000000000000000",
    hookSources: [],
    lastSeenUnixNano: "1750000000000000000",
    toolCallFailure: 0,
    toolCallSuccess: 0,
    tools: [],
    totalChatRequests: 0,
    totalChats: 0,
    totalCost: 0,
    totalInputTokens: 0,
    totalOutputTokens: 0,
    totalTokens: 0,
    totalToolCalls: 0,
    userEmail: "",
    ...overrides,
  };
}

describe("mergeUserSummaries", () => {
  it("returns null for no summaries and the summary itself for one", () => {
    expect(mergeUserSummaries([])).toBeNull();
    const only = makeSummary({ userId: "ada@example.com" });
    expect(mergeUserSummaries([only])).toBe(only);
  });

  it("folds a person's split summaries into one view", () => {
    // One person's rows key several summaries: work email, personal account
    // email, bare user id. The page renders a single view, so totals sum,
    // breakdowns merge, and accounts union.
    const merged = mergeUserSummaries([
      makeSummary({
        userId: "ada@example.com",
        userEmail: "ada@example.com",
        firstSeenUnixNano: "1750000000000000000",
        lastSeenUnixNano: "1750000300000000000",
        totalInputTokens: 700,
        totalOutputTokens: 20,
        totalTokens: 720,
        totalChatRequests: 2,
        totalCost: 3,
        tools: [
          {
            urn: "tools:http:petstore:listPets",
            count: 2,
            successCount: 2,
            failureCount: 0,
          },
        ],
        hookSources: [{ source: "claude-code", eventCount: 5 }],
        accounts: [
          {
            provider: "anthropic",
            email: "ada@example.com",
            accountType: "team",
          },
        ],
        accountTypes: ["team"],
      }),
      makeSummary({
        userId: "ada.personal@gmail.com",
        userEmail: "ada.personal@gmail.com",
        firstSeenUnixNano: "1749999000000000000",
        lastSeenUnixNano: "1750000100000000000",
        totalInputTokens: 300,
        totalOutputTokens: 60,
        totalTokens: 360,
        totalChatRequests: 1,
        totalCost: 1.5,
        tools: [
          {
            urn: "tools:http:petstore:listPets",
            count: 1,
            successCount: 0,
            failureCount: 1,
          },
          {
            urn: "tools:http:petstore:getPet",
            count: 3,
            successCount: 3,
            failureCount: 0,
          },
        ],
        hookSources: [{ source: "codex", eventCount: 2 }],
        accounts: [
          {
            provider: "anthropic",
            email: "Ada.Personal@gmail.com",
            accountType: "personal",
          },
        ],
        accountTypes: ["personal"],
      }),
    ]);

    expect(merged).not.toBeNull();
    expect(merged!.totalTokens).toBe(1080);
    expect(merged!.totalCost).toBe(4.5);
    expect(merged!.avgTokensPerRequest).toBe(360);
    expect(merged!.firstSeenUnixNano).toBe("1749999000000000000");
    expect(merged!.lastSeenUnixNano).toBe("1750000300000000000");
    expect(merged!.tools).toEqual([
      {
        urn: "tools:http:petstore:listPets",
        count: 3,
        successCount: 2,
        failureCount: 1,
      },
      {
        urn: "tools:http:petstore:getPet",
        count: 3,
        successCount: 3,
        failureCount: 0,
      },
    ]);
    expect(merged!.hookSources).toEqual([
      { source: "claude-code", eventCount: 5 },
      { source: "codex", eventCount: 2 },
    ]);
    expect(merged!.accounts?.map((a) => a.email)).toEqual([
      "ada@example.com",
      "Ada.Personal@gmail.com",
    ]);
    expect(merged!.accountTypes).toEqual(["team", "personal"]);
  });

  it("collapses a page-boundary duplicate before summing", () => {
    const merged = mergeUserSummaries([
      makeSummary({ userId: "ada@example.com", totalInputTokens: 100 }),
      makeSummary({ userId: "ada@example.com", totalInputTokens: 100 }),
      makeSummary({ userId: "user-id-1", totalInputTokens: 40 }),
    ]);

    expect(merged!.totalInputTokens).toBe(140);
  });

  it("does not double-count an account listed on two summaries", () => {
    const account = {
      provider: "anthropic",
      email: "ada@example.com",
      accountType: "team",
    };
    const merged = mergeUserSummaries([
      makeSummary({ userId: "ada@example.com", accounts: [account] }),
      makeSummary({ userId: "user-id-1", accounts: [{ ...account }] }),
    ]);

    expect(merged!.accounts).toHaveLength(1);
  });
});
