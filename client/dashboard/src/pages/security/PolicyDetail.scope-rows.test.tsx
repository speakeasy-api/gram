import { useSdkClient } from "@/contexts/Sdk";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
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
  initialPolicyMCPScopeValue,
  policyMCPScopeComplete,
  policyMCPScopePayload,
  policyMCPScopeValue,
  policyScopeSummary,
  type PolicyMCPScopeValue,
} from "./policy-mcp-scope";

const mocks = vi.hoisted(() => ({
  flagResult: vi.fn(),
  hooksStatus: vi.fn(),
  step: "scope",
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flagResult() as FeatureFlagResult,
}));

vi.mock("@gram/client/react-query/getHooksStatus.js", () => ({
  useGetHooksStatus: () => mocks.hooksStatus(),
}));

const HOOKS_CONFIGURED = {
  data: {
    configured: true,
    agentHooksKey: true,
    anthropicInferenceHooks: false,
  },
  isPending: false,
  isSuccess: true,
  isError: false,
};
const HOOKS_UNCONFIGURED = {
  data: {
    configured: false,
    agentHooksKey: false,
    anthropicInferenceHooks: false,
  },
  isPending: false,
  isSuccess: true,
  isError: false,
};

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
    name === "step" ? [mocks.step, vi.fn()] : [null, vi.fn()],
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

