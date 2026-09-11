import { useSdkClient } from "@/contexts/Sdk";
import type { RiskCategoryDefinition } from "@gram/client/models/components/riskcategorydefinition.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { StandardPolicyEditor } from "./PolicyDetail";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1" }),
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

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: vi.fn(),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({ policyCenter: { goTo: vi.fn() } }),
}));

vi.mock("nuqs", () => ({
  useQueryState: (name: string) =>
    name === "step" ? ["scope", vi.fn()] : [null, vi.fn()],
}));

vi.mock("@/components/shadow-mcp/ShadowMCPPolicyServerSelector", () => ({
  ShadowMCPPolicyServerSelector: () => null,
}));

vi.mock("@gram/client/react-query/riskCreatePolicy.js", () => ({
  useRiskCreatePolicyMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));

vi.mock("@gram/client/react-query/riskPoliciesUpdate.js", () => ({
  useRiskPoliciesUpdateMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));

vi.mock("@gram/client/react-query/riskCategories.js", () => ({
  useRiskCategories: () => ({
    data: { categories: CATEGORIES },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));

vi.mock("./detection-rules-data", () => ({
  useDetectionRulesStore: () => ({ customRules: [] }),
}));

vi.mock("./use-cel-status", () => ({
  useCelStatus: () => ({ kind: "valid" }),
}));

vi.mock("./use-cel-engine", () => ({
  useCelEngine: () => ({ status: "loading" }),
}));

vi.mock("./PolicyCenter", () => ({
  ActionPicker: () => null,
  CustomizeRulesSheet: () => null,
  PolicyAudiencePicker: () => null,
  RuleSelectList: () => null,
  ScopeCard: () => null,
}));

vi.mock("./DetectorCard", () => ({
  DetectorCard: () => null,
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

function category(
  key: string,
  label: string,
  overrides: Partial<RiskCategoryDefinition> = {},
): RiskCategoryDefinition {
  return {
    key,
    label,
    recommendedScopeApplicable: true,
    recommendedScopeInclude: "",
    recommendedScopeExempt: "",
    recommendedScopeRationale: "",
    ...overrides,
  } as RiskCategoryDefinition;
}

// Three selected categories: one with a registry recommendation, one with
// neither a recommendation nor a stored scope, and custom rules, which the
// registry synthesises as applicable-but-empty.
const CATEGORIES: RiskCategoryDefinition[] = [
  category("secrets", "Secrets", {
    recommendedScopeInclude: 'kind == "tool_response"',
  }),
  category("shadow_mcp", "Shadow MCP"),
  category("custom", "Custom rules"),
];

function policy(overrides: Partial<RiskPolicy> = {}): RiskPolicy {
  return {
    name: "Policy",
    action: "flag",
    audiencePrincipalUrns: ["user:all"],
    audienceType: "everyone",
    autoName: false,
    createdAt: new Date("2026-01-01T10:00:00Z"),
    enabled: true,
    id: "policy-1",
    messageTypes: [],
    pendingMessages: 0,
    policyType: "standard",
    projectId: "project-1",
    score: 5,
    sources: ["gitleaks", "shadow_mcp"],
    customRuleIds: ["rule-1"],
    totalMessages: 0,
    updatedAt: new Date("2026-01-01T10:00:00Z"),
    version: 1,
    ...overrides,
  };
}

function renderEditor(p: RiskPolicy) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <StandardPolicyEditor policy={p} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("StandardPolicyEditor scope rows", () => {
  afterEach(cleanup);

  beforeEach(() => {
    vi.mocked(useSdkClient).mockReturnValue({
      access: { listShadowMCPInventory: vi.fn() },
    } as unknown as ReturnType<typeof useSdkClient>);
  });

  it("renders a row for a stored custom scope with no recommendation", () => {
    renderEditor(
      policy({
        detectionScopes: [
          { category: "custom", scopeInclude: 'kind in ["tool_request"]' },
        ],
      }),
    );

    expect(screen.getByText("Custom rules")).toBeTruthy();
    expect(screen.getByText("Custom")).toBeTruthy();
  });

  it("still renders a category with a non-empty recommendation", () => {
    renderEditor(policy());

    expect(screen.getByText("Secrets")).toBeTruthy();
    expect(screen.getByText("Recommended")).toBeTruthy();
  });

  it("renders a row so a category with no recommendation can be authored", () => {
    renderEditor(policy());

    expect(screen.getByText("Shadow MCP")).toBeTruthy();
    expect(screen.getByText("Custom rules")).toBeTruthy();
    expect(screen.getByText("Recommended")).toBeTruthy();
  });
});

function secretsOnlyPolicy(overrides: Partial<RiskPolicy> = {}): RiskPolicy {
  return policy({
    sources: ["gitleaks"],
    customRuleIds: [],
    ...overrides,
  });
}

describe("StandardPolicyEditor scope chip fidelity", () => {
  afterEach(cleanup);

  beforeEach(() => {
    vi.mocked(useSdkClient).mockReturnValue({
      access: { listShadowMCPInventory: vi.fn() },
    } as unknown as ReturnType<typeof useSdkClient>);
  });

  it("does not highlight every surface for a scope that matches none", () => {
    renderEditor(
      secretsOnlyPolicy({
        detectionScopes: [{ category: "secrets", scopeInclude: "kind in []" }],
      }),
    );

    for (const label of [
      "User",
      "Tool requests",
      "Tool responses",
      "Assistant",
    ]) {
      expect(
        screen
          .getByRole("button", { name: label })
          .getAttribute("aria-pressed"),
      ).toBe("false");
    }
    expect(
      screen.getByText("This category scans no message surfaces."),
    ).toBeTruthy();
  });

  it("decodes parenthesised membership into the same surface chips", () => {
    renderEditor(
      secretsOnlyPolicy({
        detectionScopes: [
          {
            category: "secrets",
            scopeInclude: '(kind in ["tool_request"])',
          },
        ],
      }),
    );

    expect(
      screen
        .getByRole("button", { name: "Tool requests" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
    expect(
      screen.getByRole("button", { name: "User" }).getAttribute("aria-pressed"),
    ).toBe("false");
    expect(screen.queryByText(/kind in \[/)).toBeNull();
  });

  it("warns when a kind conjunction can never match", () => {
    renderEditor(
      secretsOnlyPolicy({
        detectionScopes: [
          {
            category: "secrets",
            scopeInclude: 'kind == "user_message" && kind == "tool_request"',
          },
        ],
      }),
    );

    expect(
      screen.getByText("This category scans no message surfaces."),
    ).toBeTruthy();
    for (const label of [
      "User",
      "Tool requests",
      "Tool responses",
      "Assistant",
    ]) {
      expect(
        screen
          .getByRole("button", { name: label })
          .getAttribute("aria-pressed"),
      ).toBe("false");
    }
  });

  it("lets the last surface be deselected and shows the empty-scope state", async () => {
    const user = userEvent.setup();
    renderEditor(
      secretsOnlyPolicy({
        detectionScopes: [
          { category: "secrets", scopeInclude: 'kind == "user_message"' },
        ],
      }),
    );

    const userChip = screen.getByRole("button", { name: "User" });
    expect(userChip.getAttribute("aria-pressed")).toBe("true");

    await user.click(userChip);

    expect(userChip.getAttribute("aria-pressed")).toBe("false");
    expect(
      screen.getByText("This category scans no message surfaces."),
    ).toBeTruthy();
  });
});
