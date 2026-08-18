import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { describe, expect, it } from "vitest";
import {
  deriveJourneyStatus,
  firstIncompleteStepIndex,
  hasBlockingSecretsPolicy,
  hasCatalogBackedServer,
} from "./journeyStatus";

function server(overrides: Partial<McpServer>): McpServer {
  return {
    createdAt: new Date("2026-08-01T00:00:00Z"),
    id: "server-id",
    projectId: "project-id",
    ...overrides,
  } as McpServer;
}

function policy(overrides: Partial<RiskPolicy>): RiskPolicy {
  return {
    action: "flag",
    audiencePrincipalUrns: ["user:all"],
    audienceType: "everyone",
    autoName: true,
    createdAt: new Date("2026-08-01T00:00:00Z"),
    enabled: true,
    id: "policy-id",
    name: "policy",
    policyType: "standard",
    projectId: "project-id",
    score: 5,
    sources: [],
    updatedAt: new Date("2026-08-01T00:00:00Z"),
    version: 1,
    ...overrides,
  } as RiskPolicy;
}

describe("hasCatalogBackedServer", () => {
  it("counts a server backed by a remote MCP server", () => {
    expect(
      hasCatalogBackedServer([server({ remoteMcpServerId: "remote-id" })]),
    ).toBe(true);
  });

  it("ignores toolset-, tunnel-, and unproxied-backed servers", () => {
    expect(
      hasCatalogBackedServer([
        server({ toolsetId: "toolset-id" }),
        server({ tunneledMcpServerId: "tunnel-id" }),
        server({ unproxiedMcpServerId: "unproxied-id" }),
      ]),
    ).toBe(false);
  });

  it("handles an unread list", () => {
    expect(hasCatalogBackedServer(undefined)).toBe(false);
  });
});

describe("hasBlockingSecretsPolicy", () => {
  it("matches an enabled gitleaks policy set to block", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({ action: "block", sources: ["gitleaks"] }),
      ]),
    ).toBe(true);
  });

  it("rejects a flag-only secrets policy", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({ action: "flag", sources: ["gitleaks"] }),
      ]),
    ).toBe(false);
  });

  it("rejects a disabled blocking policy", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({ action: "block", sources: ["gitleaks"], enabled: false }),
      ]),
    ).toBe(false);
  });

  it("rejects a blocking policy for another category", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({ action: "block", sources: ["shadow_mcp"] }),
      ]),
    ).toBe(false);
  });

  it("handles an unread list", () => {
    expect(hasBlockingSecretsPolicy(undefined)).toBe(false);
  });
});

describe("deriveJourneyStatus", () => {
  it("is not started with neither signal", () => {
    expect(deriveJourneyStatus({ startSignal: false, winSignal: false })).toBe(
      "not-started",
    );
  });

  it("is in progress once the journey's own artifact exists", () => {
    expect(deriveJourneyStatus({ startSignal: true, winSignal: false })).toBe(
      "in-progress",
    );
  });

  it("is done once the win has been observed", () => {
    expect(deriveJourneyStatus({ startSignal: true, winSignal: true })).toBe(
      "done",
    );
  });

  it("credits a win even when the start signal is gone, e.g. a deleted policy", () => {
    expect(deriveJourneyStatus({ startSignal: false, winSignal: true })).toBe(
      "done",
    );
  });
});

describe("firstIncompleteStepIndex", () => {
  it("opens a fresh journey on its first step", () => {
    expect(firstIncompleteStepIndex("not-started", 3)).toBe(0);
  });

  it("resumes a started journey on its second step", () => {
    expect(firstIncompleteStepIndex("in-progress", 3)).toBe(1);
  });

  it("leaves a finished journey past its last step", () => {
    expect(firstIncompleteStepIndex("done", 3)).toBe(3);
  });
});
