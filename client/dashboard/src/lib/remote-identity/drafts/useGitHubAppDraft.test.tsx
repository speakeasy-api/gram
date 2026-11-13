import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useBoundGitHubApp, useGitHubAppDraft } from "./useGitHubAppDraft";

const mocks = vi.hoisted(() => ({
  query: vi.fn(),
  configure: vi.fn(),
  detach: vi.fn(),
  invalidate: vi.fn(),
  refetch: vi.fn(),
  fetchNextPage: vi.fn(),
}));
vi.mock("@gram/client/react-query/authClients.js", () => ({
  useAuthClientsInfinite: () => mocks.query(),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    authClients: {
      configureGitHubApp: mocks.configure,
      detachFromMcpServer: mocks.detach,
    },
  }),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/authClientQueries",
  () => ({
    invalidateAuthClientQueries: mocks.invalidate,
  }),
);
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

describe("GitHub App draft refresh", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.invalidate.mockResolvedValue(undefined);
    mocks.refetch.mockResolvedValue({});
    mocks.fetchNextPage.mockResolvedValue({});
    mocks.query.mockReturnValue({
      data: {
        pages: [
          {
            result: {
              items: [
                {
                  id: "app-fixture",
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
      isLoading: false,
      isError: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      isFetchNextPageError: false,
      refetch: mocks.refetch,
      fetchNextPage: mocks.fetchNextPage,
    });
  });

  it("invalidates after save without starting a second refetch", async () => {
    const { result } = renderHook(() =>
      useGitHubAppDraft(
        "server-fixture",
        "https://proxy.example.test/mcp",
        "Fixture",
        true,
      ),
    );
    act(() =>
      result.current.setDraft({
        instanceUrl: "https://github.com",
        appId: "123",
        installationId: "456",
        privateKey: "-----BEGIN PRIVATE KEY-----",
      }),
    );
    await act(async () => {
      expect(await result.current.save()).toBe(true);
    });
    expect(mocks.configure).toHaveBeenCalledOnce();
    expect(mocks.invalidate).toHaveBeenCalledOnce();
    expect(mocks.refetch).not.toHaveBeenCalled();
  });

  it("invalidates after detach without starting a second refetch", async () => {
    const { result } = renderHook(() =>
      useGitHubAppDraft(
        "server-fixture",
        "https://proxy.example.test/mcp",
        "Fixture",
        true,
      ),
    );
    await act(async () => {
      await result.current.detach();
    });
    expect(mocks.detach).toHaveBeenCalledOnce();
    expect(mocks.invalidate).toHaveBeenCalledOnce();
    expect(mocks.refetch).not.toHaveBeenCalled();
  });

  it("retains partial data and retries only the failed later page", async () => {
    mocks.query.mockReturnValue({
      ...mocks.query(),
      hasNextPage: true,
      isError: true,
      isFetchNextPageError: true,
    });
    const { result } = renderHook(() =>
      useBoundGitHubApp("server-fixture", true),
    );
    expect(result.current.bound?.id).toBe("app-fixture");
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBe(true);
    expect(mocks.fetchNextPage).not.toHaveBeenCalled();
    await act(async () => {
      await result.current.retry();
    });
    expect(mocks.fetchNextPage).toHaveBeenCalledOnce();
    expect(mocks.refetch).not.toHaveBeenCalled();
  });
});
