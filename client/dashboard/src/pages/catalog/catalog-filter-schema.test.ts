import { describe, expect, it } from "vitest";
import {
  CATALOG_FILTER_OPTIONS,
  toCatalogFilterValues,
} from "./catalog-filter-schema";

describe("native authentication filters", () => {
  it("offers only evidence-supported auth types", () => {
    expect(CATALOG_FILTER_OPTIONS.auth).toEqual([
      { value: "none", label: "No Auth" },
      { value: "oauth", label: "OAuth" },
      { value: "unknown", label: "Unknown" },
    ]);
  });
  it("ignores retired bookmarks without discarding supported filters", () => {
    expect(
      toCatalogFilterValues({
        auth: ["apikey", "other", "oauth"],
        setup: ["manual"],
        updated: "week",
      }),
    ).toEqual({
      authTypes: ["oauth"],
      setupTypes: ["manual"],
      updatedRange: "week",
    });
    expect(
      toCatalogFilterValues({
        auth: ["apikey", "other"],
        setup: [],
        updated: "any",
      }).authTypes,
    ).toEqual([]);
  });
});
