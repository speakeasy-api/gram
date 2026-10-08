import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useHeaderDrafts, useTunneledHeaderDrafts } from "./useHeaderDrafts";

const mocks = vi.hoisted(() => ({
  headers: vi.fn(),
  tunneledHeaders: vi.fn(),
  tunneledCreate: vi.fn(),
  tunneledUpdate: vi.fn(),
  tunneledRemove: vi.fn(),
  tunneledInvalidate: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  invalidate: vi.fn(),
  // Real react-query mutations expose reset; save() calls it so a settled
  // write stops holding the plaintext secret it submitted.
  resetCreate: vi.fn(),
  resetUpdate: vi.fn(),
  resetRemove: vi.fn(),
}));

let queryClient: QueryClient;

vi.mock("@gram/client/react-query/remoteMcpServerHeaders.js", () => ({
  useRemoteMcpServerHeaders: (...args: unknown[]) => mocks.headers(...args),
  invalidateAllRemoteMcpServerHeaders: () => mocks.invalidate(),
}));
vi.mock("@gram/client/react-query/tunneledMcpServerHeaders.js", () => ({
  useTunneledMcpServerHeaders: (...args: unknown[]) =>
    mocks.tunneledHeaders(...args),
  invalidateAllTunneledMcpServerHeaders: () => mocks.tunneledInvalidate(),
}));
function mutationMock(mutateAsync: (...args: unknown[]) => unknown) {
  return { mutateAsync, reset: vi.fn(), isPending: false, error: null };
}
vi.mock("@gram/client/react-query/createTunneledMcpServerHeader.js", () => ({
  useCreateTunneledMcpServerHeaderMutation: () =>
    mutationMock(mocks.tunneledCreate),
}));
vi.mock("@gram/client/react-query/updateTunneledMcpServerHeader.js", () => ({
  useUpdateTunneledMcpServerHeaderMutation: () =>
    mutationMock(mocks.tunneledUpdate),
}));
vi.mock("@gram/client/react-query/deleteTunneledMcpServerHeader.js", () => ({
  useDeleteTunneledMcpServerHeaderMutation: () =>
    mutationMock(mocks.tunneledRemove),
}));
vi.mock("@gram/client/react-query/createRemoteMcpServerHeader.js", () => ({
  useCreateRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.create,
    reset: mocks.resetCreate,
    isPending: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/updateRemoteMcpServerHeader.js", () => ({
  useUpdateRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.update,
    reset: mocks.resetUpdate,
    isPending: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/deleteRemoteMcpServerHeader.js", () => ({
  useDeleteRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.remove,
    reset: mocks.resetRemove,
    isPending: false,
    error: null,
  }),
}));

function serverHeader(
  overrides: Partial<RemoteMcpServerHeader> & { id: string; name: string },
): RemoteMcpServerHeader {
  return {
    value: "static",
    isRequired: false,
    isSecret: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  } as RemoteMcpServerHeader;
}

