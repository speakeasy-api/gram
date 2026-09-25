import { useSdkClient } from "@/contexts/Sdk";
import type { RiskCategoryDefinition } from "@gram/client/models/components/riskcategorydefinition.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useState, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { PolicyMCPScopePicker } from "./PolicyMCPScopePicker";
import { StandardPolicyEditor } from "./PolicyDetail";
import {
  policyMCPScopePayload,
  policyMCPScopeValue,
  type PolicyMCPScopeValue,
} from "./policy-mcp-scope";

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
  useProjectSlugForRequests: () => "test-project",
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

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({
    data: {
      mcpServers: [
        {
          id: "11111111-1111-4111-8111-111111111111",
          name: "Support MCP",
          toolsetId: "toolset-1",
        },
      ],
    },
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("@gram/client/react-query/metaMcpServers.js", () => ({
  useMetaMcpServers: () => ({
    data: {
      metaMcpServers: [
        { id: "gateway-1", name: "Support gateway", memberCount: 1 },
      ],
    },
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("@gram/client/react-query/metaMcpMembers.js", () => ({
  useMetaMcpMembers: () => ({
    data: {
      members: [
        {
          mcpServerId: "11111111-1111-4111-8111-111111111111",
          mcpServerName: "Support MCP",
        },
      ],
    },
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("@gram/client/react-query/listToolsets.js", () => ({
  useListToolsets: () => ({
    data: {
      toolsets: [
        {
          id: "toolset-1",
          name: "Support tools",
          tools: [
            {
              annotations: { destructiveHint: true },
              id: "tool-1",
              name: "deleteTicket",
              toolUrn: "tools:deleteTicket",
              type: "http",
            },
          ],
        },
      ],
    },
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("@/hooks/useToolMetadata", () => ({
  useToolMetadata: () => ({
    metadataByTool: {},
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
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
  category("account_identity", "Account identity", {
    recommendedScopeApplicable: false,
  }),
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

function ScopePickerHarness(): JSX.Element {
  const [queryClient] = useState(() => new QueryClient());
  const [value, setValue] = useState<PolicyMCPScopeValue>({
    mode: "mcp",
    allServers: false,
    toolAnnotations: [],
    servers: [],
  });

  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <PolicyMCPScopePicker value={value} onChange={setValue} action="flag" />
        <output data-testid="mcp-scope-payload">
          {JSON.stringify(policyMCPScopePayload(value))}
        </output>
      </TooltipProvider>
    </QueryClientProvider>
  );
}

describe("StandardPolicyEditor scope rows", () => {
  afterEach(cleanup);

  beforeEach(() => {
    vi.mocked(useSdkClient).mockReturnValue({
      access: { listShadowMCPInventory: vi.fn() },
    } as unknown as ReturnType<typeof useSdkClient>);
  });

  it("renders one inspect row for every enabled detector", () => {
    renderEditor(policy());

    expect(screen.getByText("Secrets")).toBeTruthy();
    expect(screen.getByText("Shadow MCP")).toBeTruthy();
    expect(screen.getByText("Custom rules")).toBeTruthy();
    expect(screen.getAllByText("Tool", { selector: "span" })).toHaveLength(2);
    expect(screen.getByText("Response", { selector: "span" })).toBeTruthy();
  });

  it("keeps the final inspection surface selected", () => {
    renderEditor(policy());

    const response = screen.getByRole("checkbox", {
      name: "Secrets: Tool responses",
    });
    expect(response.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(response);
    expect(response.getAttribute("aria-checked")).toBe("true");
  });

  it("shows MCP-only columns without disabling response inspection", () => {
    renderEditor(policy());

    fireEvent.click(screen.getByText("Selected MCP servers"));

    expect(
      screen.getAllByLabelText("User is not part of an MCP call").length,
    ).toBeGreaterThan(0);
    expect(
      screen.getAllByLabelText("Assistant is not part of an MCP call").length,
    ).toBeGreaterThan(0);
    expect(
      screen
        .getByRole("checkbox", { name: "Secrets: Tool responses" })
        .hasAttribute("disabled"),
    ).toBe(false);
    expect(
      screen.getByText(
        "Tool responses are stored now. Response inspection applies once MCP response scanning ships.",
      ),
    ).toBeTruthy();
  });

  it("renders inapplicable detectors without editable inspection surfaces", () => {
    renderEditor(
      policy({
        sources: ["account_identity"],
        customRuleIds: [],
      }),
    );

    expect(
      screen.getByText("Inspection surfaces do not apply to this detector."),
    ).toBeTruthy();
    expect(
      screen.queryByRole("checkbox", { name: /^Account identity:/ }),
    ).toBeNull();

    fireEvent.click(screen.getByText("Selected MCP servers"));
    expect(
      screen.getByText("Inspection surfaces do not apply to this detector."),
    ).toBeTruthy();
    expect(
      screen.queryByRole("checkbox", { name: /^Account identity:/ }),
    ).toBeNull();
  });

  it("preserves server selection across mode switches", async () => {
    renderEditor(policy({ sources: ["gitleaks"] }));

    fireEvent.click(screen.getByText("Selected MCP servers"));
    const server = screen.getByRole("checkbox", { name: "Support MCP" });
    fireEvent.click(server);
    await waitFor(() => {
      expect(server.getAttribute("aria-checked")).toBe("true");
    });

    fireEvent.click(screen.getByText("Everywhere"));
    fireEvent.click(screen.getByText("Selected MCP servers"));
    expect(
      screen
        .getByRole("checkbox", { name: "Support MCP" })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("preserves custom CEL across mode switches", () => {
    const expression = 'content.contains("access token")';
    renderEditor(
      policy({
        sources: ["gitleaks"],
        detectionScopes: [
          { category: "secrets", scopeInclude: expression, scopeExempt: "" },
        ],
      }),
    );

    fireEvent.click(screen.getAllByRole("button", { name: "CEL" })[0]!);
    expect(screen.getByText(expression)).toBeTruthy();
    fireEvent.click(screen.getByText("Selected MCP servers"));
    expect(screen.queryByText(expression)).toBeNull();
    fireEvent.click(screen.getByText("Everywhere"));
    expect(screen.getByText(expression)).toBeTruthy();
  });

  it("configures the global annotation rule", () => {
    renderEditor(policy());

    fireEvent.click(screen.getByText("Selected MCP servers"));
    fireEvent.click(
      screen.getByRole("button", { name: "Tool rule: All tools" }),
    );
    fireEvent.click(screen.getByText("Tools with MCP annotations"));

    expect(
      screen
        .getByRole("checkbox", { name: /^Destructive/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("shows the block latency note in MCP mode", () => {
    renderEditor(policy({ action: "block" }));

    fireEvent.click(screen.getByText("Selected MCP servers"));
    expect(
      screen.getByText(
        "Block runs before each matching call. Detector time adds to call latency.",
      ),
    ).toBeTruthy();
  });
});

describe("PolicyMCPScopePicker all-server selection", () => {
  afterEach(cleanup);

  it("drops a selected gateway before producing an all-server payload", () => {
    render(<ScopePickerHarness />);

    fireEvent.click(screen.getByRole("checkbox", { name: "Support gateway" }));
    expect(
      screen
        .getByRole("checkbox", { name: "Support gateway" })
        .getAttribute("aria-checked"),
    ).toBe("true");

    fireEvent.click(screen.getByRole("checkbox", { name: "All MCP servers" }));

    expect(
      JSON.parse(screen.getByTestId("mcp-scope-payload").textContent ?? "null"),
    ).toEqual({
      allServers: true,
      toolAnnotations: [],
      servers: [],
    });
  });
});

describe("MCP scope form conversion", () => {
  it("hydrates and serializes all-server annotation rules", () => {
    const value = policyMCPScopeValue({
      allServers: true,
      toolAnnotations: ["destructiveHint"],
      servers: [
        {
          mcpServerId: "11111111-1111-4111-8111-111111111111",
          tools: ["deleteTicket"],
        },
      ],
    });

    expect(value.mode).toBe("mcp");
    expect(policyMCPScopePayload(value)).toEqual({
      allServers: true,
      toolAnnotations: ["destructiveHint"],
      servers: [
        {
          mcpServerId: "11111111-1111-4111-8111-111111111111",
          tools: ["deleteTicket"],
        },
      ],
    });
  });

  it("serializes Everywhere as no MCP scope", () => {
    const value = policyMCPScopeValue({
      allServers: true,
      toolAnnotations: ["readOnlyHint"],
      servers: [],
    });

    expect(policyMCPScopePayload({ ...value, mode: "everywhere" })).toBeNull();
  });
});
