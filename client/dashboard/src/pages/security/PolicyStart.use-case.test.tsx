import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { RiskPreset } from "@gram/client/models/components/riskpreset.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import type { SuggestRiskPolicyResult } from "@gram/client/models/components/suggestriskpolicyresult.js";
import { DraftReview, PolicyStartChooser } from "./PolicyStart";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn<(to: unknown, options?: unknown) => void>(),
  setKind: vi.fn<(kind: string | null) => void>(),
  policies: {
    data: { policies: [] as RiskPolicy[] } as
      | { policies: RiskPolicy[] }
      | undefined,
    isError: false,
  },
}));

vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => mocks.navigate,
}));

vi.mock("nuqs", () => ({
  useQueryState: () => [null, mocks.setKind],
}));

// The chooser's only flag left is MCP-scoped policies, which these cases
// do not exercise.
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "disabled" }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "default",
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => false }),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    policyCenter: { detail: { href: (id: string) => `/guardrails/${id}` } },
  }),
}));

vi.mock("./use-detector-mode", () => ({
  useDetectorMode: () => "presidio",
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// The page frame carries breadcrumbs and layout the chooser does not need.
vi.mock("@/components/page-layout", () => {
  const Slot = ({ children }: { children?: ReactNode }) => (
    <div>{children}</div>
  );
  const Page = Object.assign(Slot, {
    Header: Object.assign(Slot, { Breadcrumbs: () => null }),
    Body: Slot,
    Section: Object.assign(Slot, {
      Title: Slot,
      Description: Slot,
      Body: Slot,
    }),
  });
  return { Page };
});

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: () => mocks.policies,
}));

const presets: RiskPreset[] = [
  {
    id: "secrets_and_credentials",
    label: "Secrets and credentials",
    description: "Block API keys, tokens, private keys and passwords.",
    policyType: "standard",
    sources: ["secrets"],
    presidioEntities: [],
    action: "block",
    score: 8,
    prompt: "",
    userMessage: "",
    requiresApprovedEmailDomains: false,
  },
  {
    id: "unapproved_mcp_servers",
    label: "Unapproved MCP servers",
    description:
      "Flag tool calls that reach MCP servers your organization has not issued or approved.",
    policyType: "standard",
    sources: ["shadow_mcp"],
    presidioEntities: [],
    action: "flag",
    score: 8,
    prompt: "",
    userMessage: "",
    requiresApprovedEmailDomains: false,
  },
];

vi.mock("@gram/client/react-query/riskPresets.js", () => ({
  useRiskPresets: () => ({
    data: { presets },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/riskSuggestPolicy.js", () => ({
  useRiskSuggestPolicyMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({ data: undefined }),
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
    <MemoryRouter initialEntries={["/guardrails/new"]}>
      <TooltipProvider>
        <PolicyStartChooser />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

const shadowAIRow = () =>
  screen.getByRole("button", { name: /Detect unsanctioned AI tools/ });

describe("PolicyStartChooser Shadow AI use case", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.policies = { data: { policies: [] }, isError: false };
    sessionStorage.clear();
  });

  it("presents the use case as Shadow AI and opens the Shadow AI editor", () => {
    renderChooser();

    expect(screen.queryByText("Unapproved MCP servers")).toBeNull();
    const row = shadowAIRow();
    expect(row.textContent).toContain("Shadow AI");
    expect(row.textContent).toContain("Block");

    fireEvent.click(row);

    expect(mocks.setKind).toHaveBeenCalledWith("shadow-ai");
    expect(mocks.navigate).not.toHaveBeenCalled();
  });

  it("opens the policy already holding the slot instead of a second create", () => {
    mocks.policies = {
      data: { policies: [blockingShadowMCPPolicy()] },
      isError: false,
    };
    renderChooser();

    const row = shadowAIRow();
    expect(row.textContent).toContain("Already set up");

    fireEvent.click(row);

    expect(mocks.navigate).toHaveBeenCalledWith("/guardrails/policy-sm");
    expect(mocks.setKind).not.toHaveBeenCalled();
  });

  it("waits for the policy list before offering the use case", () => {
    mocks.policies = { data: undefined, isError: false };
    renderChooser();

    const row = shadowAIRow();
    expect(row.hasAttribute("disabled")).toBe(true);
    expect(row.textContent).toContain("Checking this project's policies");

    fireEvent.click(row);

    expect(mocks.setKind).not.toHaveBeenCalled();
    expect(mocks.navigate).not.toHaveBeenCalled();
  });

  it("leaves the other use cases on the standard editor", () => {
    renderChooser();

    fireEvent.click(
      screen.getByRole("button", { name: /Secrets and credentials/ }),
    );

    expect(mocks.navigate).toHaveBeenCalledWith(
      { pathname: "/guardrails/new", search: "?kind=standard" },
      expect.anything(),
    );
    expect(mocks.setKind).not.toHaveBeenCalled();
  });
});

describe("unsupported policy drafts", () => {
  afterEach(cleanup);

  it("keeps unsupported presets visible without offering an unsafe conversion", () => {
    presets.push({
      ...presets[0]!,
      id: "future_preset",
      label: "Newer settings",
      action: unrecognized("constructor"),
    });
    try {
      renderChooser();
      const row = screen.getByRole("button", { name: /Newer settings/ });
      expect(row.hasAttribute("disabled")).toBe(true);
      expect(row.textContent).toContain("Unknown");
      expect(row.textContent).toContain("Refresh the page");
    } finally {
      presets.pop();
    }
  });

  it.each(["policyType", "action"] as const)(
    "shows a recoverable state for an unknown suggested %s",
    (field) => {
      const suggestion: SuggestRiskPolicyResult = {
        presetId: "",
        name: "A newer policy",
        policyType: "standard",
        action: "block",
        score: 8,
        sources: [],
        presidioEntities: [],
        prompt: "",
        userMessage: "",
        rationale: "",
        confidence: 1,
        alternatives: [],
        [field]: unrecognized("future_value"),
      };
      const onBack = vi.fn<() => void>();
      const onContinue = vi.fn<() => void>();
      render(
        <DraftReview
          description="A risk"
          review={{ suggestion, overridePresetId: null }}
          presets={[]}
          mode="presidio"
          promptPoliciesEnabled
          onOverride={vi.fn<() => void>()}
          onBack={onBack}
          onContinue={onContinue}
        />,
      );
      expect(
        screen.getByText(/This draft uses newer policy settings/),
      ).toBeTruthy();
      expect(screen.queryByRole("button", { name: /Continue/ })).toBeNull();
      fireEvent.click(
        screen.getByRole("button", { name: "Choose another starting point" }),
      );
      expect(onBack).toHaveBeenCalledOnce();
      expect(onContinue).not.toHaveBeenCalled();
    },
  );
});
