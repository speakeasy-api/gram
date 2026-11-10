import type { Role } from "@gram/client/models/components/role.js";
import type { RoleGrant as SdkRoleGrant } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import { describe, expect, it } from "vitest";

import {
  addedPermissions,
  liftedExceptions,
  permissionsAddedByRoles,
} from "./reviewGrants";
import { scopeCatalogue } from "./scopeCatalogue";

function def(
  slug: string,
  label: string,
  extra: Partial<ScopeDefinition> = {},
): ScopeDefinition {
  return {
    slug: slug as ScopeDefinition["slug"],
    label,
    description: `${label}.`,
    resourceType: slug.split(":")[0] as ScopeDefinition["resourceType"],
    category: slug.split(":")[0] as ScopeDefinition["category"],
    visibility: "user_visible",
    agentEligible: true,
    sensitive: false,
    subScopes: [],
    ...extra,
  };
}

const catalogue = scopeCatalogue([
  def("org:read", "View organization"),
  def("org:admin", "Manage organization", {
    sensitive: true,
    subScopes: ["org:read", "chat:read"],
    exclusionScope: "org:blocked_admin" as ScopeDefinition["exclusionScope"],
  }),
  def("org:blocked_read", "Exception: view organization", {
    visibility: "internal" as ScopeDefinition["visibility"],
  }),
  // Not sensitive itself; sensitive through what it includes.
  def("org:owner", "Own organization", { subScopes: ["org:admin"] }),
  def("project:read", "View projects"),
  def("project:write", "Manage projects", { subScopes: ["project:read"] }),
  def("mcp:connect", "Connect to MCP servers", {
    exclusionScope: "mcp:blocked_connect" as ScopeDefinition["exclusionScope"],
  }),
  def("mcp:read", "View MCP servers", { subScopes: ["mcp:connect"] }),
  def("mcp:write", "Manage MCP servers", { subScopes: ["mcp:read"] }),
  def("chat:read", "View agent sessions", { sensitive: true }),
  def("chat:write", "Manage agent sessions", {
    sensitive: true,
    subScopes: ["chat:read"],
  }),
  def("log:read", "View tool logs", { sensitive: true }),
]);

describe("addedPermissions", () => {
  it("names each added permission once with what it carries along", () => {
    const added = addedPermissions(
      [
        { scope: "mcp:write" as never, selectors: undefined },
        {
          scope: "log:read" as never,
          selectors: [{ resourceKind: "project", resourceId: "p1" }],
        },
        {
          scope: "log:read" as never,
          selectors: [{ resourceKind: "project", resourceId: "p2" }],
        },
        { scope: "org:blocked_read" as never, selectors: undefined },
      ],
      catalogue,
    );
    expect(added).toEqual([
      {
        scope: "mcp:write",
        label: "Manage MCP servers",
        description: "Manage MCP servers.",
        sensitive: false,
        sensitiveVia: [],
        includes: ["View MCP servers", "Connect to MCP servers"],
        places: null,
      },
      {
        scope: "log:read",
        label: "View tool logs",
        description: "View tool logs.",
        sensitive: true,
        sensitiveVia: [],
        includes: [],
        places: 2,
      },
    ]);
  });

  it("says which included permissions make one sensitive", () => {
    const [owner] = addedPermissions(
      [{ scope: "org:owner" as never, selectors: undefined }],
      catalogue,
    );
    expect(owner?.sensitive).toBe(true);
    expect(owner?.sensitiveVia).toEqual([
      "Manage organization",
      "View agent sessions",
    ]);
    expect(owner?.includes).toEqual([
      "Manage organization",
      "View organization",
      "View agent sessions",
    ]);
  });

  it("names no reason for a permission that is sensitive itself", () => {
    const [admin] = addedPermissions(
      [{ scope: "org:admin" as never, selectors: undefined }],
      catalogue,
    );
    expect(admin?.sensitive).toBe(true);
    expect(admin?.sensitiveVia).toEqual([]);
  });

  it("keeps the slug for a scope the catalogue has not loaded", () => {
    const [unknown] = addedPermissions(
      [{ scope: "widget:read" as never, selectors: undefined }],
      scopeCatalogue(undefined),
    );
    expect(unknown).toMatchObject({
      scope: "widget:read",
      label: "widget:read",
      sensitive: false,
      sensitiveVia: [],
    });
  });
});

describe("liftedExceptions", () => {
  const exception: SdkRoleGrant = {
    scope: "org:blocked_admin" as never,
    selectors: [{ resourceKind: "org", resourceId: "org-1" }],
  };
  const allowAdmin: SdkRoleGrant = {
    scope: "org:admin" as never,
    selectors: undefined,
  };

  it("reviews a lifted exception as a grant of the permission it withheld", () => {
    expect(liftedExceptions([exception], [allowAdmin], catalogue)).toEqual([
      {
        scope: "org:admin",
        selectors: [{ resourceKind: "org", resourceId: "org-1" }],
      },
    ]);
  });

  it("lifts nothing when the role no longer allows the permission", () => {
    expect(liftedExceptions([exception], [], catalogue)).toEqual([]);
  });

  it("ignores removed allows", () => {
    expect(liftedExceptions([allowAdmin], [allowAdmin], catalogue)).toEqual([]);
  });
});

