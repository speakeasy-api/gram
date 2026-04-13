import { describe, expect, it } from "vitest";
import { computeChangedFields } from "./compute-changed-fields";

describe("computeChangedFields", () => {
  it("returns empty array when snapshots are identical", () => {
    const snapshot = { McpEnabled: true, Name: "test" };
    expect(computeChangedFields(snapshot, snapshot)).toEqual([]);
  });

  it("detects a single changed field", () => {
    const before = { McpEnabled: false, Name: "test" };
    const after = { McpEnabled: true, Name: "test" };
    expect(computeChangedFields(before, after)).toEqual([
      { field: "McpEnabled", oldValue: false, newValue: true },
    ]);
  });

  it("detects multiple changed fields", () => {
    const before = { McpEnabled: false, Description: "old", Name: "test" };
    const after = { McpEnabled: true, Description: "new", Name: "test" };
    const result = computeChangedFields(before, after);
    expect(result).toHaveLength(2);
    expect(result).toContainEqual({
      field: "McpEnabled",
      oldValue: false,
      newValue: true,
    });
    expect(result).toContainEqual({
      field: "Description",
      oldValue: "old",
      newValue: "new",
    });
  });

  it("detects added fields", () => {
    const before = { Name: "test" };
    const after = { Name: "test", McpEnabled: true };
    expect(computeChangedFields(before, after)).toEqual([
      { field: "McpEnabled", oldValue: undefined, newValue: true },
    ]);
  });

  it("detects removed fields", () => {
    const before = { Name: "test", McpEnabled: true };
    const after = { Name: "test" };
    expect(computeChangedFields(before, after)).toEqual([
      { field: "McpEnabled", oldValue: true, newValue: undefined },
    ]);
  });

  it("handles null/undefined snapshots", () => {
    expect(computeChangedFields(null, null)).toEqual([]);
    expect(computeChangedFields(undefined, undefined)).toEqual([]);
    expect(computeChangedFields(null, { Name: "test" })).toEqual([
      { field: "Name", oldValue: undefined, newValue: "test" },
    ]);
  });

  it("stringifies complex values for display", () => {
    const before = { Items: [1, 2] };
    const after = { Items: [1, 2, 3] };
    const result = computeChangedFields(before, after);
    expect(result).toHaveLength(1);
    expect(result[0].field).toBe("Items");
  });

  it("sorts results alphabetically by field name", () => {
    const before = { Zebra: 1, Apple: 1 };
    const after = { Zebra: 2, Apple: 2 };
    const result = computeChangedFields(before, after);
    expect(result[0].field).toBe("Apple");
    expect(result[1].field).toBe("Zebra");
  });
});