function wrapper({ children }: { children: ReactNode }): JSX.Element {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

/** The query result for a given header list, with a refetch that echoes it. */
function headersResult(headers: RemoteMcpServerHeader[]) {
  return {
    data: { headers },
    isLoading: false,
    isError: false,
    refetch: vi.fn().mockResolvedValue({ isError: false, data: { headers } }),
  };
}

let savedSequence = 0;

/** Echoes a write back the way the server answers it: an id, secrets redacted. */
async function echoSaved(args: {
  request: Record<string, Record<string, unknown>>;
}): Promise<RemoteMcpServerHeader> {
  const form = Object.values(args.request)[0] ?? {};
  savedSequence += 1;
  return serverHeader({
    id: (form.id as string | undefined) ?? `saved-${savedSequence}`,
    name: form.name as string,
    value: form.isSecret ? "***" : (form.value as string | undefined),
    valueFromRequestHeader: form.valueFromRequestHeader as string | undefined,
    isSecret: !!form.isSecret,
    isRequired: !!form.isRequired,
  });
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  mocks.headers.mockReturnValue(headersResult([]));
  mocks.tunneledHeaders.mockReturnValue(headersResult([]));
  mocks.tunneledInvalidate.mockResolvedValue(undefined);
  mocks.tunneledCreate.mockImplementation(echoSaved);
  mocks.tunneledUpdate.mockImplementation(echoSaved);
  mocks.tunneledRemove.mockResolvedValue(undefined);
  mocks.invalidate.mockResolvedValue(undefined);
  mocks.create.mockImplementation(echoSaved);
  mocks.update.mockImplementation(echoSaved);
  mocks.remove.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderDrafts(managedHeaderId?: string) {
  return renderHook(
    () =>
      useHeaderDrafts({
        remoteMcpServerId: "remote-source-1",
        identity: {
          mode: "agent",
          managed: managedHeaderId
            ? { ownedBy: "agent", headerId: managedHeaderId }
            : null,
          isError: false,
        },
      }),
    { wrapper },
  );
}

describe("useHeaderDrafts", () => {
  it("writes a row the operator added", async () => {
    const { result } = renderDrafts();

    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        name: "X-Api-Key",
        staticValue: "sk-test",
      }),
    );

    expect(result.current.isDirty).toBe(true);
    expect(result.current.validationError).toBeNull();

    await act(async () => {
      await result.current.save();
    });

    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(mocks.create.mock.calls[0]?.[0]).toMatchObject({
      request: {
        createServerHeaderForm: {
          remoteMcpServerId: "remote-source-1",
          name: "X-Api-Key",
          value: "sk-test",
        },
      },
    });
    // The submitted secret must not stay readable in mutation state once the
    // write has settled.
    expect(mocks.resetCreate).toHaveBeenCalled();
    expect(mocks.resetUpdate).toHaveBeenCalled();
  });

  it("never deletes the Authorization row identity owns", async () => {
    // The identity save writes this row and refetches, so it can appear in the
    // server list while these drafts have never heard of it. Diffing naively
    // would delete the credential the operator just configured.
    const authorization = serverHeader({
      id: "header-authorization",
      name: "Authorization",
      value: "***",
      isSecret: true,
    });
    const other = serverHeader({ id: "header-trace", name: "X-Trace" });
    mocks.headers.mockReturnValue(headersResult([other]));

    const { result, rerender } = renderDrafts("header-authorization");

    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(1, {
        ...result.current.drafts[1]!,
        name: "X-Api-Key",
        staticValue: "sk-test",
      }),
    );

    // Identity commits first: the server now holds the Authorization row too.
    mocks.headers.mockReturnValue(headersResult([other, authorization]));
    rerender();

    await act(async () => {
      await result.current.save();
    });

    expect(mocks.remove).not.toHaveBeenCalled();
    expect(mocks.create).toHaveBeenCalledTimes(1);
  });

  it("deletes a row the operator removed", async () => {
    mocks.headers.mockReturnValue(
      headersResult([serverHeader({ id: "header-trace", name: "X-Trace" })]),
    );
    const { result } = renderDrafts();

    act(() => result.current.removeHeader(0));

    await act(async () => {
      await result.current.save();
    });

    expect(mocks.remove).toHaveBeenCalledWith({
      request: { id: "header-trace" },
    });
  });

  it("keeps unsaved edits when the server list refetches underneath", () => {
    const trace = serverHeader({ id: "header-trace", name: "X-Trace" });
    mocks.headers.mockReturnValue(headersResult([trace]));
    const { result, rerender } = renderDrafts();

    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        staticValue: "mine",
      }),
    );

    mocks.headers.mockReturnValue(
      headersResult([trace, serverHeader({ id: "header-new", name: "X-New" })]),
    );
    rerender();

    expect(result.current.drafts).toHaveLength(1);
    expect(result.current.drafts[0]?.staticValue).toBe("mine");
  });

  it("stays locked until the post-save refresh lands", async () => {
    let finishRefresh: (value: unknown) => void = () => {};
    const result0 = headersResult([]);
    result0.refetch = vi.fn(
      () =>
        new Promise((resolve) => {
          finishRefresh = resolve;
        }),
    );
    mocks.headers.mockReturnValue(result0);
    const { result } = renderDrafts();

    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        name: "X-Api-Key",
        staticValue: "sk-test",
      }),
    );

    let pending: Promise<boolean> = Promise.resolve(false);
    await act(async () => {
      pending = result.current.save();
      await Promise.resolve();
    });

    // The writes have settled, but the refresh has not replaced the rows yet.
    await vi.waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
    expect(result.current.readOnly).toBe(true);
    expect(result.current.saving).toBe(true);
    await act(async () => {
      await expect(result.current.save()).resolves.toBe(false);
    });
    expect(mocks.create).toHaveBeenCalledTimes(1);

    await act(async () => {
      finishRefresh({ isError: false, data: { headers: [] } });
      await pending;
    });
    expect(result.current.readOnly).toBe(false);
    // The refresh is what the lock protects: it replaces the rows wholesale.
    expect(result.current.drafts).toHaveLength(0);
  });

  it("drops the submitted values even when a write fails", async () => {
    mocks.create.mockRejectedValue(new Error("upstream rejected"));
    const { result } = renderDrafts();

    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        name: "X-Api-Key",
        staticValue: "sk-test",
      }),
    );

    await act(async () => {
      await expect(result.current.save()).rejects.toThrow("upstream rejected");
    });

    // The failed request still carried the plaintext secret.
    expect(mocks.resetCreate).toHaveBeenCalled();
    expect(mocks.resetUpdate).toHaveBeenCalled();
    expect(mocks.resetRemove).toHaveBeenCalled();
    // Resetting the mutation must not also swallow the failure.
    expect(result.current.error?.message).toBe("upstream rejected");
  });

  it("keeps a saved secret untouched while Secret stays ticked", async () => {
    mocks.headers.mockReturnValue(
      headersResult([
        serverHeader({
          id: "header-key",
          name: "X-Api-Key",
          value: "***",
          isSecret: true,
        }),
      ]),
    );
    const { result } = renderDrafts();

    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        isRequired: true,
      }),
    );
    await act(async () => {
      await result.current.save();
    });

    const form =
      mocks.update.mock.calls[0]?.[0]?.request?.updateServerHeaderForm;
    expect(form).toMatchObject({ id: "header-key", isSecret: true });
    expect(form).not.toHaveProperty("value");
  });

  it("asks for a new value before storing a saved secret as non-secret", async () => {
    // The server never reveals a stored secret as plain text, and the
    // redaction placeholder must never become the credential.
    mocks.headers.mockReturnValue(
      headersResult([
        serverHeader({
          id: "header-key",
          name: "X-Api-Key",
          value: "***",
          isSecret: true,
        }),
      ]),
    );
    const { result } = renderDrafts();

    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        isSecret: false,
      }),
    );

    expect(result.current.fieldErrors.get("header-key")).toEqual({
      field: "value",
      message: 'Enter a new value for "X-Api-Key" to store it as non-secret.',
    });
    await act(async () => {
      await expect(result.current.save()).resolves.toBe(false);
    });
    expect(mocks.update).not.toHaveBeenCalled();

    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        staticValue: "public-key",
      }),
    );
    expect(result.current.validationError).toBeNull();
    await act(async () => {
      await result.current.save();
    });
    expect(mocks.update.mock.calls[0]?.[0]).toMatchObject({
      request: {
        updateServerHeaderForm: {
          id: "header-key",
          isSecret: false,
          value: "public-key",
        },
      },
    });
  });

  it("refuses to write rows that would not validate", async () => {
    const { result } = renderDrafts();

    act(() => result.current.addHeader());

    expect(result.current.validationError).toBe("Every header needs a name.");
    await act(async () => {
      await expect(result.current.save()).resolves.toBe(false);
    });
    expect(mocks.create).not.toHaveBeenCalled();
  });
});

