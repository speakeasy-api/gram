import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useHeaderDrafts } from "./useHeaderDrafts";

const mocks = vi.hoisted(() => ({
  headers: vi.fn(),
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
  useRemoteMcpServerHeaders: () => mocks.headers(),
  invalidateAllRemoteMcpServerHeaders: () => mocks.invalidate(),
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

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  mocks.headers.mockReturnValue(headersResult([]));
  mocks.invalidate.mockResolvedValue(undefined);
  mocks.create.mockResolvedValue(undefined);
  mocks.update.mockResolvedValue(undefined);
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

function renderDraftsWithoutIdentity() {
  return renderHook(
    () =>
      useHeaderDrafts({
        remoteMcpServerId: "remote-source-1",
        identity: { mode: "none", managed: null, isError: false },
      }),
    { wrapper },
  );
}

/** A saved row reading a header the remote header policy now refuses. */
function refusedPassThrough(
  overrides: Partial<RemoteMcpServerHeader> = {},
): RemoteMcpServerHeader {
  return serverHeader({
    id: "header-forwarded",
    name: "X-Upstream-Token",
    value: undefined,
    valueFromRequestHeader: "Authorization",
    isRequired: true,
    ...overrides,
  });
}

describe("useHeaderDrafts with rows the remote header policy refuses", () => {
  it("saves an unrelated row beside an untouched refused row", async () => {
    mocks.headers.mockReturnValue(
      headersResult([
        refusedPassThrough(),
        refusedPassThrough({
          id: "header-authorization",
          name: "Authorization",
          valueFromRequestHeader: "Authorization",
        }),
      ]),
    );
    const { result } = renderDraftsWithoutIdentity();

    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(2, {
        ...result.current.drafts[2]!,
        name: "X-Api-Key",
        staticValue: "sk-test",
      }),
    );
    expect(result.current.validationError).toBeNull();

    await act(async () => {
      await result.current.save();
    });

    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("refuses any edit that keeps the refused source", () => {
    mocks.headers.mockReturnValue(headersResult([refusedPassThrough()]));
    const { result } = renderDraftsWithoutIdentity();

    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        isRequired: false,
      }),
    );

    expect(result.current.validationError).toBe(
      'Change the source or name of "X-Upstream-Token", or remove this header.',
    );
    expect(result.current.fieldErrors.get("header-forwarded")?.field).toBe(
      "value",
    );
  });

  it("writes a repair to a separately supplied header", async () => {
    mocks.headers.mockReturnValue(headersResult([refusedPassThrough()]));
    const { result } = renderDraftsWithoutIdentity();

    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        valueFromRequestHeader: "X-Client-Upstream-Token",
      }),
    );
    expect(result.current.validationError).toBeNull();

    await act(async () => {
      await result.current.save();
    });

    expect(mocks.update.mock.calls[0]?.[0]).toMatchObject({
      request: {
        updateServerHeaderForm: {
          id: "header-forwarded",
          valueFromRequestHeader: "X-Client-Upstream-Token",
        },
      },
    });
  });

  it("deletes one refused row while another stays", async () => {
    mocks.headers.mockReturnValue(
      headersResult([
        refusedPassThrough(),
        refusedPassThrough({
          id: "header-key",
          name: "X-Key",
          valueFromRequestHeader: "Gram-Key",
        }),
      ]),
    );
    const { result } = renderDraftsWithoutIdentity();

    act(() => result.current.removeHeader(0));
    expect(result.current.validationError).toBeNull();

    await act(async () => {
      await result.current.save();
    });

    expect(mocks.remove).toHaveBeenCalledWith({
      request: { id: "header-forwarded" },
    });
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("refuses a new row reading Gram-Key and allows one reading a custom header", () => {
    const { result } = renderDraftsWithoutIdentity();

    act(() => result.current.addHeader());
    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        name: "X-Upstream-Token",
        source: "request",
        isSecret: false,
        valueFromRequestHeader: "Gram-Key",
      }),
    );
    expect(result.current.validationError).toContain(
      'Speakeasy does not forward "Gram-Key"',
    );

    act(() =>
      result.current.replaceHeader(0, {
        ...result.current.drafts[0]!,
        valueFromRequestHeader: "X-Service-Token",
      }),
    );
    expect(result.current.validationError).toBeNull();
  });

  it("still asks for a saved Authorization row to go when identity owns the name", () => {
    mocks.headers.mockReturnValue(
      headersResult([
        refusedPassThrough({
          id: "header-authorization",
          name: "Authorization",
        }),
      ]),
    );
    const { result } = renderDrafts();

    expect(result.current.validationError).not.toBeNull();
  });
});
