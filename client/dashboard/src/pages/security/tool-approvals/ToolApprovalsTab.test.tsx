import type { RiskPolicyApproval } from "@gram/client/models/components/riskpolicyapproval.js";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ToolApprovalsTab } from "./ToolApprovalsTab";

const mocks = vi.hoisted(() => ({
  listParams: [] as unknown[],
  pending: [] as RiskPolicyApproval[],
  // What reading one request by id returns.
  byID: new Map<string, RiskPolicyApproval>(),
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
      data: {
        approvals: params.status === "pending" ? mocks.pending : [decided],
      },
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
  useRiskGetApproval: (
    request: { id: string },
    _security: unknown,
    options: { enabled?: boolean },
  ) => ({
    data: options.enabled ? mocks.byID.get(request.id) : undefined,
  }),
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
  mocks.pending = [];
  mocks.byID.clear();
});

describe("ToolApprovalsTab", () => {
  it("opens the request a Slack card links to, even once it is decided", async () => {
    mocks.byID.set(decided.id, decided);
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

  it("keeps an open request current once it is decided elsewhere", async () => {
    const waiting: RiskPolicyApproval = {
      ...decided,
      id: "00000000-0000-0000-0000-000000000008",
      status: "pending",
      canDecide: true,
    };
    mocks.pending = [waiting];
    // Decided in Slack: the request now reads as approved.
    mocks.byID.set(waiting.id, {
      ...waiting,
      status: "approved",
      canDecide: false,
    });
    render(
      <MemoryRouter initialEntries={["/?tab=tool-approvals"]}>
        <ToolApprovalsTab />
      </MemoryRouter>,
    );

    fireEvent.click(
      screen.getByText("Post the release note the user asked for."),
    );

    const sheet = await screen.findByRole("dialog");
    expect(within(sheet).getByText("Approved")).toBeTruthy();
    expect(
      within(sheet).queryByRole("button", { name: "Approve once" }),
    ).toBeNull();
  });
});
