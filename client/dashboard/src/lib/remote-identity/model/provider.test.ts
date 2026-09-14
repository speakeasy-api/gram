import { describe, expect, it } from "vitest";
import {
  clientDisplayName,
  providerCapabilities,
  providerDisplayName,
  providerUrlLabel,
  scopeTier,
  supportsAutomaticRegistration,
} from "./provider";

describe("scopeTier", () => {
  it("reads ownership off the ids, narrowest first", () => {
    expect(scopeTier({ projectId: "p", organizationId: "o" })).toBe("project");
    expect(scopeTier({ projectId: "", organizationId: "o" })).toBe(
      "organization",
    );
    expect(scopeTier({})).toBe("platform");
  });
});

describe("providerCapabilities", () => {
  it("treats a provider publishing neither as manual, not broken", () => {
    // GitHub is exactly this: an authorization server with no registration
    // endpoint and no CIMD, which can only be set up by hand.
    const github = providerCapabilities({
      clientIdMetadataDocumentSupported: false,
      serviceDocumentation: "https://docs.github.com/apps",
    });
    expect(supportsAutomaticRegistration(github)).toBe(false);
    expect(github.registrationGuideUrl).toBe("https://docs.github.com/apps");
  });

  it("counts either mechanism as automatic", () => {
    expect(
      supportsAutomaticRegistration(
        providerCapabilities({ clientIdMetadataDocumentSupported: true }),
      ),
    ).toBe(true);
    expect(
      supportsAutomaticRegistration(
        providerCapabilities({ registrationEndpoint: "https://id/register" }),
      ),
    ).toBe(true);
  });

  it("ignores a blank registration endpoint", () => {
    expect(
      supportsAutomaticRegistration(
        providerCapabilities({ registrationEndpoint: "   " }),
      ),
    ).toBe(false);
  });

  it("prefers our own setup documentation over the upstream's", () => {
    expect(
      providerCapabilities({
        clientSetupDocumentationUrl: "https://ours",
        serviceDocumentation: "https://theirs",
      }).registrationGuideUrl,
    ).toBe("https://ours");
  });
});

describe("providerDisplayName", () => {
  it("prefers the name, then the host, then the slug", () => {
    expect(
      providerDisplayName({
        name: "Acme Okta",
        issuer: "https://acme.okta.com",
      }),
    ).toBe("Acme Okta");
    expect(providerDisplayName({ issuer: "https://auth.linear.app" })).toBe(
      "auth.linear.app",
    );
    expect(providerDisplayName({ issuer: "not-a-url", slug: "linear" })).toBe(
      "linear",
    );
  });
});

describe("providerUrlLabel", () => {
  it("drops the scheme and any trailing slash", () => {
    expect(providerUrlLabel("https://id.example.com/oauth/")).toBe(
      "id.example.com/oauth",
    );
  });
});

describe("clientDisplayName", () => {
  it("falls back to the row id for a CIMD client", () => {
    // A CIMD client_id is the hosted metadata-document URL: unreadable inline.
    expect(
      clientDisplayName({
        id: "b2681bca",
        clientId: "https://app.example/cimd/b2681bca",
        clientIdMetadataUri: "https://app.example/cimd/b2681bca",
      }),
    ).toBe("b2681bca");
  });

  it("keeps a real client_id", () => {
    expect(
      clientDisplayName({
        id: "row-1",
        clientId: "acme-prod",
        clientIdMetadataUri: undefined,
      }),
    ).toBe("acme-prod");
  });
});
