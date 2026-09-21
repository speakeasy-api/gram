import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useIsMutating,
  useMutation,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";

import {
  AgentPicker,
  GrantAccess,
  IssueKey,
} from "./agent-identity-onboarding";
import {
  deviceAgentPolicyGrants,
  ISSUE_AGENT_KEY_MUTATION,
} from "./agent-identity-setup";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  listDelegableGrants: vi.fn(),
  listPolicyGrants: vi.fn(),
  createPolicyGrant: vi.fn(),
  createKey: vi.fn(),
  createAgent: vi.fn(),
  serverURL: "https://app.getgram.ai",
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_example",
    projects: [{ id: "project_one", name: "Project one", slug: "project-one" }],
  }),
  useSession: () => ({ user: { id: "user_example" } }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({ agents: mocks }) }));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    agents: { href: () => "/agents" },
    deviceAgent: { href: () => "/device-agent" },
  }),
  // Agent management is project-scoped, so the page resolves it per project.
  useRoutes: () => ({
    agents: { href: () => "/org/projects/project-one/agent-management" },
  }),
}));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => mocks.serverURL,
}));
vi.mock("@gram/client/react-query/createAPIKey", () => ({
  useCreateAPIKeyMutation: (options: object) =>
    useMutation({ mutationFn: mocks.createKey, ...options }),
}));
vi.mock("@gram/client/react-query/createAgent.js", () => ({
  useCreateAgentMutation: (options: object) =>
    useMutation({ mutationFn: mocks.createAgent, ...options }),
}));

const agent: ManagedAgent = {
  id: "agent_example",
  name: "Example",
  ownerUserId: "user_example",
  lifecycle: "active",
  permissions: { read: true, write: true, authorize: true, transfer: false },
  createdAt: new Date(),
  updatedAt: new Date(),
};

function renderWithClient(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const noop = (): void => {};

function IssuanceProbe() {
  const issuing = useIsMutating({ mutationKey: ISSUE_AGENT_KEY_MUTATION });
  return <span>{issuing > 0 ? "selection locked" : "selection open"}</span>;
}

beforeEach(() => {
  mocks.list.mockResolvedValue([agent]);
  mocks.listDelegableGrants.mockResolvedValue(
    deviceAgentPolicyGrants("project_one"),
  );
  mocks.serverURL = "https://app.getgram.ai";
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("agent identity selection lock", () => {
  it("disables agent choice and picker mode while locked", async () => {
    renderWithClient(<AgentPicker agent={agent} onChange={noop} disabled />);
    expect(
      screen.getByRole("button", { name: "Existing agent" }),
    ).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: "New agent" })).toHaveProperty(
      "disabled",
      true,
    );
    expect(
      await screen.findByRole("combobox", { name: "Agent" }),
    ).toHaveProperty("disabled", true);
  });

  it("disables the project and grant while locked", () => {
    renderWithClient(
      <GrantAccess
        agent={agent}
        projectId="project_one"
        onProjectChange={noop}
        granted={false}
        onGranted={noop}
        disabled
      />,
    );
    expect(
      screen.getByRole("combobox", { name: "Hooks project" }),
    ).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: "Grant access" })).toHaveProperty(
      "disabled",
      true,
    );
  });

  it("holds the lock for the whole key issuance", async () => {
    let finish: (value: unknown) => void = () => {};
    mocks.createKey.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    const onIssued = vi.fn();
    renderWithClient(
      <>
        <IssuanceProbe />
        <IssueKey
          agent={agent}
          projectId="project_one"
          issued={false}
          onIssued={(key) => {
            onIssued(key);
          }}
        />
      </>,
    );
    expect(screen.getByText("selection open")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Create API key" }));
    expect(await screen.findByText("selection locked")).toBeTruthy();

    finish({ id: "key_new", key: "gram_live_new" });
    await waitFor(() =>
      expect(onIssued).toHaveBeenCalledWith({
        agentId: agent.id,
        projectId: "project_one",
        keyId: "key_new",
        value: "gram_live_new",
      }),
    );
    expect(await screen.findByText("selection open")).toBeTruthy();
  });

  it("refuses to issue a key for a plaintext control plane", async () => {
    mocks.serverURL = "http://gram.example.com";
    renderWithClient(
      <IssueKey
        agent={agent}
        projectId="project_one"
        issued={false}
        onIssued={noop}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Create API key" }));
    expect((await screen.findByRole("alert")).textContent).toMatch(/HTTPS/);
    expect(mocks.createKey).not.toHaveBeenCalled();
  });
});
