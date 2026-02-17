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

  it("maps externalmcp to externalmcp", () => {
    expect(sourceTypeToUrnKind("externalmcp")).toBe("externalmcp");
  });
});

describe("urnKindToSourceType", () => {
  it("maps http to openapi", () => {
    expect(urnKindToSourceType("http")).toBe("openapi");
  });

  it("maps function to function", () => {
    expect(urnKindToSourceType("function")).toBe("function");
  });

  it("maps externalmcp to externalmcp", () => {
    expect(urnKindToSourceType("externalmcp")).toBe("externalmcp");
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

  it("builds prefix for externalmcp source", () => {
    expect(attachmentToURNPrefix("externalmcp", "github")).toBe(
      "tools:externalmcp:github:",
    );
  });
});
