import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";

const mocks = vi.hoisted(() => ({
  policies: [] as Array<Record<string, unknown>>,
  create: vi.fn(),
  remove: vi.fn(),
}));

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: () => ({ data: { policies: mocks.policies } }),
  invalidateAllRiskListPolicies: async () => {},
}));
vi.mock("@gram/client/react-query/riskCreatePolicy.js", () => ({
  useRiskCreatePolicyMutation: () => ({
    mutateAsync: mocks.create,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/riskPoliciesDelete.js", () => ({
  useRiskPoliciesDeleteMutation: () => ({
    mutateAsync: mocks.remove,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/shadowMCPInventory.js", () => ({
  invalidateAllShadowMCPInventory: async () => {},
}));
vi.mock("@gram/client/react-query/shadowMCPInventoryServer.js", () => ({
  invalidateAllShadowMCPInventoryServer: async () => {},
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { ShadowMCPModeControl } from "./ShadowMCPModeControl";

function renderControl() {
  return render(
    <TooltipProvider>
      <ShadowMCPModeControl />
    </TooltipProvider>,
  );
}

describe("ShadowMCPModeControl", () => {
  beforeEach(() => {
    mocks.policies = [];
    mocks.create.mockReset();
    mocks.create.mockResolvedValue({ id: "new-policy" });
    mocks.remove.mockReset();
    mocks.remove.mockResolvedValue(undefined);
  });
  afterEach(cleanup);

  it("starts allowed by default and blocks by default after a confirmation", async () => {
    renderControl();

    expect(
      screen
        .getByRole("button", { name: "Allowed by default" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Blocked by default" }));
    expect(screen.getByText("Block servers by default?")).toBeTruthy();
    expect(mocks.create).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Block by default" }));
    await vi.waitFor(() =>
      expect(mocks.create).toHaveBeenCalledWith({
        request: {
          createRiskPolicyRequestBody: expect.objectContaining({
            sources: ["shadow_mcp"],
            action: "block",
            audienceType: "everyone",
            shadowMcpDisposition: "block_all",
          }),
        },
      }),
    );
  });

  it("removes the policy to allow by default again", async () => {
    mocks.policies = [
      {
        id: "policy-1",
        enabled: true,
        sources: ["shadow_mcp"],
        action: "block",
        shadowMcpDisposition: "block_all",
      },
    ];
    renderControl();

    fireEvent.click(screen.getByRole("button", { name: "Allowed by default" }));
    fireEvent.click(screen.getByRole("button", { name: "Allow by default" }));
    await vi.waitFor(() =>
      expect(mocks.remove).toHaveBeenCalledWith({
        request: { id: "policy-1" },
      }),
    );
  });

  it("cannot block by default while a Shadow AI policy holds the slot", () => {
    mocks.policies = [
      {
        id: "shadow-ai",
        enabled: true,
        sources: ["shadow_mcp"],
        action: "block",
        shadowMcpDisposition: "allow_all",
      },
    ];
    renderControl();

    const blocked = screen.getByRole("button", { name: "Blocked by default" });
    expect(blocked.getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(blocked);
    expect(screen.queryByText("Block servers by default?")).toBeNull();
  });
});
