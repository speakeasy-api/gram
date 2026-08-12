import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// Controllable return value for the main data hook. Set per test before render.
// A stable object reference matters: the component's accumulate effect keys on
// the `data` reference, so reusing the same object across renders mirrors how
// `keepPreviousData` hands back the previous page while a new request is in
// flight (the effect does not re-run, and `accumulated` stays reset to []).
let bucketsResult: {
  data: { buckets: ChallengeBucket[]; total: number } | undefined;
  isLoading: boolean;
  isFetching: boolean;
  isPlaceholderData: boolean;
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

/** A bucket that passes `isDisplayableBucket` (scope + resource present). */
function bucket(id: string): ChallengeBucket {
  return {
    id,
    challengeCount: 1,
    challengeIds: [id],
    evaluatedGrantCount: 0,
    matchedGrantCount: 0,
    firstSeen: new Date(),
    lastSeen: new Date(),
    operation: "require",
    organizationId: "org",
    outcome: "deny",
    principalType: "user",
    principalUrn: "user:1",
    reason: "no_grants",
    scope: "toolset:read",
    resourceKind: "toolset",
    resourceId: "resource-1",
    roleSlugs: [],
  };
}

describe("ChallengesTab loading state", () => {
  it("shows the loading skeleton on the initial load (no data yet)", () => {
    bucketsResult = {
      data: undefined,
      isLoading: true,
      isFetching: true,
      isPlaceholderData: false,
    };

    render(<ChallengesTab />);

    expect(screen.getByTestId("skeleton-table")).toBeTruthy();
    expect(screen.queryByText("No denied access attempts")).toBeNull();
  });

  it("shows a skeleton (not the empty state) while a filter switch is in flight under keepPreviousData", () => {
    // Mirrors keepPreviousData mid-switch: the previous filter's rows linger in
    // `data` (so `isLoading` is false) while the new filter's request is still
    // in flight. Those rows belong to the old filter, so there is nothing to
    // show for the new one yet — the skeleton must render, not the empty state.
    bucketsResult = {
      data: { buckets: [bucket("b1")], total: 39 },
      isLoading: false,
      isFetching: true,
      isPlaceholderData: true,
    };

    render(<ChallengesTab />);

    expect(screen.getByTestId("skeleton-table")).toBeTruthy();
    expect(screen.queryByText("No denied access attempts")).toBeNull();
  });

  it("does not flash the empty state when switching to a filter whose rows are already cached", () => {
    // Rows served straight from the query cache: real data (not placeholder) and
    // not loading, with a background refetch in flight. The filter switch resets
    // `accumulated` during render and the accumulate effect only refills it after
    // paint, so the rows must be derived during render to avoid an empty frame.
    bucketsResult = {
      data: { buckets: [bucket("b1")], total: 39 },
      isLoading: false,
      isFetching: true,
      isPlaceholderData: false,
    };

    render(<ChallengesTab />);
    expect(screen.queryByText("No denied access attempts")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /^All/ }));

    expect(screen.queryByText("No challenges found")).toBeNull();
    expect(screen.queryByTestId("skeleton-table")).toBeNull();
  });

  it("keeps the empty state (no skeleton) during a background refetch of a filter with no results", () => {
    // Real, settled data for the current filter (not placeholder) with a refetch
    // in flight. This must NOT flash a skeleton over the stable empty state.
    bucketsResult = {
      data: { buckets: [], total: 0 },
      isLoading: false,
      isFetching: true,
      isPlaceholderData: false,
    };

    render(<ChallengesTab />);

    expect(screen.getByText("No denied access attempts")).toBeTruthy();
    expect(screen.queryByTestId("skeleton-table")).toBeNull();
  });
});
