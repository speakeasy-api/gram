import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type { WorkloadDeviceJoinTokenSummary } from "@gram/client/models/components/workloaddevicejointokensummary.js";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WorkloadPlatformPicker } from "../provision/StepWorkload";
import { AgentBootstrapTokens } from "./AgentBootstrapTokens";
import { ManualSetup } from "./DeviceSetupPanel";
import { DeviceVerify } from "./DeviceVerify";

const mocks = vi.hoisted(() => ({
  tokens: [] as WorkloadDeviceJoinTokenSummary[],
  admissions: [] as WorkloadAdmission[],
  createToken: vi.fn(),
  revoke: vi.fn(),
  withdraw: vi.fn(),
  canRead: true,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_example",
    name: "Example Org",
    slug: "example",
    projects: [{ id: "project_one", name: "Project one", slug: "project-one" }],
  }),
  useProject: () => ({ id: "project_one", name: "Project one", slug: "p" }),
  useSession: () => ({
    user: { id: "user_owner", displayName: "Example Owner", email: "o@e.test" },
  }),
}));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://app.getgram.ai",
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAllScopes: () => mocks.canRead,
    hasAnyScope: () => mocks.canRead,
    isLoading: false,
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@gram/client/react-query/createWorkloadDeviceJoinToken.js", () => ({
  useCreateWorkloadDeviceJoinTokenMutation: () => ({
    mutate: mocks.createToken,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/workloadDeviceJoinTokens.js", () => ({
  useWorkloadDeviceJoinTokens: () => ({
    data: { result: { items: mocks.tokens } },
    isPending: false,
    isError: false,
  }),
  useWorkloadDeviceJoinTokensInfinite: () => ({
    data: { pages: [{ result: { items: mocks.tokens } }] },
    isPending: false,
    isError: false,
    hasNextPage: false,
    fetchNextPage: vi.fn(),
    refetch: vi.fn(),
  }),
  invalidateAllWorkloadDeviceJoinTokens: vi.fn(),
}));
vi.mock("@gram/client/react-query/revokeWorkloadDeviceJoinToken.js", () => ({
  useRevokeWorkloadDeviceJoinTokenMutation: () => ({
    mutate: mocks.revoke,
    reset: vi.fn(),
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/withdrawWorkloadSubject.js", () => ({
  useWithdrawWorkloadSubjectMutation: () => ({
    mutate: mocks.withdraw,
    reset: vi.fn(),
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () => ({
    data: { issuers: [], admissions: mocks.admissions },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  }),
  invalidateAllWorkloadIdentities: vi.fn(),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: {
      members: [{ id: "user_other", name: "Pat Example", email: "p@e.test" }],
    },
  }),
}));
vi.mock("@gram/client/react-query/agentsListPolicyGrants.js", () => ({
  useAgentsListPolicyGrants: () => ({ data: [], isPending: false }),
  invalidateAgentsListPolicyGrants: vi.fn(),
}));
vi.mock("@gram/client/react-query/agentsCreatePolicyGrant.js", () => ({
  useAgentsCreatePolicyGrantMutation: () => ({ mutateAsync: vi.fn() }),
}));

const agent = { id: "agent_one", name: "Box Agent" } as ManagedAgent;

function token(
  over: Partial<WorkloadDeviceJoinTokenSummary> = {},
): WorkloadDeviceJoinTokenSummary {
  return {
    id: "token_one",
    agentId: "agent_one",
    createdAt: new Date(Date.now() - 5 * 60_000),
    createdByUserId: "user_other",
    expiresAt: new Date(Date.now() + 55 * 60_000),
    status: "active",
    ...over,
  };
}

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>{node}</QueryClientProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.tokens = [];
  mocks.admissions = [];
  mocks.canRead = true;
});

describe("the workload picker", () => {
  const picker = (showHeadlessDevice: boolean) =>
    render(
      <WorkloadPlatformPicker
        entries={[]}
        isPending={false}
        isError={false}
        onRetry={vi.fn<() => void>()}
        selectedKey={null}
        showHeadlessDevice={showHeadlessDevice}
        onSelect={vi.fn<(key: string) => void>()}
      />,
    );

  it("offers a headless device to someone who may manage workloads", () => {
    picker(true);
    expect(screen.getByRole("radio", { name: /Headless device/ })).toBeTruthy();
  });

  it("does not offer it to anyone else", () => {
    picker(false);
    expect(screen.queryByText("Headless device")).toBeNull();
  });
});

