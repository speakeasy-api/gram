import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DistributeServersStep } from "./distribute-servers-step";

const serverState = vi.hoisted(() => ({
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
    onSkip,
    skipLabel,
  }: {
    onSkip: () => void;
    skipLabel: string;
  }) => <button onClick={onSkip}>{skipLabel}</button>,
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ catalog: { Link: () => null } }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));
vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: () => ({
    data: { servers: [] },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@/pages/catalog/useRemoteMcpInstallWorkflow", () => ({
  useRemoteMcpInstallWorkflow: () => ({
    phase: "configure",
    canInstall: false,
    statuses: [],
    startInstall: vi.fn(),
    reset: vi.fn(),
  }),
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
    renderStep(onComplete, onSkip);

    fireEvent.click(screen.getByRole("button", { name: "Skip for now" }));

    expect(onSkip).toHaveBeenCalledOnce();
    expect(onComplete).not.toHaveBeenCalled();
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
    renderStep(onComplete, onSkip);

    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(onComplete).toHaveBeenCalledOnce();
    expect(onSkip).not.toHaveBeenCalled();
  });
});
