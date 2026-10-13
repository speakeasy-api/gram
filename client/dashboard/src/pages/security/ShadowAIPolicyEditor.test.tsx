import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { accessLibrary, detection } from "./shadow-ai/access-fixtures";
import { ShadowAIPolicyEditor } from "./ShadowAIPolicyEditor";

const mocks = vi.hoisted(() => ({
  mutateCreate: vi.fn<(variables: unknown) => Promise<{ id: string }>>(),
  mutateUpdate: vi.fn<(variables: unknown) => Promise<unknown>>(),
  setDecision: vi.fn<(variables: unknown) => Promise<unknown>>(),
  goToDetail: vi.fn(),
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
  ActionPicker: () => null,
  PolicyAudiencePicker: () => <p>audience picker</p>,
}));

vi.mock("@gram/client/react-query/riskCreatePolicy.js", () => ({
  useRiskCreatePolicyMutation: () => ({
    isPending: false,
    mutateAsync: mocks.mutateCreate,
  }),
}));

vi.mock("@gram/client/react-query/riskPoliciesUpdate.js", () => ({
  useRiskPoliciesUpdateMutation: () => ({
    isPending: false,
    mutateAsync: mocks.mutateUpdate,
  }),
}));

vi.mock("@gram/client/react-query/setAIToolDecision.js", () => ({
  useSetAIToolDecisionMutation: () => ({ mutateAsync: mocks.setDecision }),
}));

vi.mock("@gram/client/react-query/aiScanTargets.js", () => ({
  useAiScanTargets: () => ({
    data: { listVersion: 7, targets: accessLibrary() },
    isPending: false,
    isError: false,
  }),
  invalidateAllAiScanTargets: vi.fn(),
}));

vi.mock("@gram/client/react-query/aiDetections.js", () => ({
  useAiDetections: () => ({
    data: { detections: [detection()] },
    isPending: false,
    isError: false,
  }),
  invalidateAllAiDetections: vi.fn(),
}));

