// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  attachmentToURNPrefix,
  sourceTypeToUrnKind,
  urnKindToSourceType,
} from "./sources";

describe("sourceTypeToUrnKind", () => {
  it("maps openapi to http", () => {
    expect(sourceTypeToUrnKind("openapi")).toBe("http");
  });

  it("maps function to function", () => {
    expect(sourceTypeToUrnKind("function")).toBe("function");
  });
});

describe("urnKindToSourceType", () => {
  it("maps http to openapi", () => {
    expect(urnKindToSourceType("http")).toBe("openapi");
  });

  it("maps function to function", () => {
    expect(urnKindToSourceType("function")).toBe("function");
  });
});

describe("attachmentToURNPrefix", () => {
  it("builds prefix for openapi source", () => {
    expect(attachmentToURNPrefix("openapi", "pet-store")).toBe(
      "tools:http:pet-store:",
    );
  });

  it("builds prefix for function source", () => {
    expect(attachmentToURNPrefix("function", "my-func")).toBe(
      "tools:function:my-func:",
    );
  });
});
