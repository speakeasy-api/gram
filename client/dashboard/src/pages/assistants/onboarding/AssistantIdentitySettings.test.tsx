import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { AssistantIdentitySettings } from "./AssistantIdentitySettings";
const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  canWrite: true,
  canManageMappings: true,
  members: [] as unknown[],
  agents: [] as { id: string; name: string }[],
  flagStatus: "enabled",
  agentData: { name: "Example agent" } as { name: string } | undefined,
  success: vi.fn(),
  onSuccess: undefined as undefined | ((result: Assistant) => void),
}));
vi.mock("sonner", () => ({
  toast: { success: mocks.success, error: vi.fn() },
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: (flag: string) => {
    expect(flag).toBe("agent-identity-credentials");
    return { status: mocks.flagStatus };
  },
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resource?: string) => {
      if (scope === "org:admin") return mocks.canManageMappings;
      expect(resource).toBe("22222222-2222-4222-8222-222222222222");
      return scope === "project:write" && mocks.canWrite;
    },
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/org/identity" } }),
  useRoutes: () => ({
    identity: { href: () => "/org/identity" },
    identities: {
      detail: { overview: { href: (id: string) => `/identities/${id}` } },
    },
  }),
}));
vi.mock("@gram/client/react-query/assistantsUpgradeIdentity.js", () => ({
  useAssistantsUpgradeIdentityMutation: (options: {
    onSuccess: (result: Assistant) => void;
  }) => {
    mocks.onSuccess = options.onSuccess;
    return { mutate: mocks.mutate, isPending: false };
  },
}));
vi.mock("@gram/client/react-query/agents.js", () => ({
  useAgents: () => ({ data: mocks.agents, isPending: false, isError: false }),
}));
vi.mock("@gram/client/react-query/agent.js", () => ({
  useAgent: () => ({ data: mocks.agentData }),
}));
vi.mock("@gram/client/react-query/slackDirectoryMembers.js", () => ({
  useSlackDirectoryMembers: () => ({ data: { members: mocks.members } }),
}));
const assistant: Assistant = {
  id: "11111111-1111-4111-8111-111111111111",
  projectId: "22222222-2222-4222-8222-222222222222",
  name: "Example assistant",
  model: "example/model",
  instructions: "",
  toolsets: [],
  mcpServers: [],
  skills: [],
  warmTtlSeconds: 300,
  maxConcurrency: 1,
  status: "active",
  createdAt: new Date(),
  updatedAt: new Date(),
  identityState: "NEVER_CONFIGURED",
  identityDiagnostics: {
    health: "legacy",
    bindings: [],
    bindingsTruncated: false,
  },
};
function setup(value: Assistant = assistant) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <AssistantIdentitySettings assistant={value} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.canWrite = true;
  mocks.canManageMappings = true;
  mocks.members = [];
  mocks.agents = [];
  mocks.flagStatus = "enabled";
  mocks.agentData = { name: "Example agent" };
});
describe("Assistant identity management", () => {
  it("requires a different new name when the assistant name is already used", () => {
    mocks.agents = [
      { id: "existing-agent", name: assistant.name.toUpperCase() },
    ];
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Set up workloads" }));
    expect(
      (
        screen.getByRole("button", {
          name: "Confirm setup",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.change(screen.getByLabelText("Identity name"), {
      target: { value: "New identity" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Confirm setup" }));
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        upgradeAssistantIdentityRequestBody: {
          id: assistant.id,
          agentName: "New identity",
        },
      },
    });
  });
  it("submits the selected existing identity without creating another", () => {
    mocks.agents = [{ id: "existing-agent", name: "Shared identity" }];
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Set up workloads" }));
    fireEvent.change(screen.getByLabelText("Agent identity"), {
      target: { value: "existing-agent" },
    });
    expect(screen.queryByLabelText("Identity name")).toBeNull();
    expect(screen.getByText("Actions will be attributed to")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Confirm setup" }));
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        upgradeAssistantIdentityRequestBody: {
          id: assistant.id,
          agentId: "existing-agent",
        },
      },
    });
  });

  it("shows only legacy attribution and setup before confirmation", () => {
    setup();
    expect(
      screen.getByText(
        "This assistant uses legacy authentication bindings. Actions are attributed to the owner.",
      ),
    ).toBeTruthy();
    expect(
      screen.queryByText(
        /health|generation|thread history|workload bindings|trigger (?:id|status|details)/i,
      ),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Set up workloads" }));
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.queryByText(assistant.id)).toBeNull();
    expect(screen.queryByText(assistant.projectId)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Confirm setup" }));
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        upgradeAssistantIdentityRequestBody: {
          id: assistant.id,
          agentName: assistant.name,
        },
      },
    });
  });
  it("hides setup without project write", () => {
    mocks.canWrite = false;
    setup();
    expect(
      screen.queryByRole("button", { name: "Set up workloads" }),
    ).toBeNull();
  });
  it("respects the existing organization feature gate", () => {
    mocks.flagStatus = "disabled";
    setup();
    expect(screen.queryByText(/legacy authentication/)).toBeNull();
  });
  it("links the assigned identity by name without diagnostics", () => {
    setup({ ...assistant, identityState: "ACTIVE", agentId: "example-agent" });
    expect(
      screen.getByRole("link", { name: "Example agent" }).getAttribute("href"),
    ).toBe("/identities/agent%3Aexample-agent");
    expect(screen.queryByText("example-agent")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Set up workloads" }),
    ).toBeNull();
  });
  it("links to Slack mapping setup when no mappings exist", () => {
    setup({ ...assistant, identityState: "ACTIVE", agentId: "example-agent" });
    expect(
      screen.getByText("Link assistant permissions to the user prompting it:"),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Set up Slack mapping" })
        .getAttribute("href"),
    ).toBe("/org/identity?tab=slack-workspaces&slack_view=members");
  });
  it("does not offer setup when a Slack mapping exists", () => {
    mocks.members = [{}];
    setup({ ...assistant, identityState: "ACTIVE", agentId: "example-agent" });
    expect(
      screen.queryByRole("link", { name: "Set up Slack mapping" }),
    ).toBeNull();
  });
  it("does not offer admin-only mapping setup to members", () => {
    mocks.canManageMappings = false;
    setup({ ...assistant, identityState: "ACTIVE", agentId: "example-agent" });
    expect(
      screen.queryByRole("link", { name: "Set up Slack mapping" }),
    ).toBeNull();
  });
});

it("keeps the assigned identity link when its name is unavailable", () => {
  mocks.agentData = undefined;
  setup({ ...assistant, identityState: "ACTIVE", agentId: "assigned-agent" });
  expect(
    screen.getByRole("link", { name: "Agent identity" }).getAttribute("href"),
  ).toContain("assigned-agent");
});
