import { describe, expect, it } from "vitest";

import {
  FUNCTION_PREFIX,
  applyFunctionToggle,
  extractFunctionSubscriptions,
} from "./autoSyncSources";

describe("extractFunctionSubscriptions", () => {
  it("returns an empty set for an empty input", () => {
    expect(extractFunctionSubscriptions([])).toEqual(new Set());
  });

  it("strips the function: prefix from each entry", () => {
    expect(
      extractFunctionSubscriptions(["function:billing", "function:catalog"]),
    ).toEqual(new Set(["billing", "catalog"]));
  });

  it("ignores entries that don't start with function:", () => {
    expect(
      extractFunctionSubscriptions(["http:stripe", "function:billing"]),
    ).toEqual(new Set(["billing"]));
  });

  it("dedupes identical entries", () => {
    expect(
      extractFunctionSubscriptions(["function:foo", "function:foo"]),
    ).toEqual(new Set(["foo"]));
  });

  it("treats the prefix constant as a single source of truth", () => {
    expect(FUNCTION_PREFIX).toBe("function:");
  });
});

describe("applyFunctionToggle", () => {
  it("adds a new entry when toggling on", () => {
    expect(applyFunctionToggle([], "billing", true)).toEqual([
      "function:billing",
    ]);
  });

  it("removes an entry when toggling off", () => {
    expect(applyFunctionToggle(["function:billing"], "billing", false)).toEqual(
      [],
    );
  });

  it("is idempotent on repeated toggling-on", () => {
    expect(applyFunctionToggle(["function:billing"], "billing", true)).toEqual([
      "function:billing",
    ]);
  });

  it("preserves non-function entries through a toggle", () => {
    const result = applyFunctionToggle(
      ["http:stripe", "function:billing"],
      "catalog",
      true,
    );
    expect(result).toContain("http:stripe");
    expect(result).toContain("function:billing");
    expect(result).toContain("function:catalog");
  });

  it("does not touch other function entries when toggling one off", () => {
    expect(
      applyFunctionToggle(
        ["function:billing", "function:catalog"],
        "billing",
        false,
      ),
    ).toEqual(["function:catalog"]);
  });
});
