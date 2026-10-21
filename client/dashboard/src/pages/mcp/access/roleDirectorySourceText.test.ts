import { describe, expect, it } from "vitest";

import type { RoleDirectorySource } from "@/pages/access/directorySourcesForRole";
import { roleDirectorySourceText } from "./roleDirectorySourceText";

function source(
  label: string,
  overrides: Partial<RoleDirectorySource> = {},
): RoleDirectorySource {
  return {
    key: label,
    kind: "group",
    label,
    memberCount: 1,
    members: [],
    missing: false,
    ...overrides,
  };
}

describe("roleDirectorySourceText", () => {
  it("says nothing for a role no directory source gives out", () => {
    expect(roleDirectorySourceText([])).toBeUndefined();
  });

  it("names every source, live ones before removed ones", () => {
    expect(
      roleDirectorySourceText([
        source("Platform (removed)", { missing: true }),
        source("Engineering"),
        source("department = Sales", { kind: "attribute" }),
      ]),
    ).toBe(
      "Via directory: Engineering, department = Sales, Platform (removed)",
    );
  });
});
