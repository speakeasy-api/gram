import type { ProxiedMcpTool } from "@/hooks/useProxiedMcpTools";
import type { ToolMetadataByName } from "@/hooks/useToolMetadata";
import type { ToolMetadata } from "@gram/client/models/components/toolmetadata.js";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSyncToolMetadata } from "./useSyncToolMetadata";

type MutationOptions = {
  onSuccess?: () => unknown;
  onError?: (error: unknown) => unknown;
};

const mocks = vi.hoisted(() => ({
  addBatch: vi.fn(),
  // The automatic pass awaits its write; each call's settlement is chosen by
  // the test.
  autoAdd: vi.fn<(vars: unknown) => Promise<unknown>>(),
  setBatch: vi.fn(),
  setOne: vi.fn(),
  deleteOne: vi.fn(),
  addOptions: [] as MutationOptions[],
  refresh: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("@gram/client/react-query/listMcpServerToolMetadata.js", () => ({
  invalidateAllListMcpServerToolMetadata: mocks.refresh,
}));

// The automatic pass writes with mutateAsync; the per-tool Record action
// with mutate.
vi.mock("@gram/client/react-query/addMcpServerToolMetadataBatch.js", () => ({
  useAddMcpServerToolMetadataBatchMutation: (options: MutationOptions) => {
    mocks.addOptions.push(options);
    return {
      mutate: mocks.addBatch,
      mutateAsync: mocks.autoAdd,
      isPending: false,
      variables: undefined,
    };
  },
}));

vi.mock("@gram/client/react-query/setMcpServerToolMetadataBatch.js", () => ({
  useSetMcpServerToolMetadataBatchMutation: () => ({
    mutate: mocks.setBatch,
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/setMcpServerToolMetadata.js", () => ({
  useSetMcpServerToolMetadataMutation: () => ({
    mutate: mocks.setOne,
    isPending: false,
    variables: undefined,
  }),
}));

vi.mock("@gram/client/react-query/deleteMcpServerToolMetadata.js", () => ({
  useDeleteMcpServerToolMetadataMutation: () => ({
    mutate: mocks.deleteOne,
    isPending: false,
    variables: undefined,
  }),
}));

vi.mock("@/lib/errors", () => ({ handleAPIError: vi.fn() }));

function live(...names: string[]): Record<string, ProxiedMcpTool> {
  return Object.fromEntries(
    names.map((name) => [name, { annotations: { readOnlyHint: true } }]),
  );
}

function stored(...names: string[]): ToolMetadataByName {
  return Object.fromEntries(
    names.map((name): [string, ToolMetadata] => [
      name,
      {
        mcpServerId: "srv-1",
        toolName: name,
        readOnlyHint: true,
        createdAt: new Date("2026-01-01T00:00:00Z"),
        updatedAt: new Date("2026-01-01T00:00:00Z"),
      },
    ]),
  );
}

type Props = {
  live: Record<string, ProxiedMcpTool> | undefined;
  stored: ToolMetadataByName;
  listedAt?: number;
  mcpServerId?: string;
};

function renderAdditive(initial: Props) {
  return renderHook(
    (props: Props) =>
      useSyncToolMetadata({
        mcpServerId: props.mcpServerId ?? "srv-1",
        live: props.live,
        listedAt: props.listedAt ?? 1,
        stored: props.stored,
        enabled: true,
        mode: "additive",
        project: { id: "proj-1", slug: "default" },
      }),
    { initialProps: initial },
  );
}

function recordedNames(call: unknown[]): string[] {
  const request = (
    call[0] as {
      request: {
        setToolMetadataBatchRequestBody: { tools: { toolName: string }[] };
      };
    }
  ).request;
  return request.setToolMetadataBatchRequestBody.tools.map((t) => t.toolName);
}

beforeEach(() => {
  mocks.autoAdd.mockResolvedValue({});
});

afterEach(() => {
  vi.resetAllMocks();
  mocks.addOptions.length = 0;
});

describe("useSyncToolMetadata additive mode", () => {
  it("offers no bulk sync and never replaces the stored set", () => {
    const { result } = renderAdditive({
      live: live("list_devices"),
      stored: stored("list_devices", "wipe_device"),
    });

    expect(result.current.sync).toBeUndefined();
    expect(result.current.toolActions).toBeDefined();
    expect(mocks.setBatch).not.toHaveBeenCalled();
    // The listing lacked wipe_device; nothing deletes it on that evidence.
    expect(mocks.deleteOne).not.toHaveBeenCalled();
  });

  it("records a tool that appears in a later listing of the same view", () => {
    const { rerender } = renderAdditive({
      live: live("list_devices"),
      stored: stored("list_devices"),
    });
    expect(mocks.autoAdd).not.toHaveBeenCalled();

    rerender({
      live: live("list_devices", "lock_device"),
      stored: stored("list_devices"),
    });

    expect(mocks.autoAdd).toHaveBeenCalledOnce();
    expect(recordedNames(mocks.autoAdd.mock.calls[0]!)).toEqual([
      "lock_device",
    ]);
  });

  it("keeps the union of disjoint listings", () => {
    const { rerender } = renderAdditive({
      live: live("list_devices"),
      stored: stored(),
    });
    expect(recordedNames(mocks.autoAdd.mock.calls[0]!)).toEqual([
      "list_devices",
    ]);

    // Another session sees a disjoint set once the first has been stored.
    rerender({ live: live("wipe_device"), stored: stored("list_devices") });

    expect(mocks.autoAdd).toHaveBeenCalledTimes(2);
    expect(recordedNames(mocks.autoAdd.mock.calls[1]!)).toEqual([
      "wipe_device",
    ]);
    expect(mocks.setBatch).not.toHaveBeenCalled();
    expect(mocks.deleteOne).not.toHaveBeenCalled();
  });

  it("does not repeat a recording for the same tools", () => {
    const { rerender } = renderAdditive({
      live: live("lock_device"),
      stored: stored(),
    });
    rerender({ live: live("lock_device"), stored: stored() });

    expect(mocks.autoAdd).toHaveBeenCalledOnce();
  });

  it("retries after a failed write on the next listing, even with identical data", async () => {
    // React Query shares unchanged listing data, so a successful refetch can
    // hand back the same objects; only the listing time moves.
    const sharedLive = live("lock_device");
    const sharedStored = stored();
    let reject: (error: unknown) => void = () => {};
    mocks.autoAdd.mockImplementationOnce(
      () =>
        new Promise((_, r) => {
          reject = r;
        }),
    );
    const { rerender } = renderAdditive({
      live: sharedLive,
      stored: sharedStored,
      listedAt: 1,
    });
    expect(mocks.autoAdd).toHaveBeenCalledOnce();

    await act(async () => {
      reject(new Error("network down"));
    });
    // Nothing new was listed yet: no immediate retry loop.
    rerender({ live: sharedLive, stored: sharedStored, listedAt: 1 });
    expect(mocks.autoAdd).toHaveBeenCalledOnce();

    mocks.autoAdd.mockResolvedValueOnce({});
    rerender({ live: sharedLive, stored: sharedStored, listedAt: 2 });
    expect(mocks.autoAdd).toHaveBeenCalledTimes(2);
    expect(recordedNames(mocks.autoAdd.mock.calls[1]!)).toEqual([
      "lock_device",
    ]);
  });

  it("refreshes and recomputes after a conflict", async () => {
    mocks.autoAdd.mockRejectedValueOnce(
      Object.assign(
        Object.create(
          (await import("@gram/client/models/errors/gramerror.js")).GramError
            .prototype,
        ) as object,
        { statusCode: 409 },
      ),
    );
    const { rerender } = renderAdditive({
      live: live("lock_device", "wipe_device"),
      stored: stored(),
    });
    await act(async () => {});
    expect(mocks.refresh).toHaveBeenCalled();

    // The refreshed snapshot shows another session recorded one of them.
    mocks.autoAdd.mockResolvedValueOnce({});
    rerender({
      live: live("lock_device", "wipe_device"),
      stored: stored("wipe_device"),
    });
    expect(mocks.autoAdd).toHaveBeenCalledTimes(2);
    expect(recordedNames(mocks.autoAdd.mock.calls[1]!)).toEqual([
      "lock_device",
    ]);
  });

  it("keeps a newer batch guarded when an older one fails late", async () => {
    let rejectFirst: (error: unknown) => void = () => {};
    mocks.autoAdd
      .mockImplementationOnce(
        () =>
          new Promise((_, r) => {
            rejectFirst = r;
          }),
      )
      .mockImplementation(() => new Promise(() => {}));
    const { rerender } = renderAdditive({
      live: live("lock_device"),
      stored: stored(),
      listedAt: 1,
    });
    rerender({
      live: live("lock_device", "wipe_device"),
      stored: stored(),
      listedAt: 2,
    });
    expect(mocks.autoAdd).toHaveBeenCalledTimes(2);

    await act(async () => {
      rejectFirst(new Error("late failure"));
    });
    // The newer batch is still in flight: re-rendering it sends nothing new.
    rerender({
      live: live("lock_device", "wipe_device"),
      stored: stored(),
      listedAt: 2,
    });
    expect(mocks.autoAdd).toHaveBeenCalledTimes(2);
  });

  it("does not let an earlier server's late failure affect the next one", async () => {
    let rejectFirst: (error: unknown) => void = () => {};
    mocks.autoAdd
      .mockImplementationOnce(
        () =>
          new Promise((_, r) => {
            rejectFirst = r;
          }),
      )
      .mockImplementation(() => new Promise(() => {}));
    const { rerender } = renderAdditive({
      live: live("lock_device"),
      stored: stored(),
      mcpServerId: "srv-1",
    });
    rerender({
      live: live("lock_device"),
      stored: stored(),
      mcpServerId: "srv-2",
    });
    expect(mocks.autoAdd).toHaveBeenCalledTimes(2);

    await act(async () => {
      rejectFirst(new Error("late failure"));
    });
    rerender({
      live: live("lock_device"),
      stored: stored(),
      mcpServerId: "srv-2",
    });
    expect(mocks.autoAdd).toHaveBeenCalledTimes(2);
  });

  it("does nothing without a listing that succeeded", () => {
    renderAdditive({ live: undefined, stored: stored() });

    expect(mocks.autoAdd).not.toHaveBeenCalled();
  });

  it("removes exactly the tool asked for", () => {
    const { result } = renderAdditive({
      live: live("list_devices"),
      stored: stored("list_devices", "wipe_device"),
    });

    act(() => result.current.toolActions!.remove("wipe_device"));

    expect(mocks.deleteOne).toHaveBeenCalledOnce();
    expect(mocks.deleteOne.mock.calls[0]![0]).toEqual({
      request: {
        gramProject: "default",
        mcpServerId: "srv-1",
        toolName: "wipe_device",
      },
    });
  });

  it("applies the advertised annotations of one tool as a full record", () => {
    const { result } = renderAdditive({
      live: { list_devices: { annotations: { destructiveHint: true } } },
      stored: stored("list_devices"),
    });

    act(() => result.current.toolActions!.apply("list_devices"));

    expect(mocks.setOne).toHaveBeenCalledOnce();
    expect(mocks.setOne.mock.calls[0]![0]).toEqual({
      request: {
        gramProject: "default",
        setToolMetadataRequestBody: {
          mcpServerId: "srv-1",
          toolName: "list_devices",
          title: undefined,
          readOnlyHint: undefined,
          destructiveHint: true,
          idempotentHint: undefined,
          openWorldHint: undefined,
        },
      },
    });
  });

  it("ignores a tool name inherited from Object.prototype", () => {
    const { result } = renderAdditive({ live: live(), stored: stored() });

    act(() => result.current.toolActions!.apply("toString"));
    act(() => result.current.toolActions!.record("toString"));

    expect(mocks.setOne).not.toHaveBeenCalled();
    expect(mocks.addBatch).not.toHaveBeenCalled();
  });
});

describe("useSyncToolMetadata mirror mode", () => {
  it("keeps the explicit full sync for remote servers", () => {
    const { result } = renderHook(() =>
      useSyncToolMetadata({
        mcpServerId: "srv-1",
        live: live("list_devices"),
        stored: stored("list_devices", "wipe_device"),
        enabled: true,
        mode: "mirror",
      }),
    );

    expect(result.current.toolActions).toBeUndefined();
    act(() => result.current.sync!());
    expect(mocks.setBatch).toHaveBeenCalledOnce();
  });
});
