import { describe, expect, it } from "vitest";
import { copyName, MAX_NAME_LENGTH } from "./widgetNames";

describe("copyName", () => {
  it("appends the copy suffix to a short name", () => {
    expect(copyName("Cost by model")).toBe("Cost by model (copy)");
  });

  it("shortens a long multi-byte name by code points to fit", () => {
    const name = "😀".repeat(MAX_NAME_LENGTH);
    const copy = copyName(name);
    expect(Array.from(copy)).toHaveLength(MAX_NAME_LENGTH);
    expect(copy).toBe(
      "😀".repeat(MAX_NAME_LENGTH - " (copy)".length) + " (copy)",
    );
  });

  it("keeps a name that just fits whole", () => {
    const name = "é".repeat(MAX_NAME_LENGTH - " (copy)".length);
    expect(copyName(name)).toBe(`${name} (copy)`);
  });
});
