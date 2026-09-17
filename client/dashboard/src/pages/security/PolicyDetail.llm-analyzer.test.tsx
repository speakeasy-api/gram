import { useSdkClient } from "@/contexts/Sdk";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { LLM_ANALYZER_NOTICE, StandardPolicyEditor } from "./PolicyDetail";
import type { RuleCategory } from "./policy-data";

const mocks = vi.hoisted(() => ({
  flagResult: vi.fn(),
  mutateCreate: vi.fn(),
  mutateUpdate: vi.fn(),
  customizeSheetCategories: [] as string[],
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flagResult() as FeatureFlagResult,
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
  useQueryState: () => [null, vi.fn()],
}));

vi.mock("@/components/shadow-mcp/ShadowMCPPolicyServerSelector", () => ({
  ShadowMCPPolicyServerSelector: () => null,
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
  CustomizeRulesSheet: ({ category }: { category: string }) => {
    mocks.customizeSheetCategories.push(category);
    return null;
  },
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

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.customizeSheetCategories.length = 0;
  mocks.flagResult.mockReturnValue({ status: "enabled" });
  vi.mocked(useSdkClient).mockReturnValue({
    access: { listShadowMCPInventory: vi.fn() },
  } as unknown as ReturnType<typeof useSdkClient>);
});

function renderEditor(
  policy: RiskPolicy | null,
  initialCategories?: ReadonlySet<RuleCategory>,
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  // A fresh element per call: React bails out on an identical one, so the
  // mocked flag would not be re-read.
  const tree = () => (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <StandardPolicyEditor
          policy={policy}
          initialCategories={initialCategories}
        />
      </TooltipProvider>
    </QueryClientProvider>
  );
  const view = render(tree());
  return { ...view, rerender: () => view.rerender(tree()) };
}

function updateBody() {
  expect(mocks.mutateUpdate).toHaveBeenCalledTimes(1);
  return mocks.mutateUpdate.mock.calls[0]?.[0]?.request
    ?.updateRiskPolicyRequestBody;
}

function presidioPolicy(overrides: Partial<RiskPolicy> = {}): RiskPolicy {
  return {
    action: "flag",
    audiencePrincipalUrns: ["user:all"],
    audienceType: "everyone",
    autoName: false,
    createdAt: new Date("2026-01-01T10:00:00Z"),
    enabled: true,
    id: "policy-1",
    name: "Personal data",
    pendingMessages: 0,
    policyType: "standard",
    projectId: "project-1",
    score: 5,
    sources: ["presidio"],
    presidioEntities: ["CREDIT_CARD", "US_SSN"],
    presidioScoreThreshold: 0.7,
    totalMessages: 0,
    updatedAt: new Date("2026-01-01T10:00:00Z"),
    version: 1,
    ...overrides,
  };
}

const PRESIDIO_ONLY_CARDS = [
  "Financial Information built-in rule",
  "Government Identifiers built-in rule",
  "Healthcare Information built-in rule",
];

function createButton(): HTMLButtonElement {
  return screen.getByRole("button", {
    name: "Create policy",
  }) as HTMLButtonElement;
}

describe("StandardPolicyEditor under the LLM analyzer", () => {
  it("renders a single category-level PII card and no sensitivity control", () => {
    renderEditor(null, new Set<RuleCategory>(["pii"]));

    expect(
      screen.getByRole("switch", { name: "PII built-in rule" }),
    ).toBeTruthy();
    for (const name of PRESIDIO_ONLY_CARDS) {
      expect(screen.queryByRole("switch", { name })).toBeNull();
    }
    expect(
      screen.queryByRole("switch", {
        name: "Off-Policy Content built-in rule",
      }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "Customize" })).toBeNull();
    expect(screen.queryByText("Detection sensitivity")).toBeNull();
    expect(screen.getByText(LLM_ANALYZER_NOTICE)).toBeTruthy();
    expect(mocks.customizeSheetCategories).toEqual([]);
  });

  it("shows the notice for a stored presidio policy even before PII is toggled", () => {
    renderEditor(presidioPolicy());

    expect(screen.getByText(LLM_ANALYZER_NOTICE)).toBeTruthy();
    const pii = screen.getByRole("switch", {
      name: "PII built-in rule",
    }) as HTMLButtonElement;
    expect(pii.getAttribute("aria-checked")).toBe("true");
  });

  it("hides the notice while nothing personal-data related is selected", () => {
    renderEditor(null);

    expect(screen.queryByText(LLM_ANALYZER_NOTICE)).toBeNull();
    expect(createButton().disabled).toBe(true);
  });

  it("enables Create with only PII selected and submits an entity-less payload", () => {
    renderEditor(null);

    fireEvent.click(screen.getByRole("switch", { name: "PII built-in rule" }));
    expect(screen.getByText(LLM_ANALYZER_NOTICE)).toBeTruthy();
    expect(createButton().disabled).toBe(false);

    fireEvent.click(createButton());

    expect(mocks.mutateCreate).toHaveBeenCalledTimes(1);
    const body =
      mocks.mutateCreate.mock.calls[0]?.[0]?.request
        ?.createRiskPolicyRequestBody;
    expect(body).toMatchObject({
      sources: ["presidio"],
      presidioEntities: [],
      disabledRules: [],
    });
    expect(body).not.toHaveProperty("presidioScoreThreshold");
  });

  it("echoes the stored entity list and leaves the threshold untouched on update", () => {
    renderEditor(presidioPolicy({ name: "Original" }));

    fireEvent.click(
      screen.getByRole("switch", { name: "Secrets built-in rule" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    const body = updateBody();
    expect(body).toMatchObject({
      id: "policy-1",
      sources: ["gitleaks", "presidio"],
      presidioEntities: ["CREDIT_CARD", "US_SSN"],
    });
    expect(body).not.toHaveProperty("presidioScoreThreshold");
  });

  it("keeps stored personal-data overrides across a PII off/on flip", () => {
    renderEditor(
      presidioPolicy({
        disabledRules: ["pii.email_address", "pii.credit_card"],
      }),
    );

    const pii = screen.getByRole("switch", { name: "PII built-in rule" });
    fireEvent.click(pii);
    fireEvent.click(pii);
    fireEvent.click(
      screen.getByRole("switch", { name: "Secrets built-in rule" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(updateBody()?.disabledRules?.sort()).toEqual([
      "pii.credit_card",
      "pii.email_address",
    ]);
  });

  it("drops the stored entity list once PII is turned off", () => {
    renderEditor(presidioPolicy());

    fireEvent.click(
      screen.getByRole("switch", { name: "Secrets built-in rule" }),
    );
    fireEvent.click(screen.getByRole("switch", { name: "PII built-in rule" }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(updateBody()).toMatchObject({
      sources: ["gitleaks"],
      presidioEntities: [],
    });
  });

  it("keeps a legacy per-category scope across an edit", () => {
    renderEditor(
      presidioPolicy({
        detectionScopes: [
          { category: "financial", scopeInclude: "message.kind == 'tool'" },
        ],
      }),
    );

    fireEvent.click(
      screen.getByRole("switch", { name: "Secrets built-in rule" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(updateBody()?.detectionScopes).toEqual([
      { category: "financial", scopeInclude: "message.kind == 'tool'" },
    ]);
  });

  it("folds a legacy selection into PII when the flag resolves after mount", () => {
    mocks.flagResult.mockReturnValue({ status: "loading" });
    const view = renderEditor(
      presidioPolicy({ presidioEntities: ["CREDIT_CARD"] }),
    );
    expect(
      screen
        .getByRole("switch", { name: "Financial Information built-in rule" })
        .getAttribute("aria-checked"),
    ).toBe("true");

    mocks.flagResult.mockReturnValue({ status: "enabled" });
    view.rerender();

    const pii = screen.getByRole("switch", { name: "PII built-in rule" });
    expect(pii.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(
      screen.getByRole("switch", { name: "Secrets built-in rule" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(updateBody()).toMatchObject({
      sources: ["gitleaks", "presidio"],
      presidioEntities: ["CREDIT_CARD"],
    });
  });
});

describe("StandardPolicyEditor with the LLM analyzer flag off", () => {
  beforeEach(() => {
    mocks.flagResult.mockReturnValue({ status: "disabled" });
  });

  it("keeps the four presidio cards, customization and sensitivity", () => {
    renderEditor(null, new Set<RuleCategory>(["pii"]));

    expect(
      screen.getByRole("switch", {
        name: "Personal Identifiable Information built-in rule",
      }),
    ).toBeTruthy();
    for (const name of PRESIDIO_ONLY_CARDS) {
      expect(screen.getByRole("switch", { name })).toBeTruthy();
    }
    expect(screen.getByRole("button", { name: "Customize" })).toBeTruthy();
    expect(screen.getByText("Detection sensitivity")).toBeTruthy();
    expect(screen.queryByText(LLM_ANALYZER_NOTICE)).toBeNull();
  });

  it("still sends the entity list and threshold", () => {
    renderEditor(null, new Set<RuleCategory>(["pii"]));

    fireEvent.click(createButton());

    const body =
      mocks.mutateCreate.mock.calls[0]?.[0]?.request
        ?.createRiskPolicyRequestBody;
    expect(body?.sources).toEqual(["presidio"]);
    expect(body?.presidioEntities).toContain("EMAIL_ADDRESS");
    expect(body?.presidioScoreThreshold).toBe(0.5);
  });
});
