import { describe, expect, it } from "vitest";
import type {
  AccessMember,
  Role,
  UserSummary,
} from "@gram/client/models/components";
import {
  buildEmployees,
  isUnattributedEmployee,
} from "./insightsEmployeesData";

const LAST_SEEN_UNIX_NANO = "1750000000000000000";

function makeMember(
  overrides: Partial<AccessMember> &
    Pick<AccessMember, "id" | "email" | "name">,
): AccessMember {
  return {
    joinedAt: new Date("2025-01-01T00:00:00Z"),
    principalUrn: `user:${overrides.id}`,
    roleIds: [],
    ...overrides,
  };
}

function makeSummary(
  overrides: Partial<UserSummary> & Pick<UserSummary, "userId">,
): UserSummary {
  return {
    avgTokensPerRequest: 0,
    cacheCreationInputTokens: 0,
    cacheReadInputTokens: 0,
    firstSeenUnixNano: LAST_SEEN_UNIX_NANO,
    hookSources: [],
    lastSeenUnixNano: LAST_SEEN_UNIX_NANO,
    toolCallFailure: 0,
    toolCallSuccess: 0,
    tools: [],
    totalChatRequests: 0,
    totalChats: 0,
    totalCost: 0,
    totalInputTokens: 100,
    totalOutputTokens: 50,
    totalTokens: 150,
    totalToolCalls: 0,
    userEmail: "",
    ...overrides,
  };
}

const noRoles: Role[] = [];

describe("buildEmployees attributed/unattributed split", () => {
  it("keeps member ids for usage matched by user id", () => {
    const member = makeMember({
      id: "member-1",
      email: "ada@example.com",
      name: "Ada Lovelace",
    });
    const employees = buildEmployees([member], noRoles, [
      makeSummary({ userId: "member-1" }),
    ]);

    expect(employees).toHaveLength(1);
    expect(employees[0]!.id).toBe("member-1");
    expect(employees[0]!.status).toBe("enrolled");
    expect(isUnattributedEmployee(employees[0]!)).toBe(false);
  });

  it("matches usage to members by email, case-insensitively", () => {
    const member = makeMember({
      id: "member-1",
      email: "Ada@Example.com",
      name: "Ada Lovelace",
    });
    const employees = buildEmployees([member], noRoles, [
      makeSummary({ userId: "ada@example.com" }),
    ]);

    expect(employees).toHaveLength(1);
    expect(employees[0]!.id).toBe("member-1");
    expect(employees[0]!.status).toBe("enrolled");
    expect(isUnattributedEmployee(employees[0]!)).toBe(false);
  });

  it("matches usage to members by user email when the user id is opaque", () => {
    const member = makeMember({
      id: "member-1",
      email: "Ada@Example.com",
      name: "Ada Lovelace",
    });
    const employees = buildEmployees([member], noRoles, [
      makeSummary({
        userId: "01924a0eb409b0ecf44e06d0ec03cbc4",
        userEmail: "ada@example.com",
      }),
    ]);

    expect(employees).toHaveLength(1);
    expect(employees[0]!.id).toBe("member-1");
    expect(employees[0]!.status).toBe("enrolled");
    expect(isUnattributedEmployee(employees[0]!)).toBe(false);
  });

  it("does not attribute one usage summary to multiple member emails", () => {
    const primaryMember = makeMember({
      id: "member-1",
      email: "primary@example.com",
      name: "Primary User",
    });
    const aliasMember = makeMember({
      id: "member-2",
      email: "alias@example.com",
      name: "Alias User",
    });

    const employees = buildEmployees([primaryMember, aliasMember], noRoles, [
      makeSummary({
        userId: "alias@example.com",
        userEmail: "primary@example.com",
      }),
    ]);

    const primary = employees.find((employee) => employee.id === "member-1")!;
    const alias = employees.find((employee) => employee.id === "member-2")!;

    expect(primary.status).toBe("enrolled");
    expect(primary.tokenCount).toBe(150);
    expect(alias.status).toBe("not_enrolled");
    expect(alias.tokenCount).toBe(0);
  });

  it("creates unattributed rows with a usage: id for unmatched summaries", () => {
    const employees = buildEmployees([], noRoles, [
      makeSummary({ userId: "ghost@example.com" }),
    ]);

    expect(employees).toHaveLength(1);
    expect(employees[0]!.id).toBe("usage:ghost@example.com");
    expect(isUnattributedEmployee(employees[0]!)).toBe(true);
  });

  it("displays user email for unattributed summaries with opaque ids", () => {
    const employees = buildEmployees([], noRoles, [
      makeSummary({
        userId: "01924a0eb409b0ecf44e06d0ec03cbc4",
        userEmail: "ghost@example.com",
      }),
    ]);

    expect(employees).toHaveLength(1);
    expect(employees[0]!.id).toBe("usage:01924a0eb409b0ecf44e06d0ec03cbc4");
    expect(employees[0]!.name).toBe("ghost@example.com");
    expect(employees[0]!.email).toBe("ghost@example.com");
    expect(isUnattributedEmployee(employees[0]!)).toBe(true);
  });

  it("marks unattributed rows as not enrolled", () => {
    const employees = buildEmployees([], noRoles, [
      makeSummary({ userId: "ghost@example.com" }),
    ]);

    expect(employees[0]!.status).toBe("not_enrolled");
  });

  it("partitions a mixed list cleanly into the two views", () => {
    const members = [
      makeMember({
        id: "member-1",
        email: "ada@example.com",
        name: "Ada Lovelace",
      }),
      makeMember({
        id: "member-2",
        email: "grace@example.com",
        name: "Grace Hopper",
      }),
    ];
    const summaries = [
      makeSummary({ userId: "member-1" }),
      makeSummary({ userId: "ghost@example.com" }),
      makeSummary({ userId: "external-user-42" }),
    ];

    const employees = buildEmployees(members, noRoles, summaries);
    const attributed = employees.filter((e) => !isUnattributedEmployee(e));
    const unattributed = employees.filter(isUnattributedEmployee);

    expect(employees).toHaveLength(4);
    expect(attributed.map((e) => e.id).sort()).toEqual([
      "member-1",
      "member-2",
    ]);
    expect(unattributed.map((e) => e.id).sort()).toEqual([
      "usage:external-user-42",
      "usage:ghost@example.com",
    ]);
    // Member without activity stays attributed, just not enrolled.
    expect(attributed.find((e) => e.id === "member-2")!.status).toBe(
      "not_enrolled",
    );
  });
});

