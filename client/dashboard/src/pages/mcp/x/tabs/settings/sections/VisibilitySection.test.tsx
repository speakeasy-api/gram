import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { VisibilitySection } from "./VisibilitySection";

const rbac = vi.hoisted(() => ({ canWrite: true }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => rbac.canWrite,
    hasAnyScope: () => rbac.canWrite,
    hasAllScopes: () => rbac.canWrite,
    isLoading: false,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useUser: () => ({ id: "user-1" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({}),
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));
vi.mock("@/pages/mcp/x/useFollowRenamedServer", () => ({
  useFollowRenamedServer: () => vi.fn(),
}));

const updateServer = vi.hoisted(() => vi.fn());
vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: () => ({
    isPending: false,
    mutate: updateServer,
  }),
}));
const latestServer = vi.hoisted(() => ({ current: undefined as unknown }));
vi.mock("@gram/client/react-query/getMcpServer.js", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@gram/client/react-query/getMcpServer.js")
  >()),
  buildGetMcpServerQuery: (_client: unknown, request: { id: string }) => ({
    queryKey: ["test-get-mcp-server", request.id],
    queryFn: async () => latestServer.current,
  }),
}));
const tunnelSource = vi.hoisted(() => ({ allowPublic: false }));
vi.mock("@gram/client/react-query/getTunneledMcpServer.js", () => ({
  useGetTunneledMcpServer: (
    _request: unknown,
    _security: unknown,
    options?: { enabled?: boolean },
  ) => ({
    data: options?.enabled
      ? { id: "tunnel-1", allowPublic: tunnelSource.allowPublic }
      : undefined,
  }),
}));
const hostedRequest = vi.hoisted(() => vi.fn());
vi.mock("@/pages/mcp/x/hosted/useHostedVisibilityGuard", () => ({
  useHostedVisibilityGuard: () => ({
    request: hostedRequest,
    publicUnavailable: false,
    dialogs: null,
  }),
}));
vi.mock("@gram/client/react-query/mcpServerUsageImpact.js", () => ({
  useMcpServerUsageImpact: (
    _request: unknown,
    _security: unknown,
    options?: { enabled?: boolean },
  ) =>
    options?.enabled === false
      ? { data: undefined, isPending: true, isError: false }
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

const base = {
  id: "server-1",
  projectId: "project-1",
  name: "Example server",
  networkAccessMode: "public_only",
  visibility: "private",
  createdAt: new Date(0),
  updatedAt: new Date(0),
};

function renderSection(mcpServer: McpServer): void {
  latestServer.current = mcpServer;
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <VisibilitySection mcpServer={mcpServer} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  updateServer.mockReset();
  hostedRequest.mockReset();
  rbac.canWrite = true;
  tunnelSource.allowPublic = false;
});

describe("VisibilitySection", () => {
  it("switches a remote server on and off, and waits for Save", () => {
    renderSection({ ...base, remoteMcpServerId: "remote-1" } as McpServer);

    expect(screen.getByRole("heading", { name: "Availability" })).toBeTruthy();
    expect(screen.getAllByRole("radio").map((r) => r.id)).toEqual([
      "mcp-visibility-private",
      "mcp-visibility-disabled",
    ]);
    const save = screen.getByRole("button", { name: /Save/ });
    expect(save).toHaveProperty("disabled", true);

    fireEvent.click(screen.getByRole("radio", { name: /Disabled/ }));
    expect(save).toHaveProperty("disabled", false);
    expect(updateServer).not.toHaveBeenCalled();
  });

  it("confirms with the server name before disabling", async () => {
    renderSection({ ...base, remoteMcpServerId: "remote-1" } as McpServer);

    fireEvent.click(screen.getByRole("radio", { name: /Disabled/ }));
    fireEvent.click(screen.getByRole("button", { name: /Save/ }));
    expect(screen.getByText(/1 active connection from 1 caller/)).toBeTruthy();
    const confirm = screen.getByRole("button", { name: "Continue" });
    expect(confirm).toHaveProperty("disabled", true);

    fireEvent.change(screen.getByLabelText("Type the server name to confirm"), {
      target: { value: "Example server" },
    });
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(updateServer).toHaveBeenCalledWith({
        request: {
          updateMcpServerForm: expect.objectContaining({
            id: "server-1",
            visibility: "disabled",
          }),
        },
      }),
    );
  });

  it("offers Public on a tunneled server only once the source allows it", () => {
    renderSection({ ...base, tunneledMcpServerId: "tunnel-1" } as McpServer);

    expect(screen.getByRole("heading", { name: "Visibility" })).toBeTruthy();
    expect(screen.getByRole("radio", { name: /Public/ })).toHaveProperty(
      "disabled",
      true,
    );
    expect(
      screen
        .getByRole("link", { name: "Allow public access below" })
        .getAttribute("href"),
    ).toBe("/#public-access");
  });

  it("makes a tunneled server public without a confirmation", async () => {
    tunnelSource.allowPublic = true;
    renderSection({ ...base, tunneledMcpServerId: "tunnel-1" } as McpServer);

    fireEvent.click(screen.getByRole("radio", { name: /Public/ }));
    fireEvent.click(screen.getByRole("button", { name: /Save/ }));
    await waitFor(() =>
      expect(updateServer).toHaveBeenCalledWith({
        request: {
          updateMcpServerForm: expect.objectContaining({
            visibility: "public",
          }),
        },
      }),
    );
  });

  it("hands a hosted server's change to the toolset confirmations", () => {
    renderSection({
      ...base,
      id: "hosted-1",
      toolsetId: "hosted-1",
    } as McpServer);

    fireEvent.click(screen.getByRole("radio", { name: /Disabled/ }));
    fireEvent.click(screen.getByRole("button", { name: /Save/ }));
    expect(hostedRequest).toHaveBeenCalledWith("disabled");
    expect(updateServer).not.toHaveBeenCalled();
  });

  it("shows the state without controls to people who cannot edit", () => {
    rbac.canWrite = false;
    renderSection({ ...base, remoteMcpServerId: "remote-1" } as McpServer);

    expect(screen.queryAllByRole("radio")).toHaveLength(0);
    expect(screen.queryByRole("button", { name: /Save/ })).toBeNull();
    expect(screen.getByText("Enabled")).toBeTruthy();
    expect(
      screen.getByText("Only people who can edit MCP servers can change this."),
    ).toBeTruthy();
  });
});
