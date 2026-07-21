import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SkillsList from "./SkillsList";

const testState = vi.hoisted(() => ({
  fetchNextPage: vi.fn().mockResolvedValue(undefined),
  isFetchNextPageError: false,
  error: null as Error | null,
  skills: [] as Array<Record<string, unknown>>,
  unknownEnabled: false,
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
  useSkillsInfinite: () => ({
    data: { pages: [{ result: { skills: testState.skills } }] },
    isPending: false,
    isFetching: false,
    isFetchingNextPage: false,
    isFetchNextPageError: testState.isFetchNextPageError,
    hasNextPage: true,
    error: testState.error,
    fetchNextPage: testState.fetchNextPage,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/unknownSkillActivations.js", () => ({
  useUnknownSkillActivationsInfinite: (
    _request: unknown,
    _security: unknown,
    options?: { enabled?: boolean },
  ) => {
    testState.unknownEnabled = options?.enabled ?? true;
    return {
      data: { pages: [{ result: { activations: [] } }] },
      isPending: false,
      isFetchingNextPage: false,
      isFetchNextPageError: false,
      hasNextPage: false,
      error: null,
      fetchNextPage: vi.fn(),
      refetch: vi.fn(),
    };
  },
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("./SkillManifestDialog", () => ({ SkillManifestDialog: () => null }));
vi.mock("@/components/page-layout", () => {
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  const Search = ({ onChange }: { onChange: (value: string) => void }) => (
    <button onClick={() => onChange("example")}>Apply search</button>
  );
  const Toolbar = Object.assign(Wrapper, {
    Search,
    Filters: () => null,
    Count: Wrapper,
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
vi.mock("@speakeasy-api/moonshine", () => ({
  Badge: ({ children }: { children: ReactNode }) => <span>{children}</span>,
  Icon: () => <span />,
  Table: ({
    data,
    noResultsMessage,
  }: {
    data: Array<{ id: string }>;
    noResultsMessage: ReactNode;
  }) => (
    <div>
      {data.length === 0
        ? noResultsMessage
        : data.map((skill) => (
            <div data-testid="skill-row" key={skill.id}>
              {skill.id}
            </div>
          ))}
    </div>
  ),
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

beforeEach(() => {
  testState.fetchNextPage.mockReset();
  testState.fetchNextPage.mockResolvedValue(undefined);
  testState.isFetchNextPageError = false;
  testState.error = null;
  testState.skills = makeSkills(250);
  testState.unknownEnabled = false;
});

afterEach(cleanup);

describe("SkillsList pagination surfaces", () => {
  it("bounds rendered rows and resets the bound when search changes", async () => {
    render(<SkillsList />);
    expect(screen.getAllByTestId("skill-row")).toHaveLength(200);
    fireEvent.click(screen.getByRole("button", { name: "Show more results" }));
    expect(screen.getAllByTestId("skill-row")).toHaveLength(250);
    fireEvent.click(screen.getByRole("button", { name: "Apply search" }));
    await waitFor(() => {
      expect(screen.getAllByTestId("skill-row")).toHaveLength(200);
    });
  });

  it("keeps loaded rows visible and exposes an explicit retry after a page failure", () => {
    testState.isFetchNextPageError = true;
    testState.error = new Error("next page failed");
    render(<SkillsList />);

    expect(screen.getAllByTestId("skill-row")).toHaveLength(200);
    expect(screen.getByText("Unable to load more skills.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.fetchNextPage).toHaveBeenCalledOnce();
  });

  it("does not claim an incomplete failed search has no matches", () => {
    testState.skills = [];
    testState.isFetchNextPageError = true;
    testState.error = new Error("next page failed");
    render(<SkillsList />);

    expect(
      screen.getByText("Search incomplete. Retry to check remaining skills."),
    ).toBeTruthy();
    expect(screen.queryByText("No matching skills.")).toBeNull();
  });

  it("loads unknown activations only when requested", () => {
    render(<SkillsList />);

    expect(testState.unknownEnabled).toBe(false);
    fireEvent.click(
      screen.getByRole("button", { name: "View unknown activations" }),
    );
    expect(testState.unknownEnabled).toBe(true);
    expect(screen.getByText("No unknown activations found.")).toBeTruthy();
  });
});
