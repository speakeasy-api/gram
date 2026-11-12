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
      <h2 data-testid="legacy-identity-section">Identity</h2>
    ) : (
      <h2>Authentication</h2>
    ),
}));
vi.mock("./sections/authentication/UpstreamCredentialsSection", () => ({
  UPSTREAM_CREDENTIALS_SECTION_ID: "upstream-credentials",
  UpstreamCredentialsSection: () => (
    <h2 data-testid="upstream-credentials-section">Identity</h2>
  ),
}));

// The enforcement state comes from the organization's product features; the
// real gate hook reads it through this query.
const productFeatures = vi.hoisted(() => ({
  result: {
    isPending: false,
    isError: false,
    data: { upstreamAuthEnforced: false } as
      | { upstreamAuthEnforced: boolean }
      | undefined,
  },
}));
vi.mock("./sections/authentication/SetupStepBanner", () => ({
  SetupStepBanner: () => null,
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => productFeatures.result,
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
vi.mock("./sections/HeadersSection", () => ({
  HeadersSection: () => <h2>Custom Headers</h2>,
}));
vi.mock("./sections/PublicRateLimitsSection", () => ({
  PublicRateLimitsSection: () => <h2>Public Rate Limits</h2>,
}));
vi.mock("./sections/RemoteMcpSessionsSection", () => ({
  MCP_SESSIONS_SECTION_ID: "sessions",
  RemoteMcpSessionsSection: () => <h2>Sessions</h2>,
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
  VisibilitySection: () => <h2>Visibility</h2>,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/hooks/usePluginWriteAccess", () => ({
  usePluginWriteAccess: () => true,
}));
vi.mock("@/pages/mcp/overview/PluginStatusBanner", () => ({
  PluginStatusBanner: () => <h2>Publishing</h2>,
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
  PublicAccessSection: () => <h2>Public Access</h2>,
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
  setEnforcement("legacy");
});

describe("SettingsTab", () => {
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
      General: ["Visibility", "General"],
      Publishing: ["Publishing"],
      // Upstream headers are the Identity panel's Custom Headers disclosure,
      // not a section of their own.
      "Sign-in and identity": ["Identity", "Identity chaining"],
      Connection: ["Server URL", "Network Access", "Sessions"],
      Tools: ["Tool Filtering"],
      "Danger zone": ["Danger Zone"],
    });
  });

  it("groups tunneled MCP settings into areas", () => {
    expect(
      renderAreas(server({ tunneledMcpServerId: "tunneled-source-1" })),
    ).toEqual({
      General: ["General"],
      Access: ["Visibility", "Public Access", "Public Rate Limits"],
      Publishing: ["Publishing"],
      "Sign-in and identity": [
        "Authentication",
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
      General: ["Visibility", "General"],
      Publishing: ["Publishing"],
      "Sign-in and identity": ["Authentication"],
      "Danger zone": ["Danger Zone"],
    });
  });

  it("lands old Access links on Availability when the area is gone", () => {
    renderTab(server({ remoteMcpServerId: "remote-source-1" }));
    expect(document.getElementById("settings-access")?.textContent).toContain(
      "Visibility",
    );
  });

  it("indexes every area it shows", () => {
    renderTab(server({ unproxiedMcpServerId: "unproxied-source-1" }));
    const index = screen.getByRole("navigation", { name: "Settings areas" });
    expect(
      Array.from(index.querySelectorAll("a")).map((link) => [
        link.textContent,
        link.getAttribute("href"),
      ]),
    ).toEqual([
      ["General", "#settings-general"],
      ["Publishing", "#publishing"],
      ["Sign-in and identity", "#settings-sign-in"],
      ["Danger zone", "#settings-danger-zone"],
    ]);
  });
});

type EnforcementState = "enforced" | "legacy" | "loading" | "error";

function setEnforcement(state: EnforcementState): void {
  productFeatures.result =
    state === "loading"
      ? { isPending: true, isError: false, data: undefined }
      : state === "error"
        ? { isPending: false, isError: true, data: undefined }
        : {
            isPending: false,
            isError: false,
            data: { upstreamAuthEnforced: state === "enforced" },
          };
}

const remoteServer = () =>
  server({
    remoteMcpServerId: "remote-source-1",
    userSessionIssuerId: "issuer-1",
  });

describe("SettingsTab upstream identity gate", () => {
  it("shows only the credentials as Identity on a remote server when enforced", () => {
    setEnforcement("enforced");
    expect(renderSettings(remoteServer())).toEqual([
      "Visibility",
      "General",
      "Publishing",
      "Identity",
      "Identity chaining",
      "Server URL",
      "Network Access",
      "Sessions",
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

  it("shows only the legacy Identity on a remote server when not enforced", () => {
    setEnforcement("legacy");
    expect(renderSettings(remoteServer())).toContain("Identity");
    expect(screen.getByTestId("legacy-identity-section")).toBeTruthy();
    expect(screen.queryByTestId("upstream-credentials-section")).toBeNull();
  });

  it.each([
    ["loading", "Loading…"],
    ["error", "The identity settings could not be loaded."],
  ] as const)(
    "shows neither identity surface on a remote server while %s",
    (state, message) => {
      setEnforcement(state);
      expect(
        renderSettings(remoteServer()).filter((h) => h === "Identity"),
      ).toHaveLength(1);
      expect(screen.getByText(message, { exact: false })).toBeTruthy();
      expect(screen.queryByTestId("legacy-identity-section")).toBeNull();
      expect(screen.queryByTestId("upstream-credentials-section")).toBeNull();
      expect(document.getElementById("authentication")).toBeTruthy();
    },
  );

  it("keeps tunneled caller authentication and adds credentials as Identity when enforced", () => {
    setEnforcement("enforced");
    const headings = renderSettings(
      server({ tunneledMcpServerId: "tunneled-source-1" }),
    );
    expect(headings.slice(0, 7)).toEqual([
      "General",
      "Visibility",
      "Public Access",
      "Public Rate Limits",
      "Publishing",
      "Authentication",
      "Identity",
    ]);
    expect(screen.getByTestId("upstream-credentials-section")).toBeTruthy();
  });

  it("keeps tunneled caller authentication without credentials when not enforced", () => {
    setEnforcement("legacy");
    const headings = renderSettings(
      server({ tunneledMcpServerId: "tunneled-source-1" }),
    );
    expect(headings).toContain("Authentication");
    expect(headings).not.toContain("Identity");
    expect(screen.queryByTestId("upstream-credentials-section")).toBeNull();
  });

  it.each(["enforced", "legacy", "loading", "error"] as const)(
    "never offers upstream identity on unproxied or toolset-backed servers (%s)",
    (state) => {
      setEnforcement(state);
      expect(
        renderSettings(server({ unproxiedMcpServerId: "unproxied-source-1" })),
      ).toEqual([
        "Visibility",
        "General",
        "Publishing",
        "Authentication",
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
      ).not.toContain("Identity");
    },
  );
});

describe("SettingsTab upstream identity gate state", () => {
  it("keeps the last known surface when a refetch fails", () => {
    productFeatures.result = {
      isPending: false,
      isError: true,
      data: { upstreamAuthEnforced: true },
    };
    renderSettings(remoteServer());
    expect(screen.getByTestId("upstream-credentials-section")).toBeTruthy();
    expect(screen.queryByTestId("legacy-identity-section")).toBeNull();
  });

  it("shows neither surface when the server could not read the state", () => {
    productFeatures.result = {
      isPending: false,
      isError: false,
      data: {} as { upstreamAuthEnforced: boolean },
    };
    renderSettings(remoteServer());
    expect(
      screen.getByText("The identity settings could not be loaded.", {
        exact: false,
      }),
    ).toBeTruthy();
    expect(screen.queryByTestId("legacy-identity-section")).toBeNull();
    expect(screen.queryByTestId("upstream-credentials-section")).toBeNull();
  });
});
