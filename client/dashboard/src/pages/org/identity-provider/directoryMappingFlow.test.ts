import { afterEach, describe, expect, it } from "vitest";
import {
  availableRoleName,
  completeCreateRoleFlow,
  finishCreateRoleFlow,
  isCreateRoleFlow,
  pendingMappingFromParams,
  startCreateRoleFlow,
  suggestedRoleName,
} from "./directoryMappingFlow";

import type { DirectoryRoleMapping } from "@gram/client/models/components/directoryrolemapping.js";

afterEach(() => {
  window.sessionStorage.clear();
});

describe("availableRoleName", () => {
  it("keeps a free name", () => {
    expect(availableRoleName("Sales", [{ name: "Admin" }])).toBe("Sales");
  });

  it("numbers a taken name, ignoring case", () => {
    expect(
      availableRoleName("Sales", [{ name: "sales" }, { name: "Sales 2" }]),
    ).toBe("Sales 3");
  });
});

describe("create role round trip", () => {
  it("carries a group to the editor and back without putting it in the URL", () => {
    const toEditor = startCreateRoleFlow(
      { sourceKind: "group", directoryGroupId: "group-1" },
      "Sales",
    );
    expect(toEditor.toString()).not.toContain("group-1");
    expect(isCreateRoleFlow(toEditor)).toBe(true);
    expect(suggestedRoleName(toEditor)).toBe("Sales");

    const back = completeCreateRoleFlow(toEditor, "role:organization:1");
    const pending = pendingMappingFromParams(back);
    expect(pending?.form).toEqual({
      sourceKind: "group",
      directoryGroupId: "group-1",
      roleUrns: ["role:organization:1"],
      expectedRoleUrns: [],
    });

    const done = finishCreateRoleFlow(back, pending!.key);
    expect(done.toString()).toBe("");
    expect(pendingMappingFromParams(back)).toBeUndefined();
  });

  it("ignores a crafted link with no stored round trip", () => {
    const crafted = new URLSearchParams({
      from: "directory-mapping",
      mapping: "made-up-key",
    });
    expect(isCreateRoleFlow(crafted)).toBe(false);
    expect(pendingMappingFromParams(crafted)).toBeUndefined();
    expect(
      completeCreateRoleFlow(crafted, "role:global:admin").toString(),
    ).toBe("");
  });

  it("guards the replacement with the source's current roles", () => {
    const back = completeCreateRoleFlow(
      startCreateRoleFlow(
        {
          sourceKind: "attribute",
          attributeKey: "department",
          attributeValue: "Sales",
        },
        "Sales",
      ),
      "role:organization:new",
    );
    const mapping = (
      roleUrn: string,
      attributeValue: string,
    ): DirectoryRoleMapping => ({
      id: roleUrn,
      sourceKind: "attribute",
      attributeKey: "department",
      attributeValue,
      roleUrn,
      createdAt: new Date(0),
      updatedAt: new Date(0),
    });
    expect(
      pendingMappingFromParams(back, [
        mapping("role:organization:old", "Sales"),
        mapping("role:organization:other", "Engineering"),
      ])?.form,
    ).toEqual({
      sourceKind: "attribute",
      attributeKey: "department",
      attributeValue: "Sales",
      roleUrns: ["role:organization:new"],
      expectedRoleUrns: ["role:organization:old"],
    });
  });

  it("does not map before the editor records a created role", () => {
    const toEditor = startCreateRoleFlow(
      { sourceKind: "group", directoryGroupId: "group-1" },
      "Sales",
    );
    expect(pendingMappingFromParams(toEditor)).toBeUndefined();
  });
});
