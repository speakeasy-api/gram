import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ShadowAIDecisionsBanner } from "./ShadowAIDecisionsBanner";

const mocks = vi.hoisted(() => ({
  flag: { status: "enabled" } as { status: string },
  count: { aiTools: 6, mcpServers: 3, total: 9, isPending: false },
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flag as FeatureFlagResult,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    policyCenter: {
      new: { href: () => "/guardrails/new" },
      detail: { href: (id: string) => `/guardrails/${id}` },
    },
  }),
}));

vi.mock("./use-undecided-tool-count", () => ({
  useUndecidedToolCount: () => mocks.count,
}));

function shadowAIPolicy(disposition: "allow_all" | "block_all"): RiskPolicy {
  return {
    id: "policy-ta",
    name: "Shadow AI",
    sources: ["shadow_mcp"],
    action: "block",
    enabled: true,
    policyType: "standard",
    shadowMcpDisposition: disposition,
  } as RiskPolicy;
}

function renderBanner(policies: RiskPolicy[]) {
  return render(
    <MemoryRouter>
      <ShadowAIDecisionsBanner policies={policies} />
    </MemoryRouter>,
  );
}

describe("ShadowAIDecisionsBanner", () => {
  afterEach(() => {
    cleanup();
    mocks.flag = { status: "enabled" };
    mocks.count = { aiTools: 6, mcpServers: 3, total: 9, isPending: false };
  });

  it("points at the Shadow AI policy when one exists", () => {
    renderBanner([shadowAIPolicy("allow_all")]);

    expect(
      screen.getByText("9 AI tools without a decision in Shadow AI"),
    ).toBeTruthy();
    expect(screen.getByText(/stay allowed until you disallow/)).toBeTruthy();
    expect(screen.getByText("Review decisions").getAttribute("href")).toBe(
      "/guardrails/policy-ta",
    );
  });

  it("says what a block_all policy does with the unreviewed", () => {
    renderBanner([shadowAIPolicy("block_all")]);

    expect(screen.getByText(/blocked by the policy default/)).toBeTruthy();
  });

  it("offers to create the policy when the project has none", () => {
    renderBanner([]);

    expect(screen.getByText("Set up Shadow AI").getAttribute("href")).toBe(
      "/guardrails/new?kind=shadow-ai",
    );
  });

  it("stays out of the way with nothing to decide, or with the fold-in off", () => {
    mocks.count = { aiTools: 0, mcpServers: 0, total: 0, isPending: false };
    const { container } = renderBanner([shadowAIPolicy("allow_all")]);
    expect(container.textContent).toBe("");

    cleanup();
    mocks.flag = { status: "disabled" };
    const off = renderBanner([shadowAIPolicy("allow_all")]);
    expect(off.container.textContent).toBe("");
  });
});
