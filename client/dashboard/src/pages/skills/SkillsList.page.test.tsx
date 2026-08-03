import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SkillsList from "./SkillsList";

const testState = vi.hoisted(() => ({
  queryClient: { id: "query-client" },
  fetchNextPage: vi.fn().mockResolvedValue(undefined),
  hasNextPage: false,
  isFetchNextPageError: false,
  error: null as Error | null,
  insightsError: null as Error | null,
  insightsData: { result: { insights: [] } } as
    | { result: { insights: unknown[] } }
    | undefined,
  insightsRefetch: vi.fn().mockResolvedValue(undefined),
  metricTotalCount: undefined as number | undefined,
  metricPageSize: 200,
  loadedMetricPageCount: 1,
  skillRequests: [] as unknown[],
  metricSkillRequests: [] as unknown[],
  searchValue: "example",
  skills: [] as Array<Record<string, unknown>>,
  unknownActivations: [] as Array<Record<string, unknown>>,
  suggestionFetchNextPage: vi.fn().mockResolvedValue(undefined),
  suggestionRefetch: vi.fn().mockResolvedValue(undefined),
  suggestionHasNextPage: false,
  suggestionError: null as Error | null,
  suggestions: [] as Array<Record<string, unknown>>,
  suggestionTotal: 0,
  suggestionRequests: [] as unknown[],
  approveAll: { mutateAsync: vi.fn(), isPending: false },
  invalidateSkills: vi.fn().mockResolvedValue(undefined),
  invalidateSkill: vi.fn().mockResolvedValue(undefined),
  invalidateDistributions: vi.fn().mockResolvedValue(undefined),
  invalidateVersions: vi.fn().mockResolvedValue(undefined),
  invalidateSuggestions: vi.fn().mockResolvedValue(undefined),
  invalidateFeedback: vi.fn().mockResolvedValue(undefined),
  invalidateEfficacy: vi.fn().mockResolvedValue(undefined),
  toastInfo: vi.fn(),
}));

