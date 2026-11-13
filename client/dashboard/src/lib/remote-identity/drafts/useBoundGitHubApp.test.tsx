import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useGitHubAppDraft, useBoundGitHubApp } from "./useGitHubAppDraft";

const mocks = vi.hoisted(() => ({
  query: vi.fn(),
  configure: vi.fn(),
  detach: vi.fn(),
}));
vi.mock("@gram/client/react-query/authClients.js", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@gram/client/react-query/authClients.js")
  >()),
  useAuthClientsInfinite: (...args: unknown[]) => mocks.query(...args),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    authClients: {
      configureGitHubApp: mocks.configure,
      detachFromMcpServer: mocks.detach,
    },
  }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

beforeEach(() => {
  mocks.query.mockReturnValue({
    data: { pages: [{ result: { items: [] } }] },
    hasNextPage: false,
    isFetchingNextPage: false,
    isFetchNextPageError: false,
    isLoading: false,
    isError: false,
    fetchNextPage: vi.fn().mockResolvedValue({}),
    refetch: vi.fn().mockResolvedValue({}),
  });
});

describe("useBoundGitHubApp", () => {
  it("finds the App on a later page and scopes the query to the server", () => {
    const app = { id: "github-client", githubApp: { appId: 123 } };
    mocks.query.mockReturnValue({
      ...mocks.query(),
      data: {
        pages: [
          { result: { items: [{ id: "other-client" }] } },
          { result: { items: [app] } },
        ],
      },
    });
    const { result } = renderHook(() => useBoundGitHubApp("server-1", true));
    expect(result.current.bound).toEqual(app);
    expect(result.current.loading).toBe(false);
    expect(mocks.query).toHaveBeenCalledWith(
      { mcpServerId: "server-1", clientType: "service_account", limit: 100 },
      undefined,
      { enabled: true, throwOnError: false },
    );
  });

  it("drains successive page transitions before resolving the identity", async () => {
    const app = { id: "github-client", githubApp: { appId: "123" } };
    const query = { ...mocks.query(), hasNextPage: true };
    mocks.query.mockReturnValue(query);
    const { result, rerender } = renderHook(() =>
      useBoundGitHubApp("server-1", true),
    );
    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(query.fetchNextPage).toHaveBeenCalledTimes(1));
    mocks.query.mockReturnValue({ ...query, isFetchingNextPage: true });
    rerender();
    mocks.query.mockReturnValue({
      ...query,
      data: { pages: [{ result: { items: [] } }, { result: { items: [] } }] },
    });
    rerender();
    await waitFor(() => expect(query.fetchNextPage).toHaveBeenCalledTimes(2));
    expect(result.current.loading).toBe(true);
    mocks.query.mockReturnValue({
      ...query,
      hasNextPage: false,
      data: {
        pages: [
          { result: { items: [] } },
          { result: { items: [] } },
          { result: { items: [app] } },
        ],
      },
    });
    rerender();
    expect(result.current.bound).toEqual(app);
    expect(result.current.loading).toBe(false);
    expect(query.fetchNextPage).toHaveBeenCalledTimes(2);
  });

  it.each([undefined, { id: "github-client", githubApp: { appId: "123" } }])(
    "retains partial data and an unresolved error until a failed page is retried: %o",
    async (app) => {
      const query = {
        ...mocks.query(),
        data: { pages: [{ result: { items: app ? [app] : [] } }] },
        hasNextPage: true,
        isFetchNextPageError: true,
      };
      mocks.query.mockReturnValue(query);
      const { result } = renderHook(() => useBoundGitHubApp("server-1", true));
      expect(result.current.bound).toEqual(app);
      expect(result.current.error).toBe(true);
      expect(result.current.loading).toBe(false);
      expect(query.fetchNextPage).not.toHaveBeenCalled();
      await act(async () => {
        await result.current.retry();
      });
      expect(query.fetchNextPage).toHaveBeenCalledOnce();
      expect(query.refetch).not.toHaveBeenCalled();
      expect(result.current.error).toBe(true);
    },
  );

  it("does not fetch pages when the server lookup is disabled", () => {
    const query = { ...mocks.query(), hasNextPage: true, isLoading: true };
    mocks.query.mockReturnValue(query);
    const { result } = renderHook(() => useBoundGitHubApp("", false));
    expect(result.current.loading).toBe(false);
    expect(query.fetchNextPage).not.toHaveBeenCalled();
  });
});

describe("GitHub App mutation cache coherence", () => {
  it.each(["save", "detach"] as const)(
    "invalidates shared auth-client readers after %s",
    async (operation) => {
      const queryClient = new QueryClient();
      const keys = [
        ["@gram/client", "authClients", "list", "palette"],
        ["@gram/client", "authClients", "get", "detail"],
      ];
      for (const key of keys) queryClient.setQueryData(key, { stale: true });
      mocks.configure.mockResolvedValue({});
      mocks.detach.mockResolvedValue({});
      mocks.query.mockReturnValue({
        ...mocks.query(),
        data: {
          pages: [
            {
              result: {
                items: [
                  {
                    id: "app-1",
                    githubApp: {
                      instanceUrl: "https://github.com",
                      appId: "123",
                      installationId: "456",
                    },
                  },
                ],
              },
            },
          ],
        },
      });
      const wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      );
      const { result } = renderHook(
        () =>
          useGitHubAppDraft(
            "server-1",
            "https://mcp.example.test",
            "Example",
            true,
          ),
        { wrapper },
      );
      if (operation === "save")
        act(() =>
          result.current.setDraft({
            instanceUrl: "https://github.com",
            appId: "123",
            installationId: "456",
            privateKey: "-----BEGIN PRIVATE KEY-----\nkey",
          }),
        );
      await act(async () => {
        await result.current[operation]();
      });
      expect(
        operation === "save" ? mocks.configure : mocks.detach,
      ).toHaveBeenCalled();
      for (const key of keys)
        expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true);
    },
  );
});
