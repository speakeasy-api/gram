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

  it("names two live sources and counts removed ones", () => {
    expect(
      roleDirectorySourceText([
        source("Platform (removed)", { missing: true }),
        source("Engineering"),
        source("department = Sales", { kind: "attribute" }),
      ]),
    ).toEqual({
      line: "Via directory: Engineering, department = Sales · 1 removed source",
      full: "Via directory: Engineering, department = Sales, Platform (removed)",
    });
  });

  it("folds live sources past the second into a count", () => {
    expect(
      roleDirectorySourceText([
        source("Engineering"),
        source("Platform"),
        source("Design"),
        source("Support"),
      ])?.line,
    ).toBe("Via directory: Engineering, Platform +2 more");
  });

  it("counts removed sources when no live one is left", () => {
    expect(
      roleDirectorySourceText([
        source("Platform (removed)", { missing: true }),
        source("Design (removed)", { missing: true }),
      ])?.line,
    ).toBe("Via directory: 2 removed sources");
  });
});
