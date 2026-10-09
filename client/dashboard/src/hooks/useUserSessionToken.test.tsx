import {
  act,
  cleanup,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PropsWithChildren } from "react";
import { ErrorBoundary } from "react-error-boundary";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useUserSessionToken } from "./useUserSessionToken";

const mint = vi.hoisted(() => vi.fn());
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project", slug: "project" }),
  useSession: () => ({ user: { id: "user" }, session: "session" }),
}));
vi.mock("@gram/client/react-query/mintUserSession.js", () => ({
  useMintUserSessionMutation: () => ({ mutateAsync: mint }),
}));
const clients: QueryClient[] = [];
const options = {
  target: { kind: "mcpServer" as const, id: "S" },
  userSessionIssuerId: "issuer-S",
};
function mount(
  props: Parameters<typeof useUserSessionToken>[0] = options,
  throwOnError = true,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, throwOnError } },
  });
  clients.push(client);
  const wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={client}>
      <ErrorBoundary fallback={<div>Boundary</div>}>{children}</ErrorBoundary>
    </QueryClientProvider>
  );
  return {
    ...renderHook((value) => useUserSessionToken(value), {
      initialProps: props,
      wrapper,
    }),
    client,
  };
}
beforeEach(() => {
  mint.mockReset();
  mint.mockResolvedValue({ accessToken: "token-S" });
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  vi.restoreAllMocks();
});
describe("useUserSessionToken error policy and retry", () => {
  it("inherits the shared error boundary by default", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    mint.mockRejectedValue(new Error("mint failed"));
    mount();
    await waitFor(() => expect(screen.getByText("Boundary")).toBeTruthy());
  });
  it.each([true, false])(
    "explicit false keeps errors inline with default %s",
    async (defaultValue) => {
      mint.mockRejectedValueOnce(new Error("mint failed"));
      const { result } = mount(
        { ...options, throwOnError: false },
        defaultValue,
      );
      await waitFor(() => expect(result.current.isError).toBe(true));
      expect(screen.queryByText("Boundary")).toBeNull();
      act(() => {
        result.current.refetch();
        result.current.refetch();
      });
      await waitFor(() => expect(result.current.accessToken).toBe("token-S"));
      expect(mint).toHaveBeenCalledTimes(2);
      expect(result.current.isError).toBe(false);
    },
  );
  it("preserves an inherited false default without an override", async () => {
    mint.mockRejectedValue(new Error("mint failed"));
    const { result } = mount(options, false);
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(screen.queryByText("Boundary")).toBeNull();
  });
  it.each([
    {
      target: { kind: "mcpServer" as const, id: undefined },
      userSessionIssuerId: "issuer-S",
    },
    {
      target: { kind: "mcpServer" as const, id: "S" },
      userSessionIssuerId: undefined,
    },
  ])("manual retry cannot bypass the mint gate: %j", async (props) => {
    const { result } = mount(props);
    await act(async () => {
      result.current.refetch();
    });
    expect(mint).not.toHaveBeenCalled();
    expect(result.current.accessToken).toBeUndefined();
  });
  it("reports failed refresh despite a cached token and recovers on retry", async () => {
    const { result } = mount({ ...options, throwOnError: false });
    await waitFor(() => expect(result.current.accessToken).toBe("token-S"));
    mint.mockRejectedValueOnce(new Error("refresh failed"));
    act(() => result.current.refetch());
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.accessToken).toBe("token-S");
    mint.mockResolvedValue({ accessToken: "fresh-S" });
    act(() => result.current.refetch());
    await waitFor(() => expect(result.current.accessToken).toBe("fresh-S"));
    expect(result.current.isError).toBe(false);
  });
});

it("does not mark a healthy cached token loading during background refresh", async () => {
  const { result, client } = mount();
  await waitFor(() => expect(result.current.accessToken).toBe("token-S"));
  let finish!: (value: { accessToken: string }) => void;
  mint.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  act(() => {
    result.current.refetch();
    result.current.refetch();
  });
  await waitFor(() => expect(client.isFetching()).toBe(1));
  expect(result.current.isLoading).toBe(false);
  expect(result.current.accessToken).toBe("token-S");
  expect(mint).toHaveBeenCalledTimes(2);
  await act(async () => finish({ accessToken: "fresh-S" }));
  await waitFor(() => expect(result.current.accessToken).toBe("fresh-S"));
});
