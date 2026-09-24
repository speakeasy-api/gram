import { describe, expect, it } from "vitest";
import { validateRegistryText } from "./registryValidation";

describe("registry syntax and size validation", () => {
  it.each([
    "{}",
    "null",
    '{"extension":9007199254740993}',
    '{"server":{"name":false}}',
  ])("leaves field validation to the server: %s", (raw) => {
    expect(validateRegistryText(raw)).toEqual([]);
  });

  it("reports invalid JSON syntax", () => {
    expect(validateRegistryText("{")[0]?.path).toBe("/");
  });

  it("bounds UTF-8 record bytes and escaped envelope bytes", () => {
    expect(
      validateRegistryText("é".repeat(4 * 1024 * 1024 + 1))[0]?.message,
    ).toMatch(/8 MiB/);
    expect(
      validateRegistryText("\u0000".repeat(3 * 1024 * 1024))[0]?.message,
    ).toMatch(/16 MiB/);
  });
});
