import { describe, expect, it } from "vitest";
import {
  canMakeShadowMCPServerOfficial,
  onboardReadiness,
  shadowMCPOfficialServerDescription,
  shadowMCPOnboardHref,
} from "./shadowMCPOfficialServer";
import { testAccessSummary } from "./shadowMCPInventoryTestFixtures";

describe("shadowMCPOnboardHref", () => {
  it("carries the server's URL and name to the new-server form", () => {
    const href = shadowMCPOnboardHref("/acme/projects/p/mcp/add/remote", {
      serverName: "Sentry",
      urlHost: "mcp.sentry.dev",
      canonicalServerUrl: "https://mcp.sentry.dev/mcp",
    });

    const url = new URL(href, "https://app.example.com");
    expect(url.pathname).toBe("/acme/projects/p/mcp/add/remote");
    expect(url.searchParams.get("url")).toBe("https://mcp.sentry.dev/mcp");
    expect(url.searchParams.get("name")).toBe("Sentry");
    expect(url.searchParams.get("from")).toBe("shadow-mcp");
  });

  it("names an unnamed server by its host", () => {
    const href = shadowMCPOnboardHref("/mcp/add/remote", {
      serverName: undefined,
      urlHost: "tools.example.com",
      canonicalServerUrl: "https://tools.example.com/mcp",
    });

    expect(
      new URL(href, "https://app.example.com").searchParams.get("name"),
    ).toBe("tools.example.com");
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
        officialServer: { mcpServerId: "id", name: "Sentry" },
      }),
    ).toBe(false);
  });
});

describe("shadowMCPOfficialServerDescription", () => {
  const official = {
    mcpServerId: "id",
    name: "Linear",
    url: "https://app.example.com/mcp/linear",
  };

  it("says direct connections are blocked when a policy enforces it", () => {
    expect(
      shadowMCPOfficialServerDescription({
        officialServer: official,
        accessSummary: testAccessSummary("blocked"),
      }),
    ).toBe(
      "Served as “Linear”. Direct connections are blocked and agents are sent there.",
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
        officialServer: { mcpServerId: "id", name: "GitHub" },
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
