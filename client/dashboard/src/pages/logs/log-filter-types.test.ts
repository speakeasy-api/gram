import { describe, expect, it } from "vitest";
import { Operator } from "@gram/client/models/components/logfilter";
import type { ActiveLogFilter } from "./log-filter-types";
import { applyFilterAdd, applyFilterEdit } from "./log-filter-types";

function makeFilter(
  overrides: Partial<ActiveLogFilter> & { path: string; op: Operator },
): ActiveLogFilter {
  return { id: "test-id", ...overrides };
}

describe("applyFilterAdd", () => {
  it("appends to empty list", () => {
    const result = applyFilterAdd([], {
      path: "http.status",
      op: Operator.Eq,
      value: "200",
    });
    expect(result).toHaveLength(1);
    expect(result[0].path).toBe("http.status");
    expect(result[0].op).toBe(Operator.Eq);
    expect(result[0].value).toBe("200");
    expect(result[0].id).toBeDefined();
  });

  it("replaces eq with eq on same path", () => {
    const existing = [makeFilter({ path: "x", op: Operator.Eq, value: "1" })];
    const result = applyFilterAdd(existing, {
      path: "x",
      op: Operator.Eq,
      value: "2",
    });
    expect(result).toHaveLength(1);
    expect(result[0].value).toBe("2");
  });

  it("replaces in with in on same path", () => {
    const existing = [makeFilter({ path: "x", op: Operator.In, value: "a,b" })];
    const result = applyFilterAdd(existing, {
      path: "x",
      op: Operator.In,
      value: "c,d",
    });
    expect(result).toHaveLength(1);
    expect(result[0].value).toBe("c,d");
  });

  it("does not replace not_eq when adding eq on same path", () => {
    const existing = [
      makeFilter({ path: "x", op: Operator.NotEq, value: "1" }),
    ];
    const result = applyFilterAdd(existing, {
      path: "x",
      op: Operator.Eq,
      value: "2",
    });
    expect(result).toHaveLength(2);
  });

  it("stacks not_eq on same path", () => {
    const existing = [
      makeFilter({ path: "x", op: Operator.NotEq, value: "A" }),
    ];
    const result = applyFilterAdd(existing, {
      path: "x",
      op: Operator.NotEq,
      value: "B",
    });
    expect(result).toHaveLength(2);
  });

  it("stacks contains on same path", () => {
    const existing = [
      makeFilter({ path: "x", op: Operator.Contains, value: "foo" }),
    ];
    const result = applyFilterAdd(existing, {
      path: "x",
      op: Operator.Contains,
      value: "bar",
    });
    expect(result).toHaveLength(2);
  });

  it("does not interfere across different paths", () => {
    const existing = [makeFilter({ path: "x", op: Operator.Eq, value: "1" })];
    const result = applyFilterAdd(existing, {
      path: "y",
      op: Operator.Eq,
      value: "1",
    });
    expect(result).toHaveLength(2);
  });
});

describe("applyFilterEdit", () => {
  it("updates op and value while preserving id and path", () => {
    const existing = [
      makeFilter({ id: "a", path: "x", op: Operator.Eq, value: "1" }),
    ];
    const result = applyFilterEdit(existing, "a", {
      op: Operator.NotEq,
      value: "2",
    });
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe("a");
    expect(result[0].path).toBe("x");
    expect(result[0].op).toBe(Operator.NotEq);
    expect(result[0].value).toBe("2");
  });

  it("preserves position in the list", () => {
    const existing = [
      makeFilter({ id: "a", path: "x", op: Operator.Eq, value: "1" }),
      makeFilter({ id: "b", path: "y", op: Operator.Eq, value: "2" }),
      makeFilter({ id: "c", path: "z", op: Operator.Eq, value: "3" }),
    ];
    const result = applyFilterEdit(existing, "b", {
      op: Operator.Contains,
      value: "yo",
    });
    expect(result.map((f) => f.id)).toEqual(["a", "b", "c"]);
    expect(result[1].op).toBe(Operator.Contains);
    expect(result[1].value).toBe("yo");
  });

  it("removes a colliding eq filter on the same path", () => {
    const existing = [
      makeFilter({ id: "a", path: "x", op: Operator.Eq, value: "1" }),
      makeFilter({ id: "b", path: "x", op: Operator.NotEq, value: "2" }),
    ];
    // Editing 'b' to be eq=1 should collide with 'a' and drop it.
    const result = applyFilterEdit(existing, "b", {
      op: Operator.Eq,
      value: "1",
    });
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe("b");
    expect(result[0].op).toBe(Operator.Eq);
  });

  it("does not collide when edit keeps a stacking op (not_eq)", () => {
    const existing = [
      makeFilter({ id: "a", path: "x", op: Operator.NotEq, value: "1" }),
      makeFilter({ id: "b", path: "x", op: Operator.NotEq, value: "2" }),
    ];
    const result = applyFilterEdit(existing, "b", {
      op: Operator.NotEq,
      value: "3",
    });
    expect(result).toHaveLength(2);
    expect(result.map((f) => f.value)).toEqual(["1", "3"]);
  });

  it("is a no-op when id is not found", () => {
    const existing = [
      makeFilter({ id: "a", path: "x", op: Operator.Eq, value: "1" }),
    ];
    const result = applyFilterEdit(existing, "missing", {
      op: Operator.NotEq,
      value: "2",
    });
    expect(result).toEqual(existing);
  });
});
