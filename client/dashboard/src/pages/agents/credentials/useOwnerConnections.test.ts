import { act, cleanup, fireEvent, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useOAuthPopup } from "@/components/sessions/useOAuthPopup";
import { connectionSelectable } from "./ownerConnections";
import { useOwnerConnections } from "./useOwnerConnections";
import type { InventoryServer } from "./useServerInventory";

const mocks = vi.hoisted(() => {
  const personalClient = { clientType: "remote_session", enabled: true };
  return {
    refetch: vi.fn(),
    personalClient,
    sharedClient: { clientType: "static_credential", enabled: true },
    // Each server's own auth read, keyed by server id. A server left out
    // offers each person's own sign-in.
    auth: new Map<string, { data?: unknown; isError?: boolean }>(),
    clients: [] as unknown[],
    sessions: [] as unknown[],
  };
});
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
  useSession: () => ({ user: { id: "user_example" } }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@gram/client/react-query/mcpServerAuthentication.js", () => ({
  queryKeyMcpServerAuthentication: (parameters: { mcpServerId: string }) => [
    "auth",
    parameters.mcpServerId,
  ],
}));
vi.mock("@tanstack/react-query", () => ({
  useQueries: ({ queries }: { queries: Array<{ queryKey: unknown[] }> }) =>
    queries.map(({ queryKey }) => {
      if (queryKey[0] === "auth")
        return {
          ...(mocks.auth.get(queryKey[1] as string) ?? {
            data: { status: "configured", clients: [mocks.personalClient] },
          }),
          refetch: mocks.refetch,
        };
      return {
        data: queryKey[1] === "clients" ? mocks.clients : mocks.sessions,
        refetch: mocks.refetch,
      };
    }),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.auth.clear();
  mocks.clients = [];
  mocks.sessions = [];
});

// The org-wide issuer lists a personal sign-in that belongs to another server.
const otherServersSignIn = { id: "client_other", credentialOwner: "subject" };
const server = {
  id: "server_example",
  projectSlug: "default",
  issuerId: "issuer_example",
} as InventoryServer;

it("treats a server whose own auth offers only a shared credential as shared", () => {
  mocks.clients = [otherServersSignIn];
  mocks.auth.set(server.id, {
    data: { status: "configured", clients: [mocks.sharedClient] },
  });
  const { result } = renderHook(() => useOwnerConnections([server], true));
  const connection = result.current.byServer.get(server.id);
  expect(connection).toEqual({ status: "not-required" });
  expect(connectionSelectable(connection)).toBe(true);
});

it("still asks for a connection when the server offers each person's sign-in", () => {
  mocks.clients = [otherServersSignIn];
  mocks.auth.set(server.id, {
    data: {
      status: "configured",
      clients: [mocks.sharedClient, mocks.personalClient],
    },
  });
  const { result } = renderHook(() => useOwnerConnections([server], true));
  const connection = result.current.byServer.get(server.id);
  expect(connection).toEqual({ status: "missing" });
  expect(connectionSelectable(connection)).toBe(false);
});

it("keeps a server locked while its auth is read or when the read fails", () => {
  const failing = { ...server, id: "server_failing" };
  mocks.clients = [otherServersSignIn];
  mocks.auth.set(server.id, {});
  mocks.auth.set(failing.id, { isError: true });
  const { result } = renderHook(() =>
    useOwnerConnections([server, failing], true),
  );
  expect(result.current.byServer.get(server.id)).toEqual({
    status: "checking",
  });
  expect(result.current.byServer.get(failing.id)).toEqual({ status: "error" });
  expect(connectionSelectable(result.current.byServer.get(failing.id))).toBe(
    false,
  );
});

it("does not trust a cached shared answer once its re-read fails", () => {
  mocks.clients = [otherServersSignIn];
  mocks.auth.set(server.id, {
    data: { status: "configured", clients: [mocks.sharedClient] },
    isError: true,
  });
  const { result } = renderHook(() =>
    useOwnerConnections([server], true, "agent_example"),
  );
  expect(result.current.byServer.get(server.id)).toEqual({ status: "error" });
  expect(result.current.assignedByServer?.get(server.id)).toEqual({
    status: "error",
  });
});

it("shows a shared server as needing no assignment when editing", () => {
  mocks.clients = [otherServersSignIn];
  mocks.auth.set(server.id, {
    data: { status: "configured", clients: [mocks.sharedClient] },
  });
  const { result } = renderHook(() =>
    useOwnerConnections([server], true, "agent_example"),
  );
  expect(result.current.assignedByServer?.get(server.id)).toEqual({
    status: "not-required",
  });
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
