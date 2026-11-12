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
import { AvailabilitySection, VisibilitySection } from "./VisibilitySection";

const rbac = vi.hoisted(() => ({
  canWrite: true,
  // When set, mcp:write holds only on this resource.
  writableResource: undefined as string | undefined,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resourceId?: string) =>
      rbac.canWrite &&
      (scope !== "mcp:write" ||
        rbac.writableResource === undefined ||
        resourceId === rbac.writableResource),
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
const updateTunnel = vi.hoisted(() => vi.fn(async () => ({})));
vi.mock("@gram/client/react-query/updateTunneledMcpServer.js", () => ({
  useUpdateTunneledMcpServerMutation: () => ({ mutateAsync: updateTunnel }),
}));
vi.mock("./sourceInvalidation", () => ({
  invalidateTunneledMcpSourceViews: vi.fn(async () => {}),
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

function renderSection(
  mcpServer: McpServer,
  Section: typeof AvailabilitySection = AvailabilitySection,
): void {
  latestServer.current = mcpServer;
  render(
    <MemoryRouter initialEntries={["/mcp/example/settings"]}>
      <QueryClientProvider client={new QueryClient()}>
        <Section mcpServer={mcpServer} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  updateServer.mockReset();
  updateTunnel.mockClear();
  hostedRequest.mockReset();
  rbac.canWrite = true;
  rbac.writableResource = undefined;
  tunnelSource.allowPublic = false;
  vi.restoreAllMocks();
});

describe("AvailabilitySection and VisibilitySection", () => {
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

  it("puts every server kind's on and off under Availability", () => {
    for (const kind of [
      { remoteMcpServerId: "remote-1" },
      { tunneledMcpServerId: "tunnel-1" },
      { id: "hosted-1", toolsetId: "hosted-1" },
    ]) {
      renderSection({ ...base, ...kind } as McpServer);
      expect(
        screen.getByRole("heading", { name: "Availability" }),
      ).toBeTruthy();
      expect(screen.getAllByRole("radio").map((r) => r.id)).toEqual([
        "mcp-visibility-private",
        "mcp-visibility-disabled",
      ]);
      cleanup();
    }
  });

  it("offers only Private and Public under Visibility, and links Private to Team Access", () => {
    renderSection(
      { ...base, tunneledMcpServerId: "tunnel-1" } as McpServer,
      VisibilitySection,
    );

    expect(screen.getByRole("heading", { name: "Visibility" })).toBeTruthy();
    expect(screen.getAllByRole("radio").map((r) => r.id)).toEqual([
      "mcp-visibility-private",
      "mcp-visibility-public",
    ]);
    expect(
      screen
        .getByRole("link", { name: "Manage who has access" })
        .getAttribute("href"),
    ).toMatch(/\/mcp\/server-1\/team-access$/);
  });

  it("sends people to Availability while the server is off", () => {
    renderSection(
      {
        ...base,
        tunneledMcpServerId: "tunnel-1",
        visibility: "disabled",
      } as McpServer,
      VisibilitySection,
    );

    expect(screen.queryAllByRole("radio")).toHaveLength(0);
    expect(screen.queryByRole("button", { name: /Save/ })).toBeNull();
    expect(
      screen
        .getByRole("link", { name: "Turn it on under Availability" })
        .getAttribute("href"),
    ).toBe("/mcp/example/settings#settings-general");
  });

  it("makes a tunneled server public in one Save, warning inline first", async () => {
    renderSection(
      { ...base, tunneledMcpServerId: "tunnel-1" } as McpServer,
      VisibilitySection,
    );

    expect(screen.queryByText(/Anyone on the internet/)).toBeNull();
    fireEvent.click(screen.getByRole("radio", { name: /Public/ }));
    expect(screen.getByText(/Anyone on the internet/)).toBeTruthy();

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
    // The tunnel source's consent goes with the same Save.
    expect(updateTunnel).toHaveBeenCalledWith({
      request: {
        updateTunneledMcpServerForm: { id: "tunnel-1", allowPublic: true },
      },
    });
  });

  it("skips the consent write when the tunnel already allows public", async () => {
    tunnelSource.allowPublic = true;
    renderSection(
      { ...base, tunneledMcpServerId: "tunnel-1" } as McpServer,
      VisibilitySection,
    );

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
    expect(updateTunnel).not.toHaveBeenCalled();
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
    cleanup();

    renderSection(
      { ...base, id: "hosted-1", toolsetId: "hosted-1" } as McpServer,
      VisibilitySection,
    );
    fireEvent.click(screen.getByRole("radio", { name: /Public/ }));
    fireEvent.click(screen.getByRole("button", { name: /Save/ }));
    expect(hostedRequest).toHaveBeenCalledWith("public");
    expect(updateServer).not.toHaveBeenCalled();
  });

  it("checks edit access on this server, and on a hosted server's toolset", () => {
    rbac.writableResource = "other-server";
    renderSection({ ...base, remoteMcpServerId: "remote-1" } as McpServer);
    expect(screen.queryAllByRole("radio")).toHaveLength(0);
    cleanup();

    rbac.writableResource = "toolset-1";
    renderSection({
      ...base,
      id: "hosted-1",
      toolsetId: "toolset-1",
    } as McpServer);
    expect(screen.getAllByRole("radio").length).toBeGreaterThan(0);
  });

  it("refreshes the page when the server already has the chosen state", async () => {
    const invalidate = vi.spyOn(QueryClient.prototype, "invalidateQueries");
    renderSection({ ...base, tunneledMcpServerId: "tunnel-1" } as McpServer);
    // Someone else disabled it since this page loaded.
    latestServer.current = { ...base, visibility: "disabled" };

    fireEvent.click(screen.getByRole("radio", { name: /Disabled/ }));
    fireEvent.click(screen.getByRole("button", { name: /Save/ }));
    fireEvent.change(screen.getByLabelText("Type the server name to confirm"), {
      target: { value: "Example server" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => expect(invalidate).toHaveBeenCalled());
    expect(updateServer).not.toHaveBeenCalled();
  });

  it("shows an unproxied server's availability as read-only", () => {
    renderSection({
      ...base,
      unproxiedMcpServerId: "unproxied-1",
    } as McpServer);

    expect(screen.getByRole("heading", { name: "Availability" })).toBeTruthy();
    expect(screen.queryAllByRole("radio")).toHaveLength(0);
    expect(screen.queryByRole("button", { name: /Save/ })).toBeNull();
    expect(
      screen.getByText(/Speakeasy can't turn this server off/),
    ).toBeTruthy();
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
