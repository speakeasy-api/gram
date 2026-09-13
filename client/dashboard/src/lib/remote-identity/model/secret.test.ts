import { describe, expect, it } from "vitest";
import {
  REDACTED_SECRET,
  UNCHANGED_SECRET,
  secretFromServerValue,
  secretHasValue,
  secretToWireValue,
  secretsEqual,
} from "./secret";

describe("secretFromServerValue", () => {
  it("reads a redacted value as unchanged, not as empty", () => {
    // The bug this type exists to prevent: treating "***" as the value, or as
    // an empty field the operator has cleared.
    expect(secretFromServerValue(REDACTED_SECRET, true)).toEqual(
      UNCHANGED_SECRET,
    );
    expect(secretFromServerValue(undefined, true)).toEqual(UNCHANGED_SECRET);
  });

  it("reads a readable value as set", () => {
    expect(secretFromServerValue("Bearer abc", false)).toEqual({
      kind: "set",
      value: "Bearer abc",
    });
  });

  it("does not redact a non-secret that happens to look like the sentinel", () => {
    expect(secretFromServerValue(REDACTED_SECRET, false)).toEqual({
      kind: "set",
      value: REDACTED_SECRET,
    });
  });
});

describe("secretToWireValue", () => {
  it("omits an unchanged secret rather than sending the sentinel back", () => {
    // Sending "***" would store the literal string as the credential.
    expect(secretToWireValue(UNCHANGED_SECRET)).toBeUndefined();
  });

  it("sends a set value, including a deliberate empty one", () => {
    expect(secretToWireValue({ kind: "set", value: "abc" })).toBe("abc");
    expect(secretToWireValue({ kind: "set", value: "" })).toBe("");
  });
});

describe("secretHasValue", () => {
  it("treats whitespace and unchanged as nothing entered", () => {
    expect(secretHasValue(UNCHANGED_SECRET)).toBe(false);
    expect(secretHasValue({ kind: "set", value: "   " })).toBe(false);
    expect(secretHasValue({ kind: "set", value: "abc" })).toBe(true);
  });
});

describe("secretsEqual", () => {
  it("separates unchanged from a set value", () => {
    expect(secretsEqual(UNCHANGED_SECRET, UNCHANGED_SECRET)).toBe(true);
    expect(secretsEqual(UNCHANGED_SECRET, { kind: "set", value: "" })).toBe(
      false,
    );
    expect(
      secretsEqual({ kind: "set", value: "a" }, { kind: "set", value: "a" }),
    ).toBe(true);
    expect(
      secretsEqual({ kind: "set", value: "a" }, { kind: "set", value: "b" }),
    ).toBe(false);
  });
});
