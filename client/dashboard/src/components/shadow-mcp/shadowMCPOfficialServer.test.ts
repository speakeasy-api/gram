import { describe, expect, it } from "vitest";
import {
  catalogCanHelpWith,
  shadowMCPCatalogMatch,
  canMakeShadowMCPServerOfficial,
  shadowMCPOfficialState,
  onboardReadiness,
  shadowMCPOfficialServerDescription,
  shadowMCPMakeOfficialHref,
} from "./shadowMCPOfficialServer";
import { testAccessSummary } from "./shadowMCPInventoryTestFixtures";

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

  it("says direct connections are blocked when a policy enforces it", () => {
    expect(
      shadowMCPOfficialServerDescription({
        officialServer: official,
        accessSummary: testAccessSummary("blocked"),
      }),
    ).toBe(
      "Linked to “Linear”. Direct connections are blocked and agents are sent there.",
    );
  });

  it("names the gateway that serves it", () => {
    expect(
      shadowMCPOfficialServerDescription({
        officialServer: { ...official, gatewayName: "Team Gateway" },
        accessSummary: testAccessSummary("blocked"),
      }),
    ).toContain("through the gateway “Team Gateway”");
  });

  it("says direct connections follow the usual rules while unlinked", () => {
    expect(
      shadowMCPOfficialServerDescription({
        officialServer: { ...official, directAccessAllowed: true },
        accessSummary: testAccessSummary("blocked"),
      }),
    ).toBe(
      "Linked to “Linear”. The link is off, so direct connections follow the usual rules.",
    );
  });

  it("does not claim a block when no policy is on", () => {
    expect(
      shadowMCPOfficialServerDescription({
        officialServer: official,
        accessSummary: testAccessSummary("none"),
      }),
    ).toContain("No Shadow MCP policy is on, so direct connections still work");
  });

  it("does not claim a block when the official server has no address", () => {
    expect(
      shadowMCPOfficialServerDescription({
        officialServer: {
          mcpServerId: "id",
          name: "GitHub",
          directAccessAllowed: false,
        },
        accessSummary: testAccessSummary("blocked"),
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

  it("reads as onboarded, or unlinked while the link is off", () => {
    expect(shadowMCPOfficialState({ officialServer: official }, false)).toBe(
      "onboarded",
    );
    expect(
      shadowMCPOfficialState(
        { officialServer: { ...official, directAccessAllowed: true } },
        false,
      ),
    ).toBe("unlinked");
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

describe("catalogCanHelpWith", () => {
  it("helps only with sign-in that needs a manual setup", () => {
    expect(catalogCanHelpWith("sign_in_manual")).toBe(true);
    expect(catalogCanHelpWith("sign_in_unknown")).toBe(true);
    expect(catalogCanHelpWith("unreachable:timeout")).toBe(false);
    expect(catalogCanHelpWith(undefined)).toBe(false);
  });
});
