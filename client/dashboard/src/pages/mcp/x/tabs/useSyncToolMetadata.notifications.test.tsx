import type { ProxiedMcpTool } from "@/hooks/useProxiedMcpTools";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
} from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useSyncToolMetadata } from "./useSyncToolMetadata";

// The automatic pass's mutation runs through a real React Query mutation, so
// the client's default error handler is in play exactly as in the dashboard.
const mocks = vi.hoisted(() => ({
  apiAdd: vi.fn<(vars: unknown) => Promise<unknown>>(),
  defaultOnError: vi.fn<(error: Error) => void>(),
  handleAPIError: vi.fn<(error: unknown, message: string) => void>(),
  refresh: vi.fn<() => Promise<void>>(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));

vi.mock("@/lib/errors", () => ({ handleAPIError: mocks.handleAPIError }));

vi.mock("@gram/client/react-query/listMcpServerToolMetadata.js", () => ({
  invalidateAllListMcpServerToolMetadata: mocks.refresh,
}));

vi.mock("@gram/client/react-query/addMcpServerToolMetadataBatch.js", () => ({
  useAddMcpServerToolMetadataBatchMutation: (options: object) =>
    useMutation({ mutationFn: mocks.apiAdd, ...options }),
}));

vi.mock("@gram/client/react-query/setMcpServerToolMetadataBatch.js", () => ({
  useSetMcpServerToolMetadataBatchMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/setMcpServerToolMetadata.js", () => ({
  useSetMcpServerToolMetadataMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
    variables: undefined,
  }),
}));

vi.mock("@gram/client/react-query/deleteMcpServerToolMetadata.js", () => ({
  useDeleteMcpServerToolMetadataMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
    variables: undefined,
  }),
}));

function gramError(statusCode: number): GramError {
  const error = Object.create(GramError.prototype) as GramError;
  Object.defineProperty(error, "statusCode", { value: statusCode });
  return error;
}

function renderWithClient({ enabled = true }: { enabled?: boolean } = {}) {
  // Mirrors the dashboard client's default: every failed mutation notifies.
  const client = new QueryClient({
    defaultOptions: { mutations: { onError: mocks.defaultOnError } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const live: Record<string, ProxiedMcpTool> = {
    lock_device: { annotations: { idempotentHint: true } },
  };
  return renderHook(
    () =>
      useSyncToolMetadata({
        mcpServerId: "srv-1",
        live,
        listedAt: 1,
        stored: {},
        enabled,
        mode: "additive",
        project: { id: "proj-1", slug: "default" },
      }),
    { wrapper },
  );
}

afterEach(() => {
  vi.resetAllMocks();
});

describe("useSyncToolMetadata automatic recording notifications", () => {
  it("refreshes silently when another session recorded the tools first", async () => {
    mocks.apiAdd.mockRejectedValue(gramError(409));
    mocks.refresh.mockResolvedValue(undefined);
    renderWithClient();
    await act(async () => {});

    expect(mocks.apiAdd).toHaveBeenCalledOnce();
    expect(mocks.refresh).toHaveBeenCalled();
    expect(mocks.defaultOnError).not.toHaveBeenCalled();
    expect(mocks.handleAPIError).not.toHaveBeenCalled();
  });

  it("reports any other failure exactly once", async () => {
    mocks.apiAdd.mockRejectedValue(gramError(500));
    renderWithClient();
    await act(async () => {});

    expect(mocks.defaultOnError).not.toHaveBeenCalled();
    expect(mocks.handleAPIError).toHaveBeenCalledOnce();
  });

  it("refreshes silently when a tool the user records was already recorded", async () => {
    mocks.apiAdd.mockRejectedValue(gramError(409));
    mocks.refresh.mockResolvedValue(undefined);
    const { result } = renderWithClient({ enabled: false });

    await act(async () => {
      result.current.toolActions?.record("lock_device");
    });

    expect(mocks.apiAdd).toHaveBeenCalledOnce();
    expect(mocks.refresh).toHaveBeenCalled();
    expect(mocks.defaultOnError).not.toHaveBeenCalled();
    expect(mocks.handleAPIError).not.toHaveBeenCalled();
  });

  it("reports any other failure of a tool the user records", async () => {
    mocks.apiAdd.mockRejectedValue(gramError(500));
    mocks.refresh.mockResolvedValue(undefined);
    const { result } = renderWithClient({ enabled: false });

    await act(async () => {
      result.current.toolActions?.record("lock_device");
    });

    expect(mocks.defaultOnError).not.toHaveBeenCalled();
    expect(mocks.handleAPIError).toHaveBeenCalledExactlyOnceWith(
      expect.anything(),
      "Failed to record tool metadata",
    );
  });
});
