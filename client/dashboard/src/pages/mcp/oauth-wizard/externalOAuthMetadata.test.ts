import { describe, expect, it } from "vitest";

import {
  metadataFromIssuerDraft,
  validateExternalMetadataJson,
  validateIssuerUrl,
} from "./externalOAuthMetadata";

const VALID_METADATA = {
  issuer: "https://auth.example.com",
  authorization_endpoint: "https://auth.example.com/authorize",
  token_endpoint: "https://auth.example.com/token",
  registration_endpoint: "https://auth.example.com/register",
};

describe("validateIssuerUrl", () => {
  it("accepts absolute HTTP and HTTPS URLs", () => {
    expect(validateIssuerUrl("https://auth.example.com")).toBeNull();
    expect(validateIssuerUrl("http://localhost:3000")).toBeNull();
  });

  it("rejects missing, relative, and non-HTTP URLs", () => {
    expect(validateIssuerUrl("")).toBe("Issuer URL is required");
    expect(validateIssuerUrl("/oauth")).toContain("absolute");
    expect(validateIssuerUrl("ftp://auth.example.com")).toContain("HTTP");
  });
});

describe("validateExternalMetadataJson", () => {
  it("accepts required OAuth and dynamic registration URLs", () => {
    expect(
      validateExternalMetadataJson(
        JSON.stringify(VALID_METADATA),
        "https://auth.example.com",
      ),
    ).toMatchObject({ ok: true });
  });

  it("reports missing and malformed required fields", () => {
    const result = validateExternalMetadataJson(
      JSON.stringify({
        ...VALID_METADATA,
        token_endpoint: "not-a-url",
        registration_endpoint: undefined,
      }),
    );

    expect(result).toMatchObject({ ok: false });
    if (!result.ok) {
      expect(result.reason).toContain("token_endpoint");
      expect(result.reason).toContain("registration_endpoint");
    }
  });

  it("requires metadata issuer to match the primary Issuer URL", () => {
    expect(
      validateExternalMetadataJson(
        JSON.stringify(VALID_METADATA),
        "https://other.example.com",
      ),
    ).toEqual({
      ok: false,
      reason: "Metadata issuer must match the Issuer URL",
    });
  });
});

describe("metadataFromIssuerDraft", () => {
  it("maps discovered issuer metadata to RFC 8414 field names", () => {
    const metadata = metadataFromIssuerDraft({
      issuer: "https://auth.example.com",
      authorizationEndpoint: "https://auth.example.com/authorize",
      tokenEndpoint: "https://auth.example.com/token",
      registrationEndpoint: "https://auth.example.com/register",
      scopesSupported: ["read"],
      grantTypesSupported: ["authorization_code"],
      responseTypesSupported: ["code"],
      tokenEndpointAuthMethodsSupported: ["client_secret_post"],
      clientIdMetadataDocumentSupported: false,
      discoveryWarnings: [],
      oidc: false,
      passthrough: true,
    });

    expect(metadata).toMatchObject({
      issuer: "https://auth.example.com",
      authorization_endpoint: "https://auth.example.com/authorize",
      token_endpoint: "https://auth.example.com/token",
      registration_endpoint: "https://auth.example.com/register",
      scopes_supported: ["read"],
      grant_types_supported: ["authorization_code"],
      response_types_supported: ["code"],
      token_endpoint_auth_methods_supported: ["client_secret_post"],
    });
  });

  it("preserves a mismatched issuer advertised by discovery", () => {
    const metadata = metadataFromIssuerDraft({
      issuer: "https://different.example.com",
      clientIdMetadataDocumentSupported: false,
      discoveryWarnings: [],
      oidc: false,
      passthrough: true,
    });

    expect(metadata.issuer).toBe("https://different.example.com");
  });
});
