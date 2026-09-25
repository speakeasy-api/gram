import { describe, expect, it } from "vitest";

import {
  availableRoleName,
  createRoleForMappingParams,
  directoryMappingReturnParams,
  pendingMappingFromParams,
  suggestedRoleName,
} from "./directoryMappingFlow";

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

describe("directory mapping round trip", () => {
  it("carries a group and suggested name to the editor and back", () => {
    const toEditor = createRoleForMappingParams(
      { sourceKind: "group", directoryGroupId: "group-1" },
      "Sales",
    );
    expect(suggestedRoleName(toEditor)).toBe("Sales");

    const back = directoryMappingReturnParams(toEditor, "role:organization:1");
    expect(pendingMappingFromParams(back)).toEqual({
      sourceKind: "group",
      directoryGroupId: "group-1",
      roleUrn: "role:organization:1",
    });
  });
});
