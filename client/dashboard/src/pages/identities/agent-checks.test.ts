import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
import { describe, expect, it } from "vitest";
import {
  checkCountLabel,
  checkResourceLabel,
  groupChecks,
} from "./agent-checks";

const check = (
  scope: string,
  resourceId: string,
  at: string,
  resourceKind = "mcp",
): AuthzChallenge =>
  ({
    id: `${scope}-${resourceId}-${at}`,
    scope,
    resourceKind,
    resourceId,
    outcome: "deny",
    timestamp: new Date(at),
  }) as AuthzChallenge;

describe("groupChecks", () => {
  it("counts each scope on each resource once, newest group first", () => {
    const groups = groupChecks([
      check("mcp:connect", "server_a", "2026-01-01T10:00:00Z"),
      check("mcp:connect", "server_b", "2026-01-01T10:05:00Z"),
      check("mcp:connect", "server_a", "2026-01-01T10:09:00Z"),
      check("mcp:auth", "server_a", "2026-01-01T10:01:00Z"),
    ]);
    expect(groups.map((g) => [g.scope, g.resourceId, g.count])).toEqual([
      ["mcp:connect", "server_a", 2],
      ["mcp:connect", "server_b", 1],
      ["mcp:auth", "server_a", 1],
    ]);
    expect(groups[0]?.latest.toISOString()).toBe("2026-01-01T10:09:00.000Z");
  });

  it("returns no groups for no checks", () => {
    expect(groupChecks([])).toEqual([]);
  });
});

describe("checkResourceLabel", () => {
  it("names the kind and the whole id", () => {
    const [group] = groupChecks([
      check(
        "mcp:connect",
        "00000000-0000-4000-8000-000000000001",
        "2026-01-01T10:00:00Z",
      ),
    ]);
    expect(group && checkResourceLabel(group)).toBe(
      "mcp 00000000-0000-4000-8000-000000000001",
    );
  });

  it("says when a check named no resource", () => {
    const [group] = groupChecks([
      check("org:read", "", "2026-01-01T10:00:00Z", ""),
    ]);
    expect(group && checkResourceLabel(group)).toBe("No resource");
  });
});

describe("checkCountLabel", () => {
  it("words one check and many", () => {
    expect(checkCountLabel("deny", 1)).toBe("Denied once");
    expect(checkCountLabel("allow", 1200)).toBe("Allowed 1,200 times");
  });
});