describe("buildEmployees most recent account", () => {
  const member = makeMember({
    id: "member-1",
    email: "ada@example.com",
    name: "Ada Lovelace",
  });

  it("picks the linked account with the latest last-seen", () => {
    const employees = buildEmployees([member], noRoles, [
      makeSummary({
        userId: "member-1",
        accounts: [
          {
            provider: "anthropic",
            email: "ada@example.com",
            accountType: "team",
            lastSeenUnixNano: "1750000000000000000",
          },
          {
            provider: "anthropic",
            email: "ada@personal.com",
            accountType: "personal",
            lastSeenUnixNano: "1760000000000000000",
          },
        ],
      }),
    ]);

    expect(employees[0]!.mostRecentAccount?.email).toBe("ada@personal.com");
    expect(employees[0]!.mostRecentAccount?.accountType).toBe("personal");
  });

  it("ranks accounts at full nanosecond precision", () => {
    // Both timestamps fall in the same millisecond; the ranking must not
    // truncate precision or the first account would win the tie.
    const employees = buildEmployees([member], noRoles, [
      makeSummary({
        userId: "member-1",
        accounts: [
          {
            provider: "anthropic",
            email: "ada@example.com",
            accountType: "team",
            lastSeenUnixNano: "1750000000000364000",
          },
          {
            provider: "cursor",
            email: "ada@personal.com",
            accountType: "personal",
            lastSeenUnixNano: "1750000000000400000",
          },
        ],
      }),
    ]);

    expect(employees[0]!.mostRecentAccount?.provider).toBe("cursor");
  });

  it("skips accounts without a last-seen when ranking", () => {
    const employees = buildEmployees([member], noRoles, [
      makeSummary({
        userId: "member-1",
        accounts: [
          {
            provider: "openai",
            email: "ada@example.com",
            accountType: "team",
          },
          {
            provider: "anthropic",
            email: "ada@example.com",
            accountType: "team",
            lastSeenUnixNano: "1750000000000000000",
          },
        ],
      }),
    ]);

    expect(employees[0]!.mostRecentAccount?.provider).toBe("anthropic");
  });

  it("returns null when no account has a last-seen", () => {
    const employees = buildEmployees([member], noRoles, [
      makeSummary({
        userId: "member-1",
        accounts: [
          {
            provider: "anthropic",
            email: "ada@example.com",
            accountType: "team",
          },
        ],
      }),
    ]);

    expect(employees[0]!.mostRecentAccount).toBeNull();
  });

  it("returns null when there are no linked accounts", () => {
    const employees = buildEmployees([member], noRoles, [
      makeSummary({ userId: "member-1" }),
    ]);

    expect(employees[0]!.mostRecentAccount).toBeNull();
  });
});
