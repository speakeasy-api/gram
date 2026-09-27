import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useUserSessionIssuersInfinite } from "@gram/client/react-query/userSessionIssuers.js";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useEffectiveUserSessionIssuers } from "./useEffectiveUserSessionIssuers";

vi.mock("@gram/client/react-query/userSessionIssuers.js", () => ({
  useUserSessionIssuersInfinite: vi.fn(),
}));

const useIssuersMock = vi.mocked(useUserSessionIssuersInfinite);

function issuer(
  id: string,
  slug: string,
  projectId: string,
): UserSessionIssuer {
  return { id, slug, projectId } as UserSessionIssuer;
}

describe("useEffectiveUserSessionIssuers", () => {
  beforeEach(() => {
    vi.resetAllMocks();
  });

  it("drains every effective issuer page before reporting ready", async () => {
    const fetchNextPage = vi.fn();
    useIssuersMock.mockReturnValue({
      data: { pages: [{ result: { items: [] } }] },
      fetchNextPage,
      hasNextPage: true,
      isFetchingNextPage: false,
      isFetchNextPageError: false,
      isLoading: false,
      isError: false,
    } as never);

    useIssuersMock.mockReturnValueOnce({
      data: { pages: [{ result: { items: [] } }] },
      fetchNextPage,
      hasNextPage: true,
      isFetchingNextPage: false,
      isFetchNextPageError: false,
      isLoading: false,
      isError: false,
    } as never);
    useIssuersMock.mockReturnValue({
      data: { pages: [{ result: { items: [] } }] },
      fetchNextPage,
      hasNextPage: false,
      isFetchingNextPage: false,
      isFetchNextPageError: false,
      isLoading: false,
      isError: false,
    } as never);

    const { result, rerender } = renderHook(() =>
      useEffectiveUserSessionIssuers(),
    );

    expect(result.current.isLoading).toBe(true);
    await waitFor(() => expect(fetchNextPage).toHaveBeenCalledOnce());
    rerender();
    expect(result.current.isLoading).toBe(false);
    expect(result.current.isError).toBe(false);
  });

  it("includes organization issuers returned on later pages", () => {
    const organizationIssuer = issuer("org", "workforce", "");
    const projectIssuer = issuer("project", "custom", "project-id");
    useIssuersMock.mockReturnValue({
      data: {
        pages: [
          { result: { items: [projectIssuer] } },
          { result: { items: [organizationIssuer] } },
        ],
      },
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isFetchingNextPage: false,
      isFetchNextPageError: false,
      isLoading: false,
      isError: false,
    } as never);

    const { result } = renderHook(() =>
      useEffectiveUserSessionIssuers({
        projectSlug: "project-slug",
        mcpResourceId: "mcp-resource-id",
      }),
    );

    expect(useIssuersMock).toHaveBeenCalledWith({
      limit: 100,
      gramProject: "project-slug",
      mcpResourceId: "mcp-resource-id",
    });
    expect(result.current.issuers).toEqual([organizationIssuer, projectIssuer]);
    expect(result.current.organizationIssuers).toEqual([organizationIssuer]);
    expect(result.current.isLoading).toBe(false);
    expect(result.current.isError).toBe(false);
  });

  it("reports a pagination cap as an error instead of loading forever", () => {
    useIssuersMock.mockReturnValue({
      data: {
        pages: Array.from({ length: 10 }, () => ({
          result: { items: [] },
        })),
      },
      fetchNextPage: vi.fn(),
      hasNextPage: true,
      isFetchingNextPage: false,
      isFetchNextPageError: false,
      isLoading: false,
      isError: false,
    } as never);

    const { result } = renderHook(() => useEffectiveUserSessionIssuers());

    expect(result.current.isLoading).toBe(false);
    expect(result.current.isError).toBe(true);
  });
});
