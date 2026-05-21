import { describe, expect, it } from "vitest";
import { Operator } from "@gram/client/models/components";
import type { AccessMember } from "@gram/client/models/components";
import {
  buildLogFilters,
  mergeFilterChip,
  resolveRoleEmails,
} from "./observeFilterUtils";

describe("buildLogFilters", () => {
  it("uses exact in matching for a single curated filter value", () => {
    const result = buildLogFilters([
      {
        display: "api",
        filters: ["api"],
        path: "gram.tool_call.source",
      },
    ]);

    expect(result).toEqual([
      {
        path: "gram.tool_call.source",
        operator: Operator.In,
        values: ["api"],
      },
    ]);
  });

  it("groups multiple curated filter values by path with in matching", () => {
    const result = buildLogFilters([
      {
        display: "api",
        filters: ["api"],
        path: "gram.tool_call.source",
      },
      {
        display: "staging",
        filters: ["api-staging"],
        path: "gram.tool_call.source",
      },
      {
        display: "alex@example.com",
        filters: ["alex@example.com"],
        path: "user.email",
      },
    ]);

    expect(result).toEqual([
      {
        path: "gram.tool_call.source",
        operator: Operator.In,
        values: ["api", "api-staging"],
      },
      {
        path: "user.email",
        operator: Operator.In,
        values: ["alex@example.com"],
      },
    ]);
  });

  it("returns undefined when no filters are active", () => {
    expect(buildLogFilters([])).toBeUndefined();
  });
});

describe("mergeFilterChip", () => {
  it("merges a partially overlapping chip for the same path", () => {
    const result = mergeFilterChip(
      [
        {
          display: "api",
          filters: ["api"],
          path: "gram.tool_call.source",
        },
      ],
      {
        display: "API group",
        filters: ["api", "api-v2"],
        path: "gram.tool_call.source",
      },
    );

    expect(result).toEqual({
      filters: [
        {
          display: "api, api-v2",
          filters: ["api", "api-v2"],
          path: "gram.tool_call.source",
        },
      ],
      merged: {
        display: "api, api-v2",
        filters: ["api", "api-v2"],
        path: "gram.tool_call.source",
      },
    });
  });

  it("skips only when the incoming chip is fully subsumed", () => {
    const activeFilters = [
      {
        display: "api, api-v2",
        filters: ["api", "api-v2"],
        path: "gram.tool_call.source",
      },
    ];

    expect(
      mergeFilterChip(activeFilters, {
        display: "api",
        filters: ["api"],
        path: "gram.tool_call.source",
      }),
    ).toEqual({ filters: activeFilters, merged: null });
  });
});

describe("resolveRoleEmails", () => {
  const members: AccessMember[] = [
    {
      id: "m1",
      email: "alice@example.com",
      name: "Alice",
      roleId: "role-admin",
      joinedAt: new Date(),
      photoUrl: undefined,
    },
    {
      id: "m2",
      email: "bob@example.com",
      name: "Bob",
      roleId: "role-member",
      joinedAt: new Date(),
      photoUrl: undefined,
    },
    {
      id: "m3",
      email: "carol@example.com",
      name: "Carol",
      roleId: "role-admin",
      joinedAt: new Date(),
      photoUrl: undefined,
    },
  ];

  it("returns emails for members matching the selected role IDs", () => {
    expect(resolveRoleEmails(["role-admin"], members)).toEqual([
      "alice@example.com",
      "carol@example.com",
    ]);
  });

  it("unions emails across multiple selected roles", () => {
    expect(resolveRoleEmails(["role-admin", "role-member"], members)).toEqual([
      "alice@example.com",
      "bob@example.com",
      "carol@example.com",
    ]);
  });

  it("returns empty array when no roles selected", () => {
    expect(resolveRoleEmails([], members)).toEqual([]);
  });

  it("returns empty array when members list is empty", () => {
    expect(resolveRoleEmails(["role-admin"], [])).toEqual([]);
  });
});

describe("buildLogFilters with role emails", () => {
  it("injects role emails as a user.email in filter when no existing user filter", () => {
    const result = buildLogFilters(
      [],
      ["alice@example.com", "carol@example.com"],
    );
    expect(result).toEqual([
      {
        path: "user.email",
        operator: Operator.In,
        values: ["alice@example.com", "carol@example.com"],
      },
    ]);
  });

  it("merges role emails with explicit user email filter (deduplicates)", () => {
    const result = buildLogFilters(
      [
        {
          display: "alice@example.com",
          filters: ["alice@example.com"],
          path: "user.email",
        },
      ],
      ["alice@example.com", "carol@example.com"],
    );
    expect(result).toEqual([
      {
        path: "user.email",
        operator: Operator.In,
        values: ["alice@example.com", "carol@example.com"],
      },
    ]);
  });

  it("returns undefined when both activeFilters and roleEmails are empty", () => {
    expect(buildLogFilters([], [])).toBeUndefined();
  });
});
