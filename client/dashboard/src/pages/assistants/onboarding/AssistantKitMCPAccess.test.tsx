import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AssistantKitMCPAccess } from "./AssistantKitMCPAccess";

const state = vi.hoisted(() => ({
  create: vi.fn().mockResolvedValue(undefined),
  remove: vi.fn().mockResolvedValue(undefined),
  canWrite: true,
  grants: [] as unknown[],
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ activeOrganizationId: "org", user: { id: "user" } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project",
}));
vi.mock("@gram/client/react-query/agent.js", () => ({
  useAgent: () => ({ data: { permissions: { write: state.canWrite } } }),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({
    data: {
      mcpServers: [
        {
          id: "server",
          toolsetId: "toolset",
          projectId: "project",
          name: "Hosted server",
        },
      ],
    },
  }),
}));
vi.mock("@gram/client/react-query/agentsListPolicyGrants.js", () => ({
  useAgentsListPolicyGrants: () => ({ data: state.grants }),
  invalidateAgentsListPolicyGrants: vi.fn(),
}));
vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  useResourceAudience: () => ({
    data: {
      entries: [
        {
          principalUrn: "role:research",
          displayName: "Research",
          kind: "role",
          appliesTo: "resource",
          level: "use",
          agentIds: ["agent"],
        },
      ],
    },
  }),
  invalidateResourceAudience: vi.fn(),
}));
vi.mock("@gram/client/react-query/agentsCreatePolicyGrant.js", () => ({
  useAgentsCreatePolicyGrantMutation: () => ({ mutateAsync: state.create }),
}));
vi.mock("@gram/client/react-query/agentsDeletePolicyGrant.js", () => ({
  useAgentsDeletePolicyGrantMutation: () => ({ mutateAsync: state.remove }),
}));

beforeEach(() => {
  state.canWrite = true;
  state.grants = [];
  vi.clearAllMocks();
});
afterEach(cleanup);

const selector = {
  resourceKind: "mcp",
  resourceId: "toolset",
  projectId: "project",
};

function renderEditor() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AssistantKitMCPAccess agentId="agent" />
    </QueryClientProvider>,
  );
}

it("keeps inherited access separate from the checkbox and grants on the hosted resource ID", async () => {
  renderEditor();
  expect(
    screen.getByText("Agent access: All tools · via Research"),
  ).toBeTruthy();
  const checkbox = screen.getByRole("checkbox", {
    name: "Direct Connect access to Hosted server",
  });
  expect(checkbox.getAttribute("aria-checked")).toBe("false");
  fireEvent.click(checkbox);
  await waitFor(() =>
    expect(state.create).toHaveBeenCalledWith({
      request: {
        createAgentPolicyGrantForm: {
          agentId: "agent",
          effect: "allow",
          scope: "mcp:connect",
          selector,
        },
      },
    }),
  );
});

it("removes only the direct Connect grant using its expected policy", async () => {
  state.grants = [
    { id: "direct", effect: "allow", scope: "mcp:connect", selector },
    {
      id: "conditioned",
      effect: "allow",
      scope: "mcp:connect",
      selector: { ...selector, tool: "search" },
    },
    { id: "auth", effect: "allow", scope: "mcp:auth", selector },
  ];
  renderEditor();
  const checkbox = screen.getByRole("checkbox");
  expect(checkbox.getAttribute("aria-checked")).toBe("true");
  fireEvent.click(checkbox);
  await waitFor(() => expect(state.remove).toHaveBeenCalledOnce());
  expect(state.remove).toHaveBeenCalledWith({
    request: {
      deleteAgentPolicyGrantForm: {
        agentId: "agent",
        grantId: "direct",
        expectedPolicy: { scope: "mcp:connect", selector },
      },
    },
  });
});

it("leaves the access explanation visible without showing write controls to readers", () => {
  state.canWrite = false;
  renderEditor();
  expect(
    screen.getByText("Agent access: All tools · via Research"),
  ).toBeTruthy();
  expect(screen.queryByRole("checkbox")).toBeNull();
});
