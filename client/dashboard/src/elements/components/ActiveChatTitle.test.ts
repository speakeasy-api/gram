import { describe, expect, it } from "vitest";
import { resolveTitleEdit } from "./activeChatTitle.helpers";

describe("resolveTitleEdit", () => {
  it("trims surrounding whitespace and marks a new value as changed", () => {
    expect(resolveTitleEdit("  My Chat  ", "")).toEqual({
      changed: true,
      value: "My Chat",
    });
  });

  it("treats an unchanged title as a no-op", () => {
    expect(resolveTitleEdit("My Chat", "My Chat")).toEqual({
      changed: false,
      value: "My Chat",
    });
  });

  it("ignores whitespace-only differences against the current title", () => {
    expect(resolveTitleEdit("  My Chat  ", "My Chat")).toEqual({
      changed: false,
      value: "My Chat",
    });
  });

  it("treats clearing a set title as a reset to auto-naming", () => {
    expect(resolveTitleEdit("", "My Chat")).toEqual({
      changed: true,
      value: "",
    });
  });

  it("does not save when an untitled chat is left blank", () => {
    expect(resolveTitleEdit("   ", "")).toEqual({
      changed: false,
      value: "",
    });
  });
});
