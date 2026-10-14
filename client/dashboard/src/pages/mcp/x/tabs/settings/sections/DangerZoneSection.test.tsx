import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DangerZoneSection } from "./DangerZoneSection";

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
  }),
}));
vi.mock("@/contexts/Auth", () => ({ useIsSpeakeasyStaff: () => false }));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@/routes", () => ({ useRoutes: () => ({}) }));
vi.mock("@gram/client/react-query/deleteMcpServer.js", () => ({
  useDeleteMcpServerMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  invalidateAllMcpServers: vi.fn(),
  useMcpServers: () => ({ data: undefined, isSuccess: false }),
}));

const base = {
  projectId: "project-1",
  name: "Example server",
  networkAccessMode: "public_only",
  visibility: "disabled",
  createdAt: new Date(0),
  updatedAt: new Date(0),
};

function renderSection(mcpServer: McpServer): void {
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <DangerZoneSection mcpServer={mcpServer} endpoints={[]} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
});

describe("DangerZoneSection availability", () => {
  it("keeps a hosted server's availability switch read-only", () => {
    renderSection({
      ...base,
      id: "hosted-1",
      toolsetId: "hosted-1",
    } as McpServer);

    expect(
      screen.getByRole("switch", { name: "Enable MCP server" }),
    ).toHaveProperty("disabled", true);
  });

  it("leaves other servers' availability switch editable", () => {
    renderSection({
      ...base,
      id: "server-1",
      unproxiedMcpServerId: "unproxied-1",
    } as McpServer);

    expect(
      screen.getByRole("switch", { name: "Enable MCP server" }),
    ).toHaveProperty("disabled", false);
  });
});

describe("DangerZoneSection delete", () => {
  it("offers no delete for a hosted server", () => {
    renderSection({
      ...base,
      id: "hosted-1",
      toolsetId: "hosted-1",
    } as McpServer);

    expect(
      screen.queryByRole("button", { name: /Delete MCP server/ }),
    ).toBeNull();
  });

  it("keeps delete for other servers", () => {
    renderSection({ ...base, id: "server-1" } as McpServer);

    expect(
      screen.getByRole("button", { name: /Delete MCP server/ }),
    ).toBeTruthy();
  });

  it("keeps delete for toolset-backed servers with a different id", () => {
    renderSection({
      ...base,
      id: "server-2",
      toolsetId: "toolset-1",
    } as McpServer);

    expect(
      screen.getByRole("button", { name: /Delete MCP server/ }),
    ).toBeTruthy();
  });
});
