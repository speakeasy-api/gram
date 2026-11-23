import type { AccessRequest } from "@gram/client/models/components/accessrequest.js";
import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const NOW = new Date();

const denied: ChallengeBucket = {
  id: "b1",
  challengeCount: 1,
  challengeIds: ["c1"],
  evaluatedGrantCount: 0,
  matchedGrantCount: 0,
  firstSeen: NOW,
  lastSeen: NOW,
  operation: "require",
  organizationId: "org",
  outcome: "deny",
  principalType: "user",
  principalUrn: "user:ada",
  reason: "no_grants",
  scope: "log:read",
  resourceKind: "project",
  resourceId: "p1",
  roleSlugs: [],
};

vi.mock("react-router", () => ({
  useSearchParams: () => [new URLSearchParams(), vi.fn()],
}));

const bucketsResult = {
  data: { buckets: [denied], total: 1 },
  isLoading: false,
  isFetching: false,
  isPlaceholderData: false,
};
vi.mock("@gram/client/react-query/challengeBuckets.js", () => ({
  useChallengeBuckets: () => bucketsResult,
}));

vi.mock("@gram/client/react-query/challenges.js", () => ({
  useChallenges: () => ({ data: undefined }),
}));

vi.mock("./useGrantFlow", () => ({
  useGrantFlow: () => ({
    actionsColumn: {
      key: "actions",
      header: "",
      render: () => <button type="button">Grant</button>,
    },
    grantFlowPortals: null,
    recentlyResolvedIds: new Set<string>(),
    animatingOutIds: new Set<string>(),
  }),
}));

vi.mock("./useChallengeRowColumns", () => ({
  useChallengeRowColumns: () => [
    {
      key: "identity",
      header: "Identity",
      render: (row: ChallengeBucket) => <span>{row.principalUrn}</span>,
    },
  ],
}));

vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => ({ data: [], isError: false }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: (state: { disabled: boolean }) => JSX.Element;
  }) => children({ disabled: false }),
}));

import { ChallengesTab } from "./ChallengesTab";

afterEach(cleanup);

const asked: AccessRequest = {
  id: "r1",
  createdAt: NOW,
  updatedAt: NOW,
  expiresAt: new Date(NOW.getTime() + 60_000),
  requesterName: "Ada",
  requesterUserId: "ada",
  scope: "log:read",
  message: "incident 42",
  selector: { resourceKind: "project", resourceId: "p1" },
  status: "pending",
} as AccessRequest;

describe("ChallengesTab with pending access requests", () => {
  it("says the member asked and reviews the request instead of granting", () => {
    const onReviewRequest = vi.fn<(request: AccessRequest) => void>();
    render(
      <ChallengesTab
        pendingRequests={[asked]}
        onReviewRequest={onReviewRequest}
      />,
    );

    expect(screen.getByTestId("challenge-request-line").textContent).toContain(
      "incident 42",
    );
    expect(screen.queryByText("Grant")).toBeNull();
    fireEvent.click(screen.getByText("Review"));
    expect(onReviewRequest).toHaveBeenCalledWith(asked);
  });

  it("keeps the grant action when no request matches", () => {
    render(
      <ChallengesTab
        pendingRequests={[{ ...asked, scope: "chat:read" }]}
        onReviewRequest={vi.fn<(request: AccessRequest) => void>()}
      />,
    );

    expect(screen.queryByTestId("challenge-request-line")).toBeNull();
    expect(screen.getByText("Grant")).toBeTruthy();
  });
});
