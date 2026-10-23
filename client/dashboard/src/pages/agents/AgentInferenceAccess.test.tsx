import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AgentInferenceAccess } from "./AgentInferenceAccess";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
const mocks = vi.hoisted(() => ({ create: vi.fn(), remove: vi.fn() }));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [{ id: "project_one", name: "Project one" }],
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    agents: {
      createPolicyGrant: mocks.create,
      deletePolicyGrant: mocks.remove,
    },
  }),
}));
const agent: ManagedAgent = {
  id: "agent_one",
  name: "Worker",
  ownerUserId: "owner_one",
  lifecycle: "active",
  permissions: { read: true, write: true, authorize: true, transfer: true },
  createdAt: new Date(),
  updatedAt: new Date(),
};
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.create.mockResolvedValue({});
  mocks.remove.mockResolvedValue(undefined);
});
it("does not grant anything until a project and explicit allow action are selected", async () => {
  const onChanged = vi.fn().mockResolvedValue(undefined);
  render(
    <AgentInferenceAccess agent={agent} grants={[]} onChanged={onChanged} />,
  );
  expect(mocks.create).not.toHaveBeenCalled();
  expect(
    (
      screen.getByRole("button", {
        name: "Allow inference in this project",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.change(screen.getByLabelText("Project for inference"), {
    target: { value: "project_one" },
  });
  expect(mocks.create).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("button", { name: "Allow inference in this project" }),
  );
  await waitFor(() => expect(onChanged).toHaveBeenCalled());
  expect(mocks.create).toHaveBeenCalledWith({
    createAgentPolicyGrantForm: {
      agentId: "agent_one",
      effect: "allow",
      scope: "inference:execute",
      selector: {
        resourceKind: "inference",
        resourceId: "*",
        projectId: "project_one",
      },
    },
  });
});
it("revokes only the chosen inference grant", async () => {
  render(
    <AgentInferenceAccess
      agent={agent}
      grants={[
        {
          id: "grant_two",
          effect: "allow",
          scope: "inference:execute",
          selector: {
            resourceKind: "inference",
            resourceId: "*",
            projectId: "project_two",
          },
          createdAt: new Date(),
          updatedAt: new Date(),
        },
        {
          id: "grant_one",
          effect: "allow",
          scope: "inference:execute",
          selector: {
            resourceKind: "inference",
            resourceId: "*",
            projectId: "project_one",
          },
          createdAt: new Date(),
          updatedAt: new Date(),
        },
      ]}
      onChanged={vi.fn().mockResolvedValue(undefined)}
    />,
  );
  fireEvent.click(
    within(
      screen.getByText("Project one", { selector: "span" }).closest("div")!,
    ).getByRole("button", { name: "Revoke inference" }),
  );
  await waitFor(() =>
    expect(mocks.remove).toHaveBeenCalledWith({
      agentPolicyGrantIDForm: { agentId: "agent_one", grantId: "grant_one" },
    }),
  );
  expect(mocks.remove).toHaveBeenCalledTimes(1);
  expect(mocks.create).not.toHaveBeenCalled();
});

it.each([undefined, "*"])(
  "labels unrestricted project selector %s as All projects",
  (projectId) => {
    render(
      <AgentInferenceAccess
        agent={agent}
        grants={[
          {
            id: "grant_all",
            effect: "allow",
            scope: "inference:execute",
            selector: { resourceKind: "inference", resourceId: "*", projectId },
            createdAt: new Date(),
            updatedAt: new Date(),
          },
        ]}
        onChanged={vi.fn().mockResolvedValue(undefined)}
      />,
    );
    expect(screen.getByText("All projects")).toBeTruthy();
    expect(screen.queryByText("*")).toBeNull();
  },
);
