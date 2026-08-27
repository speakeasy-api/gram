import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpServerActivity } from "@gram/client/models/components/mcpserveractivity.js";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { PluginServer } from "@gram/client/models/components/pluginserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import { DETECTION_RULES } from "@/pages/security/policy-data";
import { describe, expect, it } from "vitest";
import {
  catalogBackedMcpServers,
  deriveJourneyStatus,
  firstIncompleteStepIndex,
  hasBlockingSecretsPolicy,
  hasDefaultPluginServer,
  hasMcpServerActivity,
  latestSecretsFinding,
} from "./journeyStatus";

function server(overrides: Partial<McpServer>): McpServer {
  return {
    createdAt: new Date("2026-08-01T00:00:00Z"),
    id: "server-id",
    projectId: "project-id",
    ...overrides,
  } as McpServer;
}

function remote(overrides: Partial<RemoteMcpServer> = {}): RemoteMcpServer {
  return {
    createdAt: new Date("2026-08-01T00:00:00Z"),
    id: "remote-id",
    projectId: "project-id",
    transportType: "streamable-http",
    updatedAt: new Date("2026-08-01T00:00:00Z"),
    url: "https://catalog.example/mcp",
    ...overrides,
  };
}

function catalogServer(url = "https://catalog.example/mcp/"): PulseMCPServer {
  return {
    description: "Catalog server",
    isReadOnly: true,
    meta: {},
    registryId: "registry",
    registrySpecifier: "example/catalog",
    remotes: [{ transportType: "streamable-http", url }],
    supportsDcr: true,
    title: "Catalog server",
    toolCount: 1,
    version: "1.0.0",
  };
}

