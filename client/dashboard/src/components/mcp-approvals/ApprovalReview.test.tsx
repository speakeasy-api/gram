import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApprovalReview } from "./ApprovalReview";

const mocks = vi.hoisted(() => ({
  detail: undefined as unknown,
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-id", slug: "test-project" }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/lib/dates", () => ({
  HumanizeDateTime: () => <>14 Aug</>,
}));

// The evidence panel has its own tests; this file is about who asked.
vi.mock("@/components/mcp-approvals/EvidencePanel", () => ({
  EvidenceGroup: ({ children }: { children: ReactNode }) => <>{children}</>,
  EvidencePanel: () => null,
  StatusBadge: ({ status }: { status: string }) => <span>{status}</span>,
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("@gram/client/react-query/getMcpApprovalRequest.js", () => ({
  invalidateGetMcpApprovalRequest: vi.fn(),
  useGetMcpApprovalRequest: () => ({
    data: mocks.detail,
    error: null,
    refetch: vi.fn(),
  }),
}));

vi.mock("@gram/client/react-query/listMcpApprovalRequests.js", () => ({
  invalidateAllListMcpApprovalRequests: vi.fn(),
}));

vi.mock("@gram/client/react-query/refreshMcpApprovalEvidence.js", () => ({
  useRefreshMcpApprovalEvidenceMutation: () => ({
    isPending: false,
    mutateAsync: vi.fn(),
  }),
}));

vi.mock("@gram/client/react-query/startMcpResearch.js", () => ({
  useStartMcpResearchMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));

const createdAt = new Date("2026-08-14T10:00:00Z");

function detail({
  status,
  requesters = [],
  decisions = [],
}: {
  status: string;
  requesters?: unknown[];
  decisions?: unknown[];
}) {
  return {
    request: {
      id: "request-id",
      status,
      createdAt,
      versionPinned: true,
    },
    requesters,
    decisions,
    researchReports: [],
  };
}

const denial = {
  id: "decision-id",
  decision: "denied",
  decidedAt: createdAt,
  rationale: "Not needed here.",
};

describe("ApprovalReview requesters", () => {
  beforeEach(() => {
    mocks.detail = undefined;
  });

  afterEach(() => {
    cleanup();
  });

  it("says no one asked when an admin decided a review nobody requested", () => {
    mocks.detail = detail({ status: "denied", decisions: [denial] });

    render(<ApprovalReview requestId="request-id" />);

    expect(
      screen.getByText(
        "No one has asked for it. An admin decided without a request.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/identifiable user/)).toBeNull();
    // Nobody raised it, so the header must not date a request.
    expect(screen.queryByText(/raised/)).toBeNull();
    expect(screen.getByText('"Not needed here."')).toBeTruthy();
  });

  it("says no one asked when a review without requesters has no decision", () => {
    mocks.detail = detail({ status: "requested" });

    render(<ApprovalReview requestId="request-id" />);

    expect(screen.getByText("No one has asked for it.")).toBeTruthy();
    expect(screen.queryByText(/raised/)).toBeNull();
  });

  it("lists who asked and when the request was raised", () => {
    mocks.detail = detail({
      status: "requested",
      requesters: [
        {
          userId: "user-id",
          userEmail: "person@example.com",
          note: "Needed for the release.",
          requestedAt: createdAt,
        },
      ],
    });

    render(<ApprovalReview requestId="request-id" />);

    expect(screen.getByText("person@example.com")).toBeTruthy();
    expect(screen.getByText('"Needed for the release."')).toBeTruthy();
    expect(screen.getByText(/raised/)).toBeTruthy();
    expect(screen.queryByText(/No one has asked for it/)).toBeNull();
  });
});
