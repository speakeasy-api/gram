import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ShadowAIScratchOption } from "./PolicyStart";

const mocks = vi.hoisted(() => ({
  onPick: vi.fn<() => void>(),
  policies: {
    data: { policies: [] as RiskPolicy[] } as
      | { policies: RiskPolicy[] }
      | undefined,
    isError: false,
  },
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
  useProjectSlugForRequests: () => "project",
}));

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: () => mocks.policies,
}));

// Everything else PolicyStart imports is unused by the option under test;
// the SDK hooks below are stubbed so the module loads without a client.
vi.mock("@gram/client/react-query/riskPresets.js", () => ({
  useRiskPresets: () => ({ data: undefined, isLoading: false }),
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

function renderOption() {
  return render(
    <MemoryRouter>
      <ShadowAIScratchOption onPick={mocks.onPick} />
    </MemoryRouter>,
  );
}

describe("ShadowAIScratchOption", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.policies = { data: { policies: [] }, isError: false };
  });

  it("offers a Shadow AI policy when the project has none", () => {
    renderOption();

    fireEvent.click(screen.getByText("Shadow AI"));

    expect(mocks.onPick).toHaveBeenCalledTimes(1);
  });

  it("points at the existing policy instead of a second create", () => {
    mocks.policies = {
      data: { policies: [blockingShadowMCPPolicy()] },
      isError: false,
    };
    renderOption();

    expect(screen.getByText(/already has one/)).toBeTruthy();
    expect(
      screen.getByText("Open Shadow MCP Server Policy").getAttribute("href"),
    ).toBe("/org/projects/project/risk-policies/policy-sm");
    fireEvent.click(screen.getByText("Shadow AI"));
    expect(mocks.onPick).not.toHaveBeenCalled();
  });

  it("keeps the existing policy link when a refetch fails over a cached list", () => {
    mocks.policies = {
      data: { policies: [blockingShadowMCPPolicy()] },
      isError: true,
    };
    renderOption();

    expect(screen.getByText(/already has one/)).toBeTruthy();
    expect(screen.queryByText(/could not be loaded/)).toBeNull();
  });

  it("waits for the policy list before offering a create", () => {
    mocks.policies = { data: undefined, isError: false };
    renderOption();

    expect(screen.getByText(/Checking this project's policies/)).toBeTruthy();
    fireEvent.click(screen.getByText("Shadow AI"));
    expect(mocks.onPick).not.toHaveBeenCalled();
  });

  it("does not offer a create when the policy list failed to load", () => {
    mocks.policies = { data: undefined, isError: true };
    renderOption();

    expect(screen.getByText(/could not be loaded/)).toBeTruthy();
    fireEvent.click(screen.getByText("Shadow AI"));
    expect(mocks.onPick).not.toHaveBeenCalled();
  });
});
