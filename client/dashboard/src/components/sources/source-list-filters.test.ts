import { describe, expect, it } from "vitest";
import type { SourceOption } from "./source-list";
import {
  contentTypeToFormat,
  hasActiveSourceFilters,
  matchesSourceFilters,
  matchesSourceSearch,
  sourceFacets,
  sourceFailureKey,
  sourceUsedInMcp,
  type SourceFilterValues,
} from "./source-list-filters";

const NO_FILTERS: SourceFilterValues = {
  kind: [],
  usedInMcp: null,
  format: [],
  failing: false,
};

const petstore: SourceOption = {
  key: "openapi:doc-1",
  name: "Petstore API",
  kind: "openapi",
  documentId: "doc-1",
  slug: "petstore",
  contentType: "application/yaml",
};

const greeter: SourceOption = {
  key: "function:fn-1",
  name: "Greeter",
  kind: "function",
  functionId: "fn-1",
  slug: "greeter",
  contentType: "application/zip",
};

describe("contentTypeToFormat", () => {
  it("maps yaml and json content types, and nothing else", () => {
    expect(contentTypeToFormat("application/yaml")).toBe("yaml");
    expect(contentTypeToFormat("text/x-yml")).toBe("yaml");
    expect(contentTypeToFormat("application/json; charset=utf-8")).toBe("json");
    expect(contentTypeToFormat("application/zip")).toBeUndefined();
    expect(contentTypeToFormat(undefined)).toBeUndefined();
  });
});

describe("sourceUsedInMcp", () => {
  it("is used when a toolset carries a tool URN under the source's prefix", () => {
    const urns = ["tools:http:petstore:list_pets", "tools:function:other:x"];
    expect(sourceUsedInMcp(petstore, urns)).toBe(true);
    expect(sourceUsedInMcp(greeter, urns)).toBe(false);
  });

  it("does not match a slug that only shares a prefix", () => {
    expect(sourceUsedInMcp(petstore, ["tools:http:petstore-v2:list"])).toBe(
      false,
    );
  });

  it("is never used without a slug to build a prefix from", () => {
    expect(
      sourceUsedInMcp({ ...petstore, slug: undefined }, ["tools:http::x"]),
    ).toBe(false);
  });
});

describe("sourceFacets", () => {
  it("gives OpenAPI documents a format and functions none", () => {
    expect(sourceFacets(petstore, [], new Set()).format).toBe("yaml");
    expect(sourceFacets(greeter, [], new Set()).format).toBeUndefined();
  });

  it("flags failure by kind and slug, not by id", () => {
    // The failed deployment's ids differ from the active one's; the slug is
    // what both share.
    const failing = new Set([
      sourceFailureKey({ kind: "openapi", slug: "petstore" }),
    ]);
    expect(sourceFacets(petstore, [], failing).failing).toBe(true);
    expect(sourceFacets(greeter, [], failing).failing).toBe(false);
  });
});

describe("matchesSourceFilters", () => {
  const usedOpenapi = sourceFacets(
    petstore,
    ["tools:http:petstore:list_pets"],
    new Set(),
  );
  const unusedFunction = sourceFacets(greeter, [], new Set());

  it("matches everything when no filter is set", () => {
    expect(matchesSourceFilters(usedOpenapi, NO_FILTERS)).toBe(true);
    expect(matchesSourceFilters(unusedFunction, NO_FILTERS)).toBe(true);
  });

  it("narrows by kind", () => {
    const values = { ...NO_FILTERS, kind: ["function"] };
    expect(matchesSourceFilters(usedOpenapi, values)).toBe(false);
    expect(matchesSourceFilters(unusedFunction, values)).toBe(true);
  });

  it("narrows by MCP usage and ignores values it does not declare", () => {
    expect(
      matchesSourceFilters(usedOpenapi, { ...NO_FILTERS, usedInMcp: "yes" }),
    ).toBe(true);
    expect(
      matchesSourceFilters(unusedFunction, { ...NO_FILTERS, usedInMcp: "yes" }),
    ).toBe(false);
    expect(
      matchesSourceFilters(unusedFunction, { ...NO_FILTERS, usedInMcp: "no" }),
    ).toBe(true);
    expect(
      matchesSourceFilters(unusedFunction, {
        ...NO_FILTERS,
        usedInMcp: "maybe",
      }),
    ).toBe(true);
  });

  it("excludes formatless sources when a format is picked", () => {
    const values = { ...NO_FILTERS, format: ["yaml"] };
    expect(matchesSourceFilters(usedOpenapi, values)).toBe(true);
    expect(matchesSourceFilters(unusedFunction, values)).toBe(false);
    expect(
      matchesSourceFilters(usedOpenapi, { ...NO_FILTERS, format: ["json"] }),
    ).toBe(false);
  });

  it("keeps only failing sources when the errors filter is on", () => {
    const values = { ...NO_FILTERS, failing: true };
    expect(matchesSourceFilters(usedOpenapi, values)).toBe(false);
    expect(
      matchesSourceFilters({ ...usedOpenapi, failing: true }, values),
    ).toBe(true);
  });
});

describe("matchesSourceSearch", () => {
  it("matches the name or the slug, case-insensitively", () => {
    expect(matchesSourceSearch(petstore, "PET")).toBe(true);
    expect(
      matchesSourceSearch({ name: "Store", slug: "petstore" }, "pets"),
    ).toBe(true);
    expect(matchesSourceSearch(greeter, "pet")).toBe(false);
    expect(matchesSourceSearch(greeter, "   ")).toBe(true);
  });
});

describe("hasActiveSourceFilters", () => {
  it("is false at the defaults and true once any dimension is set", () => {
    expect(hasActiveSourceFilters(NO_FILTERS)).toBe(false);
    expect(hasActiveSourceFilters({ ...NO_FILTERS, failing: true })).toBe(true);
    expect(hasActiveSourceFilters({ ...NO_FILTERS, usedInMcp: "no" })).toBe(
      true,
    );
  });
});