function renderTunneledDrafts(readOnly = false) {
  return renderHook(
    () =>
      useTunneledHeaderDrafts({ tunneledMcpServerId: "tunnel-1", readOnly }),
    { wrapper },
  );
}

function queryEnabled(mock: ReturnType<typeof vi.fn>): boolean {
  const options = mock.mock.calls.at(-1)?.[2] as { enabled?: boolean };
  return options.enabled === true;
}

describe("useHeaderDrafts for a tunneled source", () => {
  it("reads and writes only the tunneled headers", async () => {
    const { result } = renderTunneledDrafts();

    expect(queryEnabled(mocks.tunneledHeaders)).toBe(true);
    expect(mocks.headers).not.toHaveBeenCalled();

    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        name: "X-Jamf-Tenant",
        staticValue: "tenant-1",
        isSecret: false,
      }),
    );
    await act(async () => {
      await result.current.save();
    });

    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.tunneledCreate).toHaveBeenCalledTimes(1);
    expect(mocks.tunneledCreate.mock.calls[0]?.[0]).toMatchObject({
      request: {
        createTunneledMcpServerHeaderForm: {
          tunneledMcpServerId: "tunnel-1",
          name: "X-Jamf-Tenant",
          value: "tenant-1",
        },
      },
    });
    expect(mocks.tunneledInvalidate).toHaveBeenCalled();
    expect(mocks.invalidate).not.toHaveBeenCalled();
  });

  it("keeps a stored secret when its placeholder is untouched", async () => {
    mocks.tunneledHeaders.mockReturnValue(
      headersResult([
        serverHeader({
          id: "h1",
          name: "X-Api-Key",
          value: "***",
          isSecret: true,
        }),
      ]),
    );
    const { result } = renderTunneledDrafts();

    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        isRequired: true,
      }),
    );
    await act(async () => {
      await result.current.save();
    });

    const form =
      mocks.tunneledUpdate.mock.calls[0]?.[0]?.request
        ?.updateTunneledMcpServerHeaderForm;
    expect(form).toMatchObject({ id: "h1", isSecret: true, isRequired: true });
    expect(form).not.toHaveProperty("value");
  });

  it.each([
    ["X-Gram-Tunnel-Forward-Token", "static"],
    ["Gram-Key", "static"],
    ["Mcp-Session-Id", "static"],
    ["x_speakeasy_identity", "static"],
    ["Cookie", "static"],
  ] as const)("refuses the reserved name %s", (name, source) => {
    const { result } = renderTunneledDrafts();
    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        name,
        source,
        staticValue: "x",
      }),
    );
    expect(result.current.validationError).toContain("reserved");
  });

  it.each(["Authorization", "Gram-Chat-Session", "gram_key", "Cookie"])(
    "refuses passing %s through",
    (source) => {
      const { result } = renderTunneledDrafts();
      act(() => result.current.addHeader());
      act(() =>
        result.current.replaceHeader(0, {
          ...result.current.drafts[0]!,
          name: "X-Upstream-Token",
          source: "request",
          isSecret: false,
          valueFromRequestHeader: source,
        }),
      );
      expect(result.current.validationError).toContain(
        "cannot be passed through",
      );
    },
  );

  it("refuses a value with a line break", () => {
    const { result } = renderTunneledDrafts();
    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        name: "X-Tenant",
        staticValue: "a\r\nX-Injected: 1",
      }),
    );
    expect(result.current.validationError).toContain("line break");
  });

  it("treats an underscore name as a duplicate of its dashed form", () => {
    mocks.tunneledHeaders.mockReturnValue(
      headersResult([serverHeader({ id: "h1", name: "X-Tenant", value: "a" })]),
    );
    const { result } = renderTunneledDrafts();
    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(1, {
        ...result.current.drafts[1]!,
        name: "X_Tenant",
        staticValue: "b",
      }),
    );
    expect(result.current.validationError).toContain("Duplicate");
  });

  it.each(["Speakeasy-AI-Key", "Speakeasy-AI-Chat-Session"])(
    "refuses %s as a name and as a pass-through source",
    (name) => {
      const { result } = renderTunneledDrafts();
      act(() => result.current.addHeader());
      act(() =>
        result.current.replaceHeader(0, {
          ...result.current.drafts[0]!,
          name,
          staticValue: "x",
        }),
      );
      expect(result.current.validationError).toContain("reserved");
      act(() =>
        result.current.replaceHeader(0, {
          ...result.current.drafts[0]!,
          name: "X-Upstream-Token",
          source: "request",
          isSecret: false,
          valueFromRequestHeader: name,
        }),
      );
      expect(result.current.validationError).toContain(
        "cannot be passed through",
      );
    },
  );

  it("allows a static Authorization header", () => {
    const { result } = renderTunneledDrafts();
    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        name: "Authorization",
        staticValue: "Basic service",
      }),
    );
    expect(result.current.validationError).toBeNull();
  });

  it("locks editing when the headers fail to load", async () => {
    mocks.tunneledHeaders.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
      refetch: vi.fn(),
    });
    const { result } = renderTunneledDrafts();

    expect(result.current.loadError).toBe(true);
    expect(result.current.readOnly).toBe(true);
    act(() => result.current.addHeader());
    await act(async () => {
      expect(await result.current.save()).toBe(false);
    });
    expect(mocks.tunneledCreate).not.toHaveBeenCalled();
    expect(mocks.tunneledRemove).not.toHaveBeenCalled();
  });

  it("does not save when the caller cannot write", async () => {
    const { result } = renderTunneledDrafts(true);
    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        name: "X-Tenant",
        staticValue: "t",
      }),
    );
    await act(async () => {
      expect(await result.current.save()).toBe(false);
    });
    expect(mocks.tunneledCreate).not.toHaveBeenCalled();
  });

  it("discards unsaved rows, secrets included", () => {
    mocks.tunneledHeaders.mockReturnValue(
      headersResult([serverHeader({ id: "h1", name: "X-Tenant", value: "t" })]),
    );
    const { result } = renderTunneledDrafts();
    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(1, {
        ...result.current.drafts[1]!,
        name: "X-Api-Key",
        staticValue: "synthetic-secret",
      }),
    );
    expect(result.current.isDirty).toBe(true);

    act(() => result.current.discard());
    expect(result.current.isDirty).toBe(false);
    expect(result.current.drafts.map((draft) => draft.name)).toEqual([
      "X-Tenant",
    ]);
  });
});

