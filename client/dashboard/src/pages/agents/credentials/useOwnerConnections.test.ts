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
  let finishSessions: () => void = () => {};
  let finishClients: () => void = () => {};
  const sessionsPending = new Promise<void>((resolve) => {
    finishSessions = resolve;
  });
  const clientsPending = new Promise<void>((resolve) => {
    finishClients = resolve;
  });
  mocks.refetch
    .mockReturnValueOnce(sessionsPending)
    .mockReturnValueOnce(clientsPending)
    .mockResolvedValue(undefined);
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
    finishSessions();
  });
  fireEvent(window, new Event("focus"));
  expect(mocks.refetch).toHaveBeenCalledTimes(2);
  await act(async () => {
    finishClients();
  });
  fireEvent(window, new Event("focus"));
  expect(mocks.refetch).toHaveBeenCalledTimes(4);
  await act(async () => {
    await Promise.resolve();
  });
});
