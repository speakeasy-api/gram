import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  gramProject: "project-one",
  goToOverview: vi.fn(),
  query: {
    data: undefined as
      | { pages: Array<{ result: { skills: Array<Record<string, unknown>> } }> }
      | undefined,
    hasNextPage: false,
    isFetchingNextPage: false,
    isFetchNextPageError: false,
    fetchNextPage: vi.fn<() => Promise<unknown>>(),
  },
  calls: [] as unknown[][],
}));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => mocks.gramProject,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: {
      detail: { overview: { goTo: mocks.goToOverview } },
    },
  }),
}));
vi.mock("@gram/client/react-query/skills.js", () => ({
  useSkillsInfinite: (...args: unknown[]) => {
    mocks.calls.push(args);
    return mocks.query;
  },
}));

import { useSkillCandidates } from "./useSkillCandidates";

function skill(
  id: string,
  displayName: string,
  name = displayName.toLowerCase(),
) {
  return {
    id,
    displayName,
    name,
    classification: "verified",
    tags: ["security"],
  };
}

describe("useSkillCandidates", () => {
  beforeEach(() => {
    mocks.gramProject = "project-one";
    mocks.goToOverview.mockReset();
    mocks.calls.length = 0;
    mocks.query.data = undefined;
    mocks.query.hasNextPage = false;
    mocks.query.isFetchingNextPage = false;
    mocks.query.isFetchNextPageError = false;
    mocks.query.fetchNextPage.mockReset().mockResolvedValue(undefined);
  });

  it("uses the project-scoped query and does not fetch while closed", () => {
    renderHook(() => useSkillCandidates({ enabled: false }));

    expect(mocks.calls.at(-1)?.[0]).toEqual({
      gramProject: "project-one",
      limit: 200,
    });
    expect(mocks.calls.at(-1)?.[2]).toMatchObject({
      enabled: false,
      retry: false,
      throwOnError: false,
    });
    expect(mocks.query.fetchNextPage).not.toHaveBeenCalled();
  });

  it("maps skills from every loaded page and opens the overview directly", () => {
    mocks.query.data = {
      pages: [
        { result: { skills: [skill("first", "Policy Review")] } },
        {
          result: {
            skills: [skill("last", "Incident Response", "incident-response")],
          },
        },
      ],
    };

    const { result } = renderHook(() => useSkillCandidates({ enabled: true }));

    expect(result.current.map((candidate) => candidate.title)).toEqual([
      "Policy Review",
      "Incident Response",
    ]);
    expect(result.current[1]).toMatchObject({
      id: "skill:last",
      kind: "skill",
      keywords: ["skill", "incident-response", "last", "security"],
      group: "Skills",
    });
    void result.current[1]!.run("open");
    expect(mocks.goToOverview).toHaveBeenCalledWith("last");
  });

  it("keeps fetching until all skill pages have loaded", async () => {
    mocks.query.hasNextPage = true;
    const { rerender } = renderHook(() =>
      useSkillCandidates({ enabled: true }),
    );

    await waitFor(() =>
      expect(mocks.query.fetchNextPage).toHaveBeenCalledTimes(1),
    );

    act(() => {
      mocks.query.isFetchingNextPage = true;
      rerender();
    });
    act(() => {
      mocks.query.isFetchingNextPage = false;
      mocks.query.data = {
        pages: [{ result: { skills: [skill("next", "Next Page")] } }],
      };
      rerender();
    });

    await waitFor(() =>
      expect(mocks.query.fetchNextPage).toHaveBeenCalledTimes(2),
    );
  });

  it("returns no candidates and stops draining when the query fails", () => {
    mocks.query.hasNextPage = true;
    mocks.query.isFetchNextPageError = true;

    const { result } = renderHook(() => useSkillCandidates({ enabled: true }));

    expect(result.current).toEqual([]);
    expect(mocks.query.fetchNextPage).not.toHaveBeenCalled();
    expect(mocks.calls.at(-1)?.[2]).toMatchObject({ throwOnError: false });
  });
});
