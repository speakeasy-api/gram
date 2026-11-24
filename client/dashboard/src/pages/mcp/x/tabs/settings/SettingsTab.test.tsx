import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SettingsTab } from "./SettingsTab";

vi.mock("@/hooks/useToolsetUrl", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useToolsetUrl")>()),
  useResolvedMcpServerUrl: () => ({
    mcpUrl: undefined,
    installPageUrl: undefined,
    loading: false,
  }),
}));

vi.mock("./sections/GeneralSection", () => ({
  GeneralSection: () => <h2>General</h2>,
}));
vi.mock("./sections/authentication/AuthenticationSection", () => ({
  MCP_AUTHENTICATION_SECTION_ID: "authentication",
  AuthenticationSection: ({ mcpServer }: { mcpServer: McpServer }) =>
    mcpServer.remoteMcpServerId ? (
      <h2 data-testid="legacy-identity-section">Sign-in method</h2>
    ) : (
      <h2>Sign-in method</h2>
    ),
}));
vi.mock("./sections/authentication/UpstreamCredentialsSection", () => ({
  UPSTREAM_CREDENTIALS_SECTION_ID: "upstream-credentials",
  UpstreamCredentialsSection: () => (
    <h2 data-testid="upstream-credentials-section">Upstream credentials</h2>
  ),
}));

vi.mock("./sections/authentication/SetupStepBanner", () => ({
  SetupStepBanner: () => null,
}));
vi.mock("@/contexts/Auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Auth")>()),
  useOrganization: () => ({ id: "org-1" }),
}));
vi.mock("./sections/authentication/IdentityChainingSection", () => ({
  IDENTITY_CHAINING_SECTION_ID: "identity-chaining",
  IdentityChainingSection: () => <h2>Identity chaining</h2>,
}));
vi.mock("./sections/DangerZoneSection", () => ({
  DangerZoneSection: () => <h2>Danger Zone</h2>,
}));
vi.mock("./sections/RestartSetupSection", () => ({
  RestartSetupSection: () => <h2>Guided setup</h2>,
}));
vi.mock("./sections/PublicRateLimitsSection", () => ({
  PublicRateLimitsSection: () => <h2>Public Rate Limits</h2>,
}));
vi.mock("./sections/RemoteMcpSessionsSection", () => ({
  MCP_SESSIONS_SECTION_ID: "sessions",
  RemoteMcpSessionsSection: () => <h2>MCP sign-in</h2>,
}));
vi.mock("./sections/UpstreamUrlSection", () => ({
  MCP_UPSTREAM_URL_SECTION_ID: "upstream-url",
  UpstreamUrlSection: () => <h2>Remote URL</h2>,
}));
vi.mock("./sections/ServerUrlSection", () => ({
  MCP_SERVER_URL_SECTION_ID: "server-url",
  ServerUrlSection: () => <h2>Server URL</h2>,
}));
vi.mock("./sections/ToolFilteringSection", () => ({
  ToolFilteringSection: () => <h2>Tool Filtering</h2>,
}));

vi.mock("./sections/VisibilitySection", () => ({
  MCP_VISIBILITY_SECTION_ID: "visibility",
  AvailabilitySection: () => <h2>Availability</h2>,
  VisibilitySection: () => <h2>Visibility</h2>,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/hooks/usePluginWriteAccess", () => ({
  usePluginWriteAccess: () => true,
}));
vi.mock("@/pages/mcp/overview/PluginStatusBanner", () => ({
  PluginStatusBanner: () => <h2>Plugins</h2>,
}));

