import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// Controllable return value for the main data hook. Set per test before render.
let bucketsResult: {
  data: unknown;
  isLoading: boolean;
  isFetching: boolean;
};

vi.mock("react-router", () => ({
  useSearchParams: () => [new URLSearchParams(), vi.fn()],
}));

vi.mock("@gram/client/react-query/challengeBuckets.js", () => ({
  useChallengeBuckets: () => bucketsResult,
}));

vi.mock("@gram/client/react-query/challenges.js", () => ({
  useChallenges: () => ({ data: undefined }),
}));

vi.mock("./useGrantFlow", () => ({
  useGrantFlow: () => ({
    actionsColumn: { key: "actions", header: "", render: () => null },
    grantFlowPortals: null,
    recentlyResolvedIds: new Set<string>(),
    animatingOutIds: new Set<string>(),
  }),
}));

vi.mock("./useChallengeRowColumns", () => ({
  useChallengeRowColumns: () => [],
}));

vi.mock("@/components/ui/Skeleton", () => ({
  SkeletonTable: () => <div data-testid="skeleton-table" />,
}));

import { ChallengesTab } from "./ChallengesTab";

afterEach(cleanup);

describe("ChallengesTab loading state", () => {
  it("shows the loading skeleton (not the empty state) while a slow first-page fetch is in flight", () => {
    // Mimics `keepPreviousData`: the query is fetching but `isLoading` is false
    // because placeholder data keeps it out of the pending state. This is the
    // exact condition that used to flash the "no challenges" empty state.
    bucketsResult = { data: undefined, isLoading: false, isFetching: true };

    render(<ChallengesTab />);

    expect(screen.getByTestId("skeleton-table")).toBeTruthy();
    expect(screen.queryByText("No denied access attempts")).toBeNull();
  });

  it("shows the empty state once the fetch settles with no results", () => {
    bucketsResult = {
      data: { buckets: [], total: 0 },
      isLoading: false,
      isFetching: false,
    };

    render(<ChallengesTab />);

    expect(screen.getByText("No denied access attempts")).toBeTruthy();
    expect(screen.queryByTestId("skeleton-table")).toBeNull();
  });
});
