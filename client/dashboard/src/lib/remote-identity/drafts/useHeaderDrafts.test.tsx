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
}));

let queryClient: QueryClient;

vi.mock("@gram/client/react-query/remoteMcpServerHeaders.js", () => ({
  useRemoteMcpServerHeaders: () => mocks.headers(),
  invalidateAllRemoteMcpServerHeaders: () => mocks.invalidate(),
}));
vi.mock("@gram/client/react-query/createRemoteMcpServerHeader.js", () => ({
  useCreateRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.create,
    isPending: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/updateRemoteMcpServerHeader.js", () => ({
  useUpdateRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.update,
    isPending: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/deleteRemoteMcpServerHeader.js", () => ({
  useDeleteRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.remove,
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
