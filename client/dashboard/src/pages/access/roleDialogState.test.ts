import { describe, it, expect } from "vitest";
import {
  effectiveGrantCount,
  grantKeysString,
  hasFormChanges,
  isSaveDisabled,
  membersHaveChanged,
  computeRuleLabel,
  computeRuleTooltip,
  type SaveButtonInput,
} from "./roleDialogState";
import type { RoleGrant, Scope, Selector } from "./types";

// --- Helpers ---

function grant(scope: Scope, selectors: Selector[] | null = null): RoleGrant {
  return {
    scope,
    rules: [{ id: "test", effect: "allow", selectors }],
  };
}

function makeInput(overrides: Partial<SaveButtonInput> = {}): SaveButtonInput {
  return {
    isMutating: false,
    isEditing: true,
    isSystemRole: false,
    name: "Engineer",
    description: "Can build things",
    grants: {
      "project:read": grant("project:read"),
      "project:write": grant("project:write"),
    },
    selectedMembers: new Set(["m1", "m2"]),
    initial: {
      name: "Engineer",
      description: "Can build things",
      grantKeys: "project:read[allow:*],project:write[allow:*]",
      members: new Set(["m1", "m2"]),
    },
    ...overrides,
  };
}

// --- effectiveGrantCount ---

describe("effectiveGrantCount", () => {
  it("counts grants with null selectors (unrestricted)", () => {
    expect(
      effectiveGrantCount({
        "project:read": grant("project:read"),
        "mcp:read": grant("mcp:read"),
      }),
    ).toBe(2);
  });

  it("excludes grants with empty selector arrays", () => {
    expect(
      effectiveGrantCount({
        "project:read": grant("project:read"),
        "mcp:read": grant("mcp:read", []),
      }),
    ).toBe(1);
  });

  it("returns 0 for empty grants", () => {
    expect(effectiveGrantCount({})).toBe(0);
  });
});

// --- membersHaveChanged ---

describe("membersHaveChanged", () => {
  it("same sets → false", () => {
    expect(membersHaveChanged(new Set(["a", "b"]), new Set(["a", "b"]))).toBe(
      false,
    );
  });

  it("added member → true", () => {
    expect(
      membersHaveChanged(new Set(["a", "b", "c"]), new Set(["a", "b"])),
    ).toBe(true);
  });

  it("removed member → true", () => {
    expect(membersHaveChanged(new Set(["a"]), new Set(["a", "b"]))).toBe(true);
  });

  it("swapped member → true", () => {
    expect(membersHaveChanged(new Set(["a", "c"]), new Set(["a", "b"]))).toBe(
      true,
    );
  });

  it("both empty → false", () => {
    expect(membersHaveChanged(new Set(), new Set())).toBe(false);
  });
});

// --- grantKeysString ---

describe("grantKeysString", () => {
  it("sorts keys and joins with rule summaries", () => {
    expect(
      grantKeysString({
        "mcp:write": grant("mcp:write"),
        "project:read": grant("project:read"),
      }),
    ).toBe("mcp:write[allow:*],project:read[allow:*]");
  });

  it("empty grants → empty string", () => {
    expect(grantKeysString({})).toBe("");
  });
});

// --- hasFormChanges ---

describe("hasFormChanges", () => {
  it("no changes → false", () => {
    expect(hasFormChanges(makeInput())).toBe(false);
  });

  it("name changed → true", () => {
    expect(hasFormChanges(makeInput({ name: "Architect" }))).toBe(true);
  });

  it("description changed → true", () => {
    expect(hasFormChanges(makeInput({ description: "New description" }))).toBe(
      true,
    );
  });

  it("grant added → true", () => {
    expect(
      hasFormChanges(
        makeInput({
          grants: {
            "project:read": grant("project:read"),
            "project:write": grant("project:write"),
            "mcp:read": grant("mcp:read"),
          },
        }),
      ),
    ).toBe(true);
  });

  it("member added → true", () => {
    expect(
      hasFormChanges(
        makeInput({ selectedMembers: new Set(["m1", "m2", "m3"]) }),
      ),
    ).toBe(true);
  });

  it("member removed → true", () => {
    expect(
      hasFormChanges(makeInput({ selectedMembers: new Set(["m1"]) })),
    ).toBe(true);
  });

  it("create mode → always true (no initial state to compare)", () => {
    expect(hasFormChanges(makeInput({ isEditing: false }))).toBe(true);
  });
});

// --- isSaveDisabled ---

