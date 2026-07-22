import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { describe, expect, it } from "vitest";
import {
  isAbsoluteHttpUrl,
  issuerDocumentationLinks,
} from "./issuerDocumentationLinks";

function issuer(overrides: Partial<RemoteSessionIssuer>): RemoteSessionIssuer {
  return {
    id: "00000000-0000-0000-0000-000000000000",
    projectId: "",
    organizationId: "org_1",
    slug: "idp",
    issuer: "https://idp.example.com",
    oidc: false,
    passthrough: false,
    clientIdMetadataDocumentSupported: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  } as RemoteSessionIssuer;
}

describe("isAbsoluteHttpUrl", () => {
  it("accepts absolute http(s) URLs", () => {
    expect(isAbsoluteHttpUrl("https://docs.example.com/oauth")).toBe(true);
    expect(isAbsoluteHttpUrl("http://docs.example.com")).toBe(true);
    expect(isAbsoluteHttpUrl("HTTPS://docs.example.com")).toBe(true);
  });

  it("rejects schemes that would execute or leak when placed in an href", () => {
    expect(isAbsoluteHttpUrl("javascript:alert(1)")).toBe(false);
    expect(isAbsoluteHttpUrl("data:text/html,<script>alert(1)</script>")).toBe(
      false,
    );
    expect(isAbsoluteHttpUrl("mailto:legal@example.com")).toBe(false);
    expect(isAbsoluteHttpUrl("ftp://files.example.com")).toBe(false);
  });

  // The URL parser strips whitespace and embedded tabs/newlines before it
  // resolves the scheme, so these all normalize back to `javascript:`. A guard
  // that pattern-matched the raw string instead of reading the parsed protocol
  // would let every one of them through.
  it("rejects schemes smuggled past a naive check with whitespace or casing", () => {
    expect(isAbsoluteHttpUrl("  javascript:alert(1)  ")).toBe(false);
    expect(isAbsoluteHttpUrl("\njavascript:alert(1)")).toBe(false);
    expect(isAbsoluteHttpUrl("java\nscript:alert(1)")).toBe(false);
    expect(isAbsoluteHttpUrl("java\tscript:alert(1)")).toBe(false);
    expect(isAbsoluteHttpUrl("JaVaScRiPt:alert(1)")).toBe(false);
  });

  it("rejects relative and malformed values", () => {
    expect(isAbsoluteHttpUrl("")).toBe(false);
    expect(isAbsoluteHttpUrl("docs")).toBe(false);
    expect(isAbsoluteHttpUrl("/relative/docs")).toBe(false);
    expect(isAbsoluteHttpUrl("//docs.example.com")).toBe(false);
    expect(isAbsoluteHttpUrl("https://")).toBe(false);
    expect(isAbsoluteHttpUrl("http://")).toBe(false);
  });
});

describe("issuerDocumentationLinks", () => {
  it("returns nothing when the issuer carries no documentation", () => {
    expect(issuerDocumentationLinks(issuer({}))).toEqual([]);
  });

  it("orders client setup documentation before service documentation", () => {
    const links = issuerDocumentationLinks(
      issuer({
        clientSetupDocumentationUrl: "https://docs.example.com/oauth/apps",
        serviceDocumentation: "https://docs.example.com/api",
      }),
    );

    expect(links).toEqual([
      {
        label: "Client Setup Documentation",
        url: "https://docs.example.com/oauth/apps",
      },
      { label: "Service Documentation", url: "https://docs.example.com/api" },
    ]);
  });

  it("trims surrounding whitespace and omits blank entries", () => {
    const links = issuerDocumentationLinks(
      issuer({
        clientSetupDocumentationUrl: "  https://docs.example.com/oauth  ",
        serviceDocumentation: "   ",
      }),
    );

    expect(links).toEqual([
      {
        label: "Client Setup Documentation",
        url: "https://docs.example.com/oauth",
      },
    ]);
  });

  // serviceDocumentation comes from an upstream issuer's metadata document, so
  // the render sink drops an unsafe scheme even if it reached the database.
  it("drops entries whose URL is not an absolute http(s) URL", () => {
    const links = issuerDocumentationLinks(
      issuer({
        clientSetupDocumentationUrl: "https://docs.example.com/oauth/apps",
        serviceDocumentation: "javascript:alert(1)",
      }),
    );

    expect(links).toEqual([
      {
        label: "Client Setup Documentation",
        url: "https://docs.example.com/oauth/apps",
      },
    ]);
  });

  it("drops an entry whose scheme is smuggled past trimming", () => {
    const links = issuerDocumentationLinks(
      issuer({ serviceDocumentation: "java\nscript:alert(1)" }),
    );

    expect(links).toEqual([]);
  });
});
