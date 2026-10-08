import { describe, expect, it } from "vitest";
import {
  installModeRows,
  MEMBERS_ROW_KEY,
  rowInstallMode,
  summarizeInstallModes,
} from "./install-modes";

describe("installModeRows", () => {
  it("gives a single member its own row", () => {
    expect(installModeRows(["*", "user:a"])).toEqual([
      { key: "*", principalUrns: ["*"] },
      { key: "user:a", principalUrns: ["user:a"] },
    ]);
  });

  it("groups two or more members into one row", () => {
    expect(
      installModeRows(["role:organization:sre", "user:a", "user:b"]),
    ).toEqual([
      {
        key: "role:organization:sre",
        principalUrns: ["role:organization:sre"],
      },
      { key: MEMBERS_ROW_KEY, principalUrns: ["user:a", "user:b"] },
    ]);
  });

  it("keeps email principals as their own rows", () => {
    expect(installModeRows(["email:a@acme.test", "user:a", "user:b"])).toEqual([
      { key: "email:a@acme.test", principalUrns: ["email:a@acme.test"] },
      { key: MEMBERS_ROW_KEY, principalUrns: ["user:a", "user:b"] },
    ]);
  });
});

describe("rowInstallMode", () => {
  const members = { key: MEMBERS_ROW_KEY, principalUrns: ["user:a", "user:b"] };

  it("returns the shared mode", () => {
    expect(
      rowInstallMode(members, { "user:a": "required", "user:b": "required" }),
    ).toBe("required");
  });

  it("returns undefined when members differ", () => {
    expect(
      rowInstallMode(members, { "user:a": "required", "user:b": "available" }),
    ).toBeUndefined();
  });

  it("treats a principal with no mode as on by default", () => {
    expect(rowInstallMode(members, { "user:a": "default" })).toBe("default");
  });
});

describe("summarizeInstallModes", () => {
  it("lists audiences under each mode, strictest first", () => {
    expect(
      summarizeInstallModes([
        { label: "Everyone", mode: "available" },
        { label: "SRE", mode: "required" },
        { label: "12 members", mode: "default" },
        { label: "Design", mode: "required" },
      ]),
    ).toBe(
      "Required: SRE, Design · On by default: 12 members · Available: Everyone",
    );
  });

  it("lists rows with mixed modes last", () => {
    expect(
      summarizeInstallModes([
        { label: "12 members", mode: undefined },
        { label: "SRE", mode: "required" },
      ]),
    ).toBe("Required: SRE · Mixed: 12 members");
  });

  it("is never empty when only mixed rows are selected", () => {
    expect(
      summarizeInstallModes([{ label: "12 members", mode: undefined }]),
    ).toBe("Mixed: 12 members");
  });
});