vi.mock("@gram/client/react-query/riskListMcpPlatformToolsets.js", () => ({
  useRiskListMcpPlatformToolsets: () => ({
    data: {
      toolsets: [
        {
          id: "33333333-3333-4333-8333-333333333333",
          name: "Gram assistant tools",
          slug: "assistants",
          tools: [
            {
              annotations: { destructiveHint: true },
              name: "forgetMemory",
            },
            {
              annotations: { readOnlyHint: true },
              name: "recallMemory",
            },
          ],
        },
      ],
    },
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({
    data: {
      mcpServers: [
        // Listed before "Support MCP" and has no tools, so a focus-tracking
        // regression (falling back to the first server in the list instead
        // of staying on the one just deselected) lands here and is visible
        // as an empty tool list rather than silently matching by luck.
        {
          id: "22222222-2222-4222-8222-222222222222",
          name: "Billing MCP",
          toolsetId: "toolset-2",
        },
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
          id: "toolset-2",
          name: "Billing tools",
          tools: [],
        },
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
            {
              annotations: { readOnlyHint: true },
              id: "tool-2",
              name: "listTickets",
              toolUrn: "tools:listTickets",
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
  ActionPicker: ({ formAction }: { formAction: string }) => (
    <output data-testid="selected-policy-action">{formAction}</output>
  ),
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

function renderEditor(p: RiskPolicy | null) {
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
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    mocks.hooksStatus.mockReturnValue(HOOKS_CONFIGURED);
    mocks.step = "scope";
    vi.mocked(useSdkClient).mockReturnValue({
      access: { listShadowMCPInventory: vi.fn() },
    } as unknown as ReturnType<typeof useSdkClient>);
  });

  it.each<FeatureFlagResult>([
    { status: "disabled" },
    { status: "loading" },
    { status: "missing" },
    { status: "error" },
  ])("hides the MCP scope picker when the flag is $status", (flag) => {
    mocks.flagResult.mockReturnValue(flag);

    renderEditor(policy());

    expect(screen.queryByText("Specific MCP servers")).toBeNull();
    expect(screen.getByText("Secrets")).toBeTruthy();
  });

  it("keeps the MCP scope picker for a stored scope when the flag is off", () => {
    mocks.flagResult.mockReturnValue({ status: "disabled" });

    renderEditor(
      policy({
        mcpScope: {
          allServers: false,
          toolAnnotations: [],
          servers: [{ mcpServerId: "11111111-1111-4111-8111-111111111111" }],
        },
      }),
    );

    expect(screen.getByText("Specific MCP servers")).toBeTruthy();
  });

  it("keeps the picker after a stored scope is switched back to everywhere", () => {
    mocks.flagResult.mockReturnValue({ status: "disabled" });

    renderEditor(
      policy({
        mcpScope: {
          allServers: false,
          toolAnnotations: [],
          servers: [{ mcpServerId: "11111111-1111-4111-8111-111111111111" }],
        },
      }),
    );
    fireEvent.click(screen.getByText("Client sessions"));

    expect(screen.getByText("Specific MCP servers")).toBeTruthy();
  });

  it("shows a hint instead of an empty card when the picker is hidden and no detector is enabled", () => {
    mocks.flagResult.mockReturnValue({ status: "disabled" });

    renderEditor(policy({ sources: [], customRuleIds: [] }));

    expect(screen.queryByText("Specific MCP servers")).toBeNull();
    expect(
      screen.getByText("Scope options appear here once you enable a detector."),
    ).toBeTruthy();
  });

  it("keeps the picker for an in-progress MCP draft when the flag becomes unavailable", async () => {
    renderEditor(policy({ sources: ["gitleaks"] }));

    fireEvent.click(screen.getByText("Specific MCP servers"));
    mocks.flagResult.mockReturnValue({ status: "error" });
    // Any picker edit re-renders the step under the now-unavailable flag.
    const server = screen.getByRole("checkbox", { name: "Support MCP" });
    fireEvent.click(server);

    await waitFor(() => {
      expect(
        screen
          .getByRole("checkbox", { name: "Support MCP" })
          .getAttribute("aria-checked"),
      ).toBe("true");
    });
  });
  it("coerces warn to block when switching to MCP scope", () => {
    renderEditor(
      policy({
        action: "warn",
        sources: ["gitleaks"],
        mcpScope: undefined,
      }),
    );

    mocks.step = "action";
    fireEvent.click(screen.getByText("Specific MCP servers"));

    expect(screen.getByTestId("selected-policy-action").textContent).toBe(
      "block",
    );
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

    fireEvent.click(screen.getByText("Specific MCP servers"));

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

    fireEvent.click(screen.getByText("Specific MCP servers"));
    expect(
      screen.getByText("Inspection surfaces do not apply to this detector."),
    ).toBeTruthy();
    expect(
      screen.queryByRole("checkbox", { name: /^Account identity:/ }),
    ).toBeNull();
  });

  it("keeps a server in scope as a wildcard when its last tool is unchecked", async () => {
    renderEditor(policy({ sources: ["gitleaks"] }));

    fireEvent.click(screen.getByText("Specific MCP servers"));
    const server = screen.getByRole("checkbox", { name: "Support MCP" });
    fireEvent.click(server);
    await waitFor(() => {
      expect(server.getAttribute("aria-checked")).toBe("true");
    });

    // "Support MCP" has two tools in the fixture: unchecking both empties
    // the tool list, which must not drop the server from scope.
    fireEvent.click(screen.getByRole("checkbox", { name: "deleteTicket" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "listTickets" }));

    expect(server.getAttribute("aria-checked")).toBe("true");
    await waitFor(() => {
      expect(
        screen
          .getByRole("checkbox", { name: "deleteTicket" })
          .getAttribute("aria-checked"),
      ).toBe("true");
      expect(
        screen
          .getByRole("checkbox", { name: "listTickets" })
          .getAttribute("aria-checked"),
      ).toBe("true");
    });
    expect(screen.getByText(/All tools, unconditionally/)).toBeTruthy();
  });

  it("drops a rule-mode server when its sole rule-matching tool is unchecked, instead of wildcarding", async () => {
    renderEditor(policy({ sources: ["gitleaks"] }));

    fireEvent.click(screen.getByText("Specific MCP servers"));
    fireEvent.click(
      screen.getByRole("button", { name: "Tool rule: All tools" }),
    );
    // Defaults the rule to destructiveHint, which in this fixture matches
    // only "deleteTicket" — genuinely narrower than "every tool".
    fireEvent.click(screen.getByText("Tools with MCP annotations"));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));

    const server = screen.getByRole("checkbox", { name: "Support MCP" });
    fireEvent.click(server);
    await waitFor(() => {
      expect(server.getAttribute("aria-checked")).toBe("true");
    });
    await waitFor(() => {
      expect(
        screen
          .getByRole("checkbox", { name: "deleteTicket" })
          .getAttribute("aria-checked"),
      ).toBe("true");
    });
    expect(
      screen
        .getByRole("checkbox", { name: "listTickets" })
        .getAttribute("aria-checked"),
    ).toBe("false");

    fireEvent.click(screen.getByRole("checkbox", { name: "deleteTicket" }));

    await waitFor(() => {
      expect(server.getAttribute("aria-checked")).toBe("false");
    });
  });

  it("drops a custom-selection server when its last tool is unchecked while a top-level rule is set, instead of saving a tool list the backend rejects", async () => {
    renderEditor(policy({ sources: ["gitleaks"] }));

    fireEvent.click(screen.getByText("Specific MCP servers"));
    fireEvent.click(
      screen.getByRole("button", { name: "Tool rule: All tools" }),
    );
    fireEvent.click(screen.getByText("Tools with MCP annotations"));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));

    // Pick a tool the rule (destructiveHint) does NOT match, so this stays a
    // "custom" selection instead of being recognized as tracking the rule.
    fireEvent.click(screen.getByRole("button", { name: /Support MCP/ }));
    const tool = screen.getByRole("checkbox", { name: "listTickets" });
    fireEvent.click(tool);
    await waitFor(() => {
      expect(tool.getAttribute("aria-checked")).toBe("true");
    });

    // NormalizeMCPScope rejects an empty tool list on a server when the
    // scope has a top-level rule, so wildcarding here (as a plain custom
    // selection normally would) would only fail at save time.
    fireEvent.click(tool);

    await waitFor(() => {
      expect(
        screen
          .getByRole("checkbox", { name: "Support MCP" })
          .getAttribute("aria-checked"),
      ).toBe("false");
    });
  });

  it("renders a stored ['*'] tool list as the wildcard, not a custom selection with a phantom tool", () => {
    renderEditor(
      policy({
        sources: ["gitleaks"],
        mcpScope: {
          allServers: false,
          toolAnnotations: [],
          servers: [
            {
              mcpServerId: "11111111-1111-4111-8111-111111111111",
              tools: ["*"],
            },
          ],
        },
      }),
    );

    fireEvent.click(screen.getByText("Specific MCP servers"));

    expect(
      screen
        .getByRole("checkbox", { name: "Support MCP" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(screen.getByText(/All tools, unconditionally/)).toBeTruthy();
    expect(screen.queryByText(/^Custom ·/)).toBeNull();
  });

  it("keeps the pane on the deselected server so its tools stay pickable", async () => {
    renderEditor(policy({ sources: ["gitleaks"] }));

    fireEvent.click(screen.getByText("Specific MCP servers"));
    const server = screen.getByRole("checkbox", { name: "Support MCP" });
    fireEvent.click(server);
    await waitFor(() => {
      expect(server.getAttribute("aria-checked")).toBe("true");
    });

    // Deselect via the server's own checkbox, the same interaction reported
    // as broken: without focus tracking, the pane falls back to the first
    // server in the list ("Billing MCP", which has no tools) instead of
    // staying on "Support MCP", stranding its tools out of view.
    fireEvent.click(server);
    await waitFor(() => {
      expect(server.getAttribute("aria-checked")).toBe("false");
    });
    expect(
      screen.queryByText("No tools are available for this server."),
    ).toBeNull();

    const tool = screen.getByRole("checkbox", { name: "deleteTicket" });
    fireEvent.click(tool);
    await waitFor(() => {
      expect(tool.getAttribute("aria-checked")).toBe("true");
    });
    expect(server.getAttribute("aria-checked")).not.toBe("false");
  });

  it("preserves server selection across mode switches", async () => {
    renderEditor(policy({ sources: ["gitleaks"] }));

    fireEvent.click(screen.getByText("Specific MCP servers"));
    const server = screen.getByRole("checkbox", { name: "Support MCP" });
    fireEvent.click(server);
    await waitFor(() => {
      expect(server.getAttribute("aria-checked")).toBe("true");
    });

    fireEvent.click(screen.getByText("Client sessions"));
    fireEvent.click(screen.getByText("Specific MCP servers"));
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
    fireEvent.click(screen.getByText("Specific MCP servers"));
    expect(screen.queryByText(expression)).toBeNull();
    fireEvent.click(screen.getByText("Client sessions"));
    expect(screen.getByText(expression)).toBeTruthy();
  });

  it("configures the global annotation rule", () => {
    renderEditor(policy());

    fireEvent.click(screen.getByText("Specific MCP servers"));
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

    fireEvent.click(screen.getByText("Specific MCP servers"));
    expect(
      screen.getByText(
        "Block runs before each matching call. Detector time adds to call latency.",
      ),
    ).toBeTruthy();
  });
});

describe("StandardPolicyEditor scope choice", () => {
  afterEach(cleanup);

  beforeEach(() => {
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    mocks.hooksStatus.mockReturnValue(HOOKS_CONFIGURED);
    mocks.step = "scope";
    vi.mocked(useSdkClient).mockReturnValue({
      access: { listShadowMCPInventory: vi.fn() },
    } as unknown as ReturnType<typeof useSdkClient>);
  });

  it("starts a new policy with no scope chosen and nothing else on the step", () => {
    renderEditor(null);

    expect(screen.getByRole("radio", { name: "Client sessions" })).toBeTruthy();
    expect(
      screen.getByRole("radio", { name: "Specific MCP servers" }),
    ).toBeTruthy();
    expect(
      screen.getByText("Choose where this policy applies to continue."),
    ).toBeTruthy();
    expect(screen.queryByText("Inspect")).toBeNull();
    expect(screen.queryByText("Servers")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("reveals the step once Client sessions is chosen", () => {
    renderEditor(null);

    fireEvent.click(screen.getByText("Client sessions"));

    expect(
      screen.queryByText("Choose where this policy applies to continue."),
    ).toBeNull();
    expect(screen.queryByText("Servers")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(false);
  });

  it("reveals the server picker once Specific MCP servers is chosen", async () => {
    renderEditor(null);

    fireEvent.click(screen.getByText("Specific MCP servers"));

    expect(screen.getByText("Servers")).toBeTruthy();
    expect(
      screen.getByText(
        "Select at least one MCP server or apply the policy to all servers.",
      ),
    ).toBeTruthy();
    // Continue follows scope completeness, not just the mode choice.
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);

    fireEvent.click(screen.getByRole("checkbox", { name: "Support MCP" }));

    await waitFor(() => {
      expect(
        screen
          .getByRole("button", { name: "Continue" })
          .hasAttribute("disabled"),
      ).toBe(false);
    });
  });

  it("preselects the stored scope when editing", () => {
    renderEditor(policy());

    expect(
      screen
        .getByRole("radio", { name: "Client sessions" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(screen.getByText("Secrets")).toBeTruthy();
  });

  it("offers only Client sessions when the flag is off and takes it for a new policy", async () => {
    mocks.flagResult.mockReturnValue({ status: "disabled" });

    renderEditor(null);

    expect(screen.queryByText("Specific MCP servers")).toBeNull();
    await waitFor(() => {
      expect(
        screen
          .getByRole("radio", { name: "Client sessions" })
          .getAttribute("aria-checked"),
      ).toBe("true");
    });
  });
});

describe("StandardPolicyEditor hooks availability", () => {
  afterEach(cleanup);

  beforeEach(() => {
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    mocks.hooksStatus.mockReturnValue(HOOKS_UNCONFIGURED);
    mocks.step = "scope";
    vi.mocked(useSdkClient).mockReturnValue({
      access: { listShadowMCPInventory: vi.fn() },
    } as unknown as ReturnType<typeof useSdkClient>);
  });

  it("hides Client sessions and goes straight to MCP scope when hooks are not configured", async () => {
    renderEditor(null);

    expect(screen.queryByRole("radio", { name: "Client sessions" })).toBeNull();
    await waitFor(() => {
      expect(
        screen
          .getByRole("radio", { name: "Specific MCP servers" })
          .getAttribute("aria-checked"),
      ).toBe("true");
    });
    expect(screen.getByText("Servers")).toBeTruthy();
    expect(
      screen.getByText(/Client sessions become available once/),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Set up hooks" })).toBeTruthy();
  });

  it("re-scopes a stored Client sessions policy to MCP when hooks are not configured", async () => {
    renderEditor(policy());

    await waitFor(() => {
      expect(
        screen
          .getByRole("radio", { name: "Specific MCP servers" })
          .getAttribute("aria-checked"),
      ).toBe("true");
    });
    expect(screen.queryByRole("radio", { name: "Client sessions" })).toBeNull();
  });

  it("shows an empty state when neither hooks nor MCP scoping are available", () => {
    mocks.flagResult.mockReturnValue({ status: "disabled" });

    renderEditor(null);

    expect(screen.getByText("Nothing to apply this policy to")).toBeTruthy();
    expect(screen.queryByRole("radio")).toBeNull();
    expect(
      screen.queryByText("Choose where this policy applies to continue."),
    ).toBeNull();
  });

  it("offers no cards until the hooks status has loaded", () => {
    mocks.hooksStatus.mockReturnValue({
      data: undefined,
      isPending: true,
      isSuccess: false,
      isError: false,
    });

    renderEditor(null);

    expect(screen.queryByRole("radio")).toBeNull();
    expect(
      screen.queryByText("Choose where this policy applies to continue."),
    ).toBeNull();
  });

  it("offers no cards and asks for a retry when the hooks status cannot be read", () => {
    const refetch = vi.fn();
    mocks.hooksStatus.mockReturnValue({
      data: undefined,
      isPending: false,
      isSuccess: false,
      isError: true,
      refetch,
    });

    renderEditor(null);

    expect(screen.queryByRole("radio")).toBeNull();
    expect(
      screen.getByText("Could not check whether hooks are configured."),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetch).toHaveBeenCalledTimes(1);
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

  it("cherry-picks a tool on a server that is not yet in scope", () => {
    render(<ScopePickerHarness />);

    // Focus the server row (not its checkbox, which would select it) before
    // picking a tool — the pane otherwise defaults to the first server in
    // the list, which is not this one.
    fireEvent.click(screen.getByRole("button", { name: /Support MCP/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: "listTickets" }));

    expect(
      JSON.parse(screen.getByTestId("mcp-scope-payload").textContent ?? "null"),
    ).toEqual({
      allServers: false,
      toolAnnotations: [],
      servers: [
        {
          mcpServerId: "11111111-1111-4111-8111-111111111111",
          tools: ["listTickets"],
        },
      ],
    });
  });

  it("selects individual Platform MCP tools", () => {
    render(<ScopePickerHarness />);

    fireEvent.click(
      screen.getByRole("button", { name: /Gram assistant tools/ }),
    );
    fireEvent.click(screen.getByRole("checkbox", { name: "recallMemory" }));

    expect(
      JSON.parse(screen.getByTestId("mcp-scope-payload").textContent ?? "null"),
    ).toEqual({
      allServers: false,
      toolAnnotations: [],
      servers: [
        {
          mcpServerId: "33333333-3333-4333-8333-333333333333",
          tools: ["recallMemory"],
        },
      ],
    });
  });

  it("includes Platform MCP toolsets in all-server selection", () => {
    render(<ScopePickerHarness />);

    fireEvent.click(screen.getByRole("checkbox", { name: "All MCP servers" }));

    expect(
      screen
        .getByRole("checkbox", { name: "Gram assistant tools" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      JSON.parse(screen.getByTestId("mcp-scope-payload").textContent ?? "null"),
    ).toEqual({
      allServers: true,
      toolAnnotations: [],
      servers: [],
    });
  });

  it("clears every server when All MCP servers is unchecked", () => {
    render(<ScopePickerHarness />);

    fireEvent.click(screen.getByRole("checkbox", { name: "All MCP servers" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "All MCP servers" }));

    expect(
      JSON.parse(screen.getByTestId("mcp-scope-payload").textContent ?? "null"),
    ).toEqual({
      allServers: false,
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

  it("serializes Client sessions as no MCP scope", () => {
    const value = policyMCPScopeValue({
      allServers: true,
      toolAnnotations: ["readOnlyHint"],
      servers: [],
    });

    expect(policyMCPScopePayload({ ...value, mode: "everywhere" })).toBeNull();
  });

  it("starts a new policy unset and hydrates a stored policy to a real mode", () => {
    const fresh = initialPolicyMCPScopeValue(null);
    expect(fresh.mode).toBe("unset");
    expect(policyMCPScopePayload(fresh)).toBeNull();
    expect(policyMCPScopeComplete(fresh)).toBe(false);

    expect(initialPolicyMCPScopeValue({ mcpScope: null }).mode).toBe(
      "everywhere",
    );
    expect(
      initialPolicyMCPScopeValue({
        mcpScope: { allServers: true, toolAnnotations: [], servers: [] },
      }).mode,
    ).toBe("mcp");
  });

  it("reports completeness and a summary per mode", () => {
    const base: PolicyMCPScopeValue = {
      mode: "mcp",
      allServers: false,
      toolAnnotations: [],
      servers: [],
    };

    expect(policyMCPScopeComplete({ ...base, mode: "everywhere" })).toBe(true);
    expect(policyMCPScopeComplete(base)).toBe(false);
    expect(policyMCPScopeComplete({ ...base, allServers: true })).toBe(true);
    expect(
      policyMCPScopeComplete({
        ...base,
        servers: [{ mcpServerId: "11111111-1111-4111-8111-111111111111" }],
      }),
    ).toBe(true);

    expect(policyScopeSummary({ ...base, mode: "everywhere" })).toBe(
      "Client sessions",
    );
    expect(policyScopeSummary({ ...base, allServers: true })).toBe(
      "Specific MCP servers · all servers",
    );
    expect(
      policyScopeSummary({
        ...base,
        servers: [{ mcpServerId: "11111111-1111-4111-8111-111111111111" }],
      }),
    ).toBe("Specific MCP servers · 1 server");
    expect(policyScopeSummary({ ...base, mode: "unset" })).toBe("Not chosen");
  });
});
