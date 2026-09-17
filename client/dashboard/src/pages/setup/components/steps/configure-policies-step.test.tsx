import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { buildPolicyPayload } from "./configure-policies-payload";
import { ConfigurePoliciesStep } from "./configure-policies-step";

const mocks = vi.hoisted(() => ({
  flagResult: vi.fn(),
  mutateCreate: vi.fn(),
  policies: [] as unknown[],
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flagResult() as FeatureFlagResult,
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "default" }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: () => ({ data: { policies: mocks.policies } }),
  invalidateAllRiskListPolicies: vi.fn(),
}));

vi.mock("@gram/client/react-query/riskCategories.js", () => ({
  useRiskCategories: () => ({ data: undefined }),
}));

vi.mock("@gram/client/react-query/riskPoliciesStatus.js", () => ({
  invalidateAllRiskPoliciesStatus: vi.fn(),
}));

vi.mock("@gram/client/react-query/riskCreatePolicy.js", () => ({
  useRiskCreatePolicyMutation: () => ({
    isPending: false,
    mutate: mocks.mutateCreate,
  }),
}));

vi.mock("@gram/client/react-query/riskPoliciesDelete.js", () => ({
  useRiskPoliciesDeleteMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));

vi.mock("@gram/client/react-query/riskPoliciesUpdate.js", () => ({
  useRiskPoliciesUpdateMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));

vi.mock("../step-container", () => ({
  StepContainer: ({
    title,
    children,
  }: {
    title: string;
    children: React.ReactNode;
  }) => (
    <div>
      <h1>{title}</h1>
      {children}
    </div>
  ),
}));

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.policies = [];
  mocks.flagResult.mockReturnValue({ status: "disabled" });
});

function renderStep() {
  // A fresh element per call: React bails out on an identical one, so the
  // mocked flag would not be re-read.
  const tree = () => (
    <MemoryRouter>
      <ConfigurePoliciesStep onComplete={() => {}} />
    </MemoryRouter>
  );
  const view = render(tree());
  return { ...view, rerender: () => view.rerender(tree()) };
}

const PRESIDIO_ONLY_ROWS = [
  "Financial Information",
  "Government Identifiers",
  "Healthcare Information",
];

describe("ConfigurePoliciesStep detector mode", () => {
  it("lists the four personal-data categories under the presidio engine", () => {
    renderStep();

    expect(screen.getByText("Personal Identifiable Information")).toBeTruthy();
    for (const label of PRESIDIO_ONLY_ROWS) {
      expect(screen.getByText(label)).toBeTruthy();
    }
    expect(screen.getByText("0/6 enabled")).toBeTruthy();
  });

  it("collapses them into a single PII row under the LLM analyzer", () => {
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    renderStep();

    expect(screen.getByText("PII")).toBeTruthy();
    for (const label of PRESIDIO_ONLY_ROWS) {
      expect(screen.queryByText(label)).toBeNull();
    }
    expect(screen.getByText("0/3 enabled")).toBeTruthy();
  });

  it("creates an entity-less presidio policy for PII under the LLM analyzer", () => {
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    renderStep();

    fireEvent.click(screen.getByRole("button", { name: /^PII/ }));
    fireEvent.click(screen.getByRole("switch", { name: "Enable detection" }));

    expect(mocks.mutateCreate).toHaveBeenCalledTimes(1);
    const body =
      mocks.mutateCreate.mock.calls[0]?.[0]?.request
        ?.createRiskPolicyRequestBody;
    expect(body).toMatchObject({
      enabled: true,
      sources: ["presidio"],
      presidioEntities: [],
      action: "flag",
    });
  });

  it("closes a legacy category sheet when the flag resolves to the analyzer", () => {
    mocks.flagResult.mockReturnValue({ status: "loading" });
    const view = renderStep();

    fireEvent.click(
      screen.getByRole("button", { name: /^Financial Information/ }),
    );
    expect(
      screen.getByRole("switch", { name: "Enable detection" }),
    ).toBeTruthy();

    mocks.flagResult.mockReturnValue({ status: "enabled" });
    view.rerender();

    expect(
      screen.queryByRole("switch", { name: "Enable detection" }),
    ).toBeNull();
  });

  it("shows an entity-scoped presidio policy as the PII row under the analyzer", () => {
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    mocks.policies = [
      {
        id: "policy-1",
        name: "Financial Blocker",
        enabled: true,
        action: "block",
        sources: ["presidio"],
        presidioEntities: ["CREDIT_CARD"],
        detectionScopes: [],
      },
    ];
    renderStep();

    expect(screen.getByText("1/3 enabled")).toBeTruthy();
  });

  it("backs every personal-data row with an entity-less presidio policy once the flag is off", () => {
    mocks.policies = [
      {
        id: "policy-1",
        name: "PII",
        enabled: true,
        action: "flag",
        sources: ["presidio"],
        presidioEntities: [],
        detectionScopes: [],
      },
    ];
    renderStep();

    expect(screen.getByText("4/6 enabled")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: /^Financial Information/ }),
    );
    fireEvent.click(screen.getByRole("switch", { name: "Enable detection" }));
    fireEvent.click(screen.getByRole("switch", { name: "Enable detection" }));
    expect(mocks.mutateCreate).not.toHaveBeenCalled();
  });
});

describe("buildPolicyPayload", () => {
  it("sends every visible entity under the presidio engine", () => {
    const payload = buildPolicyPayload("pii");
    expect(payload.sources).toEqual(["presidio"]);
    expect(payload.presidioEntities).toContain("EMAIL_ADDRESS");
  });

  it("sends no entities under the LLM analyzer", () => {
    expect(buildPolicyPayload("pii", "llm")).toEqual({
      sources: ["presidio"],
      presidioEntities: [],
    });
  });
});