function activity(
  overrides: Partial<McpServerActivity> = {},
): McpServerActivity {
  return {
    recentToolCalls: 1,
    targetId: "catalog-server",
    targetLabel: "Read item",
    targetType: "hosted_mcp_server",
    totalToolCalls: 1,
    ...overrides,
  };
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

function plugin(overrides: Partial<Plugin>): Plugin {
  return {
    agentPluginsV1Compatible: true,
    createdAt: new Date("2026-08-01T00:00:00Z"),
    id: "plugin-id",
    name: "Default",
    slug: "default",
    updatedAt: new Date("2026-08-01T00:00:00Z"),
    ...overrides,
  } as Plugin;
}

function pluginServer(mcpServerId: string): PluginServer {
  return {
    createdAt: new Date("2026-08-01T00:00:00Z"),
    displayName: "Server",
    id: "plugin-server-id",
    mcpServerId,
    policy: "optional",
    sortOrder: 0,
  };
}

describe("catalogBackedMcpServers", () => {
  it("matches catalog remote URLs and ignores unread inputs", () => {
    expect(
      catalogBackedMcpServers(
        [server({ remoteMcpServerId: "remote-id" })],
        [remote()],
        [catalogServer()],
      ),
    ).toHaveLength(1);
    expect(
      catalogBackedMcpServers(
        [server({ remoteMcpServerId: "remote-id" })],
        undefined,
        [catalogServer()],
      ),
    ).toEqual([]);
  });
});

describe("hasMcpServerActivity", () => {
  const catalogMcpServer = server({
    remoteMcpServerId: "remote-id",
    slug: "catalog-server",
  });

  it("requires hosted MCP activity for the exact catalog server slug", () => {
    expect(hasMcpServerActivity([activity()], catalogMcpServer)).toBe(true);
    expect(
      hasMcpServerActivity(
        [activity({ targetType: "tunneled_mcp_server" })],
        catalogMcpServer,
      ),
    ).toBe(false);
    expect(
      hasMcpServerActivity(
        [activity({ targetId: "another-server" })],
        catalogMcpServer,
      ),
    ).toBe(false);
  });
});

describe("hasBlockingSecretsPolicy", () => {
  it("matches an enabled gitleaks policy set to block", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({
          action: "block",
          sources: ["gitleaks"],
          messageTypes: ["tool_request", "tool_response"],
        }),
      ]),
    ).toBe(true);
  });

  it("rejects an omitted message type list", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({ action: "block", sources: ["gitleaks"] }),
      ]),
    ).toBe(false);
  });

  it("rejects an empty message type list", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({
          action: "block",
          sources: ["gitleaks"],
          messageTypes: [],
        }),
      ]),
    ).toBe(false);
  });

  it("rejects a targeted secrets policy", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({
          action: "block",
          audienceType: "targeted",
          sources: ["gitleaks"],
          messageTypes: ["tool_request", "tool_response"],
        }),
      ]),
    ).toBe(false);
  });

  it("rejects a prompt-based secrets policy", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({
          action: "block",
          policyType: "prompt_based",
          sources: ["gitleaks"],
          messageTypes: ["tool_request", "tool_response"],
        }),
      ]),
    ).toBe(false);
  });

  it("rejects a policy with every secrets rule disabled", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({
          action: "block",
          sources: ["gitleaks"],
          messageTypes: ["tool_request", "tool_response"],
          disabledRules: DETECTION_RULES.secrets.map((rule) => rule.id),
        }),
      ]),
    ).toBe(false);
  });

  it("rejects a policy scoped away from the whole project", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({
          action: "block",
          sources: ["gitleaks"],
          messageTypes: ["tool_request", "tool_response"],
          scopeInclude: "user:admin",
        }),
      ]),
    ).toBe(false);
  });

  it("does not treat an SSE or insecure remote as catalog-backed", () => {
    expect(
      catalogBackedMcpServers(
        [server({ remoteMcpServerId: "remote-id" })],
        [remote({ transportType: "sse" })],
        [catalogServer()],
      ),
    ).toEqual([]);
    expect(
      catalogBackedMcpServers(
        [server({ remoteMcpServerId: "remote-id" })],
        [remote({ url: "http://catalog.example/mcp" })],
        [catalogServer("http://catalog.example/mcp")],
      ),
    ).toEqual([]);
  });

  it("rejects extra message types outside the standard tool surfaces", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({
          action: "block",
          sources: ["gitleaks"],
          messageTypes: ["tool_request", "tool_response", "user_message"],
        }),
      ]),
    ).toBe(false);
  });

  it("rejects a policy that combines secrets with another source", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({
          action: "block",
          sources: ["gitleaks", "prompt_injection"],
          messageTypes: ["tool_request", "tool_response"],
        }),
      ]),
    ).toBe(false);
  });

  it("rejects a secrets policy that does not scan tool requests and responses", () => {
    expect(
      hasBlockingSecretsPolicy([
        policy({
          action: "block",
          sources: ["gitleaks"],
          messageTypes: ["user_message"],
        }),
      ]),
    ).toBe(false);
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

describe("hasDefaultPluginServer", () => {
  it("matches a server in the Default plugin", () => {
    expect(
      hasDefaultPluginServer(
        [
          plugin({
            isDefault: true,
            servers: [pluginServer("server-1")],
          }),
        ],
        "server-1",
      ),
    ).toBe(true);
  });

  it("ignores a server in a non-default plugin", () => {
    expect(
      hasDefaultPluginServer(
        [
          plugin({
            isDefault: false,
            servers: [pluginServer("server-1")],
          }),
        ],
        "server-1",
      ),
    ).toBe(false);
  });

  it("handles an unread plugin list", () => {
    expect(hasDefaultPluginServer(undefined, "server-1")).toBe(false);
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

describe("latestSecretsFinding", () => {
  it("selects the newest finding rather than trusting response order", () => {
    const older = { id: "older", createdAt: new Date("2026-08-01") };
    const newer = { id: "newer", createdAt: new Date("2026-08-02") };

    expect(latestSecretsFinding([newer, older] as RiskResult[])).toBe(newer);
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

  it("restarts an unreadable journey conservatively", () => {
    expect(firstIncompleteStepIndex("unreadable", 3)).toBe(0);
  });
});
