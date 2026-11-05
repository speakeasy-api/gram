import { describe, expect, it } from "vitest";
import {
  breadthLabel,
  rolesCoveringChallengeScopes,
  rolesCoveringScope,
  suggestRolesForScope,
} from "./roleSuggestions";
import { scopeCatalogue } from "./scopeCatalogue";

import type { Role } from "@gram/client/models/components/role.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";

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

function def(
  slug: string,
  extra: Partial<ScopeDefinition> = {},
): ScopeDefinition {
  const [resourceType] = slug.split(":");
  return {
    slug: slug as ScopeDefinition["slug"],
    label: slug,
    description: `${slug}.`,
    resourceType: resourceType as ScopeDefinition["resourceType"],
    category: resourceType as ScopeDefinition["category"],
    visibility: slug.includes(":blocked_") ? "internal" : "user_visible",
    agentEligible: true,
    sensitive: false,
    subScopes: [],
    ...extra,
  };
}

// The inclusions are a hand-written subset of the server's scopeExpansions;
// the server's catalog tests guard the full graph.
const catalogue = scopeCatalogue([
  def("org:read"),
  def("org:admin", {
    subScopes: ["org:read"],
    sensitive: true,
    sensitivity: "destructive_admin",
  }),
  def("project:read", { exclusionScope: "project:blocked_read" }),
  def("mcp:connect", { exclusionScope: "mcp:blocked_connect" }),
  def("mcp:read", { subScopes: ["mcp:connect"] }),
  def("mcp:write", { subScopes: ["mcp:connect", "mcp:read"] }),
  def("mcp:blocked_connect"),
  def("mcp:auth", { category: "mcp_auth_client" }),
  def("log:read", { sensitive: true, sensitivity: "tool_logs" }),
  def("chat:read", { sensitive: true, sensitivity: "conversations" }),
]);

