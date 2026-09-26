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

it.each(["\ud800", "\udfff", "x\ud800y", "\ud800\ud800", "\udc00\ud800"])(
  "rejects literal unpaired UTF-16 in raw text: %j",
  (value) => {
    const raw = '{"extension":"' + value + '"}';
    expect(() => JSON.parse(raw)).not.toThrow();
    expect(validateRegistryText(raw)).toEqual([
      {
        path: "/",
        message:
          "Record contains an unpaired UTF-16 surrogate. Use a JSON escape (\\uXXXX) or a valid Unicode character.",
      },
    ]);
  },
);

it.each([
  '{"extension":"😀"}',
  '{"extension":"\\ud800"}',
  '{"extension":"\\udc00"}',
  '{"extension":"\\ud83d\\ude00"}',
])("accepts lossless raw text without parsing its string values: %s", (raw) =>
  expect(validateRegistryText(raw)).toEqual([]),
);
