import { describe, expect, it } from "vitest";
import {
  externalMcpOAuthConfigStatus,
  primaryExternalMcpSlug,
} from "./external-mcp-oauth";

const externalMcps = [
  { slug: "linear", registryServerSpecifier: "app.linear/linear" },
  { slug: "docs", registryServerSpecifier: "io.example/docs" },
  { slug: "unlisted" },
];
const catalogAuthBySpecifier = new Map([
  ["app.linear/linear", "oauth" as const],
  ["io.example/docs", "none" as const],
]);

function status(
  externalMcpSlug: string | undefined,
  configured: { hasOAuthServer?: boolean; hasSessionIssuer?: boolean } = {},
) {
  return externalMcpOAuthConfigStatus({
    externalMcpSlug,
    externalMcps,
    catalogAuthBySpecifier,
    hasOAuthServer: configured.hasOAuthServer ?? false,
    hasSessionIssuer: configured.hasSessionIssuer ?? false,
  });
}

describe("externalMcpOAuthConfigStatus", () => {
  it("is not external without an external MCP slug", () => {
    expect(status(undefined)).toBe("not-external-mcp");
  });

  it("needs no OAuth when the catalog says another auth type or has no entry", () => {
    expect(status("docs")).toBe("not-required");
    expect(status("unlisted")).toBe("not-required");
    expect(status("missing-from-deployment")).toBe("not-required");
  });

  it("requires setup for a catalog OAuth server with neither an OAuth server nor an issuer", () => {
    expect(status("linear")).toBe("required-unconfigured");
  });

  it("is configured by an external OAuth server or a sign-in issuer", () => {
    expect(status("linear", { hasOAuthServer: true })).toBe("configured");
    expect(status("linear", { hasSessionIssuer: true })).toBe("configured");
  });
});

describe("primaryExternalMcpSlug", () => {
  it("picks the first external MCP slug in sorted order, as the server summary does", () => {
    expect(
      primaryExternalMcpSlug([
        "tools:http:petstore:list",
        "tools:externalmcp:linear:proxy",
        "tools:externalmcp:docs:search",
      ]),
    ).toBe("docs");
  });

  it("treats a proxy-only toolset as external", () => {
    expect(primaryExternalMcpSlug(["tools:externalmcp:linear:proxy"])).toBe(
      "linear",
    );
  });

  it("is undefined without external MCP tools", () => {
    expect(
      primaryExternalMcpSlug(["tools:http:petstore:list"]),
    ).toBeUndefined();
    expect(primaryExternalMcpSlug(undefined)).toBeUndefined();
  });
});
