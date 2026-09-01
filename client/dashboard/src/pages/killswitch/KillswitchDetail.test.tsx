import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createMemoryRouter,
  MemoryRouter,
  Route,
  RouterProvider,
  Routes,
} from "react-router";
import type { KillswitchDetail as Detail } from "@gram/client/models/components/killswitchdetail.js";
import KillswitchDetail from "./KillswitchDetail";

const mocks = vi.hoisted(() => ({
  detail: undefined as Detail | undefined,
  detailError: undefined as Error | undefined,
  preview: vi.fn(),
  catalogLoading: false,
  catalogDataAvailable: true,
  catalogError: undefined as Error | undefined,
  capabilitiesError: undefined as Error | undefined,
  refetchCatalog: vi.fn(),
  refetchDetail: vi.fn(),
  edit: vi.fn(),
  lift: vi.fn(),
  detailRequest: undefined as unknown,
  detailOptions: undefined as unknown,
  membersRequest: undefined as unknown,
  membersOptions: undefined as unknown,
  capabilityRequest: undefined as unknown,
  capabilityOptions: undefined as unknown,
  serverRequest: undefined as unknown,
  serverOptions: undefined as unknown,
  editorProps: undefined as Record<string, unknown> | undefined,
  liftProps: undefined as Record<string, unknown> | undefined,
  renderRealLift: false,
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session" }),
  useIsPlatformAdmin: () => false,
}));

// The member and actor names are IdentityLinks now. Their href resolves
// through useRBAC, which reaches for the SDK this suite does not provide.
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    isLoading: false,
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    killswitch: {
      href: () => "/acme/killswitch",
      detail: { href: (id: string) => `/acme/killswitch/${id}` },
    },
    // The member and actor names link to their identity pages.
    identities: {
      detail: {
        overview: { href: (urn: string) => `/acme/identities/${urn}/overview` },
      },
    },
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: (...args: unknown[]) => {
    mocks.membersRequest = args[0];
    mocks.membersOptions = args[2];
    return {
      data: mocks.catalogDataAvailable ? { members: [] } : undefined,
      isLoading: mocks.catalogLoading,
      error: mocks.catalogError,
      refetch: mocks.refetchCatalog,
    };
  },
}));
vi.mock("@gram/client/react-query/killswitch.js", () => ({
  useKillswitch: (...args: unknown[]) => {
    mocks.detailRequest = args[1];
    mocks.detailOptions = args[2];
    return {
      data: mocks.detail,
      error: mocks.detailError,
      isLoading: false,
      isFetching: false,
      refetch: mocks.refetchDetail,
    };
  },
  invalidateKillswitch: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@gram/client/react-query/killswitches.js", () => ({
  invalidateAllKillswitches: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@gram/client/react-query/killswitchCapabilities.js", () => ({
  useKillswitchCapabilities: (...args: unknown[]) => {
    mocks.capabilityRequest = args[1];
    mocks.capabilityOptions = args[2];
    return {
      data: { capabilities: [], comingSoon: [] },
      isLoading: false,
      error: mocks.capabilitiesError,
      refetch: mocks.refetchCatalog,
    };
  },
}));
vi.mock("@gram/client/react-query/killswitchMCPServers.js", () => ({
  useKillswitchMCPServers: (...args: unknown[]) => {
    mocks.serverRequest = args[1];
    mocks.serverOptions = args[2];
    return {
      data: mocks.catalogDataAvailable ? { servers: [] } : undefined,
      isLoading: false,
      error: undefined,
      refetch: mocks.refetchCatalog,
    };
  },
}));
vi.mock("@gram/client/react-query/editKillswitch.js", () => ({
  useEditKillswitchMutation: () => ({ mutateAsync: mocks.edit }),
}));
vi.mock("@gram/client/react-query/liftKillswitch.js", () => ({
  useLiftKillswitchMutation: () => ({ mutateAsync: mocks.lift }),
}));
vi.mock("@gram/client/react-query/previewKillswitchOverlaps.js", () => ({
  usePreviewKillswitchOverlapsMutation: () => ({
    mutateAsync: mocks.preview,
    isPending: false,
  }),
}));
vi.mock("./KillswitchEditorSheet", () => ({
  KillswitchEditorSheet: (props: Record<string, unknown>) => {
    mocks.editorProps = props;
    return props.open ? <div>Edit dialog open</div> : null;
  },
}));
vi.mock("./LiftKillswitchDialog", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("./LiftKillswitchDialog")>();
  return {
    LiftKillswitchDialog: (
      props: React.ComponentProps<typeof actual.LiftKillswitchDialog>,
    ) => {
      mocks.liftProps = props as unknown as Record<string, unknown>;
      return mocks.renderRealLift ? (
        <actual.LiftKillswitchDialog {...props} />
      ) : props.open ? (
        <div>Lift dialog open</div>
      ) : null;
    },
  };
});

