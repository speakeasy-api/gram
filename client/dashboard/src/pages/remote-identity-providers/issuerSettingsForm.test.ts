import { describe, expect, it } from "vitest";
import {
  buildCreateIssuerForm,
  buildUpdateIssuerForm,
} from "./issuerSettingsForm";

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
  userinfoEndpoint: "https://idp.example.com/userinfo",
  introspectionEndpoint: "https://idp.example.com/introspect",
  introspectionEndpointAuthMethodsSupported: ["client_secret_basic"],
  idTokenSigningAlgValuesSupported: ["RS256"],
  claimsSupported: ["sub", "email"],
  backchannelLogoutSupported: true,
  authorizationResponseIssParameterSupported: false,
};

const DISCOVERY_ONLY_CAPABILITY_FIELDS = [
  "userinfoEndpoint",
  "introspectionEndpoint",
  "introspectionEndpointAuthMethodsSupported",
  "idTokenSigningAlgValuesSupported",
  "claimsSupported",
  "backchannelLogoutSupported",
  "authorizationResponseIssParameterSupported",
] as const;

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

  // Discover-then-Save on an existing issuer must repoint the capability
  // columns too, not just the endpoints.
  it("forwards the discovery-only capabilities from a matching snapshot", () => {
    const form = buildUpdateIssuerForm({
      ...baseState,
      discoveredSnapshot: snapshot,
    });

    expect(form.userinfoEndpoint).toBe("https://idp.example.com/userinfo");
    expect(form.introspectionEndpoint).toBe(
      "https://idp.example.com/introspect",
    );
    expect(form.introspectionEndpointAuthMethodsSupported).toEqual([
      "client_secret_basic",
    ]);
    expect(form.idTokenSigningAlgValuesSupported).toEqual(["RS256"]);
    expect(form.claimsSupported).toEqual(["sub", "email"]);
    expect(form.backchannelLogoutSupported).toBe(true);
    expect(form.authorizationResponseIssParameterSupported).toBe(false);
  });

  // A seeded snapshot holds null for never-captured arrays/booleans; those go
  // out as undefined so the server keeps NULL rather than recording a value.
  it("omits never-captured capabilities from a seeded snapshot", () => {
    const form = buildUpdateIssuerForm({
      ...baseState,
      discoveredSnapshot: {
        ...snapshot,
        introspectionEndpointAuthMethodsSupported: null,
        idTokenSigningAlgValuesSupported: null,
        claimsSupported: null,
        backchannelLogoutSupported: null,
        authorizationResponseIssParameterSupported: null,
      },
    });

    expect(form.introspectionEndpointAuthMethodsSupported).toBeUndefined();
    expect(form.idTokenSigningAlgValuesSupported).toBeUndefined();
    expect(form.claimsSupported).toBeUndefined();
    expect(form.backchannelLogoutSupported).toBeUndefined();
    expect(form.authorizationResponseIssParameterSupported).toBeUndefined();
  });

  // "" is the "clear to NULL" sentinel for a URL the issuer stopped
  // advertising, so it is sent through verbatim, not collapsed to undefined.
  it("sends emptied capability endpoints through verbatim", () => {
    const form = buildUpdateIssuerForm({
      ...baseState,
      discoveredSnapshot: {
        ...snapshot,
        userinfoEndpoint: "",
        introspectionEndpoint: "",
        claimsSupported: [],
      },
    });

    expect(form).toHaveProperty("userinfoEndpoint", "");
    expect(form).toHaveProperty("introspectionEndpoint", "");
    expect(form.claimsSupported).toEqual([]);
  });

  it("omits the discovery-only capabilities for a mismatched-URL snapshot", () => {
    const form = buildUpdateIssuerForm({
      ...baseState,
      issuerUrl: "https://other-idp.example.com",
      discoveredSnapshot: snapshot,
    });

    for (const field of DISCOVERY_ONLY_CAPABILITY_FIELDS) {
      expect(form[field]).toBeUndefined();
    }
  });
});

describe("buildCreateIssuerForm", () => {
  const { id: _id, ...createState } = baseState;

  it("forwards the discovery-only capabilities from a matching snapshot", () => {
    const form = buildCreateIssuerForm({
      ...createState,
      discoveredSnapshot: snapshot,
    });

    expect(form.codeChallengeMethodsSupported).toEqual(["S256"]);
    expect(form.userinfoEndpoint).toBe("https://idp.example.com/userinfo");
    expect(form.introspectionEndpoint).toBe(
      "https://idp.example.com/introspect",
    );
    expect(form.introspectionEndpointAuthMethodsSupported).toEqual([
      "client_secret_basic",
    ]);
    expect(form.idTokenSigningAlgValuesSupported).toEqual(["RS256"]);
    expect(form.claimsSupported).toEqual(["sub", "email"]);
    expect(form.backchannelLogoutSupported).toBe(true);
    expect(form.authorizationResponseIssParameterSupported).toBe(false);
  });

  // A captured-empty array means "advertises none" and must survive; an
  // unadvertised endpoint is omitted so the server stores NULL.
  it("forwards captured-empty arrays and omits unadvertised endpoints", () => {
    const form = buildCreateIssuerForm({
      ...createState,
      discoveredSnapshot: {
        ...snapshot,
        userinfoEndpoint: "",
        introspectionEndpoint: "",
        claimsSupported: [],
        idTokenSigningAlgValuesSupported: [],
        introspectionEndpointAuthMethodsSupported: [],
      },
    });

    expect(form.userinfoEndpoint).toBeUndefined();
    expect(form.introspectionEndpoint).toBeUndefined();
    expect(form.claimsSupported).toEqual([]);
    expect(form.idTokenSigningAlgValuesSupported).toEqual([]);
    expect(form.introspectionEndpointAuthMethodsSupported).toEqual([]);
  });

  it("omits the discovery-only capabilities when no discovery has run", () => {
    const form = buildCreateIssuerForm(createState);

    expect(form.codeChallengeMethodsSupported).toBeUndefined();
    for (const field of DISCOVERY_ONLY_CAPABILITY_FIELDS) {
      expect(form[field]).toBeUndefined();
    }
  });

  it("drops a snapshot discovered against a different URL", () => {
    const form = buildCreateIssuerForm({
      ...createState,
      issuerUrl: "https://other-idp.example.com",
      discoveredSnapshot: snapshot,
    });

    for (const field of DISCOVERY_ONLY_CAPABILITY_FIELDS) {
      expect(form[field]).toBeUndefined();
    }
  });
});
