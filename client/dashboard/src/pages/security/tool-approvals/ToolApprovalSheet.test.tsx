import type { RiskPolicyApproval } from "@gram/client/models/components/riskpolicyapproval.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { requesterLabel } from "./approval-labels";
import { ToolApprovalSheet } from "./ToolApprovalSheet";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
}));

vi.mock("@gram/client/react-query/riskDecideApproval.js", () => ({
  useRiskDecideApprovalMutation: () => ({
    isPending: false,
    mutate: mocks.mutate,
  }),
}));

vi.mock("@gram/client/react-query/riskListApprovals.js", () => ({
  invalidateAllRiskListApprovals: vi.fn(),
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

function approval(
  overrides: Partial<RiskPolicyApproval> = {},
): RiskPolicyApproval {
  return {
    id: "00000000-0000-0000-0000-000000000001",
    policyId: "00000000-0000-0000-0000-000000000002",
    policyName: "Approve Slack posts",
    policyVersion: 1,
    status: "pending",
    requesterUrn: "agent:3f6c0d2e-0000-4000-8000-000000000003",
    surface: "hosted_mcp",
    toolName: "slack__post_message",
    argumentsPreview: { channel: "C1", text: "Deploy finished" },
    reason: "Tell the team the deploy finished.",
    canDecide: true,
    createdAt: new Date("2026-10-19T10:00:00Z"),
    updatedAt: new Date("2026-10-19T10:00:00Z"),
    ...overrides,
  };
}

afterEach(() => {
  cleanup();
  mocks.mutate.mockReset();
});

describe("ToolApprovalSheet", () => {
  it("shows the arguments and reason and approves with the note", () => {
    render(
      <ToolApprovalSheet
        approval={approval()}
        members={[]}
        onOpenChange={vi.fn<(open: boolean) => void>()}
      />,
    );

    expect(screen.getByText("slack__post_message")).toBeTruthy();
    expect(screen.getByText("Tell the team the deploy finished.")).toBeTruthy();
    // Each argument is its own row, with the value as plain text.
    expect(screen.getByText("channel")).toBeTruthy();
    expect(screen.getByText("C1")).toBeTruthy();
    expect(screen.getByText("Deploy finished")).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText("Optional"), {
      target: { value: "Go ahead." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Approve once" }));

    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        riskPolicyApprovalDecisionRequestBody: {
          id: "00000000-0000-0000-0000-000000000001",
          decision: "approve",
          note: "Go ahead.",
        },
      },
    });
  });

  it("hides the decision buttons from people who cannot decide", () => {
    render(
      <ToolApprovalSheet
        approval={approval({ canDecide: false })}
        members={[]}
        onOpenChange={vi.fn<(open: boolean) => void>()}
      />,
    );

    expect(screen.queryByRole("button", { name: "Approve once" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Deny" })).toBeNull();
  });
});

describe("requesterLabel", () => {
  it("names agents by a short id and falls back to the URN", () => {
    expect(
      requesterLabel("agent:3f6c0d2e-0000-4000-8000-000000000003", []),
    ).toBe("Agent 3f6c0d2e");
    expect(requesterLabel("user:someone", [])).toBe("user:someone");
  });
});
