import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DistributeServersStep } from "./distribute-servers-step";

const serverState = vi.hoisted(() => ({
  catalog: { data: { servers: [] as Array<Record<string, unknown>> } },
  workflow: {
    phase: "configure",
    canInstall: false,
    statuses: [] as Array<Record<string, unknown>>,
    startInstall: vi.fn(),
    reset: vi.fn(),
  },
  client: {
    plugins: {
      listPlugins: vi.fn(),
      createPlugin: vi.fn(),
      getPlugin: vi.fn(),
      addPluginServer: vi.fn(),
      publishPlugins: vi.fn(),
    },
  },
  plugins: {
    data: { plugins: [] as Array<{ id: string; isDefault: boolean }> },
  },
  plugin: { data: { servers: [] as Array<{ mcpServerId: string }> } },
  mcpServers: {
    data: {
      mcpServers: [] as Array<{ id: string; remoteMcpServerId: string }>,
    },
  },
  remoteMcpServers: {
    data: { remoteMcpServers: [] as Array<{ id: string; url: string }> },
  },
}));

vi.mock("../step-container", () => ({
  StepContainer: ({
    children,
    onContinue,
    continueLabel,
    onSkip,
    skipLabel,
  }: {
    children: ReactNode;
    onContinue: () => void;
    continueLabel: string;
    onSkip: () => void;
    skipLabel: string;
  }) => (
    <>
      {children}
      <button onClick={onSkip}>{skipLabel}</button>
      <button onClick={onContinue}>{continueLabel}</button>
    </>
  ),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => serverState.client }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ catalog: { Link: () => null } }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));
vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: () => ({
    data: serverState.catalog.data,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@/pages/catalog/useRemoteMcpInstallWorkflow", () => ({
  useRemoteMcpInstallWorkflow: () => serverState.workflow,
}));
vi.mock("@gram/client/react-query/mcpServers", () => ({
  useMcpServers: () => serverState.mcpServers,
}));
vi.mock("@gram/client/react-query/remoteMcpServers", () => ({
  useRemoteMcpServers: () => serverState.remoteMcpServers,
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({ data: undefined }),
}));
vi.mock("@gram/client/react-query/plugins", () => ({
  usePlugins: () => serverState.plugins,
  invalidateAllPlugins: vi.fn(),
}));
vi.mock("@gram/client/react-query/plugin", () => ({
  usePlugin: () => serverState.plugin,
  invalidateAllPlugin: vi.fn(),
}));

afterEach(cleanup);

beforeEach(() => {
  serverState.catalog.data.servers = [];
  serverState.workflow.phase = "configure";
  serverState.workflow.canInstall = false;
  serverState.workflow.statuses = [];
  serverState.workflow.startInstall.mockReset();
  serverState.workflow.reset.mockReset();
  serverState.client.plugins.listPlugins.mockReset();
  serverState.client.plugins.createPlugin.mockReset();
  serverState.client.plugins.getPlugin.mockReset();
  serverState.client.plugins.addPluginServer.mockReset();
  serverState.client.plugins.publishPlugins.mockReset();
  serverState.plugins.data.plugins = [];
  serverState.plugin.data.servers = [];
  serverState.mcpServers.data.mcpServers = [];
  serverState.remoteMcpServers.data.remoteMcpServers = [];
});

function renderStep(onComplete: () => void, onSkip: () => void) {
  render(
    <DistributeServersStep
      onComplete={onComplete}
      onSkip={onSkip}
      onBack={() => {}}
    />,
  );
}

describe("DistributeServersStep secondary action", () => {
  it("skips without completing before a server is distributed", () => {
    const onComplete = vi.fn();
    const onSkip = vi.fn();
    renderStep(
      () => void onComplete(),
      () => void onSkip(),
    );

    fireEvent.click(screen.getByRole("button", { name: "Skip for now" }));

    expect(onSkip).toHaveBeenCalledOnce();
    expect(onComplete).not.toHaveBeenCalled();
  });

  it("keeps successful deployment instructions visible until Finish completes the step", async () => {
    serverState.catalog.data.servers = [
      {
        registryId: "registry-1",
        registrySpecifier: "example/server",
        title: "Example Server",
        description: "Example description",
        supportsDcr: true,
        remotes: [
          { url: "https://example.com/mcp", transportType: "streamable-http" },
        ],
      },
    ];
    serverState.workflow.phase = "complete";
    serverState.workflow.statuses = [
      {
        status: "completed",
        mcpServerId: "mcp-server-1",
        name: "Example Server",
      },
    ];
    serverState.client.plugins.listPlugins.mockResolvedValue({
      plugins: [{ id: "plugin-1", slug: "default", isDefault: true }],
    });
    serverState.client.plugins.getPlugin.mockResolvedValue({ servers: [] });
    serverState.client.plugins.addPluginServer.mockResolvedValue(undefined);
    const onComplete = vi.fn();
    renderStep(
      () => void onComplete(),
      () => {},
    );

    fireEvent.click(screen.getByRole("button", { name: /Example Server/ }));
    fireEvent.click(
      screen.getByRole("button", { name: "Distribute 1 server" }),
    );

    expect(
      await screen.findByRole("heading", { name: "Distribute to your team" }),
    ).toBeTruthy();
    expect(onComplete).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Finish" }));
    await waitFor(() => expect(onComplete).toHaveBeenCalledOnce());
  });

  it("completes without skipping after a server is distributed", () => {
    serverState.plugins.data.plugins = [{ id: "plugin-1", isDefault: true }];
    serverState.plugin.data.servers = [{ mcpServerId: "mcp-server-1" }];
    serverState.mcpServers.data.mcpServers = [
      { id: "mcp-server-1", remoteMcpServerId: "remote-server-1" },
    ];
    serverState.remoteMcpServers.data.remoteMcpServers = [
      { id: "remote-server-1", url: "https://example.com/mcp" },
    ];
    const onComplete = vi.fn();
    const onSkip = vi.fn();
    renderStep(
      () => void onComplete(),
      () => void onSkip(),
    );

    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(onComplete).toHaveBeenCalledOnce();
    expect(onSkip).not.toHaveBeenCalled();
  });
});
