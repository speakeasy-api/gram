import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
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
  agents: [] as {
    id: string;
    name: string;
    projectId?: string;
    lifecycle: string;
    permissions: { authorize: boolean };
  }[],
  flagStatus: "enabled",
  agentData: { name: "Example agent" } as { name: string } | undefined,
  success: vi.fn(),
  error: vi.fn(),
  refetchAgents: vi.fn(),
  onError: undefined as undefined | ((error: Error) => void),
  onSuccess: undefined as undefined | ((result: Assistant) => void),
}));
vi.mock("sonner", () => ({
  toast: { success: mocks.success, error: mocks.error },
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
    onError: (error: Error) => void;
  }) => {
    mocks.onSuccess = options.onSuccess;
    mocks.onError = options.onError;
    return { mutate: mocks.mutate, isPending: false };
  },
}));
// The picker drains every page of agents.list rather than reading one, so the
// list arrives through the sdk client and not a generated hook.
vi.mock("@gram/client/react-query/agents.js", () => ({
  queryKeyAgents: () => ["agents"],
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ agents: { list: mocks.refetchAgents } }),
}));
vi.mock("@/components/sessions/collectPageItems", () => ({
  collectPageItems: () => Promise.resolve(mocks.agents),
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
  it("warns about a matching name without overriding server uniqueness checks", async () => {
    mocks.agents = [
      {
        id: "existing-agent",
        name: assistant.name.toUpperCase(),
        lifecycle: "active",
        permissions: { authorize: true },
      },
    ];
    setup();
    fireEvent.click(
      screen.getByRole("button", { name: "Set up agent identity" }),
    );
    // The list is fetched, so the warning is the signal that it has landed.
    expect(
      await screen.findByText(/An agent already uses this name/),
    ).toBeTruthy();
    expect(
      (
        screen.getByRole("button", {
          name: "Confirm setup",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
    fireEvent.change(screen.getByLabelText("Agent name"), {
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
  it("submits the selected existing identity without creating another", async () => {
    mocks.agents = [
      {
        id: "existing-agent",
        name: "Shared identity",
        projectId: assistant.projectId,
        lifecycle: "active",
        permissions: { authorize: true },
      },
      {
        id: "other-project-agent",
        name: "Other project identity",
        projectId: "33333333-3333-4333-8333-333333333333",
        lifecycle: "active",
        permissions: { authorize: true },
      },
      {
        id: "unauthorized-agent",
        name: "Unauthorized identity",
        projectId: assistant.projectId,
        lifecycle: "active",
        permissions: { authorize: false },
      },
    ];
    setup();
    fireEvent.click(
      screen.getByRole("button", { name: "Set up agent identity" }),
    );
    // The option only exists once the fetched list has landed.
    await screen.findByRole("option", { name: "Shared identity" });
    fireEvent.change(screen.getByLabelText("Agent"), {
      target: { value: "existing-agent" },
    });
    expect(screen.queryByLabelText("Agent name")).toBeNull();
    expect(
      screen.queryByRole("option", { name: "Other project identity" }),
    ).toBeNull();
    expect(
      screen.queryByRole("option", { name: "Unauthorized identity" }),
    ).toBeNull();
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

  it("explains who an assistant without an agent identity acts as", async () => {
    setup();
    expect(
      screen.getByText(
        "This assistant has no agent identity. It acts as the person who messages it in the dashboard, and as its creator everywhere else.",
      ),
    ).toBeTruthy();
    expect(
      screen.queryByText(
        /health|generation|thread history|workload bindings|trigger (?:id|status|details)/i,
      ),
    ).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Set up agent identity" }),
    );
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.queryByText(assistant.id)).toBeNull();
    expect(screen.queryByText(assistant.projectId)).toBeNull();
    // Confirm stays disabled until the fetched list says the name is free.
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Confirm setup",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
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
      screen.queryByRole("button", { name: "Set up agent identity" }),
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
      screen.queryByRole("button", { name: "Set up agent identity" }),
    ).toBeNull();
  });
  it("links to Slack mapping setup when no mappings exist", () => {
    setup({ ...assistant, identityState: "ACTIVE", agentId: "example-agent" });
    expect(
      screen.getByText(/Map Slack members to people in your organization/),
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

it("reports a server-side name collision without permission guidance", () => {
  setup();
  mocks.onError?.(new Error("an agent already uses this name"));
  expect(mocks.error).toHaveBeenCalledWith(
    "An agent already uses this name. Choose a different name.",
  );
  expect(mocks.refetchAgents).toHaveBeenCalled();
});
