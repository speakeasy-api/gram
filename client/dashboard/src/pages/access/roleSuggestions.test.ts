import { describe, expect, it } from "vitest";
import {
  rolesCoveringChallengeScopes,
  rolesCoveringScope,
} from "./roleSuggestions";

import type { Role } from "@gram/client/models/components/role.js";

function role(name: string, grants: Role["grants"]): Role {
  return {
    agentIds: [],
    createdAt: new Date("2026-01-01T00:00:00Z"),
    description: "",
    grants,
    id: `role-${name}`,
    isSystem: false,
    memberCount: 0,
    name,
    principalUrn: `role:organization:${name}`,
    slug: name,
    updatedAt: new Date("2026-01-01T00:00:00Z"),
  };
}

describe("rolesCoveringScope", () => {
  it("does not suggest an MCP role scoped to another project", () => {
    const roles = [
      role("unrestricted", [
        {
          scope: "mcp:connect",
          selectors: undefined,
        },
      ]),
      role("same-project", [
        {
          scope: "mcp:connect",
          selectors: [
            {
              resourceKind: "mcp",
              resourceId: "server-a",
              projectId: "project-a",
            },
          ],
        },
      ]),
      role("other-project", [
        {
          scope: "mcp:connect",
          selectors: [
            {
              resourceKind: "mcp",
              resourceId: "server-a",
              projectId: "project-b",
            },
          ],
        },
      ]),
    ];

    expect(
      rolesCoveringScope(roles, "mcp:connect", "server-a", "project-a").map(
        (item) => item.slug,
      ),
    ).toEqual(["unrestricted", "same-project"]);

    expect(
      rolesCoveringScope(roles, "mcp:connect", "server-a").map(
        (item) => item.slug,
      ),
    ).toEqual(["unrestricted"]);
  });
});

describe("rolesCoveringChallengeScopes", () => {
  it("keeps roles that cover the exact resource or an unrestricted resource", () => {
    const roles = [
      role("exact", [
        {
          scope: "project:read",
          selectors: [{ resourceKind: "project", resourceId: "project-a" }],
        },
      ]),
      role("all", [{ scope: "project:read", selectors: undefined }]),
      role("other", [
        {
          scope: "project:read",
          selectors: [{ resourceKind: "project", resourceId: "project-b" }],
        },
      ]),
    ];

    expect(
      rolesCoveringChallengeScopes(roles, [
        {
          scope: "project:read",
          selector: { resource_kind: "project", resource_id: "project-a" },
        },
      ]).map((item) => item.slug),
    ).toEqual(["exact", "all"]);
  });

  it("uses every captured selector dimension", () => {
    const roles = [
      role("server", [
        {
          scope: "mcp:connect",
          selectors: [{ resourceKind: "mcp", resourceId: "server-a" }],
        },
      ]),
      role("search-tool", [
        {
          scope: "mcp:connect",
          selectors: [
            {
              resourceKind: "mcp",
              resourceId: "server-a",
              tool: "search",
            },
          ],
        },
      ]),
      role("write-tool", [
        {
          scope: "mcp:connect",
          selectors: [
            {
              resourceKind: "mcp",
              resourceId: "server-a",
              tool: "write",
            },
          ],
        },
      ]),
      role("other-project", [
        {
          scope: "mcp:connect",
          selectors: [
            {
              resourceKind: "mcp",
              resourceId: "server-a",
              projectId: "project-b",
            },
          ],
        },
      ]),
    ];

    expect(
      rolesCoveringChallengeScopes(roles, [
        {
          scope: "mcp:connect",
          selector: {
            resource_kind: "mcp",
            resource_id: "server-a",
            project_id: "project-a",
            tool: "search",
            disposition: "read_only",
          },
        },
      ]).map((item) => item.slug),
    ).toEqual(["server", "search-tool"]);
  });

  it("only keeps roles that cover every challenge in the bucket", () => {
    const roles = [
      role("both-tools", [
        {
          scope: "mcp:connect",
          selectors: [
            { resourceKind: "mcp", resourceId: "server-a", tool: "search" },
            { resourceKind: "mcp", resourceId: "server-a", tool: "write" },
          ],
        },
      ]),
      role("search-only", [
        {
          scope: "mcp:connect",
          selectors: [
            { resourceKind: "mcp", resourceId: "server-a", tool: "search" },
          ],
        },
      ]),
    ];
    const base = { resource_kind: "mcp", resource_id: "server-a" };

    expect(
      rolesCoveringChallengeScopes(roles, [
        { scope: "mcp:connect", selector: { ...base, tool: "search" } },
        { scope: "mcp:connect", selector: { ...base, tool: "write" } },
      ]).map((item) => item.slug),
    ).toEqual(["both-tools"]);
  });

  it("returns no suggestions when any captured selector is incomplete", () => {
    expect(
      rolesCoveringChallengeScopes(
        [role("all", [{ scope: "org:read", selectors: undefined }])],
        [{ scope: "org:read" }],
      ),
    ).toEqual([]);
  });
});
