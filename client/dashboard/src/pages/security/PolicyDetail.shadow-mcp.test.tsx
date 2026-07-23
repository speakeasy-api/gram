import { useSdkClient } from "@/contexts/Sdk";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, waitFor } from "@testing-library/react";
import { Children, isValidElement, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { shadowMCPPolicyInventoryQueryKey } from "@/components/shadow-mcp/useShadowMCPPolicyInventory";
import { TooltipProvider } from "@/components/ui/tooltip";
import { StandardPolicyEditor } from "./PolicyDetail";

const mocks = vi.hoisted(() => ({
  saveDisabledRenders: [] as boolean[],
  selectionRenders: [] as string[][],
  step: "action" as string | null,
  mutateCreate: vi.fn(),
  mutateUpdate: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1" }),
}));

vi.mock("@/components/page-layout", () => ({
  Page: () => null,
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: vi.fn(),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({ policyCenter: { goTo: vi.fn() } }),
}));

vi.mock("nuqs", () => ({
  useQueryState: () => [mocks.step, vi.fn()],
}));

vi.mock("@/components/shadow-mcp/ShadowMCPPolicyServerSelector", () => ({
  ShadowMCPPolicyServerSelector: ({
    selectedURLs,
  }: {
    selectedURLs: ReadonlySet<string>;
  }) => {
    mocks.selectionRenders.push([...selectedURLs].sort());
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
  DetectorCard: () => null,
  PolicyAudiencePicker: () => null,
  RuleSelectList: () => null,
  ScopeCard: () => null,
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

vi.mock("@speakeasy-api/moonshine", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@speakeasy-api/moonshine")>();

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

function inventoryServer(): ShadowMCPInventoryServer {
  return {
    access: "allowed",
    allowedPolicyIds: ["policy-1"],
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
    mocks.step = "action";
    vi.clearAllMocks();
    vi.mocked(useSdkClient).mockReturnValue({
      access: { listShadowMCPInventory: vi.fn() },
    } as unknown as ReturnType<typeof useSdkClient>);
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
  });
});
