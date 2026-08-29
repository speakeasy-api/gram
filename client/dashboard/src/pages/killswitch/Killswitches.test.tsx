import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import Killswitches from "./Killswitches";
import { KillswitchesRoot } from "./KillswitchesRoot";

const access = vi.hoisted(() => ({
  value: { canAccess: false, isLoading: false, reason: "scope" } as {
    canAccess: boolean;
    isLoading: boolean;
    reason:
      | "allowed"
      | "loading"
      | "override"
      | "rollout"
      | "scope"
      | "support";
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
  membersOptions: undefined as unknown,
  capabilityOptions: undefined as unknown,
  serverOptions: undefined as unknown,
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
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: (...args: unknown[]) => {
    dependencies.membersOptions = args[2];
    return {
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
    };
  },
}));
vi.mock("@gram/client/react-query/killswitches.js", () => ({
  useKillswitchesInfinite: listHook,
  invalidateAllKillswitches: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@gram/client/react-query/killswitchCapabilities.js", () => ({
  useKillswitchCapabilities: (...args: unknown[]) => {
    dependencies.capabilityOptions = args[2];
    return {
      data: { capabilities: [], comingSoon: [] },
      isLoading: dependencies.capabilitiesLoading,
      error: dependencies.capabilitiesError,
      refetch: dependencies.refetchCapabilities,
    };
  },
}));
vi.mock("@gram/client/react-query/killswitchMCPServers.js", () => ({
  useKillswitchMCPServers: (...args: unknown[]) => {
    dependencies.serverOptions = args[2];
    return {
      data: { servers: [] },
      isLoading: false,
      error: dependencies.serversError,
      refetch: dependencies.refetchServers,
    };
  },
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
  dependencies.membersOptions = undefined;
  dependencies.capabilityOptions = undefined;
  dependencies.serverOptions = undefined;
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
    expect(listHook.mock.calls.at(-1)?.[2]).toEqual({
      initialPageParam: undefined,
      throwOnError: false,
    });
    expect(dependencies.membersOptions).toEqual({ throwOnError: false });
    expect(dependencies.serverOptions).toEqual({ throwOnError: false });
    expect(dependencies.capabilityOptions).toEqual({
      enabled: false,
      throwOnError: false,
    });

    await userEvent.click(
      screen.getByRole("button", { name: "New killswitch" }),
    );
    expect(dependencies.capabilityOptions).toEqual({
      enabled: true,
      throwOnError: false,
    });
    expect(dependencies.editorProps).toMatchObject({
      open: true,
      capabilitiesLoading: false,
      capabilitiesError: dependencies.capabilitiesError,
    });
    const retry = dependencies.editorProps?.onRetryCapabilities as () => void;
    retry();
    expect(dependencies.refetchCapabilities).toHaveBeenCalledTimes(1);
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
    await userEvent.click(
      screen.getByRole("button", { name: "New killswitch" }),
    );
    await waitFor(() => expect(dependencies.editorProps).toBeDefined());
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
