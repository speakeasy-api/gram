import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@gram/client/funcs/telemetryGetProjectOverview", () => ({
  telemetryGetProjectOverview: vi.fn(),
}));

import { telemetryGetProjectOverview } from "@gram/client/funcs/telemetryGetProjectOverview";
import {
  PROJECT_OVERVIEW_STALE_TIME_MS,
  buildProjectOverviewQuery,
  isProjectOverviewQueryKey,
  projectOverviewQueryKey,
  type ProjectOverviewScope,
} from "./projectOverviewQuery";

const mockGetOverview = vi.mocked(telemetryGetProjectOverview);

const client = {} as Parameters<typeof buildProjectOverviewQuery>[0];

const baseScope: ProjectOverviewScope = {
  organization: "org-a",
  project: "proj-a",
  range: { preset: "7d" },
};

type OverviewResult = Awaited<
  ReturnType<
    NonNullable<ReturnType<typeof buildProjectOverviewQuery>["queryFn"]>
  >
>;

function overviewFixture(marker: string): OverviewResult {
  return { summary: { marker } } as unknown as OverviewResult;
}

function okResult(value: OverviewResult) {
  return Promise.resolve({ ok: true as const, value }) as ReturnType<
    typeof telemetryGetProjectOverview
  >;
}

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("projectOverviewQueryKey", () => {
  it("differs across organization, project, preset, and custom range", () => {
    const keys = [
      projectOverviewQueryKey(baseScope),
      projectOverviewQueryKey({ ...baseScope, organization: "org-b" }),
      projectOverviewQueryKey({ ...baseScope, project: "proj-b" }),
      projectOverviewQueryKey({ ...baseScope, range: { preset: "30d" } }),
      projectOverviewQueryKey({
        ...baseScope,
        range: {
          from: "2026-07-01T00:00:00.000Z",
          to: "2026-07-08T00:00:00.000Z",
        },
      }),
      projectOverviewQueryKey({
        ...baseScope,
        range: {
          from: "2026-07-01T00:00:00.000Z",
          to: "2026-07-09T00:00:00.000Z",
        },
      }),
    ];
    const serialized = keys.map((k) => JSON.stringify(k));
    expect(new Set(serialized).size).toBe(keys.length);
  });

  it("produces the same key for the org-home prefetch and the project page default query", () => {
    // Each call site constructs its own scope object; the cache must treat
    // them as one entry.
    const prefetch = buildProjectOverviewQuery(client, {
      organization: "acme",
      project: "analytics",
      range: { preset: "7d" },
    });
    const page = buildProjectOverviewQuery(client, {
      organization: "acme",
      project: "analytics",
      range: { preset: "7d" },
    });
    expect(page.queryKey).toEqual(prefetch.queryKey);

    const queryClient = makeQueryClient();
    const data = overviewFixture("shared");
    queryClient.setQueryData(prefetch.queryKey, data);
    expect(queryClient.getQueryData(page.queryKey)).toEqual(data);
  });
});

describe("isProjectOverviewQueryKey", () => {
  it("matches overview keys and nothing else", () => {
    expect(isProjectOverviewQueryKey(projectOverviewQueryKey(baseScope))).toBe(
      true,
    );
    expect(isProjectOverviewQueryKey(["project", "topUsers", "a", "b"])).toBe(
      false,
    );
    expect(isProjectOverviewQueryKey(["@gram/client", "telemetry"])).toBe(
      false,
    );
    expect(isProjectOverviewQueryKey([])).toBe(false);
  });
});

