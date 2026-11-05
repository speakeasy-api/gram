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
  setEnforcement("legacy");
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
      "Identity chaining",
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
      "Caller Identity",
      "Public Access",
      "Public Rate Limits",
      "Tunnel Key",
      "Tunnel agent setup",
      "Tool Filtering",
      "Danger Zone",
    ]);
  });

  it("preserves unproxied MCP settings structure", () => {
    expect(
      renderSettings(server({ unproxiedMcpServerId: "unproxied-source-1" })),
    ).toEqual(["General", "Authentication", "Danger Zone"]);
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
      "General",
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
    expect(headings.slice(0, 5)).toEqual([
      "General",
      "Server URL",
      "Network Access",
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
      ).toEqual(["General", "Authentication", "Danger Zone"]);
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
