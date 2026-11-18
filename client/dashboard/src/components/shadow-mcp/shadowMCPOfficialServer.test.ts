import { describe, expect, it } from "vitest";
import {
  shadowMCPCatalogMatch,
  shadowMCPCatalogRemote,
  recommendedOnboardOption,
  canMakeShadowMCPServerOfficial,
  shadowMCPOfficialState,
  onboardReadiness,
  shadowMCPOfficialServerDescription,
  shadowMCPMakeOfficialHref,
} from "./shadowMCPOfficialServer";

describe("shadowMCPMakeOfficialHref", () => {
  it("opens the Make official sheet on the server's page", () => {
    expect(
      shadowMCPMakeOfficialHref("/acme/projects/p/shadow-ai/mcps/sentry-1"),
    ).toBe("/acme/projects/p/shadow-ai/mcps/sentry-1?make-official=1");
  });
});

describe("canMakeShadowMCPServerOfficial", () => {
  it("offers it for a server URL that has no official version", () => {
    expect(
      canMakeShadowMCPServerOfficial({
        targetKind: "server_url",
        officialServer: undefined,
      }),
    ).toBe(true);
  });

  it("does not offer it for a local command or an onboarded server", () => {
    expect(
      canMakeShadowMCPServerOfficial({
        targetKind: "stdio_command",
        officialServer: undefined,
      }),
    ).toBe(false);
    expect(
      canMakeShadowMCPServerOfficial({
        targetKind: "server_url",
        officialServer: {
          mcpServerId: "id",
          name: "Sentry",
          directAccessAllowed: false,
        },
      }),
    ).toBe(false);
  });
});

describe("shadowMCPOfficialServerDescription", () => {
  const official = {
    mcpServerId: "id",
    name: "Linear",
    url: "https://app.example.com/mcp/linear",
    directAccessAllowed: false,
  };

  it("says direct connections are blocked while the link is on, in every mode", () => {
    expect(
      shadowMCPOfficialServerDescription({
        officialServer: official,
      }),
    ).toBe(
      "Linked to “Linear”. Direct connections are blocked and agents are sent there.",
    );
  });

  it("names the gateway that serves it", () => {
    expect(
      shadowMCPOfficialServerDescription({
        officialServer: { ...official, gatewayName: "Team Gateway" },
      }),
    ).toContain("through the gateway “Team Gateway”");
  });

  it("says direct connections follow the usual rules while unlinked", () => {
    expect(
      shadowMCPOfficialServerDescription({
        officialServer: { ...official, directAccessAllowed: true },
      }),
    ).toBe(
      "Linked to “Linear”. The link is off, so direct connections follow the usual rules.",
    );
  });

  it("does not claim a block when the official server has no address", () => {
    expect(
      shadowMCPOfficialServerDescription({
        officialServer: {
          mcpServerId: "id",
          name: "GitHub",
          directAccessAllowed: false,
        },
      }),
    ).toContain("It has no address yet, so direct connections still work");
  });
});

describe("onboardReadiness", () => {
  it("is ready for a public server that needs no sign-in", () => {
    const readiness = onboardReadiness({ outcome: "mcp_available" });

    expect(readiness.ready).toBe(true);
    expect(readiness.checks.map((check) => check.state)).toEqual([
      "pass",
      "pass",
      "pass",
    ]);
    expect(readiness.checks[2]?.detail).toBe("It needs no sign-in.");
  });

  it("is ready for a server that names its sign-in provider", () => {
    const readiness = onboardReadiness({
      outcome: "authentication_required",
      protectedResourceMetadataUrl:
        "https://mcp.example.com/.well-known/oauth-protected-resource",
    });

    expect(readiness.ready).toBe(true);
    expect(readiness.blocker).toBeUndefined();
  });

  it("names the registration method the sign-in provider supports", () => {
    const prm = "https://mcp.example.com/.well-known/oauth-protected-resource";
    expect(
      onboardReadiness({
        outcome: "authentication_required",
        protectedResourceMetadataUrl: prm,
        signInSetup: "dcr",
      }).checks[2]?.detail,
    ).toContain("dynamic client registration");
    expect(
      onboardReadiness({
        outcome: "authentication_required",
        protectedResourceMetadataUrl: prm,
        signInSetup: "cimd",
      }).checks[2]?.detail,
    ).toContain("client ID metadata document");
  });

  it("is not ready when a client must be registered by hand", () => {
    const readiness = onboardReadiness({
      outcome: "authentication_required",
      protectedResourceMetadataUrl:
        "https://mcp.example.com/.well-known/oauth-protected-resource",
      signInSetup: "manual",
    });

    expect(readiness.ready).toBe(false);
    expect(readiness.blockerCode).toBe("sign_in_manual");
    expect(readiness.checks[2]?.state).toBe("fail");
  });

  it("is not ready when sign-in needs a key nobody can discover", () => {
    const readiness = onboardReadiness({ outcome: "authentication_required" });

    expect(readiness.ready).toBe(false);
    expect(readiness.blockerCode).toBe("no_sign_in_provider");
    expect(readiness.checks.map((check) => check.state)).toEqual([
      "pass",
      "pass",
      "fail",
    ]);
  });

  it("skips the later checks when the server cannot be reached", () => {
    const readiness = onboardReadiness({
      outcome: "unreachable",
      reason: "guardian_rejected",
    });

    expect(readiness.ready).toBe(false);
    expect(readiness.blocker).toBe(
      "It is on a private network that Speakeasy cannot reach.",
    );
    expect(readiness.blockerCode).toBe("unreachable:guardian_rejected");
    expect(readiness.checks.map((check) => check.state)).toEqual([
      "fail",
      "skipped",
      "skipped",
    ]);
  });

  it("is not ready when the address does not answer as an MCP server", () => {
    const readiness = onboardReadiness({ outcome: "invalid_mcp_response" });

    expect(readiness.ready).toBe(false);
    expect(readiness.checks.map((check) => check.state)).toEqual([
      "pass",
      "fail",
      "skipped",
    ]);
  });
});

