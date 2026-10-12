import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { PolicyNew } from "./PolicyDetail";

const mocks = vi.hoisted(() => ({
  flag: { status: "enabled" } as { status: string },
  setKind: vi.fn(),
  policies: [] as RiskPolicy[],
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1" }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: vi.fn(),
  useProjectSlugForRequests: () => "test-project",
}));

vi.mock("@/components/page-layout", () => ({
  Page: Object.assign(({ children }: { children?: ReactNode }) => children, {
    Header: Object.assign(
      ({ children }: { children?: ReactNode }) => children,
      { Breadcrumbs: () => null },
    ),
    Body: ({ children }: { children?: ReactNode }) => children,
  }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flag as FeatureFlagResult,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    policyCenter: {
      goTo: vi.fn(),
      detail: { href: (id: string) => `/guardrails/${id}` },
    },
  }),
}));

vi.mock("nuqs", () => ({
  useQueryState: () => [null, mocks.setKind],
}));

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: () => ({
    data: { policies: mocks.policies },
    isLoading: false,
  }),
  invalidateAllRiskListPolicies: vi.fn(),
}));

vi.mock("@gram/client/react-query/getHooksStatus.js", () => ({
  useGetHooksStatus: () => ({ data: undefined, isPending: false }),
}));

vi.mock("@gram/client/react-query/riskCategories.js", () => ({
  useRiskCategories: () => ({ data: undefined, isLoading: false }),
}));

vi.mock("./ShadowAIPolicyEditor", () => ({
  ShadowAIPolicyEditor: () => null,
}));

vi.mock("./detection-rules-data", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./detection-rules-data")>()),
  useDetectionRulesStore: () => ({ customRules: [] }),
}));

vi.mock("./PolicyCenter", () => ({
  ActionPicker: () => null,
  CustomizeRulesSheet: () => null,
  PolicyAudiencePicker: () => null,
  RuleSelectList: () => null,
}));

vi.mock("@/pages/chatLogs/ChatTranscript", () => ({
  ChatTranscript: () => null,
}));

vi.mock("@/pages/chatLogs/transcript", () => ({
  buildDisplayItems: () => [],
  buildTranscript: () => [],
}));

vi.mock("@/pages/chatLogs/useChatTranscript", () => ({
  useChatTranscript: () => ({ messages: [] }),
}));

vi.mock("@/pages/chatLogs/claudeUsage", () => ({
  formatUsageCost: () => "$0.00",
}));

function blockingShadowMCPPolicy(): RiskPolicy {
  return {
    id: "policy-sm",
    name: "Shadow MCP Server Policy",
    sources: ["shadow_mcp"],
    action: "block",
    enabled: true,
    policyType: "standard",
  } as RiskPolicy;
}

function renderChooser() {
  return render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <PolicyNew />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

describe("PolicyKindChooser", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.flag = { status: "enabled" };
    mocks.policies = [];
  });

  it("offers Shadow AI as a third kind", () => {
    renderChooser();

    fireEvent.click(screen.getByText("Shadow AI"));

    expect(mocks.setKind).toHaveBeenCalledWith("shadow-ai");
  });

  it("points at the existing Shadow AI policy instead of a second create", () => {
    mocks.policies = [blockingShadowMCPPolicy()];
    renderChooser();

    expect(screen.getByText(/already has one/)).toBeTruthy();
    const link = screen.getByText("Open Shadow MCP Server Policy");
    expect(link.getAttribute("href")).toBe("/guardrails/policy-sm");
    fireEvent.click(screen.getByText("Shadow AI"));
    expect(mocks.setKind).not.toHaveBeenCalled();
  });

  it("keeps the two kinds while the fold-in is off", () => {
    mocks.flag = { status: "disabled" };
    renderChooser();

    expect(screen.queryByText("Shadow AI")).toBeNull();
    expect(screen.getByText("Built-in detector")).toBeTruthy();
    expect(screen.getByText("Prompt-based")).toBeTruthy();
  });
});
