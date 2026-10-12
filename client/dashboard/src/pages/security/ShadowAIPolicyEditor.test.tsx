import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  SHADOW_AI_ACTION_LOCK_REASON,
  ShadowAIPolicyEditor,
} from "./ShadowAIPolicyEditor";

const mocks = vi.hoisted(() => ({
  mutateCreate: vi.fn(),
  mutateUpdate: vi.fn(),
  goToDetail: vi.fn(),
  pinnedActions: [] as Array<{ value: string; reason: string } | undefined>,
  decisionsFor: [] as string[],
  step: null as string | null,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("nuqs", () => ({
  useQueryState: () => [mocks.step, vi.fn()],
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    policyCenter: { goTo: vi.fn(), detail: { goTo: mocks.goToDetail } },
  }),
}));

vi.mock("./use-toggle-policy-enabled", () => ({
  useTogglePolicyEnabled: () => ({ mutate: vi.fn(), isPending: false }),
  togglePolicyEnabledVariables: vi.fn(),
}));

vi.mock("./PolicyCenter", () => ({
  ActionPicker: ({
    pinnedAction,
  }: {
    pinnedAction?: { value: string; reason: string };
  }) => {
    mocks.pinnedActions.push(pinnedAction);
    return null;
  },
  PolicyAudiencePicker: () => null,
}));

vi.mock("./shadow-ai/ToolDecisionsSection", () => ({
  ToolDecisionsSection: ({ policy }: { policy: RiskPolicy }) => {
    mocks.decisionsFor.push(policy.id);
    return <p>decisions for {policy.name}</p>;
  },
}));

vi.mock("@gram/client/react-query/riskCreatePolicy.js", () => ({
  useRiskCreatePolicyMutation: () => ({
    isPending: false,
    mutate: mocks.mutateCreate,
  }),
}));

vi.mock("@gram/client/react-query/riskPoliciesUpdate.js", () => ({
  useRiskPoliciesUpdateMutation: () => ({
    isPending: false,
    mutate: mocks.mutateUpdate,
  }),
}));

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  invalidateAllRiskListPolicies: vi.fn(),
}));

vi.mock("@gram/client/react-query/riskPoliciesGet.js", () => ({
  invalidateAllRiskPoliciesGet: vi.fn(),
}));

vi.mock("@gram/client/react-query/shadowMCPInventory.js", () => ({
  invalidateAllShadowMCPInventory: vi.fn(),
}));

function policy(overrides: Partial<RiskPolicy> = {}): RiskPolicy {
  return {
    id: "policy-1",
    projectId: "project-1",
    name: "Shadow AI",
    policyType: "standard",
    sources: ["shadow_mcp"],
    action: "block",
    enabled: true,
    audienceType: "everyone",
    score: 8,
    version: 1,
    createdAt: new Date("2026-09-01T00:00:00Z"),
    updatedAt: new Date("2026-09-01T00:00:00Z"),
    shadowMcpDisposition: "allow_all",
    ...overrides,
  } as RiskPolicy;
}

function renderEditor(target: RiskPolicy | null) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <ShadowAIPolicyEditor policy={target} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("ShadowAIPolicyEditor", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.pinnedActions.length = 0;
    mocks.decisionsFor.length = 0;
    mocks.step = null;
  });

  it("pins the action to Deny with the reason", () => {
    renderEditor(null);

    expect(mocks.pinnedActions[0]).toEqual({
      value: "block",
      reason: SHADOW_AI_ACTION_LOCK_REASON,
    });
  });

  it("creates a blocking shadow_mcp policy that allows what it has no decision for", () => {
    renderEditor(null);

    expect(
      screen.getByText(/approve or disallow them one by one/),
    ).toBeTruthy();
    expect(mocks.decisionsFor).toEqual([]);

    fireEvent.click(screen.getByText("Create policy"));

    expect(mocks.mutateCreate).toHaveBeenCalledTimes(1);
    expect(
      mocks.mutateCreate.mock.calls[0]?.[0]?.request
        .createRiskPolicyRequestBody,
    ).toEqual(
      expect.objectContaining({
        name: "Shadow AI",
        enabled: true,
        sources: ["shadow_mcp"],
        action: "block",
        audienceType: "everyone",
        audiencePrincipalUrns: [],
        autoName: false,
        shadowMcpDisposition: "allow_all",
      }),
    );
  });

  it("carries the tool decisions once the policy exists", () => {
    renderEditor(policy());

    expect(screen.getByText("decisions for Shadow AI")).toBeTruthy();
    expect(mocks.decisionsFor).toEqual(["policy-1"]);
  });

  it("summarizes a stored block_all posture on the review step", () => {
    mocks.step = "review";
    renderEditor(policy({ shadowMcpDisposition: "block_all" }));

    expect(screen.getByText("MCP servers blocked until allowed")).toBeTruthy();
  });
});