describe("shadowMCPOfficialState", () => {
  const official = {
    mcpServerId: "id",
    name: "Linear",
    directAccessAllowed: false,
  };

  it("reads as onboarded only while the link is on", () => {
    expect(shadowMCPOfficialState({ officialServer: official }, false)).toBe(
      "onboarded",
    );
    expect(
      shadowMCPOfficialState(
        { officialServer: { ...official, directAccessAllowed: true } },
        false,
      ),
    ).toBe(null);
  });

  it("leaves a pending request or an unmanaged server alone", () => {
    expect(shadowMCPOfficialState({ officialServer: official }, true)).toBe(
      null,
    );
    expect(shadowMCPOfficialState({ officialServer: undefined }, false)).toBe(
      null,
    );
  });
});

describe("shadowMCPCatalogMatch", () => {
  const catalog = [
    {
      registrySpecifier: "com.example/tracker",
      title: "Tracker",
      remotes: [{ url: "https://MCP.Example.com:443/mcp/" }],
    },
    { registrySpecifier: "com.example/other", remotes: [] },
  ];

  it("matches a remote URL that differs only in form", () => {
    expect(
      shadowMCPCatalogMatch(catalog, "https://mcp.example.com/mcp")
        ?.registrySpecifier,
    ).toBe("com.example/tracker");
  });

  it("does not match a different path", () => {
    expect(shadowMCPCatalogMatch(catalog, "https://mcp.example.com/v2")).toBe(
      null,
    );
  });

  it("does not match a different port", () => {
    expect(
      shadowMCPCatalogMatch(catalog, "https://mcp.example.com:8443/mcp"),
    ).toBe(null);
  });
});

describe("shadowMCPCatalogRemote", () => {
  it("picks the remote that serves the Shadow MCP server's URL", () => {
    const remotes = [
      { url: "https://mcp.example.com/sse" },
      { url: "https://MCP.example.com/mcp/" },
    ];
    expect(
      shadowMCPCatalogRemote(remotes, "https://mcp.example.com/mcp")?.url,
    ).toBe("https://MCP.example.com/mcp/");
    expect(shadowMCPCatalogRemote(remotes, "https://other.example.com")).toBe(
      null,
    );
  });
});

describe("recommendedOnboardOption", () => {
  const base = {
    hasSuggestedServer: false,
    hasCatalogEntry: false,
    catalogPending: false,
    readiness: { ready: true },
  };

  it("puts the server that already serves the URL first", () => {
    expect(
      recommendedOnboardOption({
        ...base,
        hasSuggestedServer: true,
        hasCatalogEntry: true,
        catalogPending: true,
        readiness: null,
      }),
    ).toBe("link");
  });

  it("puts the catalog before a new server, whatever the checks say", () => {
    expect(recommendedOnboardOption({ ...base, hasCatalogEntry: true })).toBe(
      "catalog",
    );
    expect(
      recommendedOnboardOption({
        ...base,
        hasCatalogEntry: true,
        readiness: { ready: false },
      }),
    ).toBe("catalog");
  });

  it("recommends a quick new server, then a manual one", () => {
    expect(recommendedOnboardOption(base)).toBe("quick");
    expect(
      recommendedOnboardOption({ ...base, readiness: { ready: false } }),
    ).toBe("manual");
  });

  it("waits instead of recommending one option and swapping it", () => {
    expect(recommendedOnboardOption({ ...base, catalogPending: true })).toBe(
      null,
    );
    expect(recommendedOnboardOption({ ...base, readiness: null })).toBe(null);
  });
});