describe("isSaveDisabled", () => {
  describe("create mode (non-system)", () => {
    it("valid form → enabled", () => {
      expect(isSaveDisabled(makeInput({ isEditing: false }))).toBe(false);
    });

    it("empty name → disabled", () => {
      expect(isSaveDisabled(makeInput({ isEditing: false, name: "" }))).toBe(
        true,
      );
    });

    it("empty description → disabled", () => {
      expect(
        isSaveDisabled(makeInput({ isEditing: false, description: "" })),
      ).toBe(true);
    });

    it("no grants → disabled", () => {
      expect(isSaveDisabled(makeInput({ isEditing: false, grants: {} }))).toBe(
        true,
      );
    });

    it("mutating → disabled", () => {
      expect(
        isSaveDisabled(makeInput({ isEditing: false, isMutating: true })),
      ).toBe(true);
    });
  });

  describe("edit mode (non-system)", () => {
    it("no changes → disabled", () => {
      expect(isSaveDisabled(makeInput())).toBe(true);
    });

    it("name changed → enabled", () => {
      expect(isSaveDisabled(makeInput({ name: "New Name" }))).toBe(false);
    });

    it("description changed → enabled", () => {
      expect(isSaveDisabled(makeInput({ description: "Updated" }))).toBe(false);
    });

    it("grant added → enabled", () => {
      expect(
        isSaveDisabled(
          makeInput({
            grants: {
              "project:read": grant("project:read"),
              "project:write": grant("project:write"),
              "mcp:read": grant("mcp:read"),
            },
          }),
        ),
      ).toBe(false);
    });

    it("member added → enabled", () => {
      expect(
        isSaveDisabled(
          makeInput({ selectedMembers: new Set(["m1", "m2", "m3"]) }),
        ),
      ).toBe(false);
    });

    it("member removed → enabled", () => {
      expect(
        isSaveDisabled(makeInput({ selectedMembers: new Set(["m1"]) })),
      ).toBe(false);
    });

    it("only member changed, form still valid → enabled", () => {
      expect(
        isSaveDisabled(
          makeInput({ selectedMembers: new Set(["m1", "m2", "m3"]) }),
        ),
      ).toBe(false);
    });

    it("member added on role with zero grants → enabled (role already exists)", () => {
      expect(
        isSaveDisabled(
          makeInput({
            grants: {},
            selectedMembers: new Set(["m1", "m2", "m3"]),
            initial: {
              name: "Engineer",
              description: "Can build things",
              grantKeys: "",
              members: new Set(["m1", "m2"]),
            },
          }),
        ),
      ).toBe(false);
    });

    it("grants removed to zero AND member changed → enabled (backend validates)", () => {
      expect(
        isSaveDisabled(
          makeInput({
            grants: {},
            selectedMembers: new Set(["m1", "m2", "m3"]),
          }),
        ),
      ).toBe(false);
    });

    it("mutating → disabled even with changes", () => {
      expect(
        isSaveDisabled(makeInput({ name: "Changed", isMutating: true })),
      ).toBe(true);
    });
  });

  describe("edit mode (system role)", () => {
    const systemBase = (): SaveButtonInput => makeInput({ isSystemRole: true });

    it("no changes → disabled", () => {
      expect(isSaveDisabled(systemBase())).toBe(true);
    });

    it("member added → enabled", () => {
      expect(
        isSaveDisabled({
          ...systemBase(),
          selectedMembers: new Set(["m1", "m2", "m3"]),
        }),
      ).toBe(false);
    });

    it("member removed → enabled", () => {
      expect(
        isSaveDisabled({
          ...systemBase(),
          selectedMembers: new Set(["m1"]),
        }),
      ).toBe(false);
    });

    it("enabled even with zero grants (system roles skip form validation)", () => {
      expect(
        isSaveDisabled({
          ...systemBase(),
          grants: {},
          selectedMembers: new Set(["m1", "m2", "m3"]),
        }),
      ).toBe(false);
    });

    it("mutating → disabled", () => {
      expect(
        isSaveDisabled({
          ...systemBase(),
          selectedMembers: new Set(["m1", "m2", "m3"]),
          isMutating: true,
        }),
      ).toBe(true);
    });
  });
});

// --- Selector helpers ---

const projects = [
  { id: "p1", name: "ecommerce-api" },
  { id: "p2", name: "my-app" },
];

function sel(overrides: Partial<Selector> = {}): Selector {
  return { resourceId: "*", resourceKind: "mcp_server", ...overrides };
}

// --- computeRuleLabel ---

