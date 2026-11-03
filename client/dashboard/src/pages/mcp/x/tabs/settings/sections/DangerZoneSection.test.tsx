import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
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
vi.mock("@/contexts/Auth", () => ({
  useIsSpeakeasyStaff: () => false,
  useUser: () => ({ id: "user-1" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({}),
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
}));
const deleteServer = vi.hoisted(() => vi.fn());
const updateServer = vi.hoisted(() => vi.fn());
vi.mock("@gram/client/react-query/mcpServerUsageImpact.js", () => ({
  useMcpServerUsageImpact: (
    _request: unknown,
    _security: unknown,
    options?: { enabled?: boolean },
  ) =>
    options?.enabled === false
      ? {
          data: undefined,
          isFetching: false,
          isPending: true,
          isError: false,
          fetchStatus: "idle",
        }
      : {
          data: {
            serverName: "Example server",
            signInRequired: true,
            sharedSignIn: false,
            activeConnectionCount: 1,
            activeSubjectCount: 1,
            sharedConnectionCount: 0,
            subjects: [
              {
                subjectUrn: "user:alice",
                subjectType: "user",
                displayName: "Alice",
                connectionCount: 1,
                clientNames: [],
                sharedSignIn: false,
              },
            ],
            subjectsTruncated: false,
          },
          isFetching: false,
          isPending: false,
          isError: false,
          fetchStatus: "idle",
        },
}));
vi.mock("@gram/client/react-query/deleteMcpServer.js", () => ({
  useDeleteMcpServerMutation: () => ({
    isPending: false,
    mutate: deleteServer,
  }),
}));
vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: () => ({
    isPending: false,
    mutate: updateServer,
  }),
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
  deleteServer.mockReset();
  updateServer.mockReset();
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
  it("offers no server-row delete for a hosted server without its toolset", () => {
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

describe("DangerZoneSection usage confirmation", () => {
  function typeServerName(): void {
    fireEvent.change(screen.getByLabelText("Type the server name to confirm"), {
      target: { value: "Example server" },
    });
  }

  it("shows who is connected and waits for the server name before deleting", () => {
    renderSection({ ...base, id: "server-1" } as McpServer);

    fireEvent.click(screen.getByRole("button", { name: /Delete MCP server/ }));
    expect(screen.getByText("Alice")).toBeTruthy();
    const confirm = screen.getByRole("button", { name: "Delete MCP server" });
    expect(confirm).toHaveProperty("disabled", true);

    typeServerName();
    fireEvent.click(confirm);
    expect(deleteServer).toHaveBeenCalledWith({ request: { id: "server-1" } });
  });

  it("waits for the server name before disabling", () => {
    renderSection({
      ...base,
      id: "server-1",
      visibility: "private",
    } as McpServer);

    fireEvent.click(screen.getByRole("switch", { name: "Enable MCP server" }));
    expect(screen.getByText(/1 active connection from 1 user/)).toBeTruthy();
    const confirm = screen.getByRole("button", { name: "Continue" });
    expect(confirm).toHaveProperty("disabled", true);

    typeServerName();
    expect(confirm).toHaveProperty("disabled", false);
  });
});
