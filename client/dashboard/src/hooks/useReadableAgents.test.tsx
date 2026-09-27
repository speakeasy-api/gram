import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { useReadableAgents } from "./useReadableAgents";

const mocks = vi.hoisted(() => ({ userId: "first-user", list: vi.fn() }));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "same-organization" }),
  useSession: () => ({ user: { id: mocks.userId } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ agents: { list: mocks.list } }),
}));
afterEach(cleanup);

it("does not reuse readable names or links after switching users in one organization", async () => {
  mocks.userId = "first-user";
  mocks.list.mockResolvedValueOnce([
    { id: "private-agent", name: "Private agent" },
  ]);
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const { result, rerender } = renderHook(() => useReadableAgents(true), {
    wrapper,
  });
  await waitFor(() =>
    expect(result.current.data?.[0]?.name).toBe("Private agent"),
  );
  mocks.userId = "second-user";
  mocks.list.mockImplementationOnce(() => new Promise(() => {}));
  rerender();
  expect(result.current.data).toBeUndefined();
  expect(mocks.list).toHaveBeenCalledTimes(2);
});
