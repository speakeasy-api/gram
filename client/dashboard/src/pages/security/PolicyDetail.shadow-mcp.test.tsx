import { useSdkClient } from "@/contexts/Sdk";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Children, isValidElement, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { shadowMCPPolicyInventoryQueryKey } from "@/components/shadow-mcp/useShadowMCPPolicyInventory";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { PolicyNew, StandardPolicyEditor } from "./PolicyDetail";

const mocks = vi.hoisted(() => ({
  saveDisabledRenders: [] as boolean[],
  selectionRenders: [] as string[][],
  modeRenders: [] as string[],
  step: "action" as string | null,
  mutateCreate: vi.fn(),
  mutateUpdate: vi.fn(),
  kind: null as string | null,
  category: null as string | null,
  detectorSelections: [] as Array<{ category: string; selected: boolean }>,
}));

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
  useQueryState: (name: string) => {
    if (name === "kind") return [mocks.kind, vi.fn()];
    if (name === "category") return [mocks.category, vi.fn()];
    return [mocks.step, vi.fn()];
  },
}));

vi.mock("@/components/shadow-mcp/ShadowMCPPolicyServerSelector", () => ({
  ShadowMCPPolicyServerSelector: ({
    selectedURLs,
    mode = "allow",
  }: {
    selectedURLs: ReadonlySet<string>;
    mode?: string;
  }) => {
    mocks.selectionRenders.push([...selectedURLs].sort());
    mocks.modeRenders.push(mode);
    return null;
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

vi.mock("./detection-rules-data", () => ({
  useDetectionRulesStore: () => ({ customRules: [] }),
}));

vi.mock("./use-cel-status", () => ({
  useCelStatus: () => ({ kind: "valid" }),
}));

vi.mock("./PolicyCenter", () => ({
  ActionPicker: () => null,
  CustomizeRulesSheet: () => null,
  PolicyAudiencePicker: () => null,
  RuleSelectList: () => null,
  ScopeCard: () => null,
}));

vi.mock("./DetectorCard", () => ({
  DetectorCard: ({
    category,
    selected,
  }: {
    category: string;
    selected: boolean;
  }) => {
    mocks.detectorSelections.push({ category, selected });
    return null;
  },
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

vi.mock("@/components/ui/Button", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/components/ui/Button")>();

  function buttonLabel(children: ReactNode): string | undefined {
    for (const child of Children.toArray(children)) {
      if (isValidElement<{ children?: ReactNode }>(child)) {
        const label = child.props.children;
        if (typeof label === "string") return label;
      }
    }
    return undefined;
  }

  function TestButton({
    children,
    disabled = false,
  }: {
    children?: ReactNode;
    disabled?: boolean;
  }) {
    if (buttonLabel(children) === "Save changes") {
      mocks.saveDisabledRenders.push(disabled);
    }
    return <button disabled={disabled}>{children}</button>;
  }
  TestButton.Text = ({ children }: { children?: ReactNode }) => children;
  TestButton.LeftIcon = ({ children }: { children?: ReactNode }) => children;

  return { ...actual, Button: TestButton };
});

function inventoryServer(
  overrides: Partial<ShadowMCPInventoryServer> = {},
): ShadowMCPInventoryServer {
  return {
    access: "allowed",
    allowedPolicyIds: ["policy-1"],
    blockedPolicyIds: [],
    canonicalServerUrl: "https://github.example.com/mcp",
    firstSeen: new Date("2026-01-01T10:00:00Z"),
    lastCalled: undefined,
    lastSeen: new Date("2026-01-02T10:00:00Z"),
    observedUseCount: 1,
    requestCount: 0,
    serverName: "GitHub",
    serverSlug: "github-d8860eea",
    topUsers: [],
    urlHost: "github.example.com",
    userCount: 1,
    ...overrides,
  };
}

function blockingPolicyWithDirtyDraftName(): RiskPolicy {
  // Keep an unrelated draft edit visible across the initial and effect renders
  // so the Save button is present while its initialization gate changes.
  let nameReads = 0;
  return {
    get name() {
      nameReads += 1;
      return nameReads === 1 ? "Original name" : "Dirty draft name";
    },
    action: "block",
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
    sources: ["shadow_mcp"],
    totalMessages: 0,
    updatedAt: new Date("2026-01-01T10:00:00Z"),
    version: 1,
  };
}

describe("StandardPolicyEditor cached Shadow MCP inventory", () => {
  beforeEach(() => {
    mocks.saveDisabledRenders.length = 0;
    mocks.selectionRenders.length = 0;
    mocks.modeRenders.length = 0;
    mocks.step = "action";
    mocks.kind = null;
    mocks.category = null;
    mocks.detectorSelections.length = 0;
    vi.clearAllMocks();
    vi.mocked(useSdkClient).mockReturnValue({
      access: { listShadowMCPInventory: vi.fn() },
    } as unknown as ReturnType<typeof useSdkClient>);
  });

  it("preselects the prompt injection detector from the setup link", () => {
    mocks.kind = "standard";
    mocks.category = "prompt_injection";
    mocks.step = null;
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <PolicyNew />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    expect(
      mocks.detectorSelections.find(
        (selection) => selection.category === "prompt_injection",
      )?.selected,
    ).toBe(true);
  });

  it("keeps save blocked until cached inventory preselection initializes", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(shadowMCPPolicyInventoryQueryKey("project-1"), [
      inventoryServer(),
    ]);

    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <StandardPolicyEditor policy={blockingPolicyWithDirtyDraftName()} />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(mocks.saveDisabledRenders.at(-1)).toBe(false);
      expect(mocks.selectionRenders.at(-1)).toEqual([
        "https://github.example.com/mcp",
      ]);
    });
    expect(mocks.saveDisabledRenders[0]).toBe(true);
    expect(mocks.selectionRenders[0]).toEqual([]);
    expect(mocks.modeRenders.at(-1)).toBe("allow");
  });

  it("seeds an allow_all policy's selection from its blocked-URL grants", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(shadowMCPPolicyInventoryQueryKey("project-1"), [
      inventoryServer(),
      inventoryServer({
        access: "blocked",
        allowedPolicyIds: [],
        blockedPolicyIds: ["policy-1"],
        canonicalServerUrl: "https://sketchy.example.com/mcp",
        serverName: "Sketchy",
        serverSlug: "sketchy-11111111",
        urlHost: "sketchy.example.com",
      }),
    ]);

    const allowAllPolicy: RiskPolicy = {
      ...blockingPolicyWithDirtyDraftName(),
      shadowMcpDisposition: "allow_all",
    };

    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <StandardPolicyEditor policy={allowAllPolicy} />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    await waitFor(() => {
      // The selection comes from the inventory's per-URL block-grant view
      // (blockedPolicyIds), and the selector flips to block mode.
      expect(mocks.selectionRenders.at(-1)).toEqual([
        "https://sketchy.example.com/mcp",
      ]);
      expect(mocks.modeRenders.at(-1)).toBe("block");
    });
  });
});

describe("StandardPolicyEditor policy pause", () => {
  beforeEach(() => {
    mocks.saveDisabledRenders.length = 0;
    mocks.selectionRenders.length = 0;
    mocks.modeRenders.length = 0;
    mocks.step = "action";
    mocks.kind = null;
    mocks.category = null;
    mocks.detectorSelections.length = 0;
    vi.clearAllMocks();
    vi.mocked(useSdkClient).mockReturnValue({
      access: { listShadowMCPInventory: vi.fn() },
    } as unknown as ReturnType<typeof useSdkClient>);
  });

  it("shows Inactive and an enable switch when the policy is disabled", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <StandardPolicyEditor
            policy={{ ...blockingPolicyWithDirtyDraftName(), enabled: false }}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    expect(screen.getByText("Inactive")).toBeTruthy();
    const toggle = screen.getByRole("switch", { name: "Enable policy" });
    fireEvent.click(toggle);
    expect(mocks.mutateUpdate).toHaveBeenCalledWith({
      request: {
        updateRiskPolicyRequestBody: {
          id: "policy-1",
          name: "Original name",
          enabled: true,
        },
      },
    });
  });
});
