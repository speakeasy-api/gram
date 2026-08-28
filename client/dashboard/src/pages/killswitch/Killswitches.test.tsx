import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router";
import Killswitches, { KillswitchesRoot } from "./Killswitches";

const access = vi.hoisted(() => ({
  value: { canAccess: false, isLoading: false, reason: "scope" } as {
    canAccess: boolean;
    isLoading: boolean;
    reason: "allowed" | "loading" | "rollout" | "scope" | "support";
  },
}));
vi.mock("@/hooks/useKillswitchAccess", () => ({
  useKillswitchAccess: () => access.value,
}));

const listHook = vi.hoisted(() => vi.fn());
const dependencies = vi.hoisted(() => ({
  membersError: undefined as Error | undefined,
  serversError: undefined as Error | undefined,
  capabilitiesError: undefined as Error | undefined,
  capabilitiesLoading: false,
  refetchMembers: vi.fn(),
  refetchServers: vi.fn(),
  refetchCapabilities: vi.fn(),
  create: vi.fn(),
  editorProps: undefined as Record<string, unknown> | undefined,
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session" }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    killswitch: {
      href: () => "/acme/killswitch",
      detail: { href: (id: string) => `/acme/killswitch/${id}`, goTo: vi.fn() },
    },
    mcpSessions: { href: () => "/acme/mcp-sessions" },
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: {
      members: [
        {
          id: "user-1",
          name: "Alex Morgan",
          email: "alex@example.test",
          joinedAt: new Date(),
          principalUrn: "user:user-1",
          roleIds: [],
        },
      ],
    },
    isLoading: false,
    error: dependencies.membersError,
    refetch: dependencies.refetchMembers,
  }),
}));
vi.mock("@gram/client/react-query/killswitches.js", () => ({
  useKillswitchesInfinite: listHook,
  invalidateAllKillswitches: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@gram/client/react-query/killswitchCapabilities.js", () => ({
  useKillswitchCapabilities: () => ({
    data: {
      capabilities: [{ key: "mcp_tool_calls", label: "MCP tool calls" }],
      comingSoon: [],
    },
    isLoading: dependencies.capabilitiesLoading,
    error: dependencies.capabilitiesError,
    refetch: dependencies.refetchCapabilities,
  }),
}));
vi.mock("@gram/client/react-query/killswitchMCPServers.js", () => ({
  useKillswitchMCPServers: () => ({
    data: {
      servers: [{ id: "server-1", name: "Server 1", projectId: "project-1" }],
    },
    isLoading: false,
    error: dependencies.serversError,
    refetch: dependencies.refetchServers,
  }),
}));
vi.mock("@gram/client/react-query/createKillswitch.js", () => ({
  useCreateKillswitchMutation: () => ({ mutateAsync: dependencies.create }),
}));
vi.mock("@gram/client/react-query/previewKillswitchOverlaps.js", () => ({
  usePreviewKillswitchOverlapsMutation: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("./KillswitchEditorSheet", () => ({
  KillswitchEditorSheet: (props: Record<string, unknown>) => {
    dependencies.editorProps = props;
    return null;
  },
}));
vi.mock("@/components/FeatureRequestModal", () => ({
  FeatureRequestModal: () => null,
}));

function NavigationProbe(): JSX.Element {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output data-testid="location">{location.search}</output>
      <button onClick={() => void navigate(-1)}>Back</button>
      <button onClick={() => void navigate(1)}>Forward</button>
    </>
  );
}

afterEach(() => {
  cleanup();
  listHook.mockReset();
  dependencies.membersError = undefined;
  dependencies.serversError = undefined;
  dependencies.capabilitiesError = undefined;
  dependencies.capabilitiesLoading = false;
  dependencies.refetchMembers.mockReset().mockResolvedValue(undefined);
  dependencies.refetchServers.mockReset().mockResolvedValue(undefined);
  dependencies.refetchCapabilities.mockReset().mockResolvedValue(undefined);
  dependencies.create
    .mockReset()
    .mockResolvedValue({ id: "ks-1", version: 1, replayed: false });
  dependencies.editorProps = undefined;
});

describe("KillswitchesRoot", () => {
  it("gates a direct route before mounting Killswitch data children", () => {
    const child = vi.fn(() => <div>private Killswitch data</div>);
    access.value = { canAccess: false, isLoading: false, reason: "scope" };
    render(
      <MemoryRouter initialEntries={["/acme/killswitch"]}>
        <Routes>
          <Route path=":orgSlug/killswitch" element={<KillswitchesRoot />}>
            <Route index Component={child} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );
    expect(child).not.toHaveBeenCalled();
    expect(screen.getByText("Killswitch is not available")).not.toBeNull();
  });

  it("mounts the protected child only after access is authoritative", () => {
    const child = vi.fn(() => <div>private Killswitch data</div>);
    access.value = { canAccess: true, isLoading: false, reason: "allowed" };
    render(
      <MemoryRouter initialEntries={["/acme/killswitch"]}>
        <Routes>
          <Route path=":orgSlug/killswitch" element={<KillswitchesRoot />}>
            <Route index Component={child} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );
    expect(child).toHaveBeenCalledTimes(1);
    expect(screen.getByText("private Killswitch data")).not.toBeNull();
  });
});

describe("Killswitches list", () => {
  it("retries every list dependency that can make rows inaccurate", async () => {
    const refetchList = vi.fn().mockResolvedValue(undefined);
    dependencies.membersError = new Error("members unavailable");
    listHook.mockReturnValue({
      data: { pages: [{ result: { items: [] } }] },
      error: null,
      isLoading: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      refetch: refetchList,
      fetchNextPage: vi.fn(),
    });
    render(
      <MemoryRouter initialEntries={["/acme/killswitch"]}>
        <Routes>
          <Route path=":orgSlug/killswitch" element={<Killswitches />} />
        </Routes>
      </MemoryRouter>,
    );
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(refetchList).toHaveBeenCalledTimes(1);
    expect(dependencies.refetchMembers).toHaveBeenCalledTimes(1);
    expect(dependencies.refetchServers).toHaveBeenCalledTimes(1);
  });

  it("keeps capability readiness scoped to the editor", async () => {
    dependencies.capabilitiesError = new Error("capabilities unavailable");
    listHook.mockReturnValue({
      data: { pages: [{ result: { items: [] } }] },
      error: null,
      isLoading: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      refetch: vi.fn(),
      fetchNextPage: vi.fn(),
    });
    render(
      <MemoryRouter initialEntries={["/acme/killswitch"]}>
        <Routes>
          <Route path=":orgSlug/killswitch" element={<Killswitches />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(
      screen.getByText("No Killswitches match these filters"),
    ).not.toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "New killswitch" }),
    );
    expect(
      screen.getByText("Unable to load the Killswitch editor"),
    ).not.toBeNull();
  });

  it("fails closed for contextual catalog errors and retries every failed dependency", async () => {
    dependencies.membersError = new Error("members unavailable");
    dependencies.serversError = new Error("servers unavailable");
    dependencies.capabilitiesError = new Error("capabilities unavailable");
    listHook.mockReturnValue({
      data: { pages: [{ result: { items: [] } }] },
      error: null,
      isLoading: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      refetch: vi.fn(),
      fetchNextPage: vi.fn(),
    });
    render(
      <MemoryRouter
        initialEntries={[
          "/acme/killswitch?create=1&createUser=user-1&createCapability=mcp_tool_calls&originServer=server-1",
        ]}
      >
        <Routes>
          <Route path=":orgSlug/killswitch" element={<Killswitches />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(dependencies.editorProps).toBeUndefined();
    expect(
      screen.getByText("Unable to load the Killswitch editor"),
    ).not.toBeNull();
    const retryButtons = screen.getAllByRole("button", { name: "Try again" });
    await userEvent.click(retryButtons.at(-1)!);
    expect(dependencies.refetchMembers).toHaveBeenCalledTimes(1);
    expect(dependencies.refetchServers).toHaveBeenCalledTimes(1);
    expect(dependencies.refetchCapabilities).toHaveBeenCalledTimes(1);
  });

  it("uses the create URL as editor state across open, close, back, and forward", async () => {
    listHook.mockReturnValue({
      data: { pages: [{ result: { items: [] } }] },
      error: null,
      isLoading: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      refetch: vi.fn(),
      fetchNextPage: vi.fn(),
    });
    render(
      <MemoryRouter initialEntries={["/acme/killswitch?status=active"]}>
        <Routes>
          <Route
            path=":orgSlug/killswitch"
            element={
              <>
                <Killswitches />
                <NavigationProbe />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    );

    await userEvent.click(
      screen.getByRole("button", { name: "New killswitch" }),
    );
    expect(screen.getByTestId("location").textContent).toBe(
      "?status=active&create=1",
    );
    expect(dependencies.editorProps).toMatchObject({ open: true });

    await userEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByTestId("location").textContent).toBe("?status=active");
    expect(dependencies.editorProps).toMatchObject({ open: false });

    await userEvent.click(screen.getByRole("button", { name: "Forward" }));
    expect(screen.getByTestId("location").textContent).toBe(
      "?status=active&create=1",
    );
    const onOpenChange = dependencies.editorProps?.onOpenChange as (
      open: boolean,
    ) => void;
    act(() => onOpenChange(false));
    await waitFor(() =>
      expect(screen.getByTestId("location").textContent).toBe("?status=active"),
    );

    await userEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByTestId("location").textContent).toBe("?status=active");
  });

  it("passes raw notes to the create API", async () => {
    listHook.mockReturnValue({
      data: { pages: [{ result: { items: [] } }] },
      error: null,
      isLoading: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      refetch: vi.fn(),
      fetchNextPage: vi.fn(),
    });
    render(
      <MemoryRouter initialEntries={["/acme/killswitch"]}>
        <Routes>
          <Route path=":orgSlug/killswitch" element={<Killswitches />} />
        </Routes>
      </MemoryRouter>,
    );
    const onSubmit = dependencies.editorProps?.onSubmit as (
      draft: Record<string, unknown>,
      operationId: string,
    ) => Promise<unknown>;
    await onSubmit(
      {
        userId: "user-1",
        scopeType: "all_servers",
        serverIds: [],
        startType: "now",
        startsAt: "",
        endType: "until_lifted",
        endsAt: "",
        externalNote: "  Public 🙂 note  ",
        internalNote: "  Internal note  ",
      },
      "create-op",
    );
    expect(dependencies.create).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          killswitchCreateRequest: expect.objectContaining({
            externalNote: "  Public 🙂 note  ",
            internalNote: "  Internal note  ",
          }),
        },
      }),
    );
  });

  it("validates reload-safe create context and drops a stale server hint", () => {
    listHook.mockReturnValue({
      data: { pages: [{ result: { items: [] } }] },
      error: null,
      isLoading: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      refetch: vi.fn(),
      fetchNextPage: vi.fn(),
    });
    render(
      <MemoryRouter
        initialEntries={[
          "/acme/killswitch?create=1&createUser=user-1&createCapability=mcp_tool_calls&originServer=deleted-server",
        ]}
      >
        <Routes>
          <Route path=":orgSlug/killswitch" element={<Killswitches />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(dependencies.editorProps).toMatchObject({
      open: true,
      createContext: {
        userId: "user-1",
        capabilityKey: "mcp_tool_calls",
        originatingMcpServerId: undefined,
      },
    });
    const mcpSessionsHref = dependencies.editorProps?.mcpSessionsHref as (
      userId: string,
    ) => string;
    expect(mcpSessionsHref("user-1")).toBe(
      "/acme/mcp-sessions?subjectUrn=user%3Auser-1",
    );
  });

  it("falls back to an unbound editor for a stale contextual member", () => {
    listHook.mockReturnValue({
      data: { pages: [{ result: { items: [] } }] },
      error: null,
      isLoading: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      refetch: vi.fn(),
      fetchNextPage: vi.fn(),
    });
    render(
      <MemoryRouter
        initialEntries={["/acme/killswitch?create=1&createUser=deleted-user"]}
      >
        <Routes>
          <Route path=":orgSlug/killswitch" element={<Killswitches />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(dependencies.editorProps).toMatchObject({
      open: true,
      createContext: undefined,
    });
  });

  it("preserves the principal filter in the URL and renders a dedicated empty state", () => {
    listHook.mockReturnValue({
      data: { pages: [{ result: { items: [] } }] },
      error: null,
      isLoading: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      refetch: vi.fn(),
      fetchNextPage: vi.fn(),
    });
    render(
      <MemoryRouter initialEntries={["/acme/killswitch?user=user-1"]}>
        <Routes>
          <Route path=":orgSlug/killswitch" element={<Killswitches />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(listHook.mock.calls[0]?.[1]).toMatchObject({ userId: "user-1" });
    expect(
      screen.getByText("No Killswitches match these filters"),
    ).not.toBeNull();
  });
});
