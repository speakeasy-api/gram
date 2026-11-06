import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  canRead: true,
  canWrite: false,
  data: undefined as { id: string } | undefined,
  error: null as unknown,
  mutateAsync: vi.fn(),
  setManagedData: vi.fn(),
  getManaged: vi.fn(),
  client: {},
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    slug: "example",
    projects: [{ id: "project-a", slug: "example" }],
  }),
  useSession: () => ({ session: "session" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) =>
      scope === "assistant:read" ? state.canRead : state.canWrite,
    isLoading: false,
  }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => state.client,
}));
vi.mock("@gram/client/react-query/assistantsGetManaged.js", () => ({
  useAssistantsGetManaged: (...args: unknown[]) => {
    state.getManaged(...args);
    return { data: state.data, error: state.error };
  },
  setAssistantsGetManagedData: (...args: unknown[]) =>
    state.setManagedData(...args),
}));
vi.mock("@gram/client/react-query/ensureManagedAssistant.js", () => ({
  useEnsureManagedAssistantMutation: () => ({
    mutateAsync: state.mutateAsync,
  }),
}));
vi.mock("@/lib/ServerAssistantTransport", () => ({
  createServerAssistantTransport: () => () => ({}),
}));
vi.mock("@/lib/route-errors", () => ({
  isNotFoundError: (error: unknown) => error === 404,
}));
import { useServerAssistantTransport } from "./useServerAssistantTransport";

type Deferred = {
  resolve: (value: { id: string }) => void;
  reject: (reason: unknown) => void;
};

// Each ensure call returns a promise the test settles by hand.
function deferEnsures(): Deferred[] {
  const calls: Deferred[] = [];
  state.mutateAsync.mockImplementation(
    () =>
      new Promise((resolve, reject) => {
        calls.push({ resolve, reject });
      }),
  );
  return calls;
}

let queryClient: QueryClient;
function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  queryClient = new QueryClient();
  state.canRead = true;
  state.canWrite = false;
  state.data = { id: "assistant-a" };
  state.error = null;
});
describe("managed assistant authorization", () => {
  it("hides a cached assistant immediately when read access is revoked", () => {
    const { result, rerender } = renderHook(
      () => useServerAssistantTransport("example", true),
      { wrapper },
    );
    expect(result.current.ready).toBe(true);
    state.canRead = false;
    rerender();
    expect(result.current).toMatchObject({
      assistantId: "",
      ready: false,
      transport: undefined,
      needsAdmin: false,
    });
    expect(state.getManaged.mock.lastCall?.[2].enabled).toBe(false);
  });

  it("clears provisioned IDs and ignores provisioning completed after revocation", async () => {
    state.data = undefined;
    state.error = 404;
    state.canWrite = true;
    const ensures = deferEnsures();
    const { result, rerender } = renderHook(
      () => useServerAssistantTransport("example", true),
      { wrapper },
    );
    expect(ensures).toHaveLength(1);
    await act(async () => {
      ensures[0]!.resolve({ id: "provisioned-a" });
    });
    expect(result.current.assistantId).toBe("provisioned-a");

    state.canRead = false;
    rerender();
    expect(result.current.ready).toBe(false);

    state.canRead = true;
    rerender();
    expect(result.current.assistantId).toBe("");
    expect(ensures).toHaveLength(2);
    // Revoked mid-flight: the late result must not surface.
    state.canRead = false;
    rerender();
    await act(async () => {
      ensures[1]!.resolve({ id: "late-assistant" });
    });
    expect(result.current.ready).toBe(false);
  });

  it("fires one ensure for every instance on the same 404 and seeds the shared read", async () => {
    state.data = undefined;
    state.error = 404;
    state.canWrite = true;
    const ensures = deferEnsures();
    const hooks = [1, 2, 3].map(() =>
      renderHook(() => useServerAssistantTransport("example", true), {
        wrapper,
      }),
    );
    expect(state.mutateAsync).toHaveBeenCalledTimes(1);

    await act(async () => {
      ensures[0]!.resolve({ id: "provisioned-a" });
    });
    for (const { result } of hooks) {
      expect(result.current.assistantId).toBe("provisioned-a");
    }
    expect(state.setManagedData).toHaveBeenCalledTimes(1);
    expect(state.setManagedData).toHaveBeenCalledWith(
      queryClient,
      [{ gramSession: undefined, gramProject: "example" }],
      { id: "provisioned-a" },
    );
  });

  it("lets a later mount retry after a failed ensure", async () => {
    state.data = undefined;
    state.error = 404;
    state.canWrite = true;
    const ensures = deferEnsures();
    const first = renderHook(
      () => useServerAssistantTransport("example", true),
      { wrapper },
    );
    await act(async () => {
      ensures[0]!.reject(new Error("boom"));
    });
    expect(first.result.current.error).not.toBeNull();
    expect(state.setManagedData).not.toHaveBeenCalled();

    renderHook(() => useServerAssistantTransport("example", true), {
      wrapper,
    });
    expect(state.mutateAsync).toHaveBeenCalledTimes(2);
  });

  it("does not provision from a cached 404 without read access", () => {
    state.data = undefined;
    state.error = 404;
    state.canRead = false;
    state.canWrite = true;
    const { result } = renderHook(
      () => useServerAssistantTransport("example", true),
      { wrapper },
    );
    expect(result.current.ready).toBe(false);
    expect(state.mutateAsync).not.toHaveBeenCalled();
  });
});