describe("useHeaderDrafts recovers from a partial save", () => {
  function editRow(
    result: { current: ReturnType<typeof useTunneledHeaderDrafts> },
    index: number,
    changes: Partial<
      ReturnType<typeof useTunneledHeaderDrafts>["drafts"][number]
    >,
  ) {
    act(() =>
      result.current.replaceHeader(index, {
        ...result.current.drafts[index]!,
        ...changes,
      }),
    );
  }

  it("retries an update without repeating a delete that landed", async () => {
    mocks.tunneledHeaders.mockReturnValue(
      headersResult([
        serverHeader({ id: "a", name: "X-A", value: "1" }),
        serverHeader({ id: "b", name: "X-B", value: "1" }),
      ]),
    );
    mocks.tunneledUpdate.mockRejectedValueOnce(new Error("unavailable"));
    const { result } = renderTunneledDrafts();

    act(() => result.current.removeHeader(0));
    editRow(result, 0, { staticValue: "2" });
    await act(async () => {
      await expect(result.current.save()).rejects.toThrow("unavailable");
    });
    expect(mocks.tunneledRemove).toHaveBeenCalledTimes(1);
    expect(mocks.tunneledInvalidate).toHaveBeenCalled();
    expect(result.current.isDirty).toBe(true);

    await act(async () => {
      await result.current.save();
    });
    expect(mocks.tunneledRemove).toHaveBeenCalledTimes(1);
    expect(mocks.tunneledUpdate).toHaveBeenCalledTimes(2);
    expect(mocks.tunneledUpdate.mock.calls[1]?.[0]).toMatchObject({
      request: { updateTunneledMcpServerHeaderForm: { id: "b", value: "2" } },
    });
  });

  it("does not recreate a row that was created before a later failure", async () => {
    mocks.tunneledCreate
      .mockImplementationOnce(echoSaved)
      .mockRejectedValueOnce(new Error("unavailable"));
    const { result } = renderTunneledDrafts();

    act(() => result.current.addHeader());
    editRow(result, 0, { name: "X-Api-Key", staticValue: "synthetic-secret" });
    act(() => result.current.addHeader());
    editRow(result, 1, { name: "X-Tenant", staticValue: "t", isSecret: false });
    await act(async () => {
      await expect(result.current.save()).rejects.toThrow("unavailable");
    });
    // The secret that did land is shown redacted, with its server id.
    expect(result.current.drafts[0]).toMatchObject({
      id: expect.any(String),
      staticValue: "***",
    });

    await act(async () => {
      await result.current.save();
    });
    const createdNames = mocks.tunneledCreate.mock.calls.map(
      (call) => call[0].request.createTunneledMcpServerHeaderForm.name,
    );
    expect(createdNames).toEqual(["X-Api-Key", "X-Tenant", "X-Tenant"]);
  });

  it("does not replay a create when the refresh after it failed", async () => {
    mocks.tunneledHeaders.mockReturnValue({
      data: { headers: [] },
      isLoading: false,
      isError: false,
      refetch: vi.fn().mockResolvedValue({ isError: true, data: undefined }),
    });
    const { result } = renderTunneledDrafts();

    act(() => result.current.addHeader());
    editRow(result, 0, { name: "X-Tenant", staticValue: "t", isSecret: false });
    await act(async () => {
      await result.current.save();
    });
    await act(async () => {
      await result.current.save();
    });
    expect(mocks.tunneledCreate).toHaveBeenCalledTimes(1);
    expect(result.current.drafts[0]?.id).toBeDefined();
  });
});
