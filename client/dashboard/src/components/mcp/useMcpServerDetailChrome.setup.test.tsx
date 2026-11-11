import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useMcpServerDetailChrome } from "./useMcpServerDetailChrome";

const state = vi.hoisted(() => ({
  rollout: "enabled" as "enabled" | "disabled" | "loading",
  canWrite: true,
  url: "https://mcp.slack.com/mcp",
  server: undefined as Record<string, unknown> | undefined,
  hostedToolset: undefined as
    | { id: string; slug: string; name: string }
    | undefined,
  canReadPlugins: false,
  plugins: [] as unknown[],
  publishStatus: {} as Record<string, unknown>,
  missingEnvVars: 0,
}));
const REMOTE_SERVER = {
  id: "server",
  slug: "slack",
  remoteMcpServerId: "remote",
};
vi.mock("@/hooks/useProviderSetupRollout", () => ({
  useProviderSetupRollout: () => state.rollout,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org" }),
  useProject: () => ({ id: "project" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "example", projectSlug: "default" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => scope === "mcp:write" && state.canWrite,
    hasAnyScope: () => state.canReadPlugins,
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "disabled" }),
}));
vi.mock("@/hooks/usePluginWriteAccess", () => ({
  usePluginWriteAccess: () => false,
}));
vi.mock("@/pages/plugins/usePluginQueryScope", () => ({
  usePluginQueryScope: () => ({}),
}));
vi.mock("@/pages/skills/use-skills-over-mcp", () => ({
  useSkillsOverMcpEnabled: () => false,
}));
vi.mock("@/hooks/useUpstreamAuthEnforcement", () => ({
  useUpstreamAuthEnforcement: () => "legacy",
}));
vi.mock("@/hooks/usePrivateMcpServerUrls", () => ({
  usePrivateMcpServerUrls: () => ({
    privateMcpUrls: [],
    privateInstallPageUrls: [],
    canReadPrivateUrls: false,
  }),
  mcpServerInstallPageLinks: () => [],
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useResolvedMcpServerUrl: () => ({}),
}));
vi.mock("@/pages/mcp/x/hosted/useHostedToolset", () => ({
  useHostedToolset: (server: unknown) => ({
    toolset: server ? state.hostedToolset : undefined,
    isLoading: false,
    isError: false,
    refetch: () => {},
  }),
}));
vi.mock("@/hooks/useMissingEnvironmentVariables", () => ({
  useMissingRequiredEnvVars: () => state.missingEnvVars,
}));
vi.mock("@/pages/mcp/x/MCPServerDetails", () => ({
  MCPServerAvailabilityToggle: () => null,
  MCPServerStatusDropdown: () => null,
}));
vi.mock("@/pages/mcp/x/tabs/tunnelStatus", () => ({
  useTunneledMcpConnections: () => ({}),
  tunnelAgentReadiness: () => ({}),
}));
vi.mock("@/lib/remote-identity", () => ({
  useAllRemoteSessionClients: () => ({ items: [] }),
  useUpstreamProbe: () => "unknown",
  deriveIdentityMode: () => "none",
  findPassThroughAuthorizationHeader: () => undefined,
  IdentityExplainerDialog: () => null,
}));
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  useGetMcpServer: () => ({ data: state.server ?? REMOTE_SERVER }),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServer.js", () => ({
  useGetRemoteMcpServer: () => ({ data: { url: state.url } }),
}));
vi.mock("@gram/client/react-query/getUnproxiedMcpServer.js", () => ({
  useGetUnproxiedMcpServer: () => ({}),
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: () => ({ data: { mcpEndpoints: [] } }),
}));
vi.mock("@gram/client/react-query/remoteMcpServerHeaders.js", () => ({
  useRemoteMcpServerHeaders: () => ({}),
}));
vi.mock("@gram/client/react-query/plugins", () => ({
  usePlugins: () => ({ data: { plugins: state.plugins } }),
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({ data: state.publishStatus }),
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: () => ({ data: { environments: [] }, isPending: false }),
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: () => ({ data: undefined, isPending: false }),
}));
vi.mock("@/pages/mcp/overview/PluginStatusBanner", () => ({
  PluginStatusBanner: () => null,
}));
vi.mock("@gram/client/react-query/mcpServerAuthentication.js", () => ({
  useMcpServerAuthentication: () => ({}),
}));
vi.mock("@/components/detail/detail-sidebar-nav", () => ({
  DetailSidebarInfoLabel: () => null,
}));
// The page tabs the detail frame renders from the chrome's items.
function Tabs() {
  const chrome = useMcpServerDetailChrome();
  return (
    <nav>
      {chrome?.items.map((item) => (
        <a key={item.key} href={item.href}>
          {item.title}
        </a>
      ))}
    </nav>
  );
}
function Sidebar() {
  return (
    <MemoryRouter
      initialEntries={["/example/projects/default/mcp/slack/overview"]}
    >
      <Routes>
        <Route
          path="/example/projects/default/mcp/:mcpServerSlug/:tab"
          element={<Tabs />}
        />
      </Routes>
    </MemoryRouter>
  );
}
afterEach(cleanup);
beforeEach(() => {
  state.rollout = "enabled";
  state.canWrite = true;
  state.url = "https://mcp.slack.com/mcp";
  state.server = undefined;
  state.hostedToolset = undefined;
  state.canReadPlugins = false;
  state.plugins = [];
  state.publishStatus = {};
  state.missingEnvVars = 0;
});
it.each(["disabled", "loading"] as const)(
  "hides Setup while the rollout is %s",
  (status) => {
    state.rollout = status;
    render(<Sidebar />);
    expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
    expect(screen.getByRole("link", { name: "Overview" })).toBeDefined();
  },
);
it("lets an unconfigured server start Setup only after enablement, and hides it when disabled", () => {
  state.rollout = "loading";
  const view = render(<Sidebar />);
  expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
  state.rollout = "enabled";
  view.rerender(<Sidebar />);
  expect(screen.getByRole("link", { name: "Setup" }).getAttribute("href")).toBe(
    "/example/projects/default/mcp/slack/setup",
  );
  state.rollout = "disabled";
  view.rerender(<Sidebar />);
  expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
});
it("retains the write permission requirement when the rollout is enabled", () => {
  state.canWrite = false;
  render(<Sidebar />);
  expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
});
it("does not offer Setup for unsupported providers", () => {
  state.url = "https://other.example/mcp";
  render(<Sidebar />);
  expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
});