describe("permissionsAddedByRoles", () => {
  const viewer = {
    grants: [{ scope: "project:read" as never, selectors: undefined }],
  };
  const reviewer = {
    grants: [
      { scope: "project:write" as never, selectors: undefined },
      { scope: "log:read" as never, selectors: undefined },
    ],
  };

  it("reports only what the new roles add", () => {
    expect(
      permissionsAddedByRoles([viewer], [viewer, reviewer], catalogue),
    ).toEqual({
      scopes: ["project:write", "log:read"],
      sensitive: ["log:read"],
    });
  });

  it("reports nothing when roles are only taken away", () => {
    expect(
      permissionsAddedByRoles([viewer, reviewer], [viewer], catalogue),
    ).toEqual({ scopes: [], sensitive: [] });
  });

  /** A role with one grant of the scope on one resource, or on every one with "*". */
  const roleWith = (scope: string, resourceKind: string, resourceId: string) =>
    ({
      grants: [{ scope, selectors: [{ resourceKind, resourceId }] }],
    }) as unknown as Pick<Role, "grants">;
  const oneProject = roleWith("log:read", "project", "p1");
  // The server stores an unrestricted grant as a wildcard selector.
  const everyProject = roleWith("log:read", "project", "*");
  const sessionsEverywhere = {
    grants: [{ scope: "chat:write" as never, selectors: undefined }],
  };

  it("counts a permission held on one project and now held everywhere", () => {
    expect(
      permissionsAddedByRoles(
        [oneProject],
        [oneProject, everyProject],
        catalogue,
      ),
    ).toEqual({ scopes: ["log:read"], sensitive: ["log:read"] });
  });

  it("counts nothing for a narrower grant of a permission held everywhere", () => {
    expect(
      permissionsAddedByRoles(
        [everyProject],
        [everyProject, oneProject],
        catalogue,
      ),
    ).toEqual({ scopes: [], sensitive: [] });
  });

  it("counts a permission held on one project and now on another too", () => {
    const otherProject = roleWith("log:read", "project", "p2");
    expect(
      permissionsAddedByRoles(
        [oneProject],
        [oneProject, otherProject],
        catalogue,
      ),
    ).toEqual({ scopes: ["log:read"], sensitive: ["log:read"] });
  });

  it("counts nothing for the same project again", () => {
    const sameProject = roleWith("log:read", "project", "p1");
    expect(
      permissionsAddedByRoles(
        [oneProject],
        [oneProject, sameProject],
        catalogue,
      ),
    ).toEqual({ scopes: [], sensitive: [] });
  });

  it("counts nothing for one of two projects already held", () => {
    const twoProjects = {
      grants: [
        {
          scope: "log:read" as never,
          selectors: [
            { resourceKind: "project" as const, resourceId: "p1" },
            { resourceKind: "project" as const, resourceId: "p2" },
          ],
        },
      ],
    };
    expect(
      permissionsAddedByRoles([twoProjects], [oneProject], catalogue),
    ).toEqual({ scopes: [], sensitive: [] });
  });

  const admin = {
    grants: [{ scope: "org:admin" as never, selectors: undefined }],
  };
  const blockedAdmin = {
    grants: [{ scope: "org:blocked_admin" as never, selectors: undefined }],
  };
  const blockedAdminHere = roleWith("org:blocked_admin", "org", "org-1");

  it("counts a permission an exception withheld once the exception is lifted", () => {
    expect(
      permissionsAddedByRoles([admin, blockedAdmin], [admin], catalogue),
    ).toEqual({ scopes: ["org:admin"], sensitive: ["org:admin"] });
  });

  it("counts a permission withheld on one resource once the exception is lifted", () => {
    expect(
      permissionsAddedByRoles([admin, blockedAdminHere], [admin], catalogue),
    ).toEqual({ scopes: ["org:admin"], sensitive: ["org:admin"] });
  });

  it("counts nothing from a permission the exception still withholds", () => {
    expect(
      permissionsAddedByRoles(
        [admin, blockedAdmin],
        [admin, blockedAdmin, viewer],
        catalogue,
      ),
    ).toEqual({ scopes: ["project:read"], sensitive: [] });
  });

  it("withholds only the permission the exception is for, not what it includes", () => {
    // The server blocks org:admin checks alone on org:blocked_admin; the
    // org:read and chat:read an org:admin grant satisfies stay satisfied.
    expect(
      permissionsAddedByRoles(
        [viewer],
        [viewer, admin, blockedAdmin],
        catalogue,
      ),
    ).toEqual({ scopes: ["org:read", "chat:read"], sensitive: ["chat:read"] });
  });

  it("takes an exception off the resources a restricted grant names", () => {
    const twoServers = {
      grants: [
        {
          scope: "mcp:connect" as never,
          selectors: [
            { resourceKind: "mcp" as const, resourceId: "server-a" },
            { resourceKind: "mcp" as const, resourceId: "server-b" },
          ],
        },
      ],
    };
    const blockedB = roleWith("mcp:blocked_connect", "mcp", "server-b");
    const serverB = roleWith("mcp:connect", "mcp", "server-b");
    expect(
      permissionsAddedByRoles(
        [twoServers, blockedB],
        [twoServers, blockedB, serverB],
        catalogue,
      ),
    ).toEqual({ scopes: [], sensitive: [] });
    expect(
      permissionsAddedByRoles([twoServers, blockedB], [twoServers], catalogue),
    ).toEqual({ scopes: ["mcp:connect"], sensitive: [] });
  });

  it("carries breadth down what a grant includes", () => {
    const oneSession = roleWith("chat:read", "chat", "c1");
    expect(
      permissionsAddedByRoles(
        [oneSession],
        [oneSession, sessionsEverywhere],
        catalogue,
      ),
    ).toEqual({
      scopes: ["chat:read", "chat:write"],
      sensitive: ["chat:read", "chat:write"],
    });
  });
});
