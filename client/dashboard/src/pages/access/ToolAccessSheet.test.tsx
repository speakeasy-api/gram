import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ServerWithProject } from "./mcpAccessModel";
import { ToolAccessSheet } from "./ToolAccessSheet";

const live = vi.hoisted(() => ({
  tools: undefined as
    | Record<string, { annotations?: { readOnlyHint?: boolean } }>
    | undefined,
  isError: false,
  refetch: vi.fn(),
}));
const tunnel = vi.hoisted(() => ({
  offline: false,
  refetch: vi.fn(),
  lastSourceId: undefined as string | undefined,
}));

vi.mock("@/pages/mcp/x/tabs/useTunnelAgentStatus", () => ({
  useTunnelAgentStatus: (options: { tunneledSourceId?: string }) => {
    tunnel.lastSourceId = options.tunneledSourceId;
    return { offline: tunnel.offline, refetch: tunnel.refetch };
  },
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  useGetMcpServer: () => ({
    data: {
      id: "tunneled",
      userSessionIssuerId: "issuer",
      visibility: "private",
    },
    isLoading: false,
    isError: false,
    refetch: () => {},
  }),
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: () => ({
    data: { mcpEndpoints: [{ slug: "jamf" }] },
    isLoading: false,
    isError: false,
    refetch: () => {},
  }),
}));
vi.mock("@/pages/mcp/x/tabs/useRemoteMcpToolConnection", () => ({
  useRemoteMcpToolConnection: (options: { enabled?: boolean }) => ({
    tools: options.enabled ? live.tools : undefined,
    metadataByTool: {},
    loading: !options.enabled,
    needsAuth: false,
    isError: !!options.enabled && live.isError,
    isIssuerGated: true,
    refetch: live.refetch,
    connect: () => {},
    sync: () => {},
    isSyncing: false,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [{ id: "p-default", slug: "default", name: "Default" }],
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ mcp: { x: { inspect: { href: () => "/inspect" } } } }),
}));
vi.mock("@/hooks/useToolMetadata", () => ({
  useToolMetadata: () => ({
    metadataByTool: {},
    isLoading: false,
    isError: false,
    refetch: () => {},
  }),
}));

const entry: ServerWithProject = {
  projectId: "p-default",
  projectName: "Default",
  server: {
    id: "tunneled",
    name: "JAMF",
    slug: "jamf",
    tools: [],
    dynamicTools: true,
    storedToolInventory: true,
    tunneledSourceId: "tunnel-1",
  },
};

function renderSheet() {
  render(
    <MemoryRouter>
      <ToolAccessSheet
        target={{ kind: "server", entry }}
        limit={{ kind: "tools", tools: ["list_devices"] }}
        initialTab="tools"
        onChange={() => {}}
        onClose={() => {}}
      />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  live.tools = undefined;
  live.isError = false;
  live.refetch.mockReset();
  tunnel.offline = false;
  tunnel.refetch.mockReset();
  tunnel.lastSourceId = undefined;
});

afterEach(cleanup);

describe("ToolAccessSheet on a tunneled server", () => {
  it("says the agent is offline and retries both reads", () => {
    live.isError = true;
    tunnel.offline = true;
    renderSheet();
    expect(tunnel.lastSourceId).toBe("tunnel-1");
    expect(
      screen.getByText("Tunnel offline — connect the agent to list its tools"),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(tunnel.refetch).toHaveBeenCalled();
    expect(live.refetch).toHaveBeenCalled();
  });

  it("lists the tools once its agent is connected", () => {
    live.tools = { list_devices: { annotations: { readOnlyHint: true } } };
    renderSheet();
    expect(screen.queryByText(/Tunnel offline/)).toBeNull();
    expect(screen.getByText("1 of 1 tools selected")).toBeTruthy();
  });
});
