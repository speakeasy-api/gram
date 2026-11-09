import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
import { describe, expect, it } from "vitest";
import { checkCountLabel, checksFooter, groupChecks } from "./agent-checks";

const check = (
  scope: string,
  resourceId: string,
  at: string,
  resourceKind = "mcp",
  extra: Partial<AuthzChallenge> = {},
): AuthzChallenge =>
  ({
    id: `${scope}-${resourceId}-${at}`,
    scope,
    resourceKind,
    resourceId,
    operation: "require",
    outcome: "deny",
    timestamp: new Date(at),
    ...extra,
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

  it("keeps the newest check's project, so the resource can be named", () => {
    const [group] = groupChecks([
      check("environment:read", "env_one", "2026-01-01T10:09:00Z", "env", {
        projectId: "project_new",
      }),
      check("environment:read", "env_one", "2026-01-01T10:00:00Z", "env", {
        projectId: "project_old",
      }),
    ]);
    expect(group?.projectId).toBe("project_new");
  });

  it("marks a tool list filtering pass, which names no scope or resource", () => {
    const groups = groupChecks([
      check("", "", "2026-01-01T10:00:00Z", "", { operation: "filter" }),
      check("org:read", "", "2026-01-01T10:01:00Z", "", {
        operation: "filter",
      }),
      check("", "", "2026-01-01T10:02:00Z", ""),
    ]);
    expect(groups.map((g) => [g.scope, g.toolListFilter])).toEqual([
      ["", false],
      ["org:read", false],
      ["", true],
    ]);
  });

  it("returns no groups for no checks", () => {
    expect(groupChecks([])).toEqual([]);
  });
});

describe("checkCountLabel", () => {
  it("words one check and many", () => {
    expect(checkCountLabel("deny", 1)).toBe("Denied once");
    expect(checkCountLabel("allow", 1200)).toBe("Allowed 1,200 times");
  });

  it("reads a count from a cut-off slice as a lower bound", () => {
    expect(checkCountLabel("deny", 200, true)).toBe(
      "Denied at least 200 times",
    );
    expect(checkCountLabel("allow", 1, true)).toBe("Allowed at least once");
  });
});

describe("checksFooter", () => {
  it("counts both outcomes when both loaded", () => {
    expect(checksFooter(2, 5)).toBe(
      "2 failed and 5 allowed checks in this period, grouped by scope and resource.",
    );
  });

  it("says nothing for a period with no checks", () => {
    expect(checksFooter(0, 0)).toBeUndefined();
    expect(checksFooter(undefined, undefined)).toBeUndefined();
  });

  it("says which count is missing rather than counting it as none", () => {
    expect(checksFooter(undefined, 5)).toBe(
      "5 allowed checks in this period. The failed count could not be loaded.",
    );
    expect(checksFooter(3, undefined)).toBe(
      "3 failed checks in this period. The allowed count could not be loaded.",
    );
  });
});