// The header's readiness checklist, one line per check.
function Readiness() {
  const chrome = useMcpServerDetailChrome();
  return (
    <ul>
      {chrome?.readinessChecks.map((check) => (
        <li key={check.key} data-ready={String(check.ready)}>
          {check.label}: {check.description}
        </li>
      ))}
    </ul>
  );
}
function renderReadiness() {
  render(
    <MemoryRouter
      initialEntries={["/example/projects/default/mcp/slack/overview"]}
    >
      <Routes>
        <Route
          path="/example/projects/default/mcp/:mcpServerSlug/:tab"
          element={<Readiness />}
        />
      </Routes>
    </MemoryRouter>,
  );
}
function readiness(label: string): string | null {
  return screen.getByText(new RegExp(`^${label}:`)).getAttribute("data-ready");
}

// A canonical hosted server whose read omitted the toolset summary.
const HOSTED_SERVER = {
  id: "toolset-1",
  slug: "support",
  toolsetId: "toolset-1",
};
const HOSTED_TOOLSET = { id: "toolset-1", slug: "support", name: "Support" };

it("treats a hosted server's source as done and its sign-in as Speakeasy's", () => {
  state.server = HOSTED_SERVER;
  state.hostedToolset = HOSTED_TOOLSET;
  renderReadiness();
  expect(readiness("Source")).toBe("true");
  expect(readiness("Authentication")).toBe("true");
});

it("leaves a hosted server's Authentication open while required variables are missing", () => {
  state.server = HOSTED_SERVER;
  state.hostedToolset = HOSTED_TOOLSET;
  state.missingEnvVars = 2;
  renderReadiness();
  expect(readiness("Authentication")).toBe("false");
  expect(screen.getByText(/Set 2 required environment variables/)).toBeTruthy();
});

it("can't confirm a hosted server's Authentication before its toolset loads", () => {
  state.server = HOSTED_SERVER;
  renderReadiness();
  expect(readiness("Authentication")).toBe("null");
});

it("finds plugin membership by the loaded toolset when the summary is missing", () => {
  state.server = HOSTED_SERVER;
  state.hostedToolset = HOSTED_TOOLSET;
  state.canReadPlugins = true;
  state.plugins = [
    { id: "p1", servers: [{ id: "e1", toolsetId: "toolset-1" }] },
  ];
  state.publishStatus = { repoOwner: "acme", repoName: "plugins" };
  renderReadiness();
  expect(readiness("Included in Plugin")).toBe("true");
});

it("doesn't count a plugin as included while the marketplace has no collaborators", () => {
  state.canReadPlugins = true;
  state.plugins = [
    { id: "p1", servers: [{ id: "e1", mcpServerId: "server" }] },
  ];
  state.publishStatus = {
    repoOwner: "acme",
    repoName: "plugins",
    hasCollaborators: false,
  };
  renderReadiness();
  expect(readiness("Included in Plugin")).toBe("false");
});
