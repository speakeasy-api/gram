import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({
  organization: { id: "org-fixture" },
  project: { id: "project-fixture" },
  navigate: vi.fn(),
  fetchNextPage: vi.fn<() => Promise<unknown>>().mockResolvedValue(undefined),
  options: {} as { queryKey: unknown[]; enabled: boolean },
  pages: [] as Array<{
    items: Array<{
      id: string;
      name: string;
      enabled: boolean;
      servers: Array<{ id: string; projectId: string; slug: string }>;
    }>;
  }>,
  hasNextPage: false,
  isFetchNextPageError: false,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => mocks.organization,
  useProject: () => mocks.project,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ authClients: { list: vi.fn() } }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      details: {
        settings: { href: (slug: string) => `/mcp/${slug}/settings` },
      },
    },
  }),
}));
vi.mock("react-router", () => ({ useNavigate: () => mocks.navigate }));
vi.mock("@tanstack/react-query", () => ({
  useInfiniteQuery: (options: typeof mocks.options) => {
    mocks.options = options;
    return {
      data: { pages: mocks.pages },
      fetchNextPage: mocks.fetchNextPage,
      hasNextPage: mocks.hasNextPage,
      isFetchingNextPage: false,
      isFetchNextPageError: mocks.isFetchNextPageError,
    };
  },
}));
import { useAuthClientCandidates } from "./useAuthClientCandidates";
describe("service account candidates", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.pages = [];
    mocks.hasNextPage = false;
    mocks.isFetchNextPageError = false;
  });
  it("hides cached results and stops pagination while disabled", () => {
    mocks.hasNextPage = true;
    const { result } = renderHook(() =>
      useAuthClientCandidates({ enabled: false }),
    );
    expect(result.current).toEqual([]);
    expect(mocks.options.enabled).toBe(false);
    expect(mocks.fetchNextPage).not.toHaveBeenCalled();
  });
  it("searches later pages locally and opens only current-project attachments", () => {
    mocks.pages = [
      { items: [] },
      {
        items: [
          {
            id: "client-fixture",
            name: "Repository reader",
            enabled: false,
            servers: [
              {
                id: "server-fixture",
                projectId: "project-fixture",
                slug: "github",
              },
              { id: "foreign", projectId: "other", slug: "foreign" },
            ],
          },
        ],
      },
    ];
    const { result } = renderHook(() =>
      useAuthClientCandidates({ enabled: true }),
    );
    expect(result.current).toHaveLength(1);
    expect(result.current[0]?.title).toBe("Repository reader");
    expect(result.current[0]?.fuzzyOnly).toBe(true);
    void result.current[0]?.run("open");
    expect(mocks.navigate).toHaveBeenCalledWith(
      "/mcp/github/settings#authentication",
    );
    expect(mocks.options.queryKey).toEqual([
      "launcher-auth-clients",
      "org-fixture",
      "project-fixture",
    ]);
  });
  it("drains pages but stops after a pagination failure", () => {
    mocks.hasNextPage = true;
    const { rerender } = renderHook(() =>
      useAuthClientCandidates({ enabled: true }),
    );
    expect(mocks.fetchNextPage).toHaveBeenCalledTimes(1);
    mocks.isFetchNextPageError = true;
    rerender();
    expect(mocks.fetchNextPage).toHaveBeenCalledTimes(1);
  });
});
