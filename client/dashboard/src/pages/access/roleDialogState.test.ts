import { describe, it, expect } from "vitest";
import {
  effectiveGrantCount,
  grantKeysString,
  hasFormChanges,
  isSaveDisabled,
  membersHaveChanged,
  type SaveButtonInput,
} from "./roleDialogState";
import type { RoleGrant, Scope, Selector } from "./types";

// --- Helpers ---

function grant(scope: Scope, selectors: Selector[] | null = null): RoleGrant {
  return { scope, selectors };
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
      grantKeys: "project:read,project:write",
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
  it("sorts keys and joins", () => {
    expect(
      grantKeysString({
        "mcp:write": grant("mcp:write"),
        "project:read": grant("project:read"),
      }),
    ).toBe("mcp:write,project:read");
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
