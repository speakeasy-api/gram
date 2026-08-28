import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import type { KillswitchDetail as Detail } from "@gram/client/models/components/killswitchdetail.js";
import KillswitchDetail from "./KillswitchDetail";

const mocks = vi.hoisted(() => ({
  detail: undefined as Detail | undefined,
  detailError: undefined as Error | undefined,
  preview: vi.fn(),
  catalogLoading: false,
  catalogError: undefined as Error | undefined,
  capabilitiesError: undefined as Error | undefined,
  refetchCatalog: vi.fn(),
  refetchDetail: vi.fn(),
  edit: vi.fn(),
  lift: vi.fn(),
  capabilityOptions: undefined as unknown,
  editorProps: undefined as Record<string, unknown> | undefined,
  liftProps: undefined as Record<string, unknown> | undefined,
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session" }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    killswitch: {
      href: () => "/acme/killswitch",
      detail: { href: (id: string) => `/acme/killswitch/${id}` },
    },
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: { members: [] },
    isLoading: mocks.catalogLoading,
    error: mocks.catalogError,
    refetch: mocks.refetchCatalog,
  }),
}));
vi.mock("@gram/client/react-query/killswitch.js", () => ({
  useKillswitch: () => ({
    data: mocks.detail,
    error: mocks.detailError,
    isLoading: false,
    refetch: mocks.refetchDetail,
  }),
  invalidateKillswitch: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@gram/client/react-query/killswitches.js", () => ({
  invalidateAllKillswitches: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@gram/client/react-query/killswitchCapabilities.js", () => ({
  useKillswitchCapabilities: (...args: unknown[]) => {
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
  useKillswitchMCPServers: () => ({
    data: { servers: [] },
    isLoading: false,
    error: undefined,
    refetch: mocks.refetchCatalog,
  }),
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
vi.mock("./LiftKillswitchDialog", () => ({
  LiftKillswitchDialog: (props: Record<string, unknown>) => {
    mocks.liftProps = props;
    return props.open ? <div>Lift dialog open</div> : null;
  },
}));

afterEach(cleanup);

beforeEach(() => {
  mocks.detailError = undefined;
  mocks.catalogLoading = false;
  mocks.catalogError = undefined;
  mocks.capabilitiesError = undefined;
  mocks.refetchCatalog.mockReset().mockResolvedValue(undefined);
  mocks.refetchDetail
    .mockReset()
    .mockImplementation(async () => ({ data: mocks.detail }));
  mocks.edit
    .mockReset()
    .mockResolvedValue({ id: "ks-1", version: 2, replayed: false });
  mocks.lift
    .mockReset()
    .mockResolvedValue({ remainingOverlaps: [], truncated: false });
  mocks.capabilityOptions = undefined;
  mocks.editorProps = undefined;
  mocks.liftProps = undefined;
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
    mocks.catalogError = new Error("catalog unavailable");
    renderDetail();
    expect(screen.queryByText("Deleted member")).toBeNull();
    expect(screen.getByText("catalog unavailable")).not.toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(mocks.refetchCatalog).toHaveBeenCalledTimes(2);
  });

  it("shows catalog loading before rendering deleted fallbacks", () => {
    mocks.catalogLoading = true;
    renderDetail();
    expect(screen.getByText("Loading Killswitch details…")).not.toBeNull();
    expect(screen.queryByText("Deleted member")).toBeNull();
  });

  it("loads the capability catalog only for editing without failing read detail", async () => {
    mocks.detail = activeDetail();
    mocks.capabilitiesError = new Error("catalog edit failed");
    renderDetail();
    expect(
      screen.getByRole("heading", { name: "MCP tool calls" }),
    ).not.toBeNull();
    expect(mocks.capabilityOptions).toEqual({ enabled: false });

    await userEvent.click(
      screen.getByRole("button", { name: "Edit killswitch" }),
    );
    expect(mocks.capabilityOptions).toEqual({ enabled: true });
    expect(mocks.editorProps?.capabilitiesError).toBe(mocks.capabilitiesError);
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

  it("renders scope-mode transitions and omits edit diffs for expiry events", () => {
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
    expect(screen.getAllByText(/^Added:/)).toHaveLength(1);
  });

  it("shows a dedicated read-error state", () => {
    mocks.detail = undefined;
    mocks.detailError = new Error("read failed");
    renderDetail();
    expect(screen.getByText("Killswitch unavailable")).not.toBeNull();
    expect(screen.getByText("read failed")).not.toBeNull();
  });
});