afterEach(cleanup);

beforeEach(() => {
  mocks.detailError = undefined;
  mocks.catalogLoading = false;
  mocks.catalogDataAvailable = true;
  mocks.catalogError = undefined;
  mocks.capabilitiesError = undefined;
  mocks.refetchCatalog.mockReset().mockResolvedValue(undefined);
  mocks.refetchDetail
    .mockReset()
    .mockImplementation(async () => ({ data: mocks.detail }));
  mocks.edit
    .mockReset()
    .mockResolvedValue({ id: "ks-1", version: 2, replayed: false });
  mocks.lift.mockReset().mockResolvedValue({
    remainingOverlaps: [],
    result: { id: "ks-1", version: 2, replayed: false },
    truncated: false,
  });
  mocks.detailRequest = undefined;
  mocks.detailOptions = undefined;
  mocks.membersRequest = undefined;
  mocks.membersOptions = undefined;
  mocks.capabilityRequest = undefined;
  mocks.capabilityOptions = undefined;
  mocks.serverRequest = undefined;
  mocks.serverOptions = undefined;
  mocks.editorProps = undefined;
  mocks.liftProps = undefined;
  mocks.renderRealLift = false;
  mocks.preview
    .mockReset()
    .mockResolvedValue({ overlaps: [], truncated: false });
});

function activeDetail(overrides: Partial<Detail> = {}): Detail {
  return {
    id: "ks-1",
    userId: "user-1",
    capabilityKey: "mcp_tool_calls",
    capabilityLabel: "MCP tool calls",
    version: 1,
    status: "active",
    scope: { type: "all_servers" },
    schedule: { start: "now", end: "until_lifted" },
    externalNote: "Public note",
    internalNote: "Internal note",
    history: [],
    historyTruncated: false,
    ...overrides,
  };
}

function editorDraft() {
  return {
    userId: "user-1",
    capabilityKey: "mcp_tool_calls",
    scopeType: "all_servers",
    serverIds: [],
    startType: "now",
    startsAt: "",
    endType: "until_lifted",
    endsAt: "",
    externalNote: "Public note",
    internalNote: "Internal note",
  };
}

function detailRoute() {
  return (
    <MemoryRouter initialEntries={["/acme/killswitch/ks-1"]}>
      <Routes>
        <Route
          path=":orgSlug/killswitch/:killswitchId"
          element={<KillswitchDetail />}
        />
      </Routes>
    </MemoryRouter>
  );
}

