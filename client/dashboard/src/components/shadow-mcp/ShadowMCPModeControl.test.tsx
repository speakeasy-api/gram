import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";

const mocks = vi.hoisted(() => ({
  policies: [] as Array<Record<string, unknown>>,
  create: vi.fn(),
  update: vi.fn(),
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
vi.mock("@gram/client/react-query/riskPoliciesUpdate.js", () => ({
  useRiskPoliciesUpdateMutation: () => ({
    mutateAsync: mocks.update,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: {
      members: [
        {
          name: "Priya",
          email: "priya@example.com",
          principalUrn: "user:priya",
        },
      ],
    },
  }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({
    data: {
      roles: [{ name: "Contractors", principalUrn: "role:contractors" }],
    },
  }),
}));
// Mirrors the real picker: it starts from defaultValue, toggles the clicked
// option within the current selection, and emits the whole array.
vi.mock("@/components/ui/MultiSelect", async () => {
  const { useState } = await import("react");
  return {
    MultiSelect: ({
      defaultValue,
      onValueChange,
      options,
    }: {
      defaultValue?: string[];
      onValueChange: (value: string[]) => void;
      options: Array<{ options: Array<{ label: string; value: string }> }>;
    }) => {
      const [selected, setSelected] = useState<string[]>(defaultValue ?? []);
      return (
        <div>
          {options
            .flatMap((group) => group.options)
            .map((option) => (
              <button
                key={option.value}
                type="button"
                onClick={() => {
                  const next = selected.includes(option.value)
                    ? selected.filter((value) => value !== option.value)
                    : [...selected, option.value];
                  setSelected(next);
                  onValueChange(next);
                }}
              >
                {`pick ${option.label}`}
              </button>
            ))}
        </div>
      );
    },
  };
});
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
    mocks.update.mockReset();
    mocks.update.mockResolvedValue(undefined);
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

  it("leaves a policy that also scans for other risks to Guardrails", () => {
    mocks.policies = [
      {
        id: "secrets-and-servers",
        name: "Block secrets and servers",
        enabled: true,
        sources: ["shadow_mcp", "gitleaks"],
        action: "block",
      },
    ];
    renderControl();

    const allowed = screen.getByRole("button", { name: "Allowed by default" });
    expect(allowed.getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(allowed);
    expect(screen.queryByText("Allow servers by default?")).toBeNull();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(mocks.remove).not.toHaveBeenCalled();
  });

  it("blocks by default for one role only, chosen under Advanced", async () => {
    renderControl();

    fireEvent.click(screen.getByRole("button", { name: "Blocked by default" }));
    // Everyone unless the admin opens Advanced.
    expect(screen.queryByText("pick Contractors")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Advanced" }));
    fireEvent.click(screen.getByRole("button", { name: "pick Contractors" }));
    // The dialog no longer says it applies to everyone.
    expect(
      screen.getByText(/blocked for the people and roles you chose/),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Block by default" }));

    await vi.waitFor(() =>
      expect(mocks.create).toHaveBeenCalledWith({
        request: {
          createRiskPolicyRequestBody: expect.objectContaining({
            audienceType: "targeted",
            audiencePrincipalUrns: ["role:contractors"],
            shadowMcpDisposition: "block_all",
          }),
        },
      }),
    );
  });

  it("shows who blocked by default applies to, and changes it", async () => {
    mocks.policies = [
      {
        id: "policy-1",
        name: "Shadow MCP",
        enabled: true,
        sources: ["shadow_mcp"],
        action: "block",
        shadowMcpDisposition: "block_all",
        audienceType: "targeted",
        audiencePrincipalUrns: ["role:contractors"],
      },
    ];
    renderControl();

    expect(
      screen.getByText(
        "Applies only to Contractors. Everyone else is allowed by default.",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.click(
      screen.getByRole("button", { name: "pick Priya (priya@example.com)" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await vi.waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith({
        request: {
          updateRiskPolicyRequestBody: expect.objectContaining({
            id: "policy-1",
            audienceType: "targeted",
            audiencePrincipalUrns: ["role:contractors", "user:priya"],
          }),
        },
      }),
    );
  });

  it("says the edit covers everyone until someone is picked", () => {
    mocks.policies = [
      {
        id: "policy-1",
        name: "Shadow MCP",
        enabled: true,
        sources: ["shadow_mcp"],
        action: "block",
        shadowMcpDisposition: "block_all",
        audienceType: "targeted",
        audiencePrincipalUrns: ["role:contractors"],
      },
    ];
    renderControl();

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(
      screen.getByText(/blocked for the people and roles you chose/),
    ).toBeTruthy();
    // Clearing the picker saves "everyone", so the copy says so.
    fireEvent.click(screen.getByRole("button", { name: "pick Contractors" }));
    expect(screen.getByText(/is blocked for everyone/)).toBeTruthy();
    expect(
      screen.queryByText(/blocked for the people and roles you chose/),
    ).toBeNull();
  });

  const blockingPolicy = {
    id: "policy-1",
    name: "Shadow MCP",
    enabled: true,
    sources: ["shadow_mcp"],
    action: "block",
    shadowMcpDisposition: "block_all",
    audienceType: "everyone",
    audiencePrincipalUrns: [],
  };

  it.each([
    {
      change: "blocking by default",
      policies: [],
      open: "Blocked by default",
      confirm: "Block by default",
      title: "Block servers by default?",
      mutation: () => mocks.create,
    },
    {
      change: "changing who it applies to",
      policies: [blockingPolicy],
      open: "Edit",
      confirm: "Save",
      title: "Who is blocked by default?",
      mutation: () => mocks.update,
    },
    {
      change: "allowing by default",
      policies: [blockingPolicy],
      open: "Allowed by default",
      confirm: "Allow by default",
      title: "Allow servers by default?",
      mutation: () => mocks.remove,
    },
  ])(
    "keeps the dialog open with the error when $change fails",
    async ({ policies, open, confirm, title, mutation }) => {
      mocks.policies = policies;
      mutation().mockRejectedValue(new Error("boom"));
      renderControl();

      fireEvent.click(screen.getByRole("button", { name: open }));
      fireEvent.click(screen.getByRole("button", { name: confirm }));

      expect(
        await screen.findByText("The setting could not be changed. Try again."),
      ).toBeTruthy();
      expect(mutation()).toHaveBeenCalled();
      expect(screen.getByText(title)).toBeTruthy();
    },
  );
});
