import { describe, it, expect } from "vitest";
import type { Role } from "@gram/client/models/components/role.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { RoleGrant } from "./types";
import {
  applyRemoveRule,
  diffGrants,
  grantsFromRole,
  sdkGrantsFromForm,
} from "./roleGrantTransform";

const scopeDefinitions = [
  {
    slug: "project:write",
    description: "Create and modify projects and project-related resources.",
    resourceType: "project",
    exclusionScope: "project:blocked_write",
  },
  {
    slug: "mcp:write",
    description: "Create and modify MCP servers and configuration.",
    resourceType: "mcp",
    exclusionScope: "mcp:blocked_write",
  },
  {
    slug: "mcp:connect",
    description: "Connect to and use MCP servers.",
    resourceType: "mcp",
    exclusionScope: "mcp:blocked_connect",
  },
] satisfies ScopeDefinition[];

function role(grants: Role["grants"]): Role {
  return {
    id: "role_test",
    principalUrn: "role:organization:role_test",
    name: "Test",
    slug: "org-test",
    description: "",
    isSystem: false,
    grants,
    memberCount: 0,
    createdAt: new Date("2026-06-05T00:00:00Z"),
    updatedAt: new Date("2026-06-05T00:00:00Z"),
  };
}

describe("role grant round-trip (grantsFromRole → sdkGrantsFromForm)", () => {
  it("collapses the synthetic wildcard allow to unrestricted (selectors:undefined)", () => {
    // Server stores {kind:"mcp", id:"*"} for grants saved with nil selectors.
    // The dialog must collapse this back to the unrestricted shape so the chip
    // reads "All servers" instead of "1 server".
    const r = role([
      {
        scope: "mcp:connect",
        selectors: [{ resourceKind: "mcp", resourceId: "*" }],
      },
    ]);

    const sdkGrants = sdkGrantsFromForm(
      grantsFromRole(r, scopeDefinitions),
      scopeDefinitions,
    );

    expect(sdkGrants).toEqual([{ scope: "mcp:connect", selectors: undefined }]);
  });

  it("round-trips mcp:blocked_connect as an exception for mcp:connect", () => {
    const r = role([
      {
        scope: "mcp:connect",
        selectors: [{ resourceKind: "mcp", resourceId: "*" }],
      },
      {
        scope: "mcp:blocked_connect",
        selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
      },
    ]);

    const rules = grantsFromRole(r, scopeDefinitions);
    const sdkGrants = sdkGrantsFromForm(rules, scopeDefinitions);

    expect(rules["mcp:connect"]?.rules).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ effect: "allow", selectors: null }),
        expect.objectContaining({
          effect: "deny",
          selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
        }),
      ]),
    );
    expect(sdkGrants).toEqual([
      { scope: "mcp:connect", selectors: undefined },
      {
        scope: "mcp:blocked_connect",
        selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
      },
    ]);
  });

  it("serializes project and MCP exceptions as exclusion scopes", () => {
    const grants: Record<string, RoleGrant> = {
      "project:write": {
        scope: "project:write",
        rules: [
          { id: "project-allow", effect: "allow", selectors: null },
          {
            id: "project-exception",
            effect: "deny",
            selectors: [{ resourceKind: "project", resourceId: "project_123" }],
          },
        ],
      },
      "mcp:write": {
        scope: "mcp:write",
        rules: [
          { id: "mcp-allow", effect: "allow", selectors: null },
          {
            id: "mcp-exception",
            effect: "deny",
            selectors: [
              {
                resourceKind: "mcp",
                resourceId: "*",
                projectId: "project_123",
              },
            ],
          },
        ],
      },
    };

    expect(sdkGrantsFromForm(grants, scopeDefinitions)).toEqual([
      { scope: "project:write", selectors: undefined },
      {
        scope: "project:blocked_write",
        selectors: [{ resourceKind: "project", resourceId: "project_123" }],
      },
      { scope: "mcp:write", selectors: undefined },
      {
        scope: "mcp:blocked_write",
        selectors: [
          { resourceKind: "mcp", resourceId: "*", projectId: "project_123" },
        ],
      },
    ]);
  });
});

describe("diffGrants", () => {
  it("treats two risk-policy selectors differing only in serverUrl as distinct", () => {
    // serverUrl is the differentiating dimension for risk_policy scopes.
    // selectorKey must include it or the diff collapses both grants to the
    // same identity and the change is silently dropped.
    const before = [
      {
        scope: "risk_policy:evaluate" as const,
        selectors: [
          {
            resourceKind: "risk_policy" as const,
            resourceId: "*",
            serverUrl: "https://a.example.com",
          },
        ],
      },
    ];
    const after = [
      {
        scope: "risk_policy:evaluate" as const,
        selectors: [
          {
            resourceKind: "risk_policy" as const,
            resourceId: "*",
            serverUrl: "https://b.example.com",
          },
        ],
      },
    ];

    const { addGrants, removeGrants } = diffGrants(before, after);

    expect(removeGrants).toHaveLength(1);
    expect(addGrants).toHaveLength(1);
  });
});

describe("applyRemoveRule", () => {
  it("unchecks the scope when an unrestricted allow is removed, dropping orphaned exceptions", () => {
    const grant: RoleGrant = {
      scope: "mcp:connect",
      rules: [
        { id: "a", effect: "allow", selectors: null },
        {
          id: "d",
          effect: "deny",
          selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
        },
      ],
    };

    expect(applyRemoveRule(grant, 0)).toBeNull();
  });

  it("falls back to unrestricted when the only narrower allow is removed", () => {
    const grant: RoleGrant = {
      scope: "mcp:connect",
      rules: [
        {
          id: "a",
          effect: "allow",
          selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
        },
      ],
    };

    const next = applyRemoveRule(grant, 0);
    expect(next).not.toBeNull();
    expect(next!.rules).toHaveLength(1);
    expect(next!.rules[0]!.effect).toBe("allow");
    expect(next!.rules[0]!.selectors).toBeNull();
  });

  it("filters an exception without touching surviving allows", () => {
    const grant: RoleGrant = {
      scope: "mcp:connect",
      rules: [
        { id: "a", effect: "allow", selectors: null },
        {
          id: "d",
          effect: "deny",
          selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
        },
      ],
    };

    const next = applyRemoveRule(grant, 1);
    expect(next).not.toBeNull();
    expect(next!.rules).toHaveLength(1);
    expect(next!.rules[0]!.effect).toBe("allow");
  });
});
