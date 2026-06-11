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

  it("creates unattributed rows with a usage: id for unmatched summaries", () => {
    const employees = buildEmployees([], noRoles, [
      makeSummary({ userId: "ghost@example.com" }),
    ]);

    expect(employees).toHaveLength(1);
    expect(employees[0]!.id).toBe("usage:ghost@example.com");
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
