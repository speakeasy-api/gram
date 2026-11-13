import { cleanup, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { AssistantIdentitySettings } from "./AssistantIdentitySettings";
const mocks = vi.hoisted(() => ({
  canManageMappings: true,
  members: [] as unknown[],
  agentData: { name: "Example agent" } as { name: string } | undefined,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) =>
      scope === "org:admin" && mocks.canManageMappings,
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
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
  runtimeVersion: 2,
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
  mocks.canManageMappings = true;
  mocks.members = [];
  mocks.agentData = { name: "Example agent" };
});
describe("Assistant identity management", () => {
  it("explains who an assistant without an agent identity acts as", () => {
    setup();
    expect(
      screen.getByText(
        "This assistant has no agent identity. It acts as the person who messages it in the dashboard, and as its creator everywhere else.",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });
  it("links the assigned identity by name without diagnostics", () => {
    setup({ ...assistant, identityState: "ACTIVE", agentId: "example-agent" });
    expect(
      screen.getByRole("link", { name: "Example agent" }).getAttribute("href"),
    ).toBe("/org/projects/project/identities/agent%3Aexample-agent/overview");
    expect(screen.queryByText("example-agent")).toBeNull();
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