describe("buildProjectOverviewQuery", () => {
  it("sends the explicit target gramProject", async () => {
    mockGetOverview.mockReturnValue(okResult(overviewFixture("a")));
    const queryClient = makeQueryClient();

    await queryClient.fetchQuery(
      buildProjectOverviewQuery(client, {
        organization: "acme",
        project: "analytics",
        range: { preset: "7d" },
      }),
    );

    expect(mockGetOverview).toHaveBeenCalledWith(
      client,
      expect.objectContaining({
        gramProject: "analytics",
        getProjectMetricsSummaryPayload: expect.objectContaining({
          from: expect.any(Date),
          to: expect.any(Date),
        }),
      }),
    );
  });

  it("sends the exact bounds of a custom range", async () => {
    mockGetOverview.mockReturnValue(okResult(overviewFixture("a")));
    const queryClient = makeQueryClient();
    const from = "2026-07-01T00:00:00.000Z";
    const to = "2026-07-08T00:00:00.000Z";

    await queryClient.fetchQuery(
      buildProjectOverviewQuery(client, { ...baseScope, range: { from, to } }),
    );

    expect(mockGetOverview).toHaveBeenCalledWith(
      client,
      expect.objectContaining({
        getProjectMetricsSummaryPayload: {
          from: new Date(from),
          to: new Date(to),
        },
      }),
    );
  });

  it("does not return cached project-A data for project B", async () => {
    const queryClient = makeQueryClient();
    const dataA = overviewFixture("project-a");
    queryClient.setQueryData(projectOverviewQueryKey(baseScope), dataA);

    const scopeB = { ...baseScope, project: "proj-b" };
    const scopeOtherOrg = { ...baseScope, organization: "org-b" };
    expect(
      queryClient.getQueryData(projectOverviewQueryKey(scopeB)),
    ).toBeUndefined();
    expect(
      queryClient.getQueryData(projectOverviewQueryKey(scopeOtherOrg)),
    ).toBeUndefined();

    // Fetching project B issues its own request instead of reusing A's data.
    const dataB = overviewFixture("project-b");
    mockGetOverview.mockReturnValue(okResult(dataB));
    const result = await queryClient.fetchQuery(
      buildProjectOverviewQuery(client, scopeB),
    );
    expect(result).toEqual(dataB);
    expect(mockGetOverview).toHaveBeenCalledTimes(1);
    expect(
      queryClient.getQueryData(projectOverviewQueryKey(baseScope)),
    ).toEqual(dataA);
  });

  it("reuses a prefetched result within the freshness window without a second request", async () => {
    mockGetOverview.mockReturnValue(okResult(overviewFixture("prefetched")));
    const queryClient = makeQueryClient();

    await queryClient.prefetchQuery(
      buildProjectOverviewQuery(client, baseScope),
    );
    // Navigation: the project page builds its own options object.
    const data = await queryClient.fetchQuery(
      buildProjectOverviewQuery(client, baseScope),
    );

    expect(data).toEqual(overviewFixture("prefetched"));
    expect(mockGetOverview).toHaveBeenCalledTimes(1);
  });

  it("keeps serving stale data while a background refresh is in flight", async () => {
    vi.useFakeTimers();
    const queryClient = makeQueryClient();
    const key = projectOverviewQueryKey(baseScope);
    const v1 = overviewFixture("v1");
    const v2 = overviewFixture("v2");

    mockGetOverview.mockReturnValueOnce(okResult(v1));
    await queryClient.prefetchQuery(
      buildProjectOverviewQuery(client, baseScope),
    );

    vi.advanceTimersByTime(PROJECT_OVERVIEW_STALE_TIME_MS + 1_000);

    let resolveRefetch!: (value: Awaited<ReturnType<typeof okResult>>) => void;
    mockGetOverview.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveRefetch = resolve;
      }) as ReturnType<typeof telemetryGetProjectOverview>,
    );

    const refetch = queryClient.prefetchQuery(
      buildProjectOverviewQuery(client, baseScope),
    );
    await Promise.resolve();

    // The stale entry is still readable while the refetch is pending.
    expect(mockGetOverview).toHaveBeenCalledTimes(2);
    expect(queryClient.getQueryData(key)).toEqual(v1);

    resolveRefetch({ ok: true, value: v2 });
    await refetch;
    expect(queryClient.getQueryData(key)).toEqual(v2);
  });
});
