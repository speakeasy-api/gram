import { act, cleanup, fireEvent, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useOAuthPopup } from "@/components/sessions/useOAuthPopup";
import { useOwnerConnections } from "./useOwnerConnections";
import type { InventoryServer } from "./useServerInventory";

const mocks = vi.hoisted(() => ({ refetch: vi.fn() }));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
  useSession: () => ({ user: { id: "user_example" } }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@tanstack/react-query", () => ({
  useQueries: ({ queries }: { queries: unknown[] }) =>
    queries.map(() => ({ data: [], refetch: mocks.refetch })),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("keeps focus refreshes in flight until all owner reads finish", async () => {
  let finish: () => void = () => {};
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  mocks.refetch.mockReturnValue(pending);
  const server = {
    id: "server_example",
    projectSlug: "default",
    issuerId: "issuer_example",
  } as InventoryServer;
  renderHook(() => {
    const connections = useOwnerConnections([server], true);
    useOAuthPopup(connections.refetch);
  });
  fireEvent(window, new Event("focus"));
  await act(async () => {
    await Promise.resolve();
  });
  fireEvent(document, new Event("visibilitychange"));
  expect(mocks.refetch).toHaveBeenCalledTimes(2);
  await act(async () => {
    finish();
  });
  fireEvent(window, new Event("focus"));
  expect(mocks.refetch).toHaveBeenCalledTimes(4);
  await act(async () => {
    await Promise.resolve();
  });
});