describe("setting a box up manually", () => {
  it("shows the bootstrap token once, at the top of managed.json", async () => {
    const onTokenCreated = vi.fn<(watched: object) => void>();
    const expiresAt = new Date(Date.now() + 3600_000);
    mocks.createToken.mockImplementation(
      (_vars, options: { onSuccess: (r: object) => void }) =>
        options.onSuccess({ token: "gram_dj_example", expiresAt }),
    );
    wrap(
      <ManualSetup
        agentId="agent_one"
        hooksProject={{ id: "project_one", name: "Project one", slug: "p1" }}
        lifetimeSeconds={900}
        onTokenCreated={onTokenCreated}
        defaultOpen
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Create bootstrap token" }),
    );

    expect(mocks.createToken).toHaveBeenCalledTimes(1);
    expect(mocks.createToken.mock.calls[0]?.[0]).toEqual({
      request: {
        createDeviceJoinTokenRequestBody: {
          agentId: "agent_one",
          expiresInSeconds: 900,
        },
      },
    });
    expect(onTokenCreated).toHaveBeenCalledWith({ expiresAt });
    // Highlighting fills the block in after the first render.
    const block = screen.getByLabelText("/etc/speakeasy/managed.json");
    await waitFor(() =>
      expect(JSON.parse(block.textContent ?? "")).toEqual({
        v: 1,
        workload_identity: { source: "device_key", hooks_project_slug: "p1" },
        join_token: "gram_dj_example",
      }),
    );
    // Shown once: the button that made it is gone, so there is no second
    // reveal of the same token.
    expect(
      screen.queryByRole("button", { name: "Create bootstrap token" }),
    ).toBeNull();
  });
});

describe("verifying the box enrolled", () => {
  it("waits, then names the device once its token is redeemed", () => {
    mocks.tokens = [token()];
    const view = wrap(
      <DeviceVerify agentId="agent_one" watched={[{ id: "token_one" }]} />,
    );
    expect(screen.getByText("Waiting for the box to enroll")).toBeTruthy();

    mocks.tokens = [
      token({
        status: "redeemed",
        consumedAt: new Date(),
        consumedBySubject: "device:0001",
      }),
    ];
    mocks.admissions = [
      {
        id: "adm_1",
        agentId: "agent_one",
        subject: "device:0001",
        name: "build-box-01",
        issuerName: "Speakeasy devices",
      } as WorkloadAdmission,
    ];
    view.rerender(
      <QueryClientProvider client={new QueryClient()}>
        <DeviceVerify agentId="agent_one" watched={[{ id: "token_one" }]} />
      </QueryClientProvider>,
    );
    expect(screen.getByText("Device enrolled")).toBeTruthy();
    expect(screen.getByText(/build-box-01 enrolled/)).toBeTruthy();
  });
});

describe("the agent's bootstrap tokens", () => {
  it("revokes an active token after confirming", async () => {
    mocks.tokens = [token(), token({ id: "token_two", status: "redeemed" })];
    wrap(<AgentBootstrapTokens agent={agent} />);

    expect(screen.getAllByText("Pat Example")).toHaveLength(2);
    // Only the active token can be revoked.
    const revokes = screen.getAllByRole("button", {
      name: "Revoke bootstrap token",
    });
    expect(revokes).toHaveLength(1);
    fireEvent.click(revokes[0]!);
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Revoke token" }),
    );
    await waitFor(() =>
      expect(mocks.revoke).toHaveBeenCalledWith({
        request: { id: "token_one" },
      }),
    );
  });

  it("lists enrolled devices and removes one after confirming", async () => {
    mocks.admissions = [
      {
        id: "adm_device",
        agentId: "agent_one",
        subject: "device:0001",
        name: "build-box-01",
        issuerName: "Speakeasy devices",
        createdAt: new Date(),
      } as WorkloadAdmission,
    ];
    wrap(<AgentBootstrapTokens agent={agent} />);
    fireEvent.click(
      screen.getByRole("button", { name: "Remove build-box-01" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/loses access as Box Agent/)).toBeTruthy();
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Remove device" }),
    );
    await waitFor(() =>
      expect(mocks.withdraw).toHaveBeenCalledWith({
        request: { id: "adm_device" },
      }),
    );
  });

  it("says so instead of listing anything without permission to read", () => {
    mocks.canRead = false;
    wrap(<AgentBootstrapTokens agent={agent} />);
    expect(screen.queryByText("Bootstrap tokens")).toBeNull();
  });
});
