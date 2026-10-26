import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { invalidateAllWorkloadDeviceJoinTokens } from "@gram/client/react-query/workloadDeviceJoinTokens.js";
import { invalidateAllWorkloadIdentities } from "@gram/client/react-query/workloadIdentities.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type { WorkloadDeviceJoinTokenSummary } from "@gram/client/models/components/workloaddevicejointokensummary.js";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CredentialPicker } from "../credentials/CredentialPicker";
import { AgentBootstrapTokens } from "./AgentBootstrapTokens";
import { DeviceSetupPanel, ManualSetup } from "./DeviceSetupPanel";
import { InstallCommandUnavailable } from "./device-install-command";
import { DeviceVerify } from "./DeviceVerify";

const mocks = vi.hoisted(() => ({
  tokens: [] as WorkloadDeviceJoinTokenSummary[],
  admissions: [] as WorkloadAdmission[],
  createToken: vi.fn(),
  revoke: vi.fn(),
  withdraw: vi.fn(),
  canRead: true,
  canWrite: true,
  tokensError: false,
  refetchTokens: vi.fn(),
  membersReady: true,
  serverURL: "https://app.getgram.ai",
  installFlag: "missing",
  createInstallCommand: vi.fn(),
}));

vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueries: () => [
    {
      data: { result: { items: mocks.tokens } },
      isError: mocks.tokensError,
      refetch: mocks.refetchTokens,
    },
  ],
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
  getServerURL: () => mocks.serverURL,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAllScopes: () => mocks.canRead,
    hasAnyScope: (scopes: string[]) =>
      scopes.some((scope) =>
        scope === "workload:write" ? mocks.canWrite : mocks.canRead,
      ),
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
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: mocks.installFlag }),
}));
vi.mock(
  "@gram/client/react-query/createWorkloadDeviceInstallCommand.js",
  () => ({
    useCreateWorkloadDeviceInstallCommandMutation: () => ({
      mutateAsync: mocks.createInstallCommand,
    }),
  }),
);
vi.mock("@gram/client/react-query/workloadDeviceJoinTokens.js", () => ({
  buildWorkloadDeviceJoinTokensQuery: (_client: unknown, request: unknown) => ({
    queryKey: ["tokens", request],
  }),
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
    data: mocks.membersReady
      ? {
          members: [
            { id: "user_other", name: "Pat Example", email: "p@e.test" },
          ],
        }
      : undefined,
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
  mocks.canWrite = true;
  mocks.tokensError = false;
  mocks.membersReady = true;
  mocks.installFlag = "missing";
  mocks.serverURL = "https://app.getgram.ai";
});

describe("the workload picker", () => {
  const picker = (showHeadlessDevice: boolean) =>
    render(
      <CredentialPicker
        entries={[]}
        isPending={false}
        isError={false}
        onRetry={vi.fn<() => void>()}
        credential="workload"
        platformKey={null}
        disabled={false}
        canFederate={showHeadlessDevice}
        onChoosePlatform={vi.fn<(key: string) => void>()}
        onChooseApiKey={vi.fn<() => void>()}
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
        options.onSuccess({
          id: "token_one",
          token: "gram_dj_example",
          expiresAt,
        }),
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
    expect(onTokenCreated).toHaveBeenCalledWith({ id: "token_one", expiresAt });
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

describe("the install command", () => {
  const project = { id: "project_one", name: "Project one", slug: "p1" };

  it("shows the command, and watches the token it carries", async () => {
    const onTokenCreated = vi.fn<(watched: object) => void>();
    const create = vi.fn().mockResolvedValue({
      command:
        '(umask 077; script=$(mktemp) && curl -fsSL https://example.test/v1/install/device/code -o "$script" && sudo sh "$script")',
      downloadCommand:
        '(umask 077; script=$(mktemp) && curl -fsSL https://example.test/v1/install/device/code -o "$script" && mv "$script" ./speakeasy-install.sh && sudo sh ./speakeasy-install.sh)',
      codeExpiresAt: new Date(Date.now() + 15 * 60_000),
      joinTokenId: "token_from_command",
      tokenExpiresAt: new Date(Date.now() + 3600_000),
    });
    wrap(
      <DeviceSetupPanel
        agentId="agent_one"
        hooksProject={project}
        lifetimeSeconds={3600}
        onTokenCreated={onTokenCreated}
        createInstallCommand={create}
      />,
    );
    const block = await screen.findByLabelText("install command");
    await waitFor(() =>
      expect(block.textContent).toContain('&& sudo sh "$script")'),
    );
    expect(block.closest("[data-demo-secret]")).not.toBeNull();
    expect(
      screen.getByLabelText("download, then run").closest("[data-demo-secret]"),
    ).not.toBeNull();
    expect(screen.getByText('sudo sh "$script" --user <account>')).toBeTruthy();
    expect(screen.getByText("Download and run from a file")).toBeTruthy();
    expect(screen.queryByText(/\| sudo sh -s/)).toBeNull();
    expect(create).toHaveBeenCalledTimes(1);
    expect(create).toHaveBeenCalledWith({
      agentId: "agent_one",
      hooksProjectSlug: "p1",
      expiresInSeconds: 3600,
    });
    expect(onTokenCreated).toHaveBeenCalledWith({
      id: "token_from_command",
      expiresAt: expect.any(Date),
    });
    // Manual setup is still there, folded away.
    expect(screen.getByText("Set up manually")).toBeTruthy();
  });

  it("falls back to manual setup when the server does not offer it", async () => {
    const create = vi.fn().mockRejectedValue(new InstallCommandUnavailable());
    wrap(
      <DeviceSetupPanel
        agentId="agent_one"
        hooksProject={project}
        lifetimeSeconds={3600}
        onTokenCreated={vi.fn<(watched: object) => void>()}
        createInstallCommand={create}
      />,
    );
    await waitFor(() =>
      expect(screen.queryByText(/Run it on Linux/)).toBeNull(),
    );
    expect(screen.queryByRole("alert")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Create bootstrap token" }),
    ).toBeTruthy();
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
    expect(
      screen.getByText(/You need permission to view workload identities/),
    ).toBeTruthy();
    expect(screen.queryByText("Bootstrap tokens")).toBeNull();
  });
});

describe("enrollment repair states", () => {
  const project = { id: "project_one", name: "Project one", slug: "p1" };
  const panel = () => (
    <DeviceSetupPanel
      agentId="agent_one"
      hooksProject={project}
      lifetimeSeconds={3600}
      onTokenCreated={vi.fn<(token: object) => void>()}
    />
  );
  const notFound = (message: string) =>
    new GramError(message, {
      response: new Response(null, { status: 404 }),
      request: new Request("https://example.test/rpc"),
      body: JSON.stringify({ message }),
    });

  it("preserves a selected headless tile for a read-only user", () => {
    wrap(
      <CredentialPicker
        entries={[]}
        isPending={false}
        isError={false}
        onRetry={vi.fn<() => void>()}
        credential="workload"
        platformKey="headless-device"
        canFederate={false}
        disabled
        onChoosePlatform={vi.fn<(key: string) => void>()}
        onChooseApiKey={vi.fn<() => void>()}
      />,
    );
    expect(
      screen
        .getByRole("radio", { name: /Headless device/ })
        .getAttribute("data-state"),
    ).toBe("checked");
  });

  it("falls back through the real hook when the feature flag is disabled", () => {
    mocks.installFlag = "disabled";
    wrap(panel());
    expect(
      screen.getByRole("button", { name: "Create bootstrap token" }),
    ).toBeTruthy();
    expect(mocks.createInstallCommand).not.toHaveBeenCalled();
  });

  it("falls back only on the flag-disabled 404", async () => {
    mocks.createInstallCommand.mockRejectedValue(
      notFound(
        "device install commands are not available for this organization",
      ),
    );
    wrap(panel());
    await waitFor(() =>
      expect(screen.queryByText(/Run it on Linux/)).toBeNull(),
    );
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("surfaces a missing-agent 404 instead of silently falling back", async () => {
    mocks.createInstallCommand.mockRejectedValue(notFound("agent not found"));
    wrap(panel());
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Could not prepare the install command",
    );
  });

  it("refuses manual token creation on an insecure control plane", () => {
    mocks.serverURL = "http://example.test";
    wrap(
      <ManualSetup
        agentId="agent_one"
        hooksProject={project}
        lifetimeSeconds={3600}
        onTokenCreated={vi.fn<(token: object) => void>()}
      />,
    );
    expect(screen.getByRole("alert").textContent).toContain("HTTPS");
    expect(mocks.createToken).not.toHaveBeenCalled();
  });

  it("offers a replacement for an expired manual token", () => {
    mocks.createToken.mockImplementation(
      (_vars, options: { onSuccess: (r: object) => void }) =>
        options.onSuccess({
          id: "expired_token",
          token: "example",
          expiresAt: new Date(Date.now() - 1000),
        }),
    );
    wrap(
      <ManualSetup
        agentId="agent_one"
        hooksProject={project}
        lifetimeSeconds={3600}
        onTokenCreated={vi.fn<(token: object) => void>()}
        defaultOpen
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Create bootstrap token" }),
    );
    expect(screen.getByText(/expired/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Create a new token" }));
    expect(mocks.createToken).toHaveBeenCalledTimes(2);
  });

  it("shows and retries a verification failure", () => {
    mocks.tokensError = true;
    wrap(<DeviceVerify agentId="agent_one" watched={[{ id: "token_one" }]} />);
    expect(screen.getByText("Could not check device enrollment")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(mocks.refetchTokens).toHaveBeenCalledTimes(1);
  });

  it("shows a terminal state for an expired token", () => {
    mocks.tokens = [token({ status: "expired" })];
    wrap(<DeviceVerify agentId="agent_one" watched={[{ id: "token_one" }]} />);
    expect(screen.getByText("Bootstrap token no longer active")).toBeTruthy();
    expect(screen.queryByText("Waiting for the box to enroll")).toBeNull();
  });

  it("refreshes tokens and enrolled devices when the issue sheet closes", async () => {
    wrap(<AgentBootstrapTokens agent={agent} />);
    fireEvent.click(
      screen.getByRole("button", { name: "Issue bootstrap token" }),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    expect(invalidateAllWorkloadDeviceJoinTokens).toHaveBeenCalled();
    expect(invalidateAllWorkloadIdentities).toHaveBeenCalled();
  });

  it("does not call an unknown creator a former member", () => {
    mocks.membersReady = false;
    mocks.tokens = [token()];
    wrap(<AgentBootstrapTokens agent={agent} />);
    expect(screen.queryByText("Former member")).toBeNull();
  });

  it("distinguishes a missing enrolled device from an unnamed one", () => {
    mocks.tokens = [
      token({ status: "redeemed", consumedBySubject: "device:removed" }),
    ];
    wrap(<AgentBootstrapTokens agent={agent} />);
    expect(screen.getByText("Device unavailable")).toBeTruthy();
    expect(screen.queryByText("Unnamed device")).toBeNull();
  });
});