describe("computeRuleLabel", () => {
  it("null selectors (mcp) → All servers", () => {
    expect(computeRuleLabel(null, "mcp", projects)).toBe("All servers");
  });

  it("null selectors (project) → All projects", () => {
    expect(computeRuleLabel(null, "project", projects)).toBe("All projects");
  });

  it("empty selectors → Select…", () => {
    expect(computeRuleLabel([], "mcp", projects)).toBe("Select\u2026");
  });

  it("single disposition → Destructive tools", () => {
    expect(
      computeRuleLabel([sel({ disposition: "destructive" })], "mcp", projects),
    ).toBe("Destructive tools");
  });

  it("multiple dispositions → comma-joined", () => {
    expect(
      computeRuleLabel(
        [
          sel({ disposition: "read_only" }),
          sel({ disposition: "destructive" }),
        ],
        "mcp",
        projects,
      ),
    ).toBe("Read-only, Destructive");
  });

  it("single tool → tool name", () => {
    expect(
      computeRuleLabel([sel({ tool: "listUsers" })], "mcp", projects),
    ).toBe("listUsers");
  });

  it("multiple tools → count", () => {
    expect(
      computeRuleLabel(
        [sel({ tool: "listUsers" }), sel({ tool: "deleteUser" })],
        "mcp",
        projects,
      ),
    ).toBe("2 tools");
  });

  it("single project with name → Project: name", () => {
    expect(computeRuleLabel([sel({ projectId: "p1" })], "mcp", projects)).toBe(
      "Project: ecommerce-api",
    );
  });

  it("single project without name → 1 project", () => {
    expect(computeRuleLabel([sel({ projectId: "unknown" })], "mcp", [])).toBe(
      "1 project",
    );
  });

  it("multiple projects → count", () => {
    expect(
      computeRuleLabel(
        [sel({ projectId: "p1" }), sel({ projectId: "p2" })],
        "mcp",
        projects,
      ),
    ).toBe("2 projects");
  });

  it("single server → 1 server", () => {
    expect(
      computeRuleLabel([sel({ resourceId: "srv1" })], "mcp", projects),
    ).toBe("1 server");
  });

  it("multiple servers → count", () => {
    expect(
      computeRuleLabel(
        [sel({ resourceId: "srv1" }), sel({ resourceId: "srv2" })],
        "mcp",
        projects,
      ),
    ).toBe("2 servers");
  });
});

// --- computeRuleTooltip ---

describe("computeRuleTooltip", () => {
  it("allow null (mcp) → permits all servers", () => {
    expect(computeRuleTooltip("allow", null, "mcp", projects)).toBe(
      "Permits access to all servers across your org",
    );
  });

  it("deny null (project) → denies all projects", () => {
    expect(computeRuleTooltip("deny", null, "project", projects)).toBe(
      "Denies access to all projects in your org",
    );
  });

  it("empty selectors → none selected", () => {
    expect(computeRuleTooltip("allow", [], "mcp", projects)).toBe(
      "Permits access (none selected)",
    );
  });

  it("single disposition → descriptive", () => {
    expect(
      computeRuleTooltip(
        "deny",
        [sel({ disposition: "destructive" })],
        "mcp",
        projects,
      ),
    ).toBe("Denies access to all destructive tools");
  });

  it("multiple dispositions → joined with 'and'", () => {
    expect(
      computeRuleTooltip(
        "allow",
        [sel({ disposition: "read_only" }), sel({ disposition: "idempotent" })],
        "mcp",
        projects,
      ),
    ).toBe("Permits access to all read-only and idempotent tools");
  });

  it("single tool → names the tool", () => {
    expect(
      computeRuleTooltip(
        "deny",
        [sel({ tool: "deleteUser" })],
        "mcp",
        projects,
      ),
    ).toBe("Denies access to deleteUser");
  });

  it("multiple tools → count only", () => {
    expect(
      computeRuleTooltip(
        "allow",
        [sel({ tool: "listUsers" }), sel({ tool: "getUser" })],
        "mcp",
        projects,
      ),
    ).toBe("Permits access to 2 tools");
  });

  it("single project with name → names the project", () => {
    expect(
      computeRuleTooltip("allow", [sel({ projectId: "p1" })], "mcp", projects),
    ).toBe("Permits access to all servers in ecommerce-api");
  });

  it("single project without name → generic", () => {
    expect(
      computeRuleTooltip("allow", [sel({ projectId: "x" })], "mcp", []),
    ).toBe("Permits access to 1 project");
  });

  it("multiple projects → count", () => {
    expect(
      computeRuleTooltip(
        "deny",
        [sel({ projectId: "p1" }), sel({ projectId: "p2" })],
        "mcp",
        projects,
      ),
    ).toBe("Denies access to 2 projects");
  });

  it("single server → count", () => {
    expect(
      computeRuleTooltip(
        "deny",
        [sel({ resourceId: "srv1" })],
        "mcp",
        projects,
      ),
    ).toBe("Denies access to 1 server");
  });

  it("multiple servers → count", () => {
    expect(
      computeRuleTooltip(
        "allow",
        [sel({ resourceId: "s1" }), sel({ resourceId: "s2" })],
        "mcp",
        projects,
      ),
    ).toBe("Permits access to 2 servers");
  });
});
