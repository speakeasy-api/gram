import type { Role } from "@gram/client/models/components/role.js";
import { describe, expect, it } from "vitest";
import {
  clientTypeGrantChanges,
  clientTypeRulesFromRole,
  rebaseClientTypeRules,
} from "./roleAuthClientTypes";

function role(grants: Role["grants"]): Role {
  return { grants } as Role;
}

describe("role upstream credential rules", () => {
  it("reads type-wide rules and lets a block outrank an allow", () => {
    const rules = clientTypeRulesFromRole(
      role([
        {
          scope: "mcp:auth",
          selectors: [
            {
              resourceKind: "mcp_auth_client",
              resourceId: "*",
              clientType: "service_account",
            },
            {
              resourceKind: "mcp_auth_client",
              resourceId: "*",
              clientType: "ema",
            },
          ],
        },
        {
          scope: "mcp:blocked_auth",
          selectors: [
            {
              resourceKind: "mcp_auth_client",
              resourceId: "*",
              clientType: "ema",
            },
          ],
        },
      ]),
    );
    expect(rules.service_account).toBe("allow");
    expect(rules.ema).toBe("block");
    expect(rules.remote_session).toBe("unset");
  });

  it("ignores rules narrowed to one upstream client", () => {
    const rules = clientTypeRulesFromRole(
      role([
        {
          scope: "mcp:auth",
          selectors: [
            {
              resourceKind: "mcp_auth_client",
              resourceId: "*",
              clientType: "remote_session",
              connectionOwner: "$represented_user",
              upstreamClient: "u1",
            },
          ],
        },
      ]),
    );
    expect(rules.remote_session).toBe("unset");
  });

  it("ignores rules naming one credential or one project", () => {
    const rules = clientTypeRulesFromRole(
      role([
        {
          scope: "mcp:auth",
          selectors: [
            { resourceKind: "mcp_auth_client", resourceId: "c1" },
            {
              resourceKind: "mcp_auth_client",
              resourceId: "*",
              clientType: "static_credential",
              projectId: "p1",
            },
          ],
        },
      ]),
    );
    expect(rules.static_credential).toBe("unset");
  });

  it("writes personal allows for the represented user and removes only changed types", () => {
    const current = role([
      {
        scope: "mcp:auth",
        selectors: [
          {
            resourceKind: "mcp_auth_client",
            resourceId: "*",
            clientType: "service_account",
          },
          { resourceKind: "mcp_auth_client", resourceId: "c1" },
        ],
      },
    ]);
    const changes = clientTypeGrantChanges(current, {
      ...clientTypeRulesFromRole(current),
      remote_session: "allow",
      service_account: "block",
    });

    expect(changes.addGrants).toEqual([
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
        selectors: [
          {
            resourceKind: "mcp_auth_client",
            resourceId: "*",
            clientType: "service_account",
          },
        ],
      },
    ]);
    expect(changes.removeGrants).toEqual([
      {
        scope: "mcp:auth",
        selectors: [
          {
            resourceKind: "mcp_auth_client",
            resourceId: "*",
            clientType: "service_account",
          },
        ],
      },
    ]);
  });

  it("changes nothing when no type changed", () => {
    const current = role([
      {
        scope: "mcp:auth",
        selectors: [
          {
            resourceKind: "mcp_auth_client",
            resourceId: "*",
            clientType: "service_account",
          },
        ],
      },
    ]);
    expect(
      clientTypeGrantChanges(current, clientTypeRulesFromRole(current)),
    ).toEqual({ addGrants: [], removeGrants: [] });
  });
});

describe("following the role after it is read again", () => {
  it("takes the stored rule for every type nobody chose for", () => {
    const next = rebaseClientTypeRules({
      current: { remote_session: "allow", service_account: "unset" },
      baseline: { remote_session: "unset", service_account: "unset" },
      stored: { service_account: "block", static_credential: "allow" },
    });

    // Chosen here, so it stands and still decides what the save removes.
    expect(next.remote_session).toBe("allow");
    // Not chosen here, so the role's own rules come through.
    expect(next.service_account).toBe("block");
    expect(next.static_credential).toBe("allow");
    expect(next.ema).toBe("unset");
  });

  it("lets a choice that matches the role again settle back", () => {
    const next = rebaseClientTypeRules({
      current: { remote_session: "allow" },
      baseline: { remote_session: "allow" },
      stored: {},
    });

    expect(next.remote_session).toBe("unset");
  });
});
