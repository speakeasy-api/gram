import { describe, expect, it } from "vitest";
import { buildUpdateIssuerForm } from "./issuerSettingsForm";

const snapshot = {
  url: "https://idp.example.com",
  authorizationEndpoint: "https://idp.example.com/authorize",
  tokenEndpoint: "https://idp.example.com/token",
  registrationEndpoint: "",
  jwksUri: "https://idp.example.com/jwks.json",
  scopesSupported: ["openid", "profile"],
  grantTypesSupported: ["authorization_code"],
  responseTypesSupported: ["code"],
  tokenEndpointAuthMethodsSupported: ["client_secret_basic"],
  codeChallengeMethodsSupported: ["S256"],
  clientIdMetadataDocumentSupported: true,
  revocationEndpoint: "https://idp.example.com/revoke",
  serviceDocumentation: "https://docs.example.com",
  opPolicyUri: "https://example.com/policy",
  opTosUri: "https://example.com/tos",
};

const baseState = {
  id: "issuer-1",
  name: "  Example IdP  ",
  logoAssetId: "  11111111-2222-3333-4444-555555555555  ",
  slug: "  example-idp  ",
  clientSetupDocumentationUrl: "  https://docs.example.com/oauth  ",
  issuerUrl: "  https://idp.example.com  ",
  authorizationEndpoint: "  https://idp.example.com/authorize  ",
  tokenEndpoint: "  https://idp.example.com/token  ",
  registrationEndpoint: "  ",
  jwksUri: "  https://idp.example.com/jwks.json  ",
  discoveredSnapshot: null,
};

describe("buildUpdateIssuerForm", () => {
  it("trims every operator-entered field", () => {
    const form = buildUpdateIssuerForm(baseState);

    expect(form.name).toBe("Example IdP");
    expect(form.logoAssetId).toBe("11111111-2222-3333-4444-555555555555");
    expect(form.slug).toBe("example-idp");
    expect(form.issuer).toBe("https://idp.example.com");
    expect(form.authorizationEndpoint).toBe(
      "https://idp.example.com/authorize",
    );
    expect(form.clientSetupDocumentationUrl).toBe(
      "https://docs.example.com/oauth",
    );
  });

  // The server reads "" on these as "clear to NULL", so they must be sent
  // rather than omitted — omitting would silently keep the old value.
  it("sends emptied URL fields rather than omitting them", () => {
    const form = buildUpdateIssuerForm({
      ...baseState,
      clientSetupDocumentationUrl: "",
      registrationEndpoint: "",
      logoAssetId: "",
    });

    expect(form).toHaveProperty("clientSetupDocumentationUrl", "");
    expect(form).toHaveProperty("registrationEndpoint", "");
    // "" is the explicit "clear the saved logo to NULL" sentinel.
    expect(form).toHaveProperty("logoAssetId", "");
  });

  it("omits the platform-admin tunnel field unless the caller supplies it", () => {
    expect(buildUpdateIssuerForm(baseState)).not.toHaveProperty(
      "tunneledMcpServerId",
    );
  });

  it("sets and clears the issuer tunnel binding", () => {
    expect(
      buildUpdateIssuerForm({
        ...baseState,
        tunneledMcpServerId: " 019c001e-b43d-7000-8000-000000000001 ",
      }).tunneledMcpServerId,
    ).toBe("019c001e-b43d-7000-8000-000000000001");
    expect(
      buildUpdateIssuerForm({
        ...baseState,
        tunneledMcpServerId: "",
      }).tunneledMcpServerId,
    ).toBe("");
  });

  // Without a discovery for the current URL the server must keep the metadata
  // it already has (COALESCE narg semantics), so the arrays are omitted.
  it("omits the RFC 8414 arrays when no discovery has run", () => {
    const form = buildUpdateIssuerForm(baseState);

    expect(form.scopesSupported).toBeUndefined();
    expect(form.grantTypesSupported).toBeUndefined();
    expect(form.responseTypesSupported).toBeUndefined();
    expect(form.tokenEndpointAuthMethodsSupported).toBeUndefined();
    expect(form.codeChallengeMethodsSupported).toBeUndefined();
    expect(form.clientIdMetadataDocumentSupported).toBeUndefined();
    expect(form.revocationEndpoint).toBeUndefined();
    expect(form.serviceDocumentation).toBeUndefined();
    expect(form.opPolicyUri).toBeUndefined();
    expect(form.opTosUri).toBeUndefined();
  });

  it("forwards the discovered metadata when the snapshot matches the URL", () => {
    const form = buildUpdateIssuerForm({
      ...baseState,
      discoveredSnapshot: snapshot,
    });

    expect(form.scopesSupported).toEqual(["openid", "profile"]);
    expect(form.codeChallengeMethodsSupported).toEqual(["S256"]);
    expect(form.clientIdMetadataDocumentSupported).toBe(true);
    expect(form.revocationEndpoint).toBe("https://idp.example.com/revoke");
    expect(form.serviceDocumentation).toBe("https://docs.example.com");
  });

  // A snapshot seeded from a record whose PKCE methods were never captured
  // holds null there, and the update must omit the field (keep NULL) rather
  // than send [], which would record "the issuer advertises no methods".
  it("omits never-captured PKCE methods from a seeded snapshot", () => {
    const form = buildUpdateIssuerForm({
      ...baseState,
      discoveredSnapshot: { ...snapshot, codeChallengeMethodsSupported: null },
    });

    expect(form.scopesSupported).toEqual(["openid", "profile"]);
    expect(form.codeChallengeMethodsSupported).toBeUndefined();
  });

  // An empty array is a real captured value ("the issuer advertises no
  // methods") and must be forwarded, not collapsed into the omitted case.
  it("forwards captured-empty PKCE methods", () => {
    const form = buildUpdateIssuerForm({
      ...baseState,
      discoveredSnapshot: { ...snapshot, codeChallengeMethodsSupported: [] },
    });

    expect(form.codeChallengeMethodsSupported).toEqual([]);
  });

  // The operator repointed the provider after discovering the old URL. Sending
  // the stale snapshot would attribute one authorization server's advertised
  // capabilities to a different one.
  it("drops a snapshot discovered against a different URL", () => {
    const form = buildUpdateIssuerForm({
      ...baseState,
      issuerUrl: "https://other-idp.example.com",
      discoveredSnapshot: snapshot,
    });

    expect(form.issuer).toBe("https://other-idp.example.com");
    expect(form.scopesSupported).toBeUndefined();
    expect(form.clientIdMetadataDocumentSupported).toBeUndefined();
  });
});
