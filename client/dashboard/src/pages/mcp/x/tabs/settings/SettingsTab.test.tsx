import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SettingsTab } from "./SettingsTab";

vi.mock("./sections/BrandingSection", () => ({
  BrandingSection: ({ title = "Branding" }: { title?: string }) => (
    <h2>{title}</h2>
  ),
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
vi.mock("./sections/SourceNameSection", () => ({
  MCP_SOURCE_NAME_SECTION_ID: "source-name",
  RemoteSourceNameSection: () => <h2>Source Name</h2>,
  TunneledSourceNameSection: () => <h2>Source Name</h2>,
}));
vi.mock("./sections/UpstreamUrlSection", () => ({
  MCP_UPSTREAM_URL_SECTION_ID: "upstream-url",
  UpstreamUrlSection: () => <h2>Upstream URL</h2>,
}));
vi.mock("./sections/ResourceIdentifierSection", () => ({
  MCP_RESOURCE_IDENTIFIER_SECTION_ID: "resource-identifier",
  ResourceIdentifierSection: () => <h2>Resource Identifier</h2>,
}));
vi.mock("./sections/PublicAccessSection", () => ({
  MCP_PUBLIC_ACCESS_SECTION_ID: "public-access",
  PublicAccessSection: () => <h2>Public Access</h2>,
}));
vi.mock("./sections/TunnelKeySection", () => ({
  MCP_TUNNEL_KEY_SECTION_ID: "tunnel-key",
  TunnelKeySection: () => <h2>Tunnel Key</h2>,
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
    data: query?.enabled ? { id } : undefined,
    isError: false,
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

afterEach(cleanup);

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
      "Display",
      "Source Name",
      "Upstream URL",
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
      "Branding",
      "Source Name",
      "Server URL",
      "Network Access",
      "Authentication",
      "Resource Identifier",
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
    ).toEqual(["Branding", "Authentication", "Danger Zone"]);
  });
});
