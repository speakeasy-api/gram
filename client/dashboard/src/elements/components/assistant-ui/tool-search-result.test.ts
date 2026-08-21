import { describe, expect, it } from "vitest";

import { rendersDefaultToolWidget } from "./default-tool-components";
import { isCatalogBrowseSearch } from "./tool-search-result.helpers";

describe("isCatalogBrowseSearch", () => {
  it("accepts the browse marker, however the model spaced or cased it", () => {
    expect(isCatalogBrowseSearch({ query: "browse:" })).toBe(true);
    expect(isCatalogBrowseSearch({ query: "browse: logs" })).toBe(true);
    expect(isCatalogBrowseSearch({ query: "  Browse:observability" })).toBe(
      true,
    );
  });

  it("rejects a discovery search", () => {
    // The common case: the model looking for the tools this turn needs. Drawing
    // the catalog for these put a tool browser on top of nearly every answer.
    expect(isCatalogBrowseSearch({ query: "logs telemetry errors" })).toBe(
      false,
    );
    expect(isCatalogBrowseSearch({ query: "select: mcp__p-platform_x" })).toBe(
      false,
    );
    // A keyword that merely starts like the marker is not one.
    expect(isCatalogBrowseSearch({ query: "browse the catalog" })).toBe(false);
  });

  it("rejects anything that is not a query string", () => {
    expect(isCatalogBrowseSearch({})).toBe(false);
    expect(isCatalogBrowseSearch({ query: "" })).toBe(false);
    expect(isCatalogBrowseSearch({ query: 7 })).toBe(false);
    expect(isCatalogBrowseSearch(undefined)).toBe(false);
    expect(isCatalogBrowseSearch("browse:")).toBe(false);
  });
});

describe("rendersDefaultToolWidget", () => {
  it("draws the catalog only for a browse", () => {
    expect(rendersDefaultToolWidget("tool_search", { query: "browse:" })).toBe(
      true,
    );
    expect(rendersDefaultToolWidget("tool_search", { query: "logs" })).toBe(
      false,
    );
  });

  it("says nothing about a tool Elements has no card for", () => {
    expect(rendersDefaultToolWidget("search_docs", { query: "browse:" })).toBe(
      false,
    );
  });
});
