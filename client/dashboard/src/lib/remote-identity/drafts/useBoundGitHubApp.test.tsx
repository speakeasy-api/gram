import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useBoundGitHubApp } from "./useGitHubAppDraft";

const mocks = vi.hoisted(() => ({ query: vi.fn() }));
vi.mock("@gram/client/react-query/authClients.js", () => ({
  useAuthClientsInfinite: (...args: unknown[]) => mocks.query(...args),
}));

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

  it("keeps identity loading while draining the remaining pages", async () => {
    const query = { ...mocks.query(), hasNextPage: true };
    mocks.query.mockReturnValue(query);
    const { result } = renderHook(() => useBoundGitHubApp("server-1", true));
    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(query.fetchNextPage).toHaveBeenCalledOnce());
  });

  it("fails closed when a later page fails", () => {
    mocks.query.mockReturnValue({
      ...mocks.query(),
      isFetchNextPageError: true,
    });
    const { result } = renderHook(() => useBoundGitHubApp("server-1", true));
    expect(result.current.error).toBe(true);
    expect(result.current.loading).toBe(true);
  });

  it("does not fetch pages when the server lookup is disabled", () => {
    const query = { ...mocks.query(), hasNextPage: true, isLoading: true };
    mocks.query.mockReturnValue(query);
    const { result } = renderHook(() => useBoundGitHubApp("", false));
    expect(result.current.loading).toBe(false);
    expect(query.fetchNextPage).not.toHaveBeenCalled();
  });
});
