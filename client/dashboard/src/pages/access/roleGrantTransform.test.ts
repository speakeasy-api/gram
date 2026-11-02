import { describe, it, expect } from "vitest";
import type { Role } from "@gram/client/models/components/role.js";

import type { Scope } from "@gram/client/models/components/rolegrant.js";

import { grantsFromRole, rebaseFormGrants } from "./roleGrantTransform";
import type { RoleGrant } from "./types";

describe("grantsFromRole", () => {
  it("leaves provisioned auth client grants out of the form", () => {
    const role = {
      grants: [
        {
          scope: "mcp:connect",
          selectors: [{ resourceKind: "mcp", resourceId: "*" }],
        },
        {
          scope: "mcp:auth",
          selectors: [
            {
              resourceKind: "mcp_auth_client",
              resourceId: "*",
              clientType: "remote_session",
              connectionOwner: "$represented_user",
            },
          ],
        },
        {
          scope: "mcp:blocked_auth",
          selectors: [{ resourceKind: "mcp_auth_client", resourceId: "c1" }],
        },
      ],
    } as unknown as Role;

    expect(Object.keys(grantsFromRole(role, []))).toEqual(["mcp:connect"]);
  });
});

describe("rebaseFormGrants", () => {
  function allow(scope: Scope, resourceId: string): RoleGrant {
    return {
      scope,
      rules: [
        {
          id: `rule-${scope}-${resourceId}`,
          effect: "allow",
          selectors: [{ resourceKind: "mcp", resourceId }],
        },
      ],
    };
  }

  it("follows the role for permissions nobody edited and keeps the edits", () => {
    const baseline = { "mcp:read": allow("mcp:read", "server-a") };
    const next = rebaseFormGrants({
      // The person turned Connect on and left View as it was.
      current: {
        ...baseline,
        "mcp:connect": allow("mcp:connect", "server-a"),
      },
      baseline,
      // The role gained Write, and View now names another server.
      stored: {
        "mcp:read": allow("mcp:read", "server-b"),
        "mcp:write": allow("mcp:write", "server-a"),
      },
    });

    expect(Object.keys(next).sort()).toEqual([
      "mcp:connect",
      "mcp:read",
      "mcp:write",
    ]);
    expect(next["mcp:read"]).toEqual(allow("mcp:read", "server-b"));
    expect(next["mcp:connect"]).toEqual(allow("mcp:connect", "server-a"));
  });

  it("drops a permission the person switched off and one the role lost", () => {
    const next = rebaseFormGrants({
      current: {},
      baseline: {
        "mcp:read": allow("mcp:read", "server-a"),
        "mcp:connect": allow("mcp:connect", "server-a"),
      },
      stored: { "mcp:read": allow("mcp:read", "server-a") },
    });

    expect(Object.keys(next)).toEqual([]);
  });
});
