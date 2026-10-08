import { TooltipProvider } from "@/components/ui/Tooltip";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DangerZoneSection } from "./DangerZoneSection";

// The resources the caller holds mcp:write on.
const grants = vi.hoisted(() => ({ writable: new Set<string>() }));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: (_scopes: unknown, resourceId?: string) =>
      resourceId !== undefined && grants.writable.has(resourceId),
    hasAllScopes: (_scopes: unknown, resourceId?: string) =>
      resourceId !== undefined && grants.writable.has(resourceId),
    isLoading: false,
  }),
}));
vi.mock("@/contexts/Auth", () => ({ useIsSpeakeasyStaff: () => false }));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ mcp: { href: () => "/mcp" } }),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({
    data: { mcpServers: [] },
    isSuccess: true,
    isFetching: false,
    isError: false,
  }),
  invalidateAllMcpServers: vi.fn(),
}));
vi.mock("@gram/client/react-query/deleteMcpServer.js", () => ({
  useDeleteMcpServerMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
  }),
}));
vi.mock("./DeleteTunnelDialogContent", () => ({
  DeleteTunnelDialogContent: () => <p>Tunnel delete dialog</p>,
}));

const mcpServer = {
  id: "server-a",
  projectId: "project-1",
  name: "JAMF prod",
  visibility: "private",
  tunneledMcpServerId: "tunnel-1",
} as McpServer;

const tunnel = {
  id: "tunnel-1",
  projectId: "project-1",
  name: "JAMF",
} as TunneledMcpServer;

function renderSection(deleteTarget?: {
  kind: "tunneled";
  source: TunneledMcpServer;
}) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <TooltipProvider>
          <DangerZoneSection
            mcpServer={mcpServer}
            endpoints={[]}
            deleteTarget={deleteTarget}
          />
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  grants.writable = new Set();
});

afterEach(cleanup);

describe("DangerZoneSection on a tunneled server", () => {
  it("offers deleting the server alone and deleting the tunnel separately", () => {
    grants.writable = new Set(["server-a", "project-1"]);
    renderSection({ kind: "tunneled", source: tunnel });

    expect(screen.getByText("Delete this MCP server")).toBeTruthy();
    expect(screen.getByText("Delete tunnel and its MCP servers")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Delete tunnel" }));
    expect(screen.getByText("Tunnel delete dialog")).toBeTruthy();
  });

  it("deletes only this server from the server-only action", () => {
    grants.writable = new Set(["server-a", "project-1"]);
    renderSection({ kind: "tunneled", source: tunnel });

    fireEvent.click(screen.getByRole("button", { name: "Delete MCP server" }));
    expect(screen.getByText("Delete this MCP server?")).toBeTruthy();
    expect(
      screen.getByText(/The tunnel and any other MCP servers on it are not/),
    ).toBeTruthy();
    expect(screen.queryByText("Tunnel delete dialog")).toBeNull();
  });

  it("lets a writer of this server alone delete it but not the tunnel", () => {
    grants.writable = new Set(["server-a"]);
    renderSection({ kind: "tunneled", source: tunnel });

    fireEvent.click(screen.getByRole("button", { name: "Delete tunnel" }));
    expect(screen.queryByText("Tunnel delete dialog")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Delete MCP server" }));
    expect(screen.getByText("Delete this MCP server?")).toBeTruthy();
  });

  it("keeps the server-only delete when the tunnel cannot be read", () => {
    grants.writable = new Set(["server-a"]);
    renderSection(undefined);

    expect(screen.queryByText("Delete tunnel and its MCP servers")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Delete MCP server" }));
    expect(screen.getByText("Delete this MCP server?")).toBeTruthy();
  });
});
