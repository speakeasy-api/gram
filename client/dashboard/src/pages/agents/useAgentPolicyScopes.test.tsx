import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAgentPolicyScopes } from "./useAgentPolicyScopes";

const mocks = vi.hoisted(() => ({ load: vi.fn() }));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/listAgentPolicyScopes.core.js", () => ({
  buildListAgentPolicyScopesQuery: () => ({
    queryKey: ["agent-policy-scopes"],
    queryFn: mocks.load,
  }),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("useAgentPolicyScopes", () => {
  it("reports a failed catalogue read inline instead of throwing to the route boundary", async () => {
    // The app's default throws every error but 401/403 to the nearest error
    // boundary; this read never returns 403, so a 5xx would otherwise take
    // the whole Permissions tab down with it.
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, throwOnError: true } },
    });
    mocks.load.mockRejectedValue(new Error("Internal Server Error"));
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useAgentPolicyScopes(), { wrapper });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.isLoading).toBe(false);
    expect(result.current.scopes).toEqual([]);
    expect(result.current.catalogue.size).toBe(0);
    client.clear();
  });
});
