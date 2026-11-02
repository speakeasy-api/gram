import { describe, it, expect } from "vitest";
import type { Role } from "@gram/client/models/components/role.js";

import { grantsFromRole } from "./roleGrantTransform";

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
