import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  canRead: true,
  canWrite: false,
  data: undefined as { id: string } | undefined,
  error: null as unknown,
  mutate: vi.fn(),
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
      scope === "project:read" ? state.canRead : state.canWrite,
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
}));
vi.mock("@gram/client/react-query/ensureManagedAssistant.js", () => ({
  useEnsureManagedAssistantMutation: () => ({ mutate: state.mutate }),
}));
vi.mock("@/lib/ServerAssistantTransport", () => ({
  createServerAssistantTransport: () => () => ({}),
}));
vi.mock("@/lib/route-errors", () => ({
  isNotFoundError: (error: unknown) => error === 404,
}));
import { useServerAssistantTransport } from "./useServerAssistantTransport";

beforeEach(() => {
  vi.clearAllMocks();
  state.canRead = true;
  state.canWrite = false;
  state.data = { id: "assistant-a" };
  state.error = null;
});
describe("managed assistant authorization", () => {
  it("hides a cached assistant immediately when read access is revoked", () => {
    const { result, rerender } = renderHook(() =>
      useServerAssistantTransport("example", true),
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

  it("clears provisioned IDs and ignores provisioning completed after revocation", () => {
    state.data = undefined;
    state.error = 404;
    state.canWrite = true;
    const { result, rerender } = renderHook(() =>
      useServerAssistantTransport("example", true),
    );
    const firstRequest = state.mutate.mock.lastCall?.[1];
    act(() => {
      firstRequest.onSuccess({ id: "provisioned-a" });
    });
    expect(result.current.assistantId).toBe("provisioned-a");
    state.canRead = false;
    rerender();
    act(() => {
      firstRequest.onSuccess({ id: "late-assistant" });
    });
    expect(result.current.ready).toBe(false);
    state.canRead = true;
    rerender();
    expect(result.current.assistantId).toBe("");
    expect(state.mutate).toHaveBeenCalledTimes(2);
    act(() => {
      firstRequest.onSuccess({ id: "stale-assistant" });
    });
    expect(result.current.assistantId).toBe("");
  });

  it("does not provision from a cached 404 without read access", () => {
    state.data = undefined;
    state.error = 404;
    state.canRead = false;
    state.canWrite = true;
    const { result } = renderHook(() =>
      useServerAssistantTransport("example", true),
    );
    expect(result.current.ready).toBe(false);
    expect(state.mutate).not.toHaveBeenCalled();
  });
});
