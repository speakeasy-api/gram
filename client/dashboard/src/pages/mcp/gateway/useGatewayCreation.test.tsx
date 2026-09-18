import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useGatewayCreation } from "./useGatewayCreation";

const api = vi.hoisted(() => ({
  add: vi.fn(),
  list: vi.fn(),
  navigate: vi.fn(),
  invalidate: vi.fn(),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    metaMcp: { addMember: api.add, listMembers: api.list },
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { gateway: { overview: { href: (id: string) => `/gateway/${id}` } } },
  }),
}));
vi.mock("react-router", async (original) => ({
  ...(await original<typeof import("react-router")>()),
  useNavigate: () => api.navigate,
}));
vi.mock("@gram/client/react-query/metaMcpMembers.js", () => ({
  invalidateAllMetaMcpMembers: api.invalidate,
}));
beforeEach(() => {
  vi.resetAllMocks();
  api.list.mockResolvedValue({ members: [] });
  api.add.mockResolvedValue({});
});
afterEach(cleanup);
function setup(search = "?attachToGateway=gateway") {
  const client = new QueryClient();
  return renderHook(() => useGatewayCreation(), {
    wrapper: ({ children }) => (
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[`/create${search}`]}>
          {children}
        </MemoryRouter>
      </QueryClientProvider>
    ),
  });
}
it("attaches the created server, invalidates membership and returns to the gateway", async () => {
  const { result } = setup();
  await act(() => result.current.complete("server"));
  expect(api.add).toHaveBeenCalledWith({
    addMetaMcpMemberForm: {
      metaMcpServerId: "gateway",
      mcpServerId: "server",
      sortOrder: 0,
    },
  });
  expect(api.invalidate).toHaveBeenCalledTimes(1);
  expect(api.navigate).toHaveBeenCalledWith("/gateway/gateway");
});
it("preserves creation after failure and retries only attachment", async () => {
  api.add.mockRejectedValueOnce(new Error("offline"));
  const { result } = setup();
  await act(async () => {
    await expect(result.current.complete("server")).rejects.toThrow();
  });
  expect(result.current.createdServerId).toBe("server");
  expect(result.current.attachmentError).toContain("created");
  expect(api.navigate).not.toHaveBeenCalled();
  await act(() => result.current.retry());
  expect(api.add).toHaveBeenCalledTimes(2);
  expect(result.current.attachmentError).toBeNull();
  expect(api.navigate).toHaveBeenCalledTimes(1);
});
it("reconciles a lost write response as success", async () => {
  api.add.mockRejectedValueOnce(new Error("lost response"));
  api.list
    .mockResolvedValueOnce({ members: [] })
    .mockResolvedValueOnce({ members: [{ mcpServerId: "server" }] });
  const { result } = setup();
  await act(() => result.current.complete("server"));
  expect(api.add).toHaveBeenCalledTimes(1);
  expect(api.navigate).toHaveBeenCalledTimes(1);
});
it("does not duplicate membership already confirmed on retry", async () => {
  api.add.mockRejectedValueOnce(new Error("offline"));
  const { result } = setup();
  await act(async () => {
    await expect(result.current.complete("server")).rejects.toThrow();
  });
  api.list.mockResolvedValue({ members: [{ mcpServerId: "server" }] });
  await act(() => result.current.retry());
  expect(api.add).toHaveBeenCalledTimes(1);
  expect(api.navigate).toHaveBeenCalledTimes(1);
});
it("cancel returns without attaching; standalone does nothing", async () => {
  const gateway = setup();
  act(() => {
    expect(gateway.result.current.cancel()).toBe(true);
  });
  expect(api.navigate).toHaveBeenCalledTimes(1);
  const standalone = setup("");
  act(() => {
    expect(standalone.result.current.cancel()).toBe(false);
  });
  await act(() => standalone.result.current.complete("server"));
  expect(api.add).not.toHaveBeenCalled();
  expect(api.navigate).toHaveBeenCalledTimes(1);
});
it("serializes concurrent attachment attempts", async () => {
  let release!: () => void;
  api.add.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        release = resolve;
      }),
  );
  const { result } = setup();
  await act(async () => {
    const first = result.current.complete("server");
    const second = result.current.complete("server");
    await waitFor(() => expect(api.add).toHaveBeenCalledTimes(1));
    release();
    await Promise.all([first, second]);
  });
  expect(api.add).toHaveBeenCalledTimes(1);
});
it("does not write when membership cannot be read", async () => {
  api.list.mockRejectedValue(new Error("offline"));
  const { result } = setup();
  await act(async () => {
    await expect(result.current.complete("server")).rejects.toThrow();
  });
  expect(api.add).not.toHaveBeenCalled();
  expect(api.navigate).not.toHaveBeenCalled();
  expect(result.current.createdServerId).toBe("server");
});
it("retries a failed cache refresh without duplicating attachment", async () => {
  api.invalidate.mockRejectedValueOnce(new Error("refresh failed"));
  const { result } = setup();
  await act(async () => {
    await expect(result.current.complete("server")).rejects.toThrow();
  });
  api.list.mockResolvedValue({ members: [{ mcpServerId: "server" }] });
  await act(() => result.current.retry());
  expect(api.add).toHaveBeenCalledTimes(1);
  expect(api.navigate).toHaveBeenCalledTimes(1);
});
it("does not attach when source creation finishes after cancellation", async () => {
  const { result } = setup();
  act(() => {
    result.current.cancel();
  });
  await act(() => result.current.complete("server"));
  expect(api.add).not.toHaveBeenCalled();
  expect(api.navigate).toHaveBeenCalledTimes(1);
});
it("does not attach when cancelled during the membership read", async () => {
  let release!: (value: { members: [] }) => void;
  api.list.mockImplementation(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  const { result } = setup();
  await act(async () => {
    const pending = result.current.complete("server");
    result.current.cancel();
    release({ members: [] });
    await pending;
  });
  expect(api.add).not.toHaveBeenCalled();
  expect(api.navigate).toHaveBeenCalledTimes(1);
});

it("appends after the highest fetched sort order on each attempt", async () => {
  api.list.mockResolvedValue({
    members: [
      { mcpServerId: "other", sortOrder: 7 },
      { mcpServerId: "first", sortOrder: 2 },
    ],
  });
  api.add.mockRejectedValueOnce(new Error("offline"));
  const { result } = setup();
  await act(async () => {
    await expect(result.current.complete("server")).rejects.toThrow("offline");
  });
  expect(api.add).toHaveBeenLastCalledWith({
    addMetaMcpMemberForm: {
      metaMcpServerId: "gateway",
      mcpServerId: "server",
      sortOrder: 8,
    },
  });
  api.list.mockResolvedValue({
    members: [{ mcpServerId: "other", sortOrder: 12 }],
  });
  await act(() => result.current.retry());
  expect(api.add).toHaveBeenLastCalledWith({
    addMetaMcpMemberForm: {
      metaMcpServerId: "gateway",
      mcpServerId: "server",
      sortOrder: 13,
    },
  });
});
