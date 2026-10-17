import { describe, expect, it } from "vitest";
import {
  enumPresentation,
  isKnownEnumValue,
  requireKnownEnumValue,
} from "./enum";

describe("enum compatibility", () => {
  const values = { Allow: "allow", Deny: "deny" } as const;
  const labels = { allow: "Allowed", deny: "Denied" };

  it.each(["future_value", "__proto__", "constructor", "toString"])(
    "presents %s neutrally and rejects it at closed request boundaries",
    (value) => {
      expect(enumPresentation(labels, value, "Unknown")).toBe("Unknown");
      expect(isKnownEnumValue(values, value)).toBe(false);
      expect(() => requireKnownEnumValue(values, value)).toThrow(
        "not supported",
      );
    },
  );
  it("preserves known values and presentation", () => {
    expect(enumPresentation(labels, "allow", "Unknown")).toBe("Allowed");
    expect(requireKnownEnumValue(values, "deny")).toBe("deny");
  });
});
