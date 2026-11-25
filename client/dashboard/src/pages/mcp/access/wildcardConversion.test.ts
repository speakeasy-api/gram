import { describe, expect, it } from "vitest";
import { convertWildcardGrants, holdsWildcard } from "./wildcardConversion";

const base = {
  lines: ["view" as const],
  resourceId: "srv-here",
  resourceIds: ["srv-here", "toolset-here"],
  projectId: "proj-1",
  projectServerIds: ["srv-here", "toolset-here", "srv-a", "srv-b"],
  otherProjectIds: ["proj-2"],
};

describe("convertWildcardGrants", () => {
  it("names the other servers here and keeps other projects for a rule reaching every project", () => {
    const result = convertWildcardGrants({
      ...base,
      grants: [
        {
          scope: "mcp:read",
          selectors: [{ resourceKind: "mcp", resourceId: "*" }],
        },
      ],
    });

    expect(result.removeGrants).toEqual([
      {
        scope: "mcp:read",
        selectors: [{ resourceKind: "mcp", resourceId: "*" }],
      },
    ]);
    expect(result.addGrants).toEqual([
      {
        scope: "mcp:read",
        selectors: [
          { resourceKind: "mcp", resourceId: "srv-a" },
          { resourceKind: "mcp", resourceId: "srv-b" },
          { resourceKind: "mcp", resourceId: "*", projectId: "proj-2" },
        ],
      },
    ]);
    expect(result.coveredEveryProject).toBe(true);
  });

  it("names only this project's servers for a rule confined to this project", () => {
    const result = convertWildcardGrants({
      ...base,
      grants: [
        {
          scope: "mcp:read",
          selectors: [
            { resourceKind: "mcp", resourceId: "*", projectId: "proj-1" },
          ],
        },
      ],
    });

    expect(result.addGrants).toEqual([
      {
        scope: "mcp:read",
        selectors: [
          { resourceKind: "mcp", resourceId: "srv-a" },
          { resourceKind: "mcp", resourceId: "srv-b" },
        ],
      },
    ]);
    expect(result.coveredEveryProject).toBe(false);
  });

  it("carries narrowing onto every rule that replaces it", () => {
    const result = convertWildcardGrants({
      ...base,
      lines: ["use"],
      otherProjectIds: [],
      grants: [
        {
          scope: "mcp:blocked_connect",
          selectors: [
            { resourceKind: "*", resourceId: "*", disposition: "destructive" },
          ],
        },
      ],
    });

    expect(result.addGrants).toEqual([
      {
        scope: "mcp:blocked_connect",
        selectors: [
          {
            resourceKind: "mcp",
            resourceId: "srv-a",
            disposition: "destructive",
          },
          {
            resourceKind: "mcp",
            resourceId: "srv-b",
            disposition: "destructive",
          },
        ],
      },
    ]);
  });

  it("removes the role's own rules naming this server, under either id", () => {
    const result = convertWildcardGrants({
      ...base,
      grants: [
        {
          scope: "mcp:blocked_read",
          selectors: [
            { resourceKind: "mcp", resourceId: "toolset-here" },
            { resourceKind: "mcp", resourceId: "srv-a" },
          ],
        },
      ],
    });

    expect(result.removeGrants).toEqual([
      {
        scope: "mcp:blocked_read",
        selectors: [{ resourceKind: "mcp", resourceId: "toolset-here" }],
      },
    ]);
    expect(result.addGrants).toEqual([]);
    expect(result.convertsWildcard).toBe(false);
  });

  it("leaves lines it was not asked to clear and rules confined to other projects alone", () => {
    const result = convertWildcardGrants({
      ...base,
      grants: [
        {
          scope: "mcp:connect",
          selectors: [{ resourceKind: "mcp", resourceId: "*" }],
        },
        {
          scope: "mcp:read",
          selectors: [
            { resourceKind: "mcp", resourceId: "*", projectId: "proj-2" },
          ],
        },
      ],
    });

    expect(result.removeGrants).toEqual([]);
    expect(result.addGrants).toEqual([]);
  });

  it("leaves rules confined to other projects alone when an unrestricted rule is removed", () => {
    // The unrestricted removal deletes only rules naming no project, so a
    // rule confined to another project is not touched and not written again.
    const result = convertWildcardGrants({
      ...base,
      otherProjectIds: [],
      grants: [
        { scope: "mcp:read" },
        {
          scope: "mcp:read",
          selectors: [
            { resourceKind: "mcp", resourceId: "*", projectId: "proj-3" },
          ],
        },
      ],
    });

    expect(result.removeGrants).toEqual([{ scope: "mcp:read" }]);
    expect(result.addGrants).toEqual([
      {
        scope: "mcp:read",
        selectors: [
          { resourceKind: "mcp", resourceId: "srv-a" },
          { resourceKind: "mcp", resourceId: "srv-b" },
        ],
      },
    ]);
  });
});

describe("holdsWildcard", () => {
  it("counts rules covering this project, not ones confined to another", () => {
    expect(
      holdsWildcard(
        [
          {
            scope: "mcp:read",
            selectors: [
              { resourceKind: "mcp", resourceId: "*", projectId: "proj-2" },
            ],
          },
        ],
        ["view"],
        "proj-1",
      ),
    ).toBe(false);
    expect(
      holdsWildcard(
        [
          {
            scope: "mcp:blocked_read",
            selectors: [{ resourceKind: "*", resourceId: "*" }],
          },
        ],
        ["view"],
        "proj-1",
      ),
    ).toBe(true);
  });
});
