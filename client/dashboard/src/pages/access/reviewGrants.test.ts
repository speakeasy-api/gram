import type { RoleGrant as SdkRoleGrant } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import { describe, expect, it } from "vitest";

import {
  addedPermissions,
  liftedExceptions,
  permissionsAddedByRoles,
  rolePermissionSummary,
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
  def("project:read", "View projects"),
  def("project:write", "Manage projects", { subScopes: ["project:read"] }),
  def("mcp:connect", "Connect to MCP servers"),
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
        sensitiveVia: [],
        includes: ["View MCP servers", "Connect to MCP servers"],
        places: null,
      },
      {
        scope: "log:read",
        label: "View tool logs",
        description: "View tool logs.",
        sensitiveVia: ["log:read"],
        includes: [],
        places: 2,
      },
    ]);
  });

  it("marks a permission sensitive through what it includes", () => {
    const [admin] = addedPermissions(
      [{ scope: "org:admin" as never, selectors: undefined }],
      catalogue,
    );
    expect(admin?.sensitiveVia).toEqual(["org:admin", "chat:read"]);
    expect(admin?.includes).toEqual([
      "View organization",
      "View agent sessions",
    ]);
  });

  it("keeps the slug for a scope the catalogue has not loaded", () => {
    const [unknown] = addedPermissions(
      [{ scope: "widget:read" as never, selectors: undefined }],
      scopeCatalogue(undefined),
    );
    expect(unknown).toMatchObject({
      scope: "widget:read",
      label: "widget:read",
      sensitiveVia: [],
    });
  });
});

describe("rolePermissionSummary", () => {
  it("lists what a role gives in catalogue order with the sensitive subset", () => {
    const summary = rolePermissionSummary(
      {
        grants: [
          { scope: "chat:write" as never, selectors: undefined },
          { scope: "mcp:write" as never, selectors: undefined },
          { scope: "org:blocked_read" as never, selectors: undefined },
        ],
      },
      catalogue,
    );
    expect(summary.scopes).toEqual([
      "mcp:connect",
      "mcp:read",
      "mcp:write",
      "chat:read",
      "chat:write",
    ]);
    expect(summary.sensitive).toEqual(["chat:read", "chat:write"]);
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
});