describe("rolesCoveringScope", () => {
  it("does not suggest an MCP role scoped to another project", () => {
    const roles = [
      role("unrestricted", [
        {
          scope: "mcp:connect",
          selectors: undefined,
        },
      ]),
      role("all-projects", [
        {
          scope: "mcp:connect",
          selectors: [
            {
              resourceKind: "mcp",
              resourceId: "server-a",
              projectId: "*",
            },
          ],
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
      rolesCoveringScope(
        roles,
        "mcp:connect",
        catalogue,
        "server-a",
        "project-a",
      ).map((item) => item.slug),
    ).toEqual(["unrestricted", "all-projects", "same-project"]);

    expect(
      rolesCoveringScope(roles, "mcp:connect", catalogue, "server-a").map(
        (item) => item.slug,
      ),
    ).toEqual(["unrestricted", "all-projects"]);
  });

  it("uses the inclusions the catalogue serves", () => {
    const roles = [
      role("writer", [{ scope: "mcp:write", selectors: undefined }]),
      role("admin", [{ scope: "org:admin", selectors: undefined }]),
    ];
    expect(
      rolesCoveringScope(roles, "mcp:connect", catalogue).map((r) => r.slug),
    ).toEqual(["writer"]);
    expect(
      rolesCoveringScope(roles, "org:read", catalogue).map((r) => r.slug),
    ).toEqual(["admin"]);
    // Before the catalogue loads nothing is implied, so only an exact grant
    // matches and no role is wrongly suggested.
    expect(
      rolesCoveringScope(roles, "org:read", scopeCatalogue(undefined)),
    ).toEqual([]);
  });

  it("leaves out a role that blocks the scope on the requested resource", () => {
    const roles = [
      role("writer-blocked-everywhere", [
        { scope: "mcp:write", selectors: undefined },
        { scope: "mcp:blocked_connect", selectors: undefined },
      ]),
      role("writer-blocked-elsewhere", [
        { scope: "mcp:write", selectors: undefined },
        {
          scope: "mcp:blocked_connect",
          selectors: [{ resourceKind: "mcp", resourceId: "server-b" }],
        },
      ]),
      role("writer", [{ scope: "mcp:write", selectors: undefined }]),
    ];

    expect(
      rolesCoveringScope(roles, "mcp:connect", catalogue, "server-a").map(
        (r) => r.slug,
      ),
    ).toEqual(["writer-blocked-elsewhere", "writer"]);
    // Without a resource only a block on everything disqualifies.
    expect(
      rolesCoveringScope(roles, "mcp:connect", catalogue).map((r) => r.slug),
    ).toEqual(["writer-blocked-elsewhere", "writer"]);
    expect(
      rolesCoveringScope(roles, "mcp:connect", catalogue, "server-b").map(
        (r) => r.slug,
      ),
    ).toEqual(["writer"]);
  });
});

describe("suggestRolesForScope", () => {
  const roles = [
    role("admin", [
      { scope: "org:admin", selectors: undefined },
      { scope: "mcp:write", selectors: undefined },
      { scope: "log:read", selectors: undefined },
      { scope: "chat:read", selectors: undefined },
      { scope: "mcp:auth", selectors: undefined },
    ]),
    role("log-reviewer", [
      { scope: "log:read", selectors: undefined },
      { scope: "mcp:blocked_connect", selectors: undefined },
    ]),
    role("observer", [
      { scope: "log:read", selectors: undefined },
      { scope: "project:read", selectors: undefined },
    ]),
    role("unrelated", [{ scope: "mcp:connect", selectors: undefined }]),
  ];

  it("puts roles that give exactly the scope first and names what broader roles add", () => {
    const suggestions = suggestRolesForScope(roles, "log:read", catalogue);

    expect(suggestions.exact.map(({ role }) => role.slug)).toEqual([
      "log-reviewer",
    ]);
    expect(
      suggestions.broader.map(({ role, extraScopes, extraSensitive }) => ({
        slug: role.slug,
        extraScopes,
        extraSensitive,
      })),
    ).toEqual([
      {
        slug: "observer",
        extraScopes: ["project:read"],
        extraSensitive: [],
      },
      {
        slug: "admin",
        extraScopes: [
          "org:admin",
          "org:read",
          "mcp:write",
          "mcp:connect",
          "mcp:read",
          "chat:read",
        ],
        extraSensitive: ["org:admin", "chat:read"],
      },
    ]);
  });

  it("treats what the requested scope includes as part of the request", () => {
    const suggestions = suggestRolesForScope(
      [role("mcp-writer", [{ scope: "mcp:write", selectors: undefined }])],
      "mcp:write",
      catalogue,
    );
    expect(suggestions.exact.map(({ role }) => role.slug)).toEqual([
      "mcp-writer",
    ]);
    expect(suggestions.broader).toEqual([]);
  });

  it("does not count exceptions or provisioned auth-client grants as more", () => {
    const suggestions = suggestRolesForScope(
      [
        role("narrow", [
          { scope: "log:read", selectors: undefined },
          { scope: "mcp:blocked_connect", selectors: undefined },
          { scope: "mcp:auth", selectors: undefined },
        ]),
      ],
      "log:read",
      catalogue,
    );
    expect(suggestions.exact.map(({ role }) => role.slug)).toEqual(["narrow"]);
  });

  it("puts a role limited to the requested project before one that grants the scope everywhere", () => {
    const suggestions = suggestRolesForScope(
      [
        role("everywhere", [{ scope: "log:read", selectors: undefined }]),
        role("one-project", [
          {
            scope: "log:read",
            selectors: [{ resourceKind: "project", resourceId: "project-a" }],
          },
        ]),
        role("admin-everywhere", [
          { scope: "org:admin", selectors: undefined },
          { scope: "log:read", selectors: undefined },
        ]),
        role("admin-one-project", [
          { scope: "org:admin", selectors: undefined },
          {
            scope: "log:read",
            selectors: [{ resourceKind: "project", resourceId: "project-a" }],
          },
        ]),
      ],
      "log:read",
      catalogue,
      "project-a",
    );

    expect(
      suggestions.exact.map(({ role, breadth }) => [role.slug, breadth]),
    ).toEqual([
      ["one-project", "project"],
      ["everywhere", "all"],
    ]);
    expect(
      suggestions.broader.map(({ role, breadth }) => [role.slug, breadth]),
    ).toEqual([
      ["admin-one-project", "project"],
      ["admin-everywhere", "all"],
    ]);
    expect(breadthLabel("log:read", "all")).toBe("across all projects");
    expect(breadthLabel("log:read", "project")).toBe(
      "limited to specific projects",
    );
    expect(breadthLabel("mcp:connect", "resource")).toBe(
      "limited to specific MCP servers",
    );
    // An organization scope reaches the whole organization either way.
    expect(breadthLabel("org:read", "all")).toBeNull();
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
      rolesCoveringChallengeScopes(
        roles,
        [
          {
            scope: "project:read",
            selector: { resource_kind: "project", resource_id: "project-a" },
          },
        ],
        catalogue,
      ).map((item) => item.slug),
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
      rolesCoveringChallengeScopes(
        roles,
        [
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
        ],
        catalogue,
      ).map((item) => item.slug),
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
      rolesCoveringChallengeScopes(
        roles,
        [
          { scope: "mcp:connect", selector: { ...base, tool: "search" } },
          { scope: "mcp:connect", selector: { ...base, tool: "write" } },
        ],
        catalogue,
      ).map((item) => item.slug),
    ).toEqual(["both-tools"]);
  });

  it("returns no suggestions when any captured selector is incomplete", () => {
    expect(
      rolesCoveringChallengeScopes(
        [role("all", [{ scope: "org:read", selectors: undefined }])],
        [{ scope: "org:read" }],
        catalogue,
      ),
    ).toEqual([]);
  });
});
