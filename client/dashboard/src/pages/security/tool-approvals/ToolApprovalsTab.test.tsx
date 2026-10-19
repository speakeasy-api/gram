import type { RiskPolicyApproval } from "@gram/client/models/components/riskpolicyapproval.js";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ToolApprovalsTab } from "./ToolApprovalsTab";

const mocks = vi.hoisted(() => ({
  listParams: [] as unknown[],
}));

const decided: RiskPolicyApproval = {
  id: "00000000-0000-0000-0000-000000000009",
  policyId: "00000000-0000-0000-0000-000000000002",
  policyName: "Approve team Slack posts",
  policyVersion: 1,
  status: "approved",
  requesterUrn: "agent:3f6c0d2e-0000-4000-8000-000000000003",
  surface: "hosted_mcp",
  toolName: "send_message",
  argumentsPreview: { channel: "#releases", text: "Release 2.5 ships Friday." },
  reason: "Post the release note the user asked for.",
  canDecide: false,
  createdAt: new Date("2026-10-19T10:00:00Z"),
  updatedAt: new Date("2026-10-19T10:00:00Z"),
};

vi.mock("@gram/client/react-query/riskListApprovals.js", () => ({
  // The decided request is only in the All list, as on the server.
  useRiskListApprovals: (params: { status?: string }) => {
    mocks.listParams.push(params);
    return {
      data: { approvals: params.status === "pending" ? [] : [decided] },
      isPending: false,
      isError: false,
    };
  },
  invalidateAllRiskListApprovals: vi.fn(),
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));

vi.mock("@gram/client/react-query/riskDecideApproval.js", () => ({
  useRiskDecideApprovalMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));

vi.mock("@gram/client/react-query/riskGetApproval.js", () => ({
  invalidateAllRiskGetApproval: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

afterEach(() => {
  cleanup();
  mocks.listParams.length = 0;
});

describe("ToolApprovalsTab", () => {
  it("opens the request a Slack card links to, even once it is decided", async () => {
    render(
      <MemoryRouter
        initialEntries={[`/?tab=tool-approvals&request=${decided.id}`]}
      >
        <ToolApprovalsTab />
      </MemoryRouter>,
    );

    const sheet = await screen.findByRole("dialog");
    expect(
      within(sheet).getByText("Post the release note the user asked for."),
    ).toBeTruthy();
    expect(mocks.listParams).toContainEqual({ limit: 100 });
  });
});