function renderDetail() {
  return render(detailRoute());
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function detailRouter() {
  return createMemoryRouter(
    [
      {
        path: ":orgSlug/killswitch/:killswitchId",
        element: <KillswitchDetail />,
      },
    ],
    { initialEntries: ["/acme/killswitch/ks-1"] },
  );
}

describe("KillswitchDetail", () => {
  it("renders unsafe-looking external notes literally with deleted historical fallbacks", async () => {
    mocks.detail = {
      id: "ks-1",
      userId: "deleted-user",
      capabilityKey: "mcp_tool_calls",
      capabilityLabel: "MCP tool calls",
      version: 2,
      status: "active",
      scope: { type: "selected_servers", serverIds: ["deleted-server"] },
      schedule: { start: "now", end: "until_lifted" },
      externalNote: "<script>alert(1)</script>\n**not markdown**",
      internalNote: "Line one\nLine two",
      history: [
        {
          sequence: 1,
          version: 1,
          action: "created",
          status: "active",
          changedAt: new Date("2030-01-01T00:00:00Z"),
          actorUserId: "deleted-actor",
          scope: { type: "selected_servers", serverIds: ["deleted-server"] },
          schedule: { start: "now", end: "until_lifted" },
          externalNote: "<b>literal</b>",
          internalNote: "audit note",
        },
      ],
      historyTruncated: false,
    };
    renderDetail();
    expect(screen.getAllByText("Deleted member").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Deleted MCP server").length).toBeGreaterThan(0);
    expect(
      screen.getByText(
        (_, element) =>
          element?.tagName === "P" &&
          element.textContent === "<script>alert(1)</script>\n**not markdown**",
      ),
    ).not.toBeNull();
    expect(screen.getByText(/Deleted actor/)).not.toBeNull();
    const historicalPublic = screen.getByText("<b>literal</b>");
    expect(historicalPublic.tagName).toBe("DD");
    expect(
      historicalPublic.parentElement?.querySelector("dt")?.textContent,
    ).toBe("Public member-facing message");
    const historicalInternal = screen.getByText("audit note");
    expect(historicalInternal.tagName).toBe("DD");
    expect(
      historicalInternal.parentElement?.querySelector("dt")?.textContent,
    ).toBe("Internal note");
    expect(document.querySelector("script")).toBeNull();
    expect(
      await screen.findByText(
        "No overlapping active or scheduled Killswitches.",
      ),
    ).not.toBeNull();
  });

  it("does not label catalog failures as deleted resources and retries them", async () => {
    mocks.detail = {
      id: "ks-1",
      userId: "user-1",
      capabilityKey: "mcp_tool_calls",
      capabilityLabel: "MCP tool calls",
      version: 1,
      status: "active",
      scope: { type: "all_servers" },
      schedule: { start: "now", end: "until_lifted" },
      externalNote: "Public note",
      internalNote: "Internal note",
      history: [],
      historyTruncated: false,
    };
    mocks.catalogDataAvailable = false;
    mocks.catalogError = new Error("catalog unavailable");
    renderDetail();
    expect(screen.queryByText("Deleted member")).toBeNull();
    expect(screen.getByText("catalog unavailable")).not.toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(mocks.refetchCatalog).toHaveBeenCalledTimes(2);
  });

  it("preserves an open editor when a catalog refetch fails with retained data", async () => {
    mocks.detail = activeDetail();
    const view = renderDetail();
    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    mocks.catalogError = new Error("catalog refresh failed");
    view.rerender(detailRoute());

    expect(
      screen.getByText("Latest Killswitch resources unavailable"),
    ).not.toBeNull();
    expect(screen.getByText("Edit dialog open")).not.toBeNull();
    expect(
      screen.queryByRole("button", { name: "Lift killswitch" }),
    ).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(mocks.refetchCatalog).toHaveBeenCalledTimes(2);
  });

  it("shows catalog loading before rendering deleted fallbacks", () => {
    mocks.catalogDataAvailable = false;
    mocks.catalogLoading = true;
    renderDetail();
    expect(screen.getByText("Loading Killswitch details…")).not.toBeNull();
    expect(screen.queryByText("Deleted member")).toBeNull();
  });

  it("scopes cached reads by session and loads capabilities only for editing", async () => {
    mocks.detail = activeDetail();
    mocks.capabilitiesError = new Error("catalog edit failed");
    renderDetail();
    expect(
      screen.getByRole("heading", { name: "MCP tool calls" }),
    ).not.toBeNull();
    expect(mocks.detailRequest).toEqual({
      id: "ks-1",
      gramSession: "session",
    });
    expect(mocks.membersRequest).toEqual({ gramSession: "session" });
    expect(mocks.capabilityRequest).toEqual({ gramSession: "session" });
    expect(mocks.serverRequest).toEqual({ gramSession: "session" });
    expect(mocks.detailOptions).toEqual({ throwOnError: false });
    expect(mocks.membersOptions).toEqual({ throwOnError: false });
    expect(mocks.serverOptions).toEqual({ throwOnError: false });
    expect(mocks.capabilityOptions).toEqual({
      enabled: false,
      throwOnError: false,
    });

    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    expect(mocks.capabilityOptions).toEqual({
      enabled: true,
      throwOnError: false,
    });
    expect(mocks.editorProps?.capabilitiesError).toBe(mocks.capabilitiesError);
  });

  it("keys overlap previews by record and ignores out-of-order responses", async () => {
    const initialPreview = deferred<{ overlaps: []; truncated: boolean }>();
    const liftPreview = deferred<{ overlaps: []; truncated: boolean }>();
    const nextPreview = deferred<{ overlaps: []; truncated: boolean }>();
    mocks.preview
      .mockReset()
      .mockImplementationOnce(() => initialPreview.promise)
      .mockImplementationOnce(() => liftPreview.promise)
      .mockImplementationOnce(() => nextPreview.promise);
    mocks.detail = activeDetail({ id: "ks-1", version: 1 });
    const router = detailRouter();
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(1));

    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    expect(screen.getByText("Edit dialog open")).not.toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("Lift dialog open")).not.toBeNull();

    mocks.detail = activeDetail({ id: "ks-2", version: 1 });
    await act(async () => {
      await router.navigate("/acme/killswitch/ks-2");
    });
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(3));
    expect(screen.queryByText("Edit dialog open")).toBeNull();
    expect(screen.queryByText("Lift dialog open")).toBeNull();
    expect(
      mocks.preview.mock.calls.map(
        ([input]) =>
          input.request.killswitchPreviewOverlapsRequest.id as string,
      ),
    ).toEqual(["ks-1", "ks-1", "ks-2"]);

    await act(async () => {
      nextPreview.resolve({ overlaps: [], truncated: true });
      await nextPreview.promise;
    });
    expect(
      screen.getByText("Additional overlaps are not shown by the API."),
    ).not.toBeNull();

    await act(async () => {
      liftPreview.resolve({ overlaps: [], truncated: false });
      initialPreview.resolve({ overlaps: [], truncated: false });
      await Promise.all([liftPreview.promise, initialPreview.promise]);
    });
    expect(
      screen.getByText("Additional overlaps are not shown by the API."),
    ).not.toBeNull();
  });

  it("ignores a conflict refresh that finishes after route navigation", async () => {
    const refetch = deferred<{
      data: Detail;
      error: null;
      isError: false;
    }>();
    mocks.detail = activeDetail({ id: "ks-1" });
    mocks.refetchDetail.mockReturnValue(refetch.promise);
    const router = detailRouter();
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(1));
    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    const refresh = (
      mocks.editorProps!.onRefreshConflict as () => Promise<unknown>
    )().then(
      () => undefined,
      (error: unknown) => error,
    );

    mocks.detail = activeDetail({ id: "ks-2" });
    await act(async () => {
      await router.navigate("/acme/killswitch/ks-2");
    });
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));
    refetch.resolve({
      data: activeDetail({ id: "ks-1" }),
      error: null,
      isError: false,
    });
    expect(await refresh).toBeInstanceOf(Error);
    expect(mocks.preview).toHaveBeenCalledTimes(2);

    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    await waitFor(() => expect(mocks.liftProps?.previewStatus).toBe("ready"));
  });

  it("ignores a delayed edit conflict from the previous route", async () => {
    const mutation = deferred<never>();
    const conflict = Object.assign(new Error("stale"), {
      statusCode: 409,
      data$: { name: "version_conflict" },
    });
    mocks.detail = activeDetail({ id: "ks-1" });
    mocks.edit.mockReturnValue(mutation.promise);
    const router = detailRouter();
    render(<RouterProvider router={router} />);
    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    const submit = (
      mocks.editorProps!.onSubmit as (
        draft: Record<string, unknown>,
        operationId: string,
        expectedVersion: number,
      ) => Promise<unknown>
    )(editorDraft(), "edit-op", 1).then(
      () => undefined,
      (error: unknown) => error,
    );

    mocks.detail = activeDetail({ id: "ks-2" });
    await act(async () => {
      await router.navigate("/acme/killswitch/ks-2");
    });
    mutation.reject(conflict);
    expect(await submit).toBe(conflict);
    expect(mocks.editorProps?.initiallyStale).toBe(false);
    expect(
      screen.getByRole("button", { name: "Lift killswitch" }),
    ).not.toBeNull();
  });

  it("keeps the real lift dialog open after a successful conflict refresh", async () => {
    const conflict = Object.assign(new Error("stale"), {
      statusCode: 409,
      data$: { name: "version_conflict" },
    });
    const refetch = deferred<{
      data: Detail;
      error: null;
      isError: false;
    }>();
    mocks.renderRealLift = true;
    mocks.detail = activeDetail();
    mocks.lift.mockRejectedValue(conflict);
    mocks.refetchDetail.mockReturnValue(refetch.promise);
    renderDetail();
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(1));

    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    await screen.findByRole("heading", { name: "Lift killswitch" });
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));
    await userEvent.click(
      screen.getAllByRole("button", { name: "Lift killswitch" }).at(-1)!,
    );
    await waitFor(() => expect(mocks.refetchDetail).toHaveBeenCalledTimes(1));

    const latest = activeDetail({ version: 2 });
    mocks.detail = latest;
    await act(async () => {
      refetch.resolve({ data: latest, error: null, isError: false });
      await refetch.promise;
    });

    expect(
      await screen.findByText(/latest version and overlaps are now shown/),
    ).not.toBeNull();
    expect(
      screen.getByRole("heading", { name: "Lift killswitch" }),
    ).not.toBeNull();
    expect(screen.getByText(/Version 2/)).not.toBeNull();
    expect(mocks.preview).toHaveBeenCalledTimes(3);
  });

  it("rejects an old reviewed lift after a committed background version change", async () => {
    mocks.renderRealLift = true;
    mocks.detail = activeDetail();
    const view = renderDetail();
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(1));

    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    await screen.findByRole("heading", { name: "Lift killswitch" });
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));

    mocks.detail = activeDetail({ version: 2 });
    view.rerender(detailRoute());
    expect(await screen.findByText(/Version 2/)).not.toBeNull();
    expect(mocks.preview).toHaveBeenCalledTimes(2);

    await userEvent.click(
      screen.getAllByRole("button", { name: "Lift killswitch" }).at(-1)!,
    );

    expect(
      await screen.findByText(/latest version and overlaps are now shown/),
    ).not.toBeNull();
    expect(mocks.refetchDetail).toHaveBeenCalledTimes(1);
    expect(mocks.lift).not.toHaveBeenCalled();

    await userEvent.click(
      screen.getAllByRole("button", { name: "Lift killswitch" }).at(-1)!,
    );
    await waitFor(() => expect(mocks.lift).toHaveBeenCalledTimes(1));
    expect(mocks.lift).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          killswitchLiftRequest: expect.objectContaining({
            id: "ks-1",
            expectedVersion: 2,
          }),
        },
      }),
    );
  });

  it("keeps successful lift overlaps authoritative for the returned version", async () => {
    const mutation = deferred<{
      remainingOverlaps: Array<{
        id: string;
        scope: { type: "selected_servers"; serverIds: string[] };
        schedule: { start: "now"; end: "until_lifted" };
        status: "active";
      }>;
      result: { id: string; version: number; replayed: boolean };
      truncated: boolean;
    }>();
    mocks.renderRealLift = true;
    mocks.detail = activeDetail();
    mocks.lift.mockReturnValue(mutation.promise);
    renderDetail();
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(1));

    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    await screen.findByRole("heading", { name: "Lift killswitch" });
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));
    await userEvent.click(
      screen.getAllByRole("button", { name: "Lift killswitch" }).at(-1)!,
    );

    mocks.detail = activeDetail({ version: 2, status: "lifted" });
    await act(async () => {
      mutation.resolve({
        remainingOverlaps: [
          {
            id: "overlap-authoritative",
            scope: { type: "selected_servers", serverIds: ["deleted"] },
            schedule: { start: "now", end: "until_lifted" },
            status: "active",
          },
        ],
        result: { id: "ks-1", version: 2, replayed: false },
        truncated: false,
      });
      await mutation.promise;
    });

    expect(await screen.findByText("Deleted MCP server")).not.toBeNull();
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));
  });

  it("does not let a stale real lift completion close the next route dialog", async () => {
    const mutation = deferred<{
      remainingOverlaps: [];
      result: { id: string; version: number; replayed: boolean };
      truncated: boolean;
    }>();
    mocks.renderRealLift = true;
    mocks.detail = activeDetail({ id: "ks-1" });
    mocks.lift.mockReturnValue(mutation.promise);
    const router = detailRouter();
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(1));

    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    await screen.findByRole("heading", { name: "Lift killswitch" });
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));
    await userEvent.click(
      screen.getAllByRole("button", { name: "Lift killswitch" }).at(-1)!,
    );
    await waitFor(() => expect(mocks.lift).toHaveBeenCalledTimes(1));

    mocks.detail = activeDetail({ id: "ks-2" });
    await act(async () => {
      await router.navigate("/acme/killswitch/ks-2");
    });
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(3));
    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    await screen.findByRole("heading", { name: "Lift killswitch" });
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(4));

    await act(async () => {
      mutation.resolve({
        remainingOverlaps: [],
        result: { id: "ks-1", version: 2, replayed: false },
        truncated: false,
      });
      await mutation.promise;
    });

    expect(
      screen.getByRole("heading", { name: "Lift killswitch" }),
    ).not.toBeNull();
  });

  it("consumes rejected overlap retries after showing the latest error", async () => {
    mocks.detail = activeDetail();
    mocks.preview
      .mockReset()
      .mockRejectedValueOnce(new Error("initial preview failed"))
      .mockRejectedValueOnce(new Error("retry preview failed"));
    renderDetail();
    expect(await screen.findByText("initial preview failed")).not.toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("retry preview failed")).not.toBeNull();
    expect(mocks.preview).toHaveBeenCalledTimes(2);
  });

  it("persists edit conflict authority across closing and reopening", async () => {
    const conflict = Object.assign(new Error("stale"), {
      statusCode: 409,
      data$: { name: "version_conflict" },
    });
    mocks.detail = activeDetail();
    mocks.edit.mockRejectedValue(conflict);
    renderDetail();
    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    const onSubmit = mocks.editorProps?.onSubmit as (
      draft: Record<string, unknown>,
      operationId: string,
      expectedVersion: number,
    ) => Promise<unknown>;
    await expect(onSubmit(editorDraft(), "edit-op", 1)).rejects.toBe(conflict);
    await waitFor(() => expect(mocks.editorProps?.initiallyStale).toBe(true));
    expect(
      screen.queryByRole("button", { name: "Lift killswitch" }),
    ).toBeNull();

    act(() => {
      (mocks.editorProps!.onOpenChange as (open: boolean) => void)(false);
    });
    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    expect(mocks.editorProps?.initiallyStale).toBe(true);
  });

  it("rejects errored conflict refetches with retained data and keeps actions blocked", async () => {
    mocks.detail = activeDetail();
    const view = renderDetail();
    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));

    const retained = activeDetail();
    mocks.refetchDetail.mockResolvedValue({
      data: retained,
      error: new Error("refresh failed"),
      isError: true,
    });
    const onRefreshConflict = mocks.editorProps
      ?.onRefreshConflict as () => Promise<unknown>;
    await expect(onRefreshConflict()).rejects.toThrow(
      "latest version could not be loaded",
    );
    expect(mocks.preview).toHaveBeenCalledTimes(2);

    mocks.detailError = new Error("refresh failed");
    view.rerender(detailRoute());
    expect(
      screen.getByText("Latest Killswitch version unavailable"),
    ).not.toBeNull();
    expect(screen.getByText("Edit dialog open")).not.toBeNull();
    mocks.detailError = undefined;
    view.rerender(detailRoute());

    act(() => {
      (mocks.editorProps!.onOpenChange as (open: boolean) => void)(false);
    });
    expect(screen.queryByText("Lift dialog open")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Lift killswitch" }),
    ).toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    expect(mocks.editorProps?.initiallyStale).toBe(true);

    const onSubmit = mocks.editorProps?.onSubmit as (
      draft: Record<string, unknown>,
      operationId: string,
      expectedVersion: number,
    ) => Promise<unknown>;
    const onLift = mocks.liftProps?.onLift as (
      operationId: string,
    ) => Promise<unknown>;
    await expect(onSubmit({}, "edit-op", 1)).rejects.toThrow(
      "latest Killswitch version is unavailable",
    );
    await expect(onLift("lift-op")).rejects.toThrow(
      "latest Killswitch version is unavailable",
    );
    expect(mocks.edit).not.toHaveBeenCalled();
    expect(mocks.lift).not.toHaveBeenCalled();

    const latest = activeDetail({ version: 2 });
    mocks.detail = latest;
    mocks.detailError = undefined;
    mocks.refetchDetail.mockResolvedValue({
      data: latest,
      error: null,
      isError: false,
    });
    await (mocks.editorProps!.onRefreshConflict as () => Promise<unknown>)();
    await waitFor(() => expect(mocks.editorProps?.initiallyStale).toBe(false));
    expect(
      screen.getByRole("button", { name: "Lift killswitch" }),
    ).not.toBeNull();
  });

  it("closes terminal dialogs and guards stale mutation callbacks", async () => {
    mocks.detail = activeDetail();
    const view = renderDetail();
    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    expect(screen.getByText("Edit dialog open")).not.toBeNull();
    expect(screen.getByText("Lift dialog open")).not.toBeNull();

    mocks.detail = activeDetail({ status: "expired", version: 2 });
    view.rerender(detailRoute());
    expect(screen.queryByText("Edit dialog open")).toBeNull();
    expect(screen.queryByText("Lift dialog open")).toBeNull();

    const onRefreshConflict = mocks.editorProps
      ?.onRefreshConflict as () => Promise<unknown>;
    await act(async () => {
      await expect(onRefreshConflict()).rejects.toThrow(
        "can no longer be changed",
      );
    });

    const onSubmit = mocks.editorProps?.onSubmit as (
      draft: Record<string, unknown>,
      operationId: string,
      expectedVersion: number,
    ) => Promise<unknown>;
    const onLift = mocks.liftProps?.onLift as (
      operationId: string,
    ) => Promise<unknown>;
    await act(async () => {
      await expect(onSubmit({}, "edit-op", 2)).rejects.toThrow(
        "can no longer be changed",
      );
      await expect(onLift("lift-op")).rejects.toThrow(
        "can no longer be changed",
      );
    });
    expect(mocks.edit).not.toHaveBeenCalled();
    expect(mocks.lift).not.toHaveBeenCalled();
  });

  it("passes raw notes to the edit API", async () => {
    mocks.detail = activeDetail();
    renderDetail();
    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    const onSubmit = mocks.editorProps?.onSubmit as (
      draft: Record<string, unknown>,
      operationId: string,
      expectedVersion: number,
    ) => Promise<unknown>;
    await act(() =>
      onSubmit(
        {
          scopeType: "all_servers",
          serverIds: [],
          startType: "now",
          startsAt: "",
          endType: "until_lifted",
          endsAt: "",
          externalNote: "  Public 🙂 note  ",
          internalNote: "  Internal note  ",
        },
        "edit-op",
        1,
      ),
    );
    expect(mocks.edit).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          killswitchEditRequest: expect.objectContaining({
            externalNote: "  Public 🙂 note  ",
            internalNote: "  Internal note  ",
          }),
        },
      }),
    );
  });

  it("omits unverifiable all-server diffs and expiry diffs", () => {
    const baseEvent = {
      actorUserId: "actor-1",
      changedAt: new Date("2030-01-01T00:00:00Z"),
      schedule: { start: "now", end: "until_lifted" } as const,
      externalNote: "Public note",
      internalNote: "Internal note",
    };
    mocks.detail = activeDetail({
      status: "expired",
      version: 4,
      history: [
        {
          ...baseEvent,
          sequence: 1,
          version: 1,
          action: "created",
          status: "active",
          scope: { type: "selected_servers", serverIds: ["server-a"] },
        },
        {
          ...baseEvent,
          sequence: 2,
          version: 2,
          action: "edited",
          status: "active",
          scope: { type: "all_servers" },
        },
        {
          ...baseEvent,
          sequence: 3,
          version: 3,
          action: "edited",
          status: "active",
          scope: { type: "selected_servers", serverIds: ["server-a"] },
        },
        {
          ...baseEvent,
          sequence: 4,
          version: 4,
          action: "expired",
          status: "expired",
          scope: { type: "selected_servers", serverIds: ["server-b"] },
        },
      ],
    });
    renderDetail();
    expect(
      screen.getByText(
        /Scope changed from selected servers to all MCP servers/,
      ),
    ).not.toBeNull();
    expect(
      screen.getByText(
        /Scope changed from all MCP servers to selected servers/,
      ),
    ).not.toBeNull();
    expect(screen.queryByText(/^Added:/)).toBeNull();
  });

  it("shows a dedicated read-error state", () => {
    mocks.detail = undefined;
    mocks.detailError = new Error("read failed");
    renderDetail();
    expect(screen.getByText("Killswitch unavailable")).not.toBeNull();
    expect(screen.getByText("read failed")).not.toBeNull();
  });
});