vi.mock("./sections/NetworkAccessSection", () => ({
  NetworkAccessSection: () => <h2>Network Access</h2>,
}));
vi.mock("./sections/ResourceIdentifierSection", () => ({
  MCP_RESOURCE_IDENTIFIER_SECTION_ID: "resource-identifier",
  ResourceIdentifierSection: () => <h2>Resource Identifier</h2>,
}));
vi.mock("./sections/CallerIdentitySection", () => ({
  CallerIdentitySection: () => <h2>Caller Identity</h2>,
}));
vi.mock("./sections/PublicAccessSection", () => ({
  MCP_PUBLIC_ACCESS_SECTION_ID: "public-access",
}));
vi.mock("./sections/TunnelKeySection", () => ({
  MCP_TUNNEL_KEY_SECTION_ID: "tunnel-key",
  TunnelKeySection: () => <h2>Tunnel Key</h2>,
}));
vi.mock("./sections/AgentSetupSection", () => ({
  MCP_AGENT_SETUP_SECTION_ID: "agent-setup",
  AgentSetupSection: () => <h2>Tunnel agent setup</h2>,
}));

// The source rows the tab fetches for the sections that edit them. Only the
// enabled query resolves, the same way the real hooks behave.
const sourceUrl = vi.hoisted(() => ({
  value: "https://mcp.example.com/mcp",
}));
function sourceRow(id: string) {
  return (
    _args: unknown,
    _options: unknown,
    query?: { enabled?: boolean },
  ) => ({
    data: query?.enabled ? { id, url: sourceUrl.value } : undefined,
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

// The section headings, in page order. Area headings are left out.
function renderSettings(mcpServer: McpServer): string[] {
  renderTab(mcpServer);
  return screen
    .getAllByRole("heading")
    .filter((heading) => !heading.id.endsWith("-title"))
    .map((heading) => heading.textContent ?? "");
}

// Each area with the section headings under it.
function renderAreas(mcpServer: McpServer): Record<string, string[]> {
  renderTab(mcpServer);
  return Object.fromEntries(
    screen.getAllByRole("region").map((region) => [
      region.getAttribute("aria-label") ??
        document.getElementById(region.getAttribute("aria-labelledby") ?? "")
          ?.textContent,
      Array.from(region.querySelectorAll("h2"))
        .filter((heading) => !heading.id.endsWith("-title"))
        .map((heading) => heading.textContent ?? ""),
    ]),
  );
}

function renderTab(mcpServer: McpServer): void {
  render(
    <MemoryRouter>
      <SettingsTab
        mcpServer={mcpServer}
        endpoints={[]}
        isLoadingEndpoints={false}
      />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
});

describe("SettingsTab", () => {
  it("adds a Guided setup area above Danger zone once guided setup is done", () => {
    sourceUrl.value = "https://mcp.slack.com/mcp";
    const areas = renderAreas(
      server({
        remoteMcpServerId: "remote-source-1",
        setupGuideCompletedAt: new Date(),
      }),
    );
    sourceUrl.value = "https://mcp.example.com/mcp";
    expect(areas["Guided setup"]).toEqual(["Guided setup"]);
    const names = Object.keys(areas);
    expect(names.indexOf("Guided setup")).toBe(
      names.indexOf("Danger zone") - 1,
    );
  });

  it.each([
    ["guided setup is not done", "https://mcp.slack.com/mcp", undefined],
    [
      "the provider has no guided setup",
      "https://mcp.example.com/mcp",
      new Date(),
    ],
  ])(
    "has no Guided setup area when %s",
    (_case, url, setupGuideCompletedAt) => {
      sourceUrl.value = url;
      const areas = renderAreas(
        server({ remoteMcpServerId: "remote-source-1", setupGuideCompletedAt }),
      );
      sourceUrl.value = "https://mcp.example.com/mcp";
      expect(areas["Guided setup"]).toBeUndefined();
    },
  );

  it("groups Remote MCP settings into areas", () => {
    expect(
      renderAreas(
        server({
          remoteMcpServerId: "remote-source-1",
          userSessionIssuerId: "issuer-1",
        }),
      ),
    ).toEqual({
      // On or off is about the server itself, so it leads General and the
      // server has no Access area.
      General: ["Availability", "General"],
      Plugins: ["Plugins"],
      "Sign-in": ["Upstream credentials", "MCP sign-in", "Identity chaining"],
      Connection: ["Server URL", "Remote URL", "Network Access"],
      Tools: ["Tool Filtering"],
      "Danger zone": ["Danger Zone"],
    });
  });

  it("groups tunneled MCP settings into areas", () => {
    expect(
      renderAreas(server({ tunneledMcpServerId: "tunneled-source-1" })),
    ).toEqual({
      // Every server leads with on or off; who may connect is Access.
      General: ["Availability", "General"],
      Access: ["Visibility", "Public Rate Limits"],
      Plugins: ["Plugins"],
      "Sign-in": [
        "Sign-in method",
        "Upstream credentials",
        "Resource Identifier",
        "Caller Identity",
      ],
      Connection: [
        "Server URL",
        "Network Access",
        "Tunnel Key",
        "Tunnel agent setup",
      ],
      Tools: ["Tool Filtering"],
      "Danger zone": ["Danger Zone"],
    });
  });

  it("groups unproxied MCP settings into areas", () => {
    expect(
      renderAreas(server({ unproxiedMcpServerId: "unproxied-source-1" })),
    ).toEqual({
      General: ["Availability", "General"],
      Plugins: ["Plugins"],
      "Sign-in": ["Sign-in method"],
      "Danger zone": ["Danger Zone"],
    });
  });

  it("lands old Access links on Availability when the area is gone", () => {
    renderTab(server({ remoteMcpServerId: "remote-source-1" }));
    expect(document.getElementById("settings-access")?.textContent).toContain(
      "Availability",
    );
  });

  it("indexes every area it shows, by its name while no section title registers", () => {
    renderTab(server({ unproxiedMcpServerId: "unproxied-source-1" }));
    const index = screen.getByRole("navigation", { name: "Settings sections" });
    expect(
      Array.from(index.querySelectorAll("a")).map((link) => [
        link.textContent,
        link.getAttribute("href"),
      ]),
    ).toEqual([
      ["General", "#settings-general"],
      ["Plugins", "#publishing"],
      ["Sign-in", "#settings-sign-in"],
      ["Danger zone", "#settings-danger-zone"],
    ]);
  });
});

const remoteServer = () =>
  server({
    remoteMcpServerId: "remote-source-1",
    userSessionIssuerId: "issuer-1",
  });

describe("SettingsTab typed upstream identity", () => {
  it("shows typed credentials and keeps sessions on a proxied server", () => {
    expect(renderSettings(remoteServer())).toEqual([
      "Availability",
      "General",
      "Plugins",
      "Upstream credentials",
      "MCP sign-in",
      "Identity chaining",
      "Server URL",
      "Remote URL",
      "Network Access",
      "Tool Filtering",
      "Danger Zone",
    ]);
    expect(screen.getByTestId("upstream-credentials-section")).toBeTruthy();
    expect(screen.queryByTestId("legacy-identity-section")).toBeNull();
    // The legacy anchor resolves to the replacement.
    expect(
      document
        .getElementById("authentication")
        ?.querySelector("[data-testid='upstream-credentials-section']"),
    ).toBeTruthy();
  });

  it("keeps tunneled caller authentication alongside upstream credentials", () => {
    const headings = renderSettings(
      server({ tunneledMcpServerId: "tunneled-source-1" }),
    );
    expect(headings.slice(0, 7)).toEqual([
      "Availability",
      "General",
      "Visibility",
      "Public Rate Limits",
      "Plugins",
      "Sign-in method",
      "Upstream credentials",
    ]);
    expect(screen.getByTestId("upstream-credentials-section")).toBeTruthy();
  });

  it("never offers upstream identity on unproxied or toolset-backed servers", () => {
    expect(
      renderSettings(server({ unproxiedMcpServerId: "unproxied-source-1" })),
    ).toEqual([
      "Availability",
      "General",
      "Plugins",
      "Sign-in method",
      "Danger Zone",
    ]);
    cleanup();
    expect(
      renderSettings(
        server({
          tunneledMcpServerId: "tunneled-source-1",
          toolsetId: "toolset-1",
        }),
      ),
    ).not.toContain("Upstream credentials");
  });
});
