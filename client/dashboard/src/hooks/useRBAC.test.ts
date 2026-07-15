import { describe, expect, it } from "vitest";
import {
  exclusionScopesForScope,
  hasScopeInGrants,
  resourceKindForScope,
  selectorMatches,
  selectorMatchesStrict,
} from "./useRBAC";

describe("resourceKindForScope", () => {
  it("returns 'project' for project scopes", () => {
    expect(resourceKindForScope("project:read")).toBe("project");
    expect(resourceKindForScope("project:write")).toBe("project");
  });

  it("returns 'mcp' for mcp scopes", () => {
    expect(resourceKindForScope("mcp:read")).toBe("mcp");
    expect(resourceKindForScope("mcp:write")).toBe("mcp");
    expect(resourceKindForScope("mcp:connect")).toBe("mcp");
  });

  it("returns 'mcp' for remote-mcp scopes", () => {
    expect(resourceKindForScope("remote-mcp:read")).toBe("mcp");
    expect(resourceKindForScope("remote-mcp:write")).toBe("mcp");
    expect(resourceKindForScope("remote-mcp:connect")).toBe("mcp");
  });

  it("returns 'org' for org scopes", () => {
    expect(resourceKindForScope("org:read")).toBe("org");
    expect(resourceKindForScope("org:admin")).toBe("org");
  });

  it("returns 'environment' for environment scopes", () => {
    expect(resourceKindForScope("environment:read")).toBe("environment");
    expect(resourceKindForScope("environment:write")).toBe("environment");
  });

  it("returns 'skill' for skill scopes", () => {
    expect(resourceKindForScope("skill:read")).toBe("skill");
    expect(resourceKindForScope("skill:write")).toBe("skill");
  });

  it("returns 'risk_policy' for risk_policy scopes", () => {
    expect(resourceKindForScope("risk_policy:evaluate")).toBe("risk_policy");
    expect(resourceKindForScope("risk_policy:bypass")).toBe("risk_policy");
  });

  // Regression: chat scopes must map to "chat" so a restricted chat:read grant
  // (selector {resource_kind:"chat", resource_id:"*"}) matches the hasScope
  // check. When this returned "*" the check selector ({resource_kind:"*"}) never
  // matched the grant, so admins with chat:read still saw the "own sessions
  // only" banner.
  it("returns 'chat' for chat scopes", () => {
    expect(resourceKindForScope("chat:read")).toBe("chat");
  });

  it("returns '*' for unknown scope families", () => {
    expect(resourceKindForScope("root")).toBe("*");
    expect(resourceKindForScope("unknown:thing")).toBe("*");
  });
});

describe("selectorMatches", () => {
  it("wildcard grant matches anything", () => {
    const grant = { resourceId: "*" };
    expect(selectorMatches(grant, { resourceId: "proj_123" })).toBe(true);
    expect(selectorMatches(grant, { resourceId: "anything" })).toBe(true);
  });

  it("empty grant matches anything", () => {
    expect(selectorMatches({}, { resourceId: "proj_123" })).toBe(true);
    expect(selectorMatches({}, {})).toBe(true);
  });

  it("exact key match", () => {
    const grant = { resourceId: "proj_123" };
    expect(selectorMatches(grant, { resourceId: "proj_123" })).toBe(true);
    expect(selectorMatches(grant, { resourceId: "proj_456" })).toBe(false);
  });

  it("grant key absent from check is skipped", () => {
    const grant = { resourceId: "proj_123" };
    expect(selectorMatches(grant, {})).toBe(true);
    expect(selectorMatches(grant, { otherKey: "val" })).toBe(true);
  });

  it("multiple keys must all match", () => {
    const grant = { resourceId: "proj_123", tool: "tool_abc" };
    expect(
      selectorMatches(grant, { resourceId: "proj_123", tool: "tool_abc" }),
    ).toBe(true);
    expect(
      selectorMatches(grant, { resourceId: "proj_123", tool: "tool_xyz" }),
    ).toBe(false);
    // check without tool — not constraining that dimension
    expect(selectorMatches(grant, { resourceId: "proj_123" })).toBe(true);
  });

  it("resourceKind mismatch fails", () => {
    const grant = { resourceKind: "project", resourceId: "proj_123" };
    expect(
      selectorMatches(grant, {
        resourceKind: "mcp",
        resourceId: "proj_123",
      }),
    ).toBe(false);
  });

  it("skill selectors match only the selected project", () => {
    const grant = { resourceKind: "skill", resourceId: "project_a" };
    expect(
      selectorMatches(grant, {
        resourceKind: "skill",
        resourceId: "project_a",
      }),
    ).toBe(true);
    expect(
      selectorMatches(grant, {
        resourceKind: "skill",
        resourceId: "project_b",
      }),
    ).toBe(false);
  });

  it("resourceKind wildcard matches any kind", () => {
    const grant = { resourceKind: "*", resourceId: "*" };
    expect(
      selectorMatches(grant, {
        resourceKind: "project",
        resourceId: "proj_123",
      }),
    ).toBe(true);
    expect(
      selectorMatches(grant, {
        resourceKind: "mcp",
        resourceId: "tool_a",
      }),
    ).toBe(true);
  });

  it("disposition grant matches connection check without disposition", () => {
    const grant = {
      resourceKind: "mcp",
      resourceId: "*",
      disposition: "read_only",
    };
    const check = { resourceKind: "mcp", resourceId: "toolsetA" };
    expect(selectorMatches(grant, check)).toBe(true);
  });

  it("disposition grant denies wrong disposition", () => {
    const grant = {
      resourceKind: "mcp",
      resourceId: "*",
      disposition: "read_only",
    };
    expect(
      selectorMatches(grant, {
        resourceKind: "mcp",
        resourceId: "toolsetA",
        disposition: "read_only",
      }),
    ).toBe(true);
    expect(
      selectorMatches(grant, {
        resourceKind: "mcp",
        resourceId: "toolsetA",
        disposition: "destructive",
      }),
    ).toBe(false);
  });
});