vi.mock("@/components/filters", () => ({
  defineFilters: <T,>(value: T) => value,
  useFilterState: () => ({
    values: { sourceKind: [], classification: [] },
    setValue: vi.fn(),
    clearValue: vi.fn(),
    clearAll: vi.fn(),
  }),
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project_a" }) }));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => testState.queryClient,
}));
vi.mock("nuqs", () => ({
  useQueryState: () => [null, vi.fn()],
}));
vi.mock("./ArchiveSkillDialog", () => ({ ArchiveSkillDialog: () => null }));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  Navigate: () => null,
  useNavigate: () => vi.fn(),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: {
      href: () => "/skills",
      detail: { href: (id: string) => `/skills/${id}` },
    },
  }),
}));
vi.mock("@gram/client/react-query/skills.js", () => ({
  useSkills: (request: {
    cursor?: string;
    limit?: number;
    search?: string;
  }) => {
    testState.skillRequests.push(request);
    const matchingSkills = request.search
      ? testState.skills.filter((skill) =>
          String(skill.displayName).toLowerCase().includes(request.search!),
        )
      : testState.skills;
    const start = Number(request.cursor ?? 0);
    const limit = request.limit ?? 50;
    const next = start + limit;
    return {
      data: {
        result: {
          skills: matchingSkills.slice(start, next),
          totalCount: matchingSkills.length,
          nextCursor: next < matchingSkills.length ? String(next) : undefined,
        },
      },
      isPending: false,
      isFetching: false,
      error: testState.error,
      refetch: vi.fn(),
    };
  },
  useSkillsInfinite: (
    request: { search?: string },
    _client: unknown,
    options?: { enabled?: boolean },
  ) => {
    if (options?.enabled) {
      testState.metricSkillRequests.push(request);
    }
    const matchingSkills = request.search
      ? testState.skills.filter((skill) =>
          String(skill.displayName).toLowerCase().includes(request.search!),
        )
      : testState.skills;
    const pages = Array.from(
      { length: testState.loadedMetricPageCount },
      (_, page) => ({
        result: {
          skills: matchingSkills.slice(
            page * testState.metricPageSize,
            (page + 1) * testState.metricPageSize,
          ),
          totalCount: testState.metricTotalCount ?? matchingSkills.length,
        },
      }),
    );
    return {
      data: { pages },
      isPending: false,
      isFetching: false,
      isFetchingNextPage: false,
      isFetchNextPageError: testState.isFetchNextPageError,
      hasNextPage: testState.hasNextPage,
      error: testState.error,
      fetchNextPage: async () => {
        testState.loadedMetricPageCount += 1;
        testState.hasNextPage = false;
        return testState.fetchNextPage();
      },
      refetch: vi.fn(),
    };
  },
  invalidateAllSkills: testState.invalidateSkills,
}));
vi.mock("@gram/client/react-query/skillSuggestions.js", () => ({
  useSkillSuggestionsInfinite: (request: unknown) => {
    testState.suggestionRequests.push(request);
    return {
      data: {
        pages: [
          {
            result: {
              suggestions: testState.suggestions,
              totalOpenCount: testState.suggestionTotal,
            },
          },
        ],
      },
      isFetching: false,
      isFetchingNextPage: false,
      hasNextPage: testState.suggestionHasNextPage,
      isError: testState.suggestionError !== null,
      error: testState.suggestionError,
      fetchNextPage: testState.suggestionFetchNextPage,
      refetch: testState.suggestionRefetch,
    };
  },
  invalidateAllSkillSuggestions: testState.invalidateSuggestions,
}));
vi.mock("@gram/client/react-query/approveAllSkillSuggestions.js", () => ({
  useApproveAllSkillSuggestionsMutation: () => testState.approveAll,
}));
vi.mock("@gram/client/react-query/skill.js", () => ({
  invalidateAllSkill: testState.invalidateSkill,
}));
vi.mock("@gram/client/react-query/skillDistributions.js", () => ({
  invalidateAllSkillDistributions: testState.invalidateDistributions,
}));
vi.mock("@gram/client/react-query/skillVersions.js", () => ({
  invalidateAllSkillVersions: testState.invalidateVersions,
}));
vi.mock("@gram/client/react-query/skillFeedback.js", () => ({
  invalidateAllSkillFeedback: testState.invalidateFeedback,
}));
vi.mock("@gram/client/react-query/skillEfficacyInsights.js", () => ({
  useSkillEfficacyInsights: () => ({
    data: testState.insightsData,
    error: testState.insightsError,
    isFetching: false,
    refetch: testState.insightsRefetch,
  }),
  invalidateAllSkillEfficacyInsights: testState.invalidateEfficacy,
}));
vi.mock("@gram/client/react-query/unknownSkillActivations.js", () => ({
  useUnknownSkillActivationsInfinite: () => ({
    data: {
      pages: [{ result: { activations: testState.unknownActivations } }],
    },
    isPending: false,
    isFetchingNextPage: false,
    isFetchNextPageError: false,
    hasNextPage: false,
    error: null,
    fetchNextPage: vi.fn(),
    refetch: vi.fn(),
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/ui/Tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("./SkillManifestDialog", () => ({ SkillManifestDialog: () => null }));
vi.mock("sonner", () => ({
  toast: { info: testState.toastInfo, success: vi.fn() },
}));
vi.mock("@/components/page-layout", () => {
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  const Search = ({ onChange }: { onChange: (value: string) => void }) => (
    <button onClick={() => onChange(testState.searchValue)}>
      Apply search
    </button>
  );
  const Filters = ({
    onChange,
  }: {
    onChange: (id: string, value: string[]) => void;
  }) => (
    <button onClick={() => onChange("sourceKind", ["manual"])}>
      Apply filters
    </button>
  );
  const Toolbar = Object.assign(Wrapper, {
    Search,
    Filters,
    Count: Wrapper,
    Actions: Wrapper,
    Refresh: () => null,
  });
  return {
    Page: Object.assign(Wrapper, {
      Header: Object.assign(Wrapper, { Breadcrumbs: () => null }),
      Body: Wrapper,
      Section: Object.assign(Wrapper, {
        Title: Wrapper,
        Description: Wrapper,
        CTA: Wrapper,
        Body: Wrapper,
      }),
      Toolbar,
    }),
  };
});
vi.mock("@/components/ui/Badge", () => ({
  Badge: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

vi.mock("@/components/ui/Icon", () => ({
  Icon: () => <span />,
}));

vi.mock("@/components/ui/Table", () => ({
  Table: ({
    columns,
    data,
    noResultsMessage,
    sort,
    onSortChange,
  }: {
    columns: Array<{
      key: string;
      header: ReactNode;
      sortable?: boolean;
      sortLabel?: string;
      render?: (row: Record<string, unknown>) => ReactNode;
    }>;
    data: Array<Record<string, unknown> & { id: string }>;
    noResultsMessage: ReactNode;
    sort: { id: string; direction: "asc" | "desc" } | null;
    onSortChange: (
      sort: { id: string; direction: "asc" | "desc" } | null,
    ) => void;
  }) => {
    const sortableColumns = columns.filter((column) => column.sortable);
    const nextSort = (column: (typeof columns)[number]) => {
      if (sort?.id !== column.key) {
        return { id: column.key, direction: "asc" as const };
      }
      if (sort.direction === "asc") {
        return { id: column.key, direction: "desc" as const };
      }
      return null;
    };

    return (
      <div>
        <div data-testid="table-column-keys">
          {columns.map((column) => (
            <span data-column-key={column.key} key={column.key} />
          ))}
        </div>
        {sortableColumns.map((column) => {
          const label =
            column.sortLabel ??
            (typeof column.header === "string" ? column.header : column.key);
          const direction = sort?.id === column.key ? sort.direction : null;
          const action =
            direction === "asc"
              ? `Sort by ${label} descending`
              : direction === "desc"
                ? `Clear sort for ${label}`
                : `Sort by ${label} ascending`;
          return (
            <button
              data-sortable-id={column.key}
              key={column.key}
              onClick={() => onSortChange(nextSort(column))}
            >
              {action}
            </button>
          );
        })}
        {data.length === 0
          ? noResultsMessage
          : data.map((skill) => (
              <div data-testid="skill-row" key={skill.id}>
                {columns.slice(0, 1).map((column) => (
                  <div key={column.key}>{column.render?.(skill)}</div>
                ))}
              </div>
            ))}
      </div>
    );
  },
}));

function makeSkills(count: number): Array<Record<string, unknown>> {
  return Array.from({ length: count }, (_, index) => ({
    id: `skill_${index}`,
    projectId: "project_a",
    name: `example-${index}`,
    displayName: `Example ${index}`,
    summary: "Example skill",
    sourceKind: "manual",
    classification: "custom",
    latestVersionId: `version_${index}`,
    versionCount: 1,
    seenCount: 0,
    createdAt: new Date("2026-07-16T00:00:00Z"),
    updatedAt: new Date("2026-07-16T00:00:00Z"),
  }));
}

function makeSuggestion(index: number): Record<string, unknown> {
  return {
    id: `suggestion_${index}`,
    skillId: `skill_${index}`,
    skillDisplayName: `Example ${index}`,
    skillName: `example-${index}`,
    status: "open",
  };
}

function renderedSkillNames(): string[] {
  return screen
    .getAllByTestId("skill-row")
    .map((row) => row.querySelector("a")?.textContent ?? "");
}

beforeEach(() => {
  testState.fetchNextPage.mockReset();
  testState.fetchNextPage.mockResolvedValue(undefined);
  testState.hasNextPage = false;
  testState.isFetchNextPageError = false;
  testState.error = null;
  testState.insightsError = null;
  testState.insightsData = { result: { insights: [] } };
  testState.insightsRefetch.mockReset();
  testState.insightsRefetch.mockResolvedValue(undefined);
  testState.metricTotalCount = undefined;
  testState.metricPageSize = 200;
  testState.loadedMetricPageCount = 1;
  testState.skillRequests = [];
  testState.metricSkillRequests = [];
  testState.searchValue = "example";
  testState.skills = makeSkills(250);
  testState.unknownActivations = [];
  testState.suggestionFetchNextPage.mockReset().mockResolvedValue(undefined);
  testState.suggestionRefetch.mockReset().mockResolvedValue(undefined);
  testState.suggestionHasNextPage = false;
  testState.suggestionError = null;
  testState.suggestions = [];
  testState.suggestionTotal = 0;
  testState.suggestionRequests = [];
  testState.approveAll.isPending = false;
  testState.approveAll.mutateAsync.mockReset();
  testState.toastInfo.mockReset();
  testState.invalidateSkills.mockReset().mockResolvedValue(undefined);
  testState.invalidateSkill.mockReset().mockResolvedValue(undefined);
  testState.invalidateDistributions.mockReset().mockResolvedValue(undefined);
  testState.invalidateVersions.mockReset().mockResolvedValue(undefined);
  testState.invalidateSuggestions.mockReset().mockResolvedValue(undefined);
  testState.invalidateFeedback.mockReset().mockResolvedValue(undefined);
  testState.invalidateEfficacy.mockReset().mockResolvedValue(undefined);
});

afterEach(cleanup);

describe("SkillsList pagination surfaces", () => {
  it("shows only the compact skills table columns", () => {
    render(<SkillsList />);

    expect(
      Array.from(
        screen
          .getByTestId("table-column-keys")
          .querySelectorAll("[data-column-key]"),
      ).map((element) => element.getAttribute("data-column-key")),
    ).toEqual([
      "name",
      "source",
      "activations",
      "efficacy",
      "estimatedSavings",
      "updated",
      "share",
      "actions",
    ]);
  });

  it("sorts the approved columns through header controls without a toolbar sort", () => {
    render(<SkillsList />);

    expect(
      screen
        .getAllByRole("button", { name: /sort by .* ascending/i })
        .map((button) => button.getAttribute("data-sortable-id")),
    ).toEqual([
      "name",
      "source",
      "activations",
      "efficacy",
      "estimatedSavings",
      "updated",
    ]);
    expect(
      screen.queryByRole("button", { name: "Apply metric sort" }),
    ).toBeNull();
  });

  it("sorts the current server page by skill and clears back to backend order", () => {
    testState.skills = [
      { ...makeSkills(1)[0], id: "zulu", displayName: "Zulu" },
      { ...makeSkills(1)[0], id: "alpha", displayName: "Alpha" },
      { ...makeSkills(1)[0], id: "mike", displayName: "Mike" },
    ];
    render(<SkillsList />);

    expect(renderedSkillNames()).toEqual(["Zulu", "Alpha", "Mike"]);
    fireEvent.click(
      screen.getByRole("button", { name: "Sort by Skill ascending" }),
    );
    expect(renderedSkillNames()).toEqual(["Alpha", "Mike", "Zulu"]);
    fireEvent.click(
      screen.getByRole("button", { name: "Sort by Skill descending" }),
    );
    expect(renderedSkillNames()).toEqual(["Zulu", "Mike", "Alpha"]);
    fireEvent.click(
      screen.getByRole("button", { name: "Clear sort for Skill" }),
    );
    expect(renderedSkillNames()).toEqual(["Zulu", "Alpha", "Mike"]);
    expect(testState.metricSkillRequests).toEqual([]);
  });

  it("sorts activations across loaded pages and keeps missing efficacy values last", () => {
    testState.skills = [
      { ...makeSkills(1)[0], id: "missing", displayName: "Missing" },
      { ...makeSkills(1)[0], id: "high", displayName: "High" },
      { ...makeSkills(1)[0], id: "low", displayName: "Low" },
    ];
    testState.insightsData = {
      result: {
        insights: [
          {
            skillId: "high",
            metrics: {
              activations: 5,
              efficacy: { averageScore: 0.9, estimatedMinutesSavedTotal: 9 },
            },
          },
          {
            skillId: "low",
            metrics: {
              activations: 2,
              efficacy: { averageScore: 0.2, estimatedMinutesSavedTotal: 2 },
            },
          },
        ],
      },
    };
    render(<SkillsList />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) ascending",
      }),
    );
    expect(renderedSkillNames()).toEqual(["Missing", "Low", "High"]);
    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) descending",
      }),
    );
    expect(renderedSkillNames()).toEqual(["High", "Low", "Missing"]);
    expect(testState.metricSkillRequests).not.toEqual([]);

    fireEvent.click(
      screen.getByRole("button", { name: "Sort by Efficacy ascending" }),
    );
    expect(renderedSkillNames()).toEqual(["Low", "High", "Missing"]);
    fireEvent.click(
      screen.getByRole("button", { name: "Sort by Efficacy descending" }),
    );
    expect(renderedSkillNames()).toEqual(["High", "Low", "Missing"]);

    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Estimated savings ascending",
      }),
    );
    expect(renderedSkillNames()).toEqual(["Low", "High", "Missing"]);
    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Estimated savings descending",
      }),
    );
    expect(renderedSkillNames()).toEqual(["High", "Low", "Missing"]);
  });

  it("drains metric pages before sorting activations across the page boundary", async () => {
    testState.skills = makeSkills(201);
    testState.hasNextPage = true;
    testState.insightsData = {
      result: {
        insights: [
          { skillId: "skill_0", metrics: { activations: 90 } },
          { skillId: "skill_200", metrics: { activations: 100 } },
        ],
      },
    };
    const { rerender } = render(<SkillsList />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) ascending",
      }),
    );
    await waitFor(() => expect(testState.fetchNextPage).toHaveBeenCalledOnce());
    rerender(<SkillsList />);
    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) descending",
      }),
    );

    expect(renderedSkillNames().slice(0, 2)).toEqual([
      "Example 200",
      "Example 0",
    ]);
    expect(screen.getByText("1-50 of 201")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(renderedSkillNames()[0]).toBe("Example 49");
    expect(screen.getByText("51-100 of 201")).toBeTruthy();
  });

  it("keeps an active header sort when search resets pagination", () => {
    render(<SkillsList />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) ascending",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByText("Example 50")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Apply search" }));
    expect(screen.getByText("Example 0")).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) descending",
      }),
    ).toBeTruthy();
  });

  it("keeps an active header sort when filters reset pagination", () => {
    render(<SkillsList />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) ascending",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByText("Example 50")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    expect(screen.getByText("Example 0")).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) descending",
      }),
    ).toBeTruthy();
  });

  it("requests suggestion pages with the exact supported limit", () => {
    render(<SkillsList />);

    expect(testState.suggestionRequests[0]).toEqual({ limit: 50 });
  });

  it("pages rendered rows and resets to the first page when search changes", async () => {
    render(<SkillsList />);
    expect(screen.getAllByTestId("skill-row")).toHaveLength(50);
    expect(screen.getByText("Example 0")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getAllByTestId("skill-row")).toHaveLength(50);
    expect(screen.getByText("Example 50")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Apply search" }));
    await waitFor(() => {
      expect(screen.getByText("Example 0")).toBeTruthy();
    });
  });

  it("keeps loaded rows visible and exposes an explicit retry after a page failure", () => {
    testState.skills = makeSkills(100);
    testState.metricTotalCount = 1000;
    testState.hasNextPage = true;
    testState.isFetchNextPageError = true;
    testState.error = new Error("next page failed");
    render(<SkillsList />);
    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) ascending",
      }),
    );

    expect(screen.getAllByTestId("skill-row")).toHaveLength(50);
    expect(screen.getByText("Unable to load more skills.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getAllByTestId("skill-row")).toHaveLength(50);
    expect(
      screen.getByRole("button", { name: "Next" }).hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.fetchNextPage).toHaveBeenCalledOnce();
  });

  it("does not claim an incomplete failed search has no matches", () => {
    testState.searchValue = "missing";
    testState.hasNextPage = true;
    testState.isFetchNextPageError = true;
    testState.error = new Error("next page failed");
    render(<SkillsList />);
    fireEvent.click(screen.getByRole("button", { name: "Apply search" }));
    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) ascending",
      }),
    );

    expect(
      screen.getByText("Search incomplete. Retry to check remaining skills."),
    ).toBeTruthy();
    expect(screen.queryByText("No matching skills.")).toBeNull();
  });

  it("hides unknown activations when none exist", () => {
    render(<SkillsList />);

    expect(screen.queryByText("Unknown activations")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "View unknown activations" }),
    ).toBeNull();
  });

  it("offers unknown activations after finding one", () => {
    testState.unknownActivations = [
      {
        id: "activation_a",
        skillName: "unmatched-skill",
        provider: "claude-code",
        source: "hook",
        reason: "unresolved_hash",
        seenAt: new Date("2026-07-16T00:00:00Z"),
      },
    ];
    render(<SkillsList />);

    fireEvent.click(
      screen.getByRole("button", { name: "View unknown activations" }),
    );
    expect(screen.getByText("unmatched-skill")).toBeTruthy();
  });

  it("only drains skill pages when a metric sort is selected", async () => {
    testState.hasNextPage = true;
    render(<SkillsList />);

    expect(testState.fetchNextPage).not.toHaveBeenCalled();
    expect(screen.getAllByTestId("skill-row")).toHaveLength(50);
    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) ascending",
      }),
    );
    await waitFor(() => expect(testState.fetchNextPage).toHaveBeenCalledOnce());
    expect(
      screen.getByText("Loading all skills to finish this view..."),
    ).toBeTruthy();
    expect(screen.queryByTestId("skill-row")).toBeNull();
  });

  it("shows and retries an insights failure without hiding loaded skills", () => {
    testState.insightsData = undefined;
    testState.insightsError = new Error("insights unavailable");
    render(<SkillsList />);

    expect(screen.getByText("Unable to load skill insights")).toBeTruthy();
    expect(screen.getAllByTestId("skill-row")).toHaveLength(50);
    fireEvent.click(screen.getByRole("button", { name: "Retry insights" }));
    expect(testState.insightsRefetch).toHaveBeenCalledOnce();
  });

  it("returns to paginated skills when metric insights are unavailable", async () => {
    testState.hasNextPage = true;
    testState.insightsData = undefined;
    testState.insightsError = new Error("insights unavailable");
    render(<SkillsList />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) ascending",
      }),
    );

    await waitFor(() =>
      expect(screen.getAllByTestId("skill-row")).toHaveLength(50),
    );
    expect(testState.fetchNextPage).not.toHaveBeenCalled();
  });

  it("keeps metric sorting cleared after insights recover", async () => {
    testState.skills = [
      { ...makeSkills(1)[0], id: "zulu", displayName: "Zulu" },
      { ...makeSkills(1)[0], id: "alpha", displayName: "Alpha" },
      { ...makeSkills(1)[0], id: "mike", displayName: "Mike" },
      ...makeSkills(48),
    ];
    const { rerender } = render(<SkillsList />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) ascending",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByText("Example 47")).toBeTruthy();

    testState.insightsData = undefined;
    testState.insightsError = new Error("insights unavailable");
    rerender(<SkillsList />);
    await waitFor(() =>
      expect(
        screen
          .getByRole("button", { name: "Previous" })
          .hasAttribute("disabled"),
      ).toBe(true),
    );

    testState.metricSkillRequests = [];
    testState.insightsData = { result: { insights: [] } };
    testState.insightsError = null;
    rerender(<SkillsList />);

    expect(
      screen.getByRole("button", {
        name: "Sort by Activations (30d) ascending",
      }),
    ).toBeTruthy();
    expect(testState.metricSkillRequests).toEqual([]);
    expect(renderedSkillNames().slice(0, 3)).toEqual(["Zulu", "Alpha", "Mike"]);
    expect(
      screen.getByRole("button", { name: "Previous" }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("badges skills with open suggestions and drains suggestion pages", async () => {
    testState.suggestions = [makeSuggestion(0)];
    testState.suggestionTotal = 2;
    testState.suggestionHasNextPage = true;
    render(<SkillsList />);

    expect(screen.getByText("Suggested edit")).toBeTruthy();
    await waitFor(() =>
      expect(testState.suggestionFetchNextPage).toHaveBeenCalledOnce(),
    );
    expect(
      screen
        .getByRole("button", { name: "Approve all (2)" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });

  it("disables bulk approval and retries when suggestion loading fails", () => {
    testState.suggestions = [makeSuggestion(0)];
    testState.suggestionTotal = 2;
    testState.suggestionError = new Error("suggestions unavailable");
    render(<SkillsList />);

    expect(screen.getByText("suggestions unavailable")).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Approve all (2)" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(
      screen.getByRole("button", { name: "Retry suggested edits" }),
    );
    expect(testState.suggestionRefetch).toHaveBeenCalledOnce();
  });

  it("lists every skill and reports mixed bulk outcomes exactly", async () => {
    testState.suggestions = [0, 1, 2, 3].map(makeSuggestion);
    testState.suggestionTotal = 4;
    testState.approveAll.mutateAsync.mockResolvedValue({
      items: [
        { outcome: "applied" },
        { outcome: "superseded" },
        { outcome: "conflict" },
        { outcome: "failed" },
      ],
    });
    render(<SkillsList />);

    fireEvent.click(screen.getByRole("button", { name: "Approve all (4)" }));
    const region = screen.getByRole("region", {
      name: "Skills included in bulk approval",
    });
    expect(region.getAttribute("tabindex")).toBe("0");
    expect(screen.getAllByText("Example 0").length).toBeGreaterThan(1);
    expect(screen.getAllByText("Example 3").length).toBeGreaterThan(1);
    fireEvent.click(screen.getByRole("button", { name: "Approve 4 edits" }));

    await waitFor(() =>
      expect(testState.approveAll.mutateAsync).toHaveBeenCalledWith({
        request: {
          approveAllSkillSuggestionsRequestBody: {
            suggestionIds: [
              "suggestion_0",
              "suggestion_1",
              "suggestion_2",
              "suggestion_3",
            ],
          },
        },
      }),
    );
    expect(testState.toastInfo).toHaveBeenCalledWith(
      "Applied 1, superseded 1, conflicts 1, failed 1, skipped 0.",
    );
    expect(testState.invalidateSuggestions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateEfficacy).toHaveBeenCalledWith(
      testState.queryClient,
    );
  });

  it("confirms loaded IDs and excludes suggestions that appear after opening", async () => {
    testState.suggestions = [makeSuggestion(0), makeSuggestion(1)];
    testState.suggestionTotal = 99;
    testState.approveAll.mutateAsync.mockResolvedValue({ items: [] });
    const { rerender } = render(<SkillsList />);

    fireEvent.click(screen.getByRole("button", { name: "Approve all (2)" }));
    const region = screen.getByRole("region", {
      name: "Skills included in bulk approval",
    });
    expect(within(region).getByText("Example 0")).toBeTruthy();
    expect(within(region).getByText("Example 1")).toBeTruthy();

    testState.suggestions = [
      makeSuggestion(0),
      makeSuggestion(1),
      makeSuggestion(2),
    ];
    rerender(<SkillsList />);
    expect(within(region).queryByText("Example 2")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Approve 2 edits" }));

    await waitFor(() =>
      expect(testState.approveAll.mutateAsync).toHaveBeenCalledWith({
        request: {
          approveAllSkillSuggestionsRequestBody: {
            suggestionIds: ["suggestion_0", "suggestion_1"],
          },
        },
      }),
    );
    expect(testState.toastInfo).toHaveBeenCalledWith(
      "Applied 0, superseded 0, conflicts 0, failed 0, skipped 2.",
    );
  });

  it("keeps a transport warning visible through a zero-result refresh", async () => {
    testState.suggestions = [makeSuggestion(0)];
    testState.suggestionTotal = 1;
    testState.approveAll.mutateAsync.mockRejectedValue(
      new Error("connection lost"),
    );
    const { rerender } = render(<SkillsList />);

    fireEvent.click(screen.getByRole("button", { name: "Approve all (1)" }));
    fireEvent.click(screen.getByRole("button", { name: "Approve 1 edits" }));
    expect(
      await screen.findByText(
        /Some edits may have applied. Review the refreshed state before retrying/,
      ),
    ).toBeTruthy();
    expect(testState.invalidateSuggestions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateSkills).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateSkill).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateDistributions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateVersions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateFeedback).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateEfficacy).toHaveBeenCalledWith(
      testState.queryClient,
    );

    testState.suggestions = [];
    testState.suggestionTotal = 0;
    rerender(<SkillsList />);
    expect(screen.getByText(/Some edits may have applied/)).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Approve 1 edits" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Approve 1 edits" }));
    expect(testState.approveAll.mutateAsync).toHaveBeenCalledOnce();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByText(/Some edits may have applied/)).toBeNull();
    expect(screen.queryByRole("button", { name: /Approve all/ })).toBeNull();
  });

  it("disables bulk controls through reconciliation and resets uncertainty on reopen", async () => {
    testState.suggestions = [makeSuggestion(0)];
    testState.suggestionTotal = 1;
    testState.approveAll.mutateAsync
      .mockRejectedValueOnce(new Error("connection lost"))
      .mockResolvedValueOnce({ items: [{ outcome: "applied" }] });
    render(<SkillsList />);

    fireEvent.click(screen.getByRole("button", { name: "Approve all (1)" }));
    const submit = screen.getByRole("button", { name: "Approve 1 edits" });
    const cancel = screen.getByRole("button", { name: "Cancel" });
    let finishInvalidation!: () => void;
    testState.invalidateSuggestions.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        finishInvalidation = resolve;
      }),
    );
    fireEvent.click(submit);

    await waitFor(() =>
      expect(testState.invalidateSuggestions).toHaveBeenCalled(),
    );
    expect(submit.hasAttribute("disabled")).toBe(true);
    expect(cancel.hasAttribute("disabled")).toBe(true);
    fireEvent.click(cancel);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByText("Approve all suggested edits?")).toBeTruthy();

    await act(async () => finishInvalidation());
    expect(await screen.findByText(/Some edits may have applied/)).toBeTruthy();
    expect(submit.hasAttribute("disabled")).toBe(true);
    expect(cancel.hasAttribute("disabled")).toBe(false);
    fireEvent.click(submit);
    expect(testState.approveAll.mutateAsync).toHaveBeenCalledOnce();

    fireEvent.click(cancel);
    fireEvent.click(screen.getByRole("button", { name: "Approve all (1)" }));
    expect(
      screen
        .getByRole("button", { name: "Approve 1 edits" })
        .hasAttribute("disabled"),
    ).toBe(false);
    expect(screen.queryByText(/Some edits may have applied/)).toBeNull();
  });

  it("does not turn refresh failure into a bulk mutation failure", async () => {
    testState.suggestions = [makeSuggestion(0)];
    testState.suggestionTotal = 1;
    testState.approveAll.mutateAsync.mockResolvedValue({
      items: [{ outcome: "applied" }],
    });
    testState.invalidateSuggestions.mockRejectedValue(
      new Error("refresh failed"),
    );
    render(<SkillsList />);

    fireEvent.click(screen.getByRole("button", { name: "Approve all (1)" }));
    fireEvent.click(screen.getByRole("button", { name: "Approve 1 edits" }));

    await waitFor(() =>
      expect(testState.toastInfo).toHaveBeenCalledWith(
        "Applied 1, superseded 0, conflicts 0, failed 0, skipped 0.",
      ),
    );
    expect(screen.queryByText("Bulk approval failed")).toBeNull();
  });
});
