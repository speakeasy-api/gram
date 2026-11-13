import { describe, expect, it } from "vitest";
import { indexErrorRequiresLegacyFallback } from "./useProjectResourceIndexCandidates";

describe("project resource index failure fallback", () => {
  it("falls back after a failed refresh even when cached rows exist", () => {
    expect(indexErrorRequiresLegacyFallback(true, false, true)).toBe(true);
  });

  it("does not fall back while the refresh is still in flight", () => {
    expect(indexErrorRequiresLegacyFallback(true, true, false)).toBe(false);
  });
});