describe("selectorMatchesStrict", () => {
  it("requires every exclusion dimension in the check", () => {
    const exclusion = {
      resourceKind: "mcp",
      resourceId: "server_a",
      tool: "removeUser",
    };
    expect(
      selectorMatchesStrict(exclusion, {
        resourceKind: "mcp",
        resourceId: "server_a",
      }),
    ).toBe(false);
    expect(
      selectorMatchesStrict(exclusion, {
        resourceKind: "mcp",
        resourceId: "server_a",
        tool: "removeUser",
      }),
    ).toBe(true);
  });
});

describe("exclusionScopesForScope", () => {
  it("matches project, MCP, and Skills exclusion expansions", () => {
    expect(exclusionScopesForScope("project:read")).toEqual([
      "project:blocked_read",
    ]);
    expect(exclusionScopesForScope("project:write")).toEqual([
      "project:blocked_write",
      "project:blocked_read",
    ]);
    expect(exclusionScopesForScope("mcp:read")).toEqual([
      "mcp:blocked_read",
      "mcp:blocked_connect",
    ]);
    expect(exclusionScopesForScope("mcp:write")).toEqual([
      "mcp:blocked_write",
      "mcp:blocked_read",
      "mcp:blocked_connect",
    ]);
    expect(exclusionScopesForScope("mcp:connect")).toEqual([
      "mcp:blocked_connect",
    ]);
    expect(exclusionScopesForScope("skill:read")).toEqual([
      "skill:blocked_read",
    ]);
    expect(exclusionScopesForScope("skill:write")).toEqual([
      "skill:blocked_write",
      "skill:blocked_read",
    ]);
  });
});

describe("hasScopeInGrants", () => {
  const skillWildcard = {
    scope: "skill:write",
    selectors: [{ resourceKind: "skill", resourceId: "*" }],
    subScopes: ["skill:read"],
  };

  it("applies an unrestricted exclusion to an unscoped check", () => {
    const grants = [
      skillWildcard,
      {
        scope: "skill:blocked_read",
        selectors: [{ resourceKind: "skill", resourceId: "*" }],
      },
    ];

    expect(hasScopeInGrants(grants, "skill:read")).toBe(false);
  });

  it("allows a specific resource grant to satisfy an unscoped check", () => {
    const grants = [
      {
        scope: "skill:read",
        selectors: [{ resourceKind: "skill", resourceId: "project_a" }],
      },
    ];

    expect(hasScopeInGrants(grants, "skill:read")).toBe(true);
  });

  it("limits a project-selected skill grant to that project", () => {
    const grants = [
      {
        scope: "skill:read",
        selectors: [{ resourceKind: "skill", resourceId: "project_a" }],
      },
    ];

    expect(hasScopeInGrants(grants, "skill:read", "project_a")).toBe(true);
    expect(hasScopeInGrants(grants, "skill:read", "project_b")).toBe(false);
  });

  it("does not let a specific exclusion erase unscoped access", () => {
    const grants = [
      skillWildcard,
      {
        scope: "skill:blocked_read",
        selectors: [{ resourceKind: "skill", resourceId: "project_a" }],
      },
    ];

    expect(hasScopeInGrants(grants, "skill:read")).toBe(true);
  });

  it("subtracts a project-specific skill write exclusion", () => {
    const grants = [
      skillWildcard,
      {
        scope: "skill:blocked_write",
        selectors: [{ resourceKind: "skill", resourceId: "project_a" }],
      },
    ];

    expect(hasScopeInGrants(grants, "skill:write", "project_a")).toBe(false);
    expect(hasScopeInGrants(grants, "skill:write", "project_b")).toBe(true);
    expect(hasScopeInGrants(grants, "skill:read", "project_a")).toBe(true);
  });

  it("applies skill read exclusions to read and write", () => {
    const grants = [
      skillWildcard,
      {
        scope: "skill:blocked_read",
        selectors: [{ resourceKind: "skill", resourceId: "project_a" }],
      },
    ];

    expect(hasScopeInGrants(grants, "skill:read", "project_a")).toBe(false);
    expect(hasScopeInGrants(grants, "skill:write", "project_a")).toBe(false);
    expect(hasScopeInGrants(grants, "skill:read", "project_b")).toBe(true);
  });

  it("does not broaden a dimension-specific exclusion", () => {
    const grants = [
      {
        scope: "mcp:connect",
        selectors: [{ resourceKind: "mcp", resourceId: "server_a" }],
      },
      {
        scope: "mcp:blocked_connect",
        selectors: [
          {
            resourceKind: "mcp",
            resourceId: "server_a",
            tool: "removeUser",
          },
        ],
      },
    ];

    expect(hasScopeInGrants(grants, "mcp:connect", "server_a")).toBe(true);
  });
});
