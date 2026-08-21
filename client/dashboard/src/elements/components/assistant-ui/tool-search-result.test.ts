import { describe, expect, it } from "vitest";

import { rendersDefaultToolWidget } from "./default-tool-components";
import { isCatalogBrowseSearch } from "./tool-search-result.helpers";

describe("isCatalogBrowseSearch", () => {
  it("accepts a search the model flagged as a browse", () => {
    expect(isCatalogBrowseSearch({ query: "", browse: true })).toBe(true);
    expect(isCatalogBrowseSearch({ query: "logs", browse: true })).toBe(true);
  });

  it("rejects a discovery search", () => {
    // The common case: the model looking for the tools this turn needs. Drawing
    // the catalog for these put a tool browser on top of nearly every answer.
    expect(isCatalogBrowseSearch({ query: "logs telemetry errors" })).toBe(
      false,
    );
    expect(isCatalogBrowseSearch({ query: "tools", browse: false })).toBe(
      false,
    );
  });

  it("takes only a literal true, not a value that merely looks set", () => {
    // Arguments are a partial parse while the model streams them, and a
    // half-written flag must not draw the card.
    expect(isCatalogBrowseSearch({ browse: "true" })).toBe(false);
    expect(isCatalogBrowseSearch({ browse: 1 })).toBe(false);
    expect(isCatalogBrowseSearch({})).toBe(false);
    expect(isCatalogBrowseSearch(undefined)).toBe(false);
    expect(isCatalogBrowseSearch("browse")).toBe(false);
  });
});

describe("rendersDefaultToolWidget", () => {
  it("draws the catalog only for a browse", () => {
    expect(rendersDefaultToolWidget("tool_search", { browse: true })).toBe(
      true,
    );
    expect(rendersDefaultToolWidget("tool_search", { query: "logs" })).toBe(
      false,
    );
  });

  it("says nothing about a tool Elements has no card for", () => {
    expect(rendersDefaultToolWidget("search_docs", { browse: true })).toBe(
      false,
    );
  });
});