vi.mock("@gram/client/react-query/aiDetectionUsers.js", () => ({
  invalidateAllAiDetectionUsers: vi.fn(),
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

const cursorControl = () =>
  screen.getByRole("group", { name: "Access decision for Cursor" });

describe("ShadowAIPolicyEditor", () => {
  beforeEach(() => {
    mocks.mutateCreate.mockResolvedValue({ id: "policy-new" });
    mocks.mutateUpdate.mockResolvedValue({});
    mocks.setDecision.mockResolvedValue({});
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.step = null;
  });

  it("walks Access, Scope and Review over the whole target library", () => {
    renderEditor(null);

    for (const title of ["Access", "Scope", "Review"]) {
      expect(screen.getAllByText(title).length).toBeGreaterThan(0);
    }
    expect(screen.getByText("Cursor")).toBeTruthy();
    expect(screen.getByText("Acme Tool")).toBeTruthy();
    expect(screen.getByText("Not seen yet")).toBeTruthy();
  });

  it("creates the policy, then applies the changed decisions", async () => {
    renderEditor(null);

    fireEvent.click(within(cursorControl()).getByText("Deny"));
    fireEvent.click(screen.getByText("Create policy"));

    await waitFor(() =>
      expect(mocks.goToDetail).toHaveBeenCalledWith("policy-new"),
    );
    expect(mocks.mutateCreate).toHaveBeenCalledTimes(1);
    const created = mocks.mutateCreate.mock.calls[0]?.[0] as
      | { request: { createRiskPolicyRequestBody: unknown } }
      | undefined;
    expect(created?.request.createRiskPolicyRequestBody).toEqual(
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
    expect(mocks.setDecision).toHaveBeenCalledTimes(1);
    expect(mocks.setDecision).toHaveBeenCalledWith({
      request: {
        setAIToolDecisionRequestBody: {
          targetId: "cursor",
          decision: "blocked",
          rationale: undefined,
        },
      },
    });
    expect(mocks.setDecision.mock.invocationCallOrder[0]).toBeGreaterThan(
      mocks.mutateCreate.mock.invocationCallOrder[0] ?? 0,
    );
  });

  it("locks the draft and holds the decisions until the policy write resolves", async () => {
    let resolveCreate: (value: { id: string }) => void = () => {};
    mocks.mutateCreate.mockImplementation(
      () =>
        new Promise<{ id: string }>((resolve) => {
          resolveCreate = resolve;
        }),
    );
    renderEditor(null);

    fireEvent.click(within(cursorControl()).getByText("Deny"));
    fireEvent.click(screen.getByText("Create policy"));

    await waitFor(() => expect(mocks.mutateCreate).toHaveBeenCalledTimes(1));
    expect(mocks.setDecision).not.toHaveBeenCalled();
    expect(
      within(cursorControl()).getByText("Deny").hasAttribute("disabled"),
    ).toBe(true);

    resolveCreate({ id: "policy-new" });

    await waitFor(() => expect(mocks.setDecision).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(mocks.goToDetail).toHaveBeenCalledWith("policy-new"),
    );
  });

  it("names the target whose decision failed and withholds the success toast", async () => {
    mocks.setDecision.mockRejectedValue(new Error("boom"));
    renderEditor(null);

    fireEvent.click(within(cursorControl()).getByText("Deny"));
    fireEvent.click(screen.getByText("Create policy"));

    await waitFor(() =>
      expect(mocks.goToDetail).toHaveBeenCalledWith("policy-new"),
    );
    expect(vi.mocked(toast.error)).toHaveBeenCalledWith(
      expect.stringContaining("the decision for Cursor could not be saved"),
    );
    expect(vi.mocked(toast.success)).not.toHaveBeenCalled();
  });

  it("writes nothing until the policy is created", () => {
    renderEditor(null);

    fireEvent.click(within(cursorControl()).getByText("Deny"));

    expect(mocks.setDecision).not.toHaveBeenCalled();
    expect(mocks.mutateCreate).not.toHaveBeenCalled();
  });

  it("writes nothing for a decision withdrawn before Create", async () => {
    renderEditor(null);

    fireEvent.click(within(cursorControl()).getByText("Deny"));
    fireEvent.click(within(cursorControl()).getByText("Deny"));
    fireEvent.click(screen.getByText("Create policy"));

    await waitFor(() => expect(mocks.goToDetail).toHaveBeenCalled());
    expect(mocks.setDecision).not.toHaveBeenCalled();
  });

  it("saves an existing policy before applying its changed decisions", async () => {
    renderEditor(policy());

    fireEvent.click(within(cursorControl()).getByText("Approve"));
    fireEvent.click(screen.getByText("Save changes"));

    await waitFor(() => expect(mocks.setDecision).toHaveBeenCalledTimes(1));
    expect(mocks.mutateUpdate).toHaveBeenCalledTimes(1);
    expect(mocks.setDecision.mock.invocationCallOrder[0]).toBeGreaterThan(
      mocks.mutateUpdate.mock.invocationCallOrder[0] ?? 0,
    );
    expect(mocks.goToDetail).not.toHaveBeenCalled();
  });

  it("scopes the audience to MCP enforcement and names the refusal message", () => {
    mocks.step = "scope";
    renderEditor(null);

    expect(screen.getByText("audience picker")).toBeTruthy();
    expect(
      screen.getByText(/Audience applies to MCP server enforcement/),
    ).toBeTruthy();
    expect(
      screen.getByText("Message shown when a server is refused"),
    ).toBeTruthy();
    expect(screen.getByText(/Harness refusals at sign-in/)).toBeTruthy();
    expect(screen.queryByText("Deny the request")).toBeNull();
    expect(screen.getByText("Severity")).toBeTruthy();
  });

  it("counts the drafted decisions on the review step", () => {
    mocks.step = "review";
    renderEditor(policy({ shadowMcpDisposition: "block_all" }));

    expect(
      screen.getByText("0 approved · 0 denied · 2 unreviewed"),
    ).toBeTruthy();
    expect(screen.getByText("1 inventory only")).toBeTruthy();
    expect(screen.getByText("MCP servers blocked until allowed")).toBeTruthy();
  });
});
