import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useMcpServerDetailChrome } from "./useMcpServerDetailChrome";

const state = vi.hoisted(() => ({
  canWrite: true,
  url: "https://mcp.slack.com/mcp",
  server: undefined as Record<string, unknown> | undefined,
  hostedToolset: undefined as
    | { id: string; slug: string; name: string }
    | undefined,
  hostedToolsetLoading: false,
  canReadPlugins: false,
  plugins: [] as unknown[],
  publishStatus: {} as Record<string, unknown>,
  missingEnvVars: 0,
  upstreamAuth: "legacy" as "legacy" | "enforced",
  upstreamAuthQuery: {} as Record<string, unknown>,
  guide: null as {
    only: { title: string } | undefined;
    openGuide: null;
  } | null,
}));
const REMOTE_SERVER = {
  id: "server",
  slug: "slack",
  remoteMcpServerId: "remote",
};
vi.mock("@/components/setup-guide/useSetupGuide", () => ({
  useSetupGuide: () => state.guide,
}));
vi.mock("./useMcpServerReachability", () => ({
  useMcpServerReachability: () => ({ kind: "off" }),
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
  useUpstreamAuthEnforcement: () => state.upstreamAuth,
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
  useCustomDomains: () => ({ domains: [], isLoading: false }),
  useMcpEndpointAddresses: () => [],
}));
vi.mock("@/pages/mcp/x/hosted/useHostedToolset", () => ({
  useHostedToolset: (server: unknown) => ({
    toolset: server ? state.hostedToolset : undefined,
    isLoading: !!server && state.hostedToolsetLoading,
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
vi.mock("@gram/client/react-query/serverClientAssociations.js", () => ({
  useServerClientAssociations: () => ({
    data: { requiredClientIds: [] },
    isError: false,
    isLoading: false,
  }),
}));
vi.mock("@gram/client/react-query/mcpServerAuthentication.js", () => ({
  useMcpServerAuthentication: () => state.upstreamAuthQuery,
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
      {chrome?.setupHref ? <a href={chrome.setupHref}>Setup</a> : null}
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
  state.canWrite = true;
  state.url = "https://mcp.slack.com/mcp";
  state.server = undefined;
  state.hostedToolset = undefined;
  state.hostedToolsetLoading = false;
  state.canReadPlugins = false;
  state.plugins = [];
  state.publishStatus = {};
  state.missingEnvVars = 0;
  state.upstreamAuth = "legacy";
  state.upstreamAuthQuery = {};
  state.guide = null;
});
it("offers Setup for a supported provider", () => {
  render(<Sidebar />);
  expect(screen.getByRole("link", { name: "Setup" }).getAttribute("href")).toBe(
    "/example/projects/default/mcp/slack/setup",
  );
});
it("hides guided Setup once it is marked done", () => {
  state.server = { ...REMOTE_SERVER, setupGuideCompletedAt: new Date() };
  render(<Sidebar />);
  expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
  expect(screen.getByRole("link", { name: "Overview" })).toBeDefined();
});
it("keeps a written guide's Setup after it is marked done", () => {
  state.url = "https://mcp.linear.app/mcp";
  state.guide = { only: { title: "Linear" }, openGuide: null };
  state.server = { ...REMOTE_SERVER, setupGuideCompletedAt: new Date() };
  render(<Sidebar />);
  expect(screen.getByRole("link", { name: "Setup" })).toBeDefined();
});
it("requires write permission to offer Setup", () => {
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
  state.hostedToolsetLoading = true;
  renderReadiness();
  expect(readiness("Authentication")).toBe("null");
  expect(
    screen.getByText(/Checking required environment variables/),
  ).toBeTruthy();
});

it("reports a finished toolset lookup with no match as not found", () => {
  state.server = HOSTED_SERVER;
  renderReadiness();
  expect(readiness("Authentication")).toBe("null");
  expect(screen.getByText(/toolset could not be found/)).toBeTruthy();
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
  expect(readiness("Included in a plugin")).toBe("true");
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
  expect(readiness("Included in a plugin")).toBe("false");
});

it("reports a turned-off server as broken, not as a setup step", () => {
  state.server = { ...HOSTED_SERVER, visibility: "disabled" };
  state.hostedToolset = HOSTED_TOOLSET;
  renderReadiness();
  expect(readiness("Availability")).toBe("false");
  expect(screen.getByText(/This server is turned off/)).toBeTruthy();

  cleanup();
  state.server = { ...HOSTED_SERVER, visibility: "private" };
  renderReadiness();
  expect(readiness("Availability")).toBe("true");
});

it("sends a guide-only provider to the Setup tab, as the step after Authentication, until marked done", () => {
  state.url = "https://mcp.linear.app/mcp";
  state.server = REMOTE_SERVER;
  state.guide = { only: { title: "Linear" }, openGuide: null };
  renderReadiness();
  const labels = screen
    .getAllByRole("listitem")
    .map((item) => item.textContent?.split(":")[0]);
  expect(labels.indexOf("Set up Linear")).toBe(
    labels.indexOf("Authentication") + 1,
  );
  expect(readiness("Set up Linear")).toBe("false");
  cleanup();

  state.server = { ...REMOTE_SERVER, setupGuideCompletedAt: new Date() };
  renderReadiness();
  expect(readiness("Set up Linear")).toBe("true");
  expect(screen.getByText(/Marked done/)).toBeTruthy();
});

it("offers Setup for a guide-only provider only with write access", () => {
  state.url = "https://mcp.linear.app/mcp";
  state.guide = { only: { title: "Linear" }, openGuide: null };
  render(<Sidebar />);
  expect(screen.getByRole("link", { name: "Setup" })).toBeDefined();
  cleanup();
  renderReadiness();
  cleanup();
  state.canWrite = false;
  render(<Sidebar />);
  expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
});

it("leaves a guided provider's steps to its guided flow", () => {
  state.server = REMOTE_SERVER;
  state.guide = { only: { title: "Slack" }, openGuide: null };
  renderReadiness();
  expect(screen.queryByText(/^Set up Slack:/)).toBeNull();
});

it("lists no setup step for a provider without a guide", () => {
  state.url = "https://other.example/mcp";
  state.server = REMOTE_SERVER;
  renderReadiness();
  expect(screen.queryByText(/^Set up /)).toBeNull();
});

function IdentityRow() {
  const chrome = useMcpServerDetailChrome();
  return (
    <span data-warn={String(!!chrome?.identity?.warn)}>
      {chrome?.identity?.label}
    </span>
  );
}
function renderIdentity() {
  render(
    <MemoryRouter
      initialEntries={["/example/projects/default/mcp/slack/overview"]}
    >
      <Routes>
        <Route
          path="/example/projects/default/mcp/:mcpServerSlug/:tab"
          element={<IdentityRow />}
        />
      </Routes>
    </MemoryRouter>,
  );
}
const credential = (enabled: boolean) => ({
  id: String(Math.random()),
  enabled,
});

it.each([
  [[true, true], "2 credentials configured"],
  [[true, false], "1 credential configured"],
])(
  "counts the turned-on credentials under enforced upstream auth",
  (enabled, label) => {
    state.upstreamAuth = "enforced";
    state.upstreamAuthQuery = {
      data: { status: "configured", clients: enabled.map(credential) },
      isPending: false,
      isError: false,
    };
    renderIdentity();
    expect(screen.getByText(label)).toBeDefined();
  },
);

it("names an enforced server with no credentials by its sign-in status", () => {
  state.url = "https://other.example/mcp";
  state.upstreamAuth = "enforced";
  state.upstreamAuthQuery = {
    data: { status: "no_auth_required", clients: [] },
    isPending: false,
    isError: false,
  };
  renderIdentity();
  expect(screen.getByText("No authentication")).toBeDefined();
});

it("warns when an enforced server's credentials can't be read", () => {
  state.upstreamAuth = "enforced";
  state.upstreamAuthQuery = { isPending: false, isError: true };
  renderIdentity();
  expect(screen.getByText("Couldn't check").getAttribute("data-warn")).toBe(
    "true",
  );
});

it.each([
  ["setup_incomplete", "enforced"],
  ["no_auth_required", "enforced"],
] as const)(
  "sends a guided server whose credentials are %s to Setup",
  (status, upstreamAuth) => {
    state.upstreamAuth = upstreamAuth;
    state.upstreamAuthQuery = {
      data: { status, clients: [] },
      isPending: false,
      isError: false,
    };
    renderIdentity();
    expect(screen.getByText("Setup pending").getAttribute("data-warn")).toBe(
      "true",
    );
  },
);

it("keeps a guided server's configured credentials", () => {
  state.upstreamAuth = "enforced";
  state.upstreamAuthQuery = {
    data: { status: "configured", clients: [credential(true)] },
    isPending: false,
    isError: false,
  };
  renderIdentity();
  expect(screen.getByText("1 credential configured")).toBeDefined();
  expect(screen.queryByText("Setup pending")).toBeNull();
});
