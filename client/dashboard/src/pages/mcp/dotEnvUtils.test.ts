import { describe, expect, it } from "vitest";
import { parseDotEnv } from "./dotEnvUtils";

describe("parseDotEnv", () => {
  it("parses dotenv assignments while ignoring comments and blank lines", () => {
    expect(
      parseDotEnv(`
# API credentials
API_KEY=secret-value
BASE_URL = https://example.test/path?a=b

export EMPTY_VALUE=
`),
    ).toEqual({
      entries: [
        { key: "API_KEY", value: "secret-value" },
        { key: "BASE_URL", value: "https://example.test/path?a=b" },
        { key: "EMPTY_VALUE", value: "" },
      ],
      invalidLineNumbers: [],
    });
  });

  it("handles quoted values and inline comments", () => {
    expect(
      parseDotEnv(`
HASH_VALUE="value # kept"
SINGLE_QUOTED='also # kept'
COMMENTED=value # removed
MULTILINE="first\\nsecond"
`),
    ).toEqual({
      entries: [
        { key: "HASH_VALUE", value: "value # kept" },
        { key: "SINGLE_QUOTED", value: "also # kept" },
        { key: "COMMENTED", value: "value" },
        { key: "MULTILINE", value: "first\nsecond" },
      ],
      invalidLineNumbers: [],
    });
  });

  it("reports malformed lines without discarding valid assignments", () => {
    expect(
      parseDotEnv('VALID=value\nnot an assignment\nUNCLOSED="value'),
    ).toEqual({
      entries: [{ key: "VALID", value: "value" }],
      invalidLineNumbers: [2, 3],
    });
  });

  it("does not mistake a plain key for dotenv content", () => {
    expect(parseDotEnv("API_KEY")).toEqual({
      entries: [],
      invalidLineNumbers: [1],
    });
  });
});
