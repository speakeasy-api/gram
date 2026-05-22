import { describe, it, expect } from "vitest";
import {
  addRoleToSelection,
  removeRoleFromSelection,
  getUnselectedRoles,
  hasRolesChanged,
  isUpdateDisabled,
  membersWithRole,
  isMemberLockedToRole,
  getSelectableMembers,
} from "./changeRoleState";

// --- addRoleToSelection ---

describe("addRoleToSelection", () => {
  it("appends a new role", () => {
    expect(addRoleToSelection(["a"], "b")).toEqual(["a", "b"]);
  });

  it("returns same array if role already present", () => {
    const selected = ["a", "b"];
    expect(addRoleToSelection(selected, "b")).toBe(selected);
  });

  it("works on empty selection", () => {
    expect(addRoleToSelection([], "a")).toEqual(["a"]);
  });
});

// --- removeRoleFromSelection ---

describe("removeRoleFromSelection", () => {
  it("removes a role", () => {
    expect(removeRoleFromSelection(["a", "b"], "a")).toEqual(["b"]);
  });

  it("enforces minimum of 1 role", () => {
    const selected = ["a"];
    expect(removeRoleFromSelection(selected, "a")).toBe(selected);
  });

  it("enforces minimum even with empty array", () => {
    const selected: string[] = [];
    expect(removeRoleFromSelection(selected, "a")).toBe(selected);
  });

  it("returns same array if role not found (but length > 1)", () => {
    expect(removeRoleFromSelection(["a", "b"], "c")).toEqual(["a", "b"]);
  });
});

// --- getUnselectedRoles ---

describe("getUnselectedRoles", () => {
  const roles = [
    { id: "1", name: "Admin" },
    { id: "2", name: "Builder" },
    { id: "3", name: "Viewer" },
  ];

  it("filters out selected roles", () => {
    expect(getUnselectedRoles(roles, ["1", "3"])).toEqual([
      { id: "2", name: "Builder" },
    ]);
  });

  it("returns all when nothing selected", () => {
    expect(getUnselectedRoles(roles, [])).toEqual(roles);
  });

  it("returns empty when all selected", () => {
    expect(getUnselectedRoles(roles, ["1", "2", "3"])).toEqual([]);
  });
});

// --- hasRolesChanged ---

describe("hasRolesChanged", () => {
  it("same roles same order → false", () => {
    expect(hasRolesChanged(["a", "b"], ["a", "b"])).toBe(false);
  });

  it("same roles different order → false (order-insensitive)", () => {
    expect(hasRolesChanged(["b", "a"], ["a", "b"])).toBe(false);
  });

  it("added role → true", () => {
    expect(hasRolesChanged(["a", "b", "c"], ["a", "b"])).toBe(true);
  });

  it("removed role → true", () => {
    expect(hasRolesChanged(["a"], ["a", "b"])).toBe(true);
  });

  it("swapped role → true", () => {
    expect(hasRolesChanged(["a", "c"], ["a", "b"])).toBe(true);
  });

  it("both empty → false", () => {
    expect(hasRolesChanged([], [])).toBe(false);
  });
});

// --- isUpdateDisabled ---

describe("isUpdateDisabled", () => {
  it("disabled when pending", () => {
    expect(
      isUpdateDisabled({
        isPending: true,
        selectedIds: ["a", "b"],
        originalIds: ["a"],
      }),
    ).toBe(true);
  });

  it("disabled when selection empty", () => {
    expect(
      isUpdateDisabled({
        isPending: false,
        selectedIds: [],
        originalIds: ["a"],
      }),
    ).toBe(true);
  });

  it("disabled when no changes", () => {
    expect(
      isUpdateDisabled({
        isPending: false,
        selectedIds: ["a", "b"],
        originalIds: ["a", "b"],
      }),
    ).toBe(true);
  });

  it("enabled when roles changed", () => {
    expect(
      isUpdateDisabled({
        isPending: false,
        selectedIds: ["a", "c"],
        originalIds: ["a", "b"],
      }),
    ).toBe(false);
  });

  it("enabled when role added", () => {
    expect(
      isUpdateDisabled({
        isPending: false,
        selectedIds: ["a", "b", "c"],
        originalIds: ["a", "b"],
      }),
    ).toBe(false);
  });
});

// --- membersWithRole ---

describe("membersWithRole", () => {
  const members = [
    { id: "m1", roleIds: ["admin", "builder"] },
    { id: "m2", roleIds: ["builder"] },
    { id: "m3", roleIds: ["viewer"] },
  ];

  it("returns members that have the role", () => {
    expect(membersWithRole(members, "builder")).toEqual(["m1", "m2"]);
  });

  it("returns empty for unknown role", () => {
    expect(membersWithRole(members, "nonexistent")).toEqual([]);
  });

  it("handles members with multiple roles", () => {
    expect(membersWithRole(members, "admin")).toEqual(["m1"]);
  });
});

// --- isMemberLockedToRole ---

describe("isMemberLockedToRole", () => {
  it("true when editing and member has the role", () => {
    expect(isMemberLockedToRole(true, "admin", ["admin", "builder"])).toBe(
      true,
    );
  });

  it("false when not editing", () => {
    expect(isMemberLockedToRole(false, "admin", ["admin"])).toBe(false);
  });

  it("false when editingRoleId is undefined", () => {
    expect(isMemberLockedToRole(true, undefined, ["admin"])).toBe(false);
  });

  it("false when editingRoleId is empty string", () => {
    expect(isMemberLockedToRole(true, "", ["admin"])).toBe(false);
  });

  it("false when member does not have the role", () => {
    expect(isMemberLockedToRole(true, "admin", ["builder"])).toBe(false);
  });
});

// --- getSelectableMembers ---

describe("getSelectableMembers", () => {
  const members = [
    { id: "m1", roleIds: ["admin", "builder"] },
    { id: "m2", roleIds: ["builder"] },
    { id: "m3", roleIds: ["viewer"] },
  ];

  it("excludes members with the editing role", () => {
    const result = getSelectableMembers(members, true, "builder");
    expect(result.map((m) => m.id)).toEqual(["m3"]);
  });

  it("returns all when not editing", () => {
    const result = getSelectableMembers(members, false, "builder");
    expect(result.map((m) => m.id)).toEqual(["m1", "m2", "m3"]);
  });

  it("returns all when editingRoleId is undefined", () => {
    const result = getSelectableMembers(members, true, undefined);
    expect(result.map((m) => m.id)).toEqual(["m1", "m2", "m3"]);
  });

  it("returns all when no members have the editing role", () => {
    const result = getSelectableMembers(members, true, "nonexistent");
    expect(result.map((m) => m.id)).toEqual(["m1", "m2", "m3"]);
  });
});
