import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SettingsTab } from "./SettingsTab";

const received = vi.hoisted(() => ({
  general: [] as unknown[],
  tunnelKey: [] as unknown[],
  // Lets a test hand back a retained row from a refetch that failed.
  source: { environmentLinked: false as boolean | undefined, isError: false },
}));

vi.mock("./sections/GeneralSection", () => ({
  GeneralSection: (props: { remoteMcpServer?: unknown }) => {
    received.general.push(props.remoteMcpServer);
    return <h2>General</h2>;
  },
}));
vi.mock("./sections/authentication/AuthenticationSection", () => ({
  MCP_AUTHENTICATION_SECTION_ID: "authentication",
  AuthenticationSection: ({ mcpServer }: { mcpServer: McpServer }) => (
    <h2>{mcpServer.remoteMcpServerId ? "Identity" : "Authentication"}</h2>
  ),
}));
vi.mock("./sections/DangerZoneSection", () => ({
  DangerZoneSection: () => <h2>Danger Zone</h2>,
}));
vi.mock("./sections/HeadersSection", () => ({
  HeadersSection: () => <h2>Custom Headers</h2>,
}));
vi.mock("./sections/PublicRateLimitsSection", () => ({
  PublicRateLimitsSection: () => <h2>Public Rate Limits</h2>,
}));
vi.mock("./sections/RemoteMcpSessionsSection", () => ({
  RemoteMcpSessionsSection: () => <h2>Sessions</h2>,
}));
vi.mock("./sections/ServerUrlSection", () => ({
  MCP_SERVER_URL_SECTION_ID: "server-url",
  ServerUrlSection: () => <h2>Server URL</h2>,
}));
vi.mock("./sections/ToolFilteringSection", () => ({
  ToolFilteringSection: () => <h2>Tool Filtering</h2>,
}));

vi.mock("./sections/NetworkAccessSection", () => ({
  NetworkAccessSection: () => <h2>Network Access</h2>,
}));
vi.mock("./sections/ResourceIdentifierSection", () => ({
  MCP_RESOURCE_IDENTIFIER_SECTION_ID: "resource-identifier",
  ResourceIdentifierSection: () => <h2>Resource Identifier</h2>,
}));
vi.mock("./sections/TunneledHeadersSection", () => ({
  TunneledHeadersSection: () => <h2>Upstream Headers</h2>,
}));
vi.mock("./sections/CallerIdentitySection", () => ({
  CallerIdentitySection: () => <h2>Caller Identity</h2>,
}));
vi.mock("./sections/PublicAccessSection", () => ({
  MCP_PUBLIC_ACCESS_SECTION_ID: "public-access",
  PublicAccessSection: () => <h2>Public Access</h2>,
}));
vi.mock("./sections/TunnelKeySection", () => ({
  MCP_TUNNEL_KEY_SECTION_ID: "tunnel-key",
  TunnelKeySection: (props: { tunneledMcpServer: unknown }) => {
    received.tunnelKey.push(props.tunneledMcpServer);
    return <h2>Tunnel Key</h2>;
  },
}));
vi.mock("./sections/AgentSetupSection", () => ({
  MCP_AGENT_SETUP_SECTION_ID: "agent-setup",
  AgentSetupSection: () => <h2>Agent Setup</h2>,
}));

// The source rows the tab fetches for the sections that edit them. Only the
// enabled query resolves, the same way the real hooks behave.
function sourceRow(id: string) {
  return (
    _args: unknown,
    _options: unknown,
    query?: { enabled?: boolean },
  ) => ({
    data: query?.enabled
      ? { id, environmentLinked: received.source.environmentLinked }
      : undefined,
    isError: received.source.isError,
  });
}
vi.mock("@gram/client/react-query/getRemoteMcpServer.js", () => ({
  useGetRemoteMcpServer: sourceRow("remote-source-1"),
}));
vi.mock("@gram/client/react-query/getTunneledMcpServer.js", () => ({
  useGetTunneledMcpServer: sourceRow("tunneled-source-1"),
}));
vi.mock("@gram/client/react-query/getUnproxiedMcpServer.js", () => ({
  useGetUnproxiedMcpServer: sourceRow("unproxied-source-1"),
}));

function server(overrides: Partial<McpServer>): McpServer {
  return {
    id: "mcp-server-1",
    projectId: "project-1",
    name: "Example server",
    networkAccessMode: "public_only",
    visibility: "private",
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  } as McpServer;
}

function renderSettings(mcpServer: McpServer): string[] {
  render(
    <MemoryRouter>
      <SettingsTab
        mcpServer={mcpServer}
        endpoints={[]}
        isLoadingEndpoints={false}
      />
    </MemoryRouter>,
  );
  return screen
    .getAllByRole("heading")
    .map((heading) => heading.textContent ?? "");
}

afterEach(() => {
  cleanup();
  received.general = [];
  received.tunnelKey = [];
  received.source = { environmentLinked: false, isError: false };
});

describe("SettingsTab", () => {
  it("orders Remote MCP settings around display, identity, and sessions", () => {
    expect(
      renderSettings(
        server({
          remoteMcpServerId: "remote-source-1",
          userSessionIssuerId: "issuer-1",
        }),
      ),
    ).toEqual([
      "General",
      // Upstream headers are the Identity panel's Custom Headers disclosure
      // not a section of their own.
      "Identity",
      "Server URL",
      "Network Access",
      "Sessions",
      "Tool Filtering",
      "Danger Zone",
    ]);
  });

  it("preserves tunneled MCP settings structure", () => {
    expect(
      renderSettings(server({ tunneledMcpServerId: "tunneled-source-1" })),
    ).toEqual([
      "General",
      "Server URL",
      "Network Access",
      "Authentication",
      "Resource Identifier",
      "Upstream Headers",
      "Caller Identity",
      "Public Access",
      "Public Rate Limits",
      "Tunnel Key",
      "Agent Setup",
      "Tool Filtering",
      "Danger Zone",
    ]);
  });

  it("preserves unproxied MCP settings structure", () => {
    expect(
      renderSettings(server({ unproxiedMcpServerId: "unproxied-source-1" })),
    ).toEqual(["General", "Authentication", "Danger Zone"]);
  });

  it("passes a confirmed environment link to the sections that move the source", () => {
    renderSettings(server({ remoteMcpServerId: "remote-source-1" }));
    expect(received.general.at(-1)).toMatchObject({
      id: "remote-source-1",
      environmentLinked: false,
    });
    cleanup();
    renderSettings(server({ tunneledMcpServerId: "tunneled-source-1" }));
    expect(received.tunnelKey.at(-1)).toMatchObject({
      id: "tunneled-source-1",
      environmentLinked: false,
    });
  });

  it("treats a retained row from a failed refetch as an unknown link", () => {
    received.source = { environmentLinked: false, isError: true };
    renderSettings(server({ remoteMcpServerId: "remote-source-1" }));
    const general = received.general.at(-1) as Record<string, unknown>;
    expect(general.id).toBe("remote-source-1");
    expect(general.environmentLinked).toBeUndefined();

    cleanup();
    renderSettings(server({ tunneledMcpServerId: "tunneled-source-1" }));
    const tunnelKey = received.tunnelKey.at(-1) as Record<string, unknown>;
    expect(tunnelKey.id).toBe("tunneled-source-1");
    expect(tunnelKey.environmentLinked).toBeUndefined();
  });
});
