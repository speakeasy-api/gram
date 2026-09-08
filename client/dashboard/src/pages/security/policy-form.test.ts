import { describe, expect, it } from "vitest";
import {
  ALL_CATEGORIES,
  AVAILABLE_CATEGORIES,
  PRESIDIO_CATEGORIES,
} from "./policy-form";

describe("policy category availability", () => {
  it("keeps off-policy visible but unavailable", () => {
    expect(ALL_CATEGORIES).toContain("off_policy");
    expect(AVAILABLE_CATEGORIES.has("off_policy")).toBe(false);
    expect(PRESIDIO_CATEGORIES).not.toContain("off_policy");
    expect(ALL_CATEGORIES.indexOf("off_policy")).toBe(
      ALL_CATEGORIES.indexOf("healthcare") + 1,
    );
  });
});
