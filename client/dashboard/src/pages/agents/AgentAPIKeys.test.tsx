import { queryKeyRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { queryKeyRemoteSessions } from "@gram/client/react-query/remoteSessions.js";
import { queryKeyRemoteSessionsListBindings } from "@gram/client/react-query/remoteSessionsListBindings.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { useEffect, useState } from "react";
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
  useMutation,
  useQuery,
} from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { AgentAPIKeys } from "./AgentAPIKeys";
import { validateAgentAPIKeyName } from "./agent-api-key-grants";

const mocks = vi.hoisted(() => ({
  serverScoping: false,
  selectedServerId: "server_one",
  selectedIssuerId: undefined as string | undefined,
  list: vi.fn(),
  create: vi.fn(),
  revoke: vi.fn(),
  listDelegableGrants: vi.fn(),
  toolsets: vi.fn(),
  mcpServers: vi.fn(),
  toolMetadata: vi.fn(),
  flag: "enabled",
  org: "org_example",
  user: "user_example",
  projects: [{ id: "project_one", name: "Project one", slug: "project-one" }],
}));
// Legacy selector/mutation regression cases deliberately isolate the generic
// grant editor. MCP-centered integration cases below enable the real scoping
// adapter; its complete narrowing matrix also has dedicated unit tests.
vi.mock("./agent-key-server-grants", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("./agent-key-server-grants")>();
  return {
    narrowGrantsToServers: (
      ...args: Parameters<typeof actual.narrowGrantsToServers>
    ) =>
      mocks.serverScoping ? actual.narrowGrantsToServers(...args) : args[0],
  };
});
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: mocks.org, projects: mocks.projects }),
  useSession: () => ({ user: { id: mocks.user } }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({ agents: mocks }) }));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: mocks.flag }),
}));
vi.mock("@gram/client/react-query/listAPIKeys", () => ({
  useListAPIKeys: (request: unknown, security: unknown, options: object) =>
    useQuery({
      queryKey: ["@gram/client", "keys", "list", request],
      queryFn: () => mocks.list(request, security),
      ...options,
    }),
}));
vi.mock("@gram/client/react-query/createAPIKey", () => ({
  useCreateAPIKeyMutation: (options: object) =>
    useMutation({ mutationFn: mocks.create, ...options }),
}));
vi.mock("@gram/client/react-query/revokeAPIKey", () => ({
  useRevokeAPIKeyMutation: (options: object) =>
    useMutation({ mutationFn: mocks.revoke, ...options }),
}));
vi.mock("@gram/client/react-query/listToolsetsForOrg.js", () => ({
  useListToolsetsForOrg: (_r: unknown, _s: unknown, options: object) =>
    useQuery({
      queryKey: ["org-toolsets"],
      queryFn: () => mocks.toolsets(),
      ...options,
    }),
}));
vi.mock("@gram/client/react-query/listMcpServersForOrg.js", () => ({
  useListMcpServersForOrg: (_r: unknown, _s: unknown, options: object) =>
    useQuery({
      queryKey: ["org-mcp-servers"],
      queryFn: () => mocks.mcpServers(),
      ...options,
    }),
}));
vi.mock("@gram/client/react-query/listMcpServerToolMetadata.js", () => ({
  useListMcpServerToolMetadata: (
    request: { mcpServerId: string; gramProject?: string },
    _s: unknown,
    options: object,
  ) =>
    useQuery({
      queryKey: ["mcp-server-tool-metadata", request],
      queryFn: () => mocks.toolMetadata(request),
      ...options,
    }),
}));
// Account/server transport is covered separately. Keep these tests focused on
// the existing delegation ceiling, mutations, and credential lifetime.
vi.mock("./AgentKeyServers", () => ({
  AgentKeyServers: ({
    step,
    onChange,
    onReady,
    onInventory,
    discoveryComplete,
    discoveryError,
    grants,
  }: {
    onInventory: (servers: unknown[]) => void;
    discoveryComplete: boolean;
    discoveryError: boolean;
    grants: unknown[];
    step: number;
    onChange: (servers: unknown[]) => void;
    onReady: (ready: boolean) => void;
  }) => {
    useEffect(() => {
      onInventory([
        {
          id: mocks.selectedServerId,
          resourceId: mocks.selectedServerId,
          projectId: "project_one",
          projectSlug: "project-one",
          kind: "Toolset",
          name: "Server one",
        },
      ]);
    }, [onInventory]);
    useEffect(() => {
      onReady(true);
    }, [step, onReady]);
    return (
      <button
        data-discovery-state={
          discoveryComplete
            ? grants.length
              ? "ready"
              : "empty"
            : discoveryError
              ? "error"
              : "loading"
        }
        onClick={() =>
          onChange([
            {
              id: mocks.selectedServerId,
              resourceId: mocks.selectedServerId,
              issuerId: mocks.selectedIssuerId,
              name: "Server one",
              projectId: "project_one",
              projectSlug: "project-one",
              kind: "Toolset",
              endpoints: ["https://example.test/mcp/server-one"],
            },
          ])
        }
      >
        Choose fixture server
      </button>
    );
  },
}));
function KeyPage({ agent }: { agent: ManagedAgent }) {
  const [creation, setCreation] = useState(false);
  const [busy, setBusy] = useState(false);
  return (
    <>
      {creation && (
        <button disabled={busy} onClick={() => setCreation(false)}>
          Back to agent
        </button>
      )}
      <AgentAPIKeys
        agent={agent}
        creation={creation}
        onCreate={() => setCreation(true)}
        onDone={() => setCreation(false)}
        onBusy={setBusy}
      />
    </>
  );
}
const agent: ManagedAgent = {
  id: "agent_example",
  name: "Example",
  ownerUserId: "user_example",
  lifecycle: "active",
  permissions: { read: true, write: false, authorize: true, transfer: false },
  createdAt: new Date(),
  updatedAt: new Date(),
};
const key = {
  id: "key_example",
  name: "Example key",
  keyPrefix: "gram_example",
  expiresAt: new Date("2099-01-01"),
};
const grant = {
  effect: "allow",
  scope: "mcp:connect",
  selector: { resourceKind: "mcp", resourceId: "server_one" },
};
const creationGrant = {
  ...grant,
  selector: {
    resourceKind: "mcp",
    resourceId: "server_one",
    projectId: "project_one",
    tool: "search",
  },
};
function setup(current = agent) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = (value: ManagedAgent) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <KeyPage
          key={`${mocks.org}:${mocks.user}:${value.id}:${value.permissions.authorize}`}
          agent={value}
        />
      </TooltipProvider>
    </QueryClientProvider>
  );
  const result = render(view(current));
  return {
    ...result,
    client,
    change: (value = current) => result.rerender(view(value)),
  };
}
async function beginCreate() {
  fireEvent.click(await screen.findByRole("button", { name: "Issue a key" }));
  if (!screen.queryByRole("button", { name: "Choose fixture server" })) return;
  fireEvent.click(
    screen.getByRole("button", { name: "Choose fixture server" }),
  );
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Continue" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false),
  );
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Continue" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false),
  );
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
}
function submitKey() {
  const review = screen.queryByRole("button", { name: "Review key" });
  if (review) fireEvent.click(review);
  const create = screen.queryByRole("button", { name: "Create key" });
  if (create) fireEvent.click(create);
}
async function openCreate() {
  await beginCreate();
  fireEvent.change(screen.getByLabelText("Key name"), {
    target: { value: "New key" },
  });
  await waitFor(() =>
    expect(screen.queryByText("Loading delegable permissions…")).toBeNull(),
  );
}
async function createWithGrant() {
  await openCreate();
  fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
  submitKey();
}
beforeEach(() => {
  vi.clearAllMocks();
  HTMLElement.prototype.scrollIntoView = vi.fn<() => void>();
  HTMLElement.prototype.hasPointerCapture = vi.fn(() => false);
  HTMLElement.prototype.releasePointerCapture =
    vi.fn<(pointerId: number) => void>();
  mocks.serverScoping = false;
  mocks.selectedServerId = "server_one";
  mocks.selectedIssuerId = undefined;
  mocks.flag = "enabled";
  mocks.org = "org_example";
  mocks.user = "user_example";
  mocks.projects = [
    { id: "project_one", name: "Project one", slug: "project-one" },
  ];
  mocks.list.mockResolvedValue({ keys: [key] });
  mocks.create.mockResolvedValue({ ...key, key: "secret_example_once" });
  mocks.revoke.mockResolvedValue(undefined);
  mocks.listDelegableGrants.mockResolvedValue([creationGrant]);
  mocks.toolsets.mockResolvedValue({
    toolsets: [
      {
        id: "server_one",
        name: "Server one",
        slug: "server-one",
        projectId: "project_one",
        mcpSlug: "server-one",
        defaultEnvironmentSlug: "default",
        tools: [{ id: "tool_one", name: "search", type: "http" }],
      },
    ],
  });
  mocks.mcpServers.mockResolvedValue({ mcpServers: [] });
  mocks.toolMetadata.mockResolvedValue({
    tools: [{ toolName: "remote_search", readOnlyHint: true }],
  });
});
afterEach(cleanup);

describe("Agent API keys", () => {
  it("reviews a confirmed zero-client issuer without claiming account identity is unavailable", async () => {
    mocks.selectedIssuerId = "issuer_example";
    const { client } = setup();
    client.setQueryData(
      [
        ...queryKeyRemoteSessionClients({
          gramProject: "project-one",
          userSessionIssuerId: "issuer_example",
        }),
        { organizationId: mocks.org, userId: mocks.user },
        "all-pages",
      ],
      [],
    );
    await openCreate();
    fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
    fireEvent.click(screen.getByRole("button", { name: "Review key" }));
    expect(
      await screen.findByText("No connected account required."),
    ).toBeTruthy();
    expect(screen.queryByText(/Account identity unavailable/)).toBeNull();
  });
  it.each([true, false])(
    "only carries a bound canonical upstream identity into review (canonical: %s)",
    async (canonical) => {
      mocks.selectedIssuerId = "issuer_example";
      const { client } = setup();
      const request = {
        gramProject: "project-one",
        principalId: agent.id,
        userSessionIssuerId: "issuer_example",
      };
      const ownerScope = { organizationId: mocks.org, userId: mocks.user };
      client.setQueryData(
        [...queryKeyRemoteSessions(request), ownerScope, "all-pages"],
        [
          {
            id: "session_example",
            remoteSessionClientId: "client_example",
            scopes: [],
            upstreamDisplayName: "Example upstream user",
            upstreamEmail: "upstream@example.test",
            subjectEmail: "gram@example.test",
          },
        ],
      );
      client.setQueryData(
        [...queryKeyRemoteSessionsListBindings(request), ownerScope],
        {
          items: [
            {
              remoteSessionId: "session_example",
              remoteSession: canonical
                ? {
                    upstreamDisplayName: "Example upstream user",
                    upstreamEmail: "upstream@example.test",
                  }
                : undefined,
            },
          ],
        },
      );
      await openCreate();
      fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
      fireEvent.click(screen.getByRole("button", { name: "Review key" }));
      if (canonical) {
        expect(
          await screen.findByText(
            "Example upstream user · upstream@example.test",
          ),
        ).toBeTruthy();
      } else {
        expect(
          screen.queryByText("Example upstream user · upstream@example.test"),
        ).toBeNull();
        expect(screen.getByText(/Account identity unavailable/)).toBeTruthy();
      }
      expect(screen.queryByText(/gram@example.test/)).toBeNull();
      expect(mocks.create).not.toHaveBeenCalled();
    },
  );

  it("reviews the key and selected server before creating a credential", async () => {
    setup();
    await openCreate();
    fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
    fireEvent.click(screen.getByRole("button", { name: "Review key" }));
    expect(mocks.create).not.toHaveBeenCalled();
    expect(
      screen.getByRole("heading", { name: "Review your key" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("region", { name: "Key review" }).textContent,
    ).toContain("Server one");
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(mocks.create).toHaveBeenCalledTimes(1);
  });
  it("does not offer creation again when success omits the secret", async () => {
    mocks.create.mockResolvedValue(key);
    setup();
    await createWithGrant();
    await screen.findByText(
      /The key was created but its secret was not returned/,
    );
    expect(screen.queryByRole("button", { name: "Create key" })).toBeNull();
    // Without a secret there is nothing to copy, so the key row is absent
    // rather than present and inert. The connect snippets still render, with a
    // placeholder where the key goes.
    expect(screen.queryByText("API key — shown once")).toBeNull();
    expect(screen.getByText("Connect your agent")).toBeTruthy();
    expect(mocks.create).toHaveBeenCalledTimes(1);
  });

  it("offers the gateway endpoint and the key once issuance succeeds", async () => {
    mocks.create.mockResolvedValue({ ...key, key: "secret_example_once" });
    setup();
    await createWithGrant();

    await screen.findByText("secret_example_once");
    expect(screen.getByText("API key — shown once")).toBeTruthy();
    // One endpoint for the whole agent: the snippet must carry the gateway
    // path, not a per-server URL.
    // Every recipe stays mounted, so the endpoint appears in each of them.
    expect(
      screen.getAllByText(new RegExp(`/agent-mcp/${agent.id}`)).length,
    ).toBeGreaterThan(0);
    // The claim this step makes is that one copy connects a runtime, so the
    // key has to be in the snippet — not merely named by an environment
    // variable nothing sets.
    expect(
      screen.getByText(/Authorization: Bearer secret_example_once/),
    ).toBeTruthy();

    // The env-var recipes name a variable, so they must also set it. Without
    // the export line a copied recipe points at nothing.
    expect(
      screen.getAllByText(/export GRAM_AGENT_KEY='secret_example_once'/).length,
    ).toBeGreaterThan(0);
  });

  it("requires authorize to list or issue credentials", () => {
    setup({
      ...agent,
      permissions: { ...agent.permissions, authorize: false },
    });
    expect(mocks.list).not.toHaveBeenCalled();
    expect(screen.getByText(/do not have permission/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Issue a key" })).toBeNull();
  });
  it.each(["suspended", "revoked"] as const)(
    "keeps list and revoke usable for %s agents but blocks issuance",
    async (lifecycle) => {
      setup({ ...agent, lifecycle });
      expect(await screen.findByText("Example key")).toBeTruthy();
      expect(
        (
          screen.getByRole("button", {
            name: "Issue a key",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(true);
      expect(
        screen.getByRole("button", { name: /^Revoke API key/ }),
      ).toBeTruthy();
    },
  );
  it("blocks issuance while owner reassignment is required", async () => {
    setup({ ...agent, ownerReassignmentRequiredAt: new Date() });
    expect(await screen.findByText("Example key")).toBeTruthy();
    expect(
      (
        screen.getByRole("button", {
          name: "Issue a key",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
  it("lists by agent and confirms revocation by credential id", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: /^Revoke API key/ }),
    );
    expect(mocks.revoke).not.toHaveBeenCalled();
    mocks.list.mockResolvedValue({ keys: [] });
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    await waitFor(() =>
      expect(mocks.revoke).toHaveBeenCalledWith(
        { security: { sessionHeaderGramSession: "" }, request: { id: key.id } },
        expect.anything(),
      ),
    );
    expect(await screen.findByText(/No key issued yet/)).toBeTruthy();
    expect(mocks.list).toHaveBeenCalledWith(
      { agentId: agent.id },
      { sessionHeaderGramSession: "" },
    );
  });
  it("blocks header navigation during submission and the deferred post-issue refetch", async () => {
    let finishCreate!: (value: typeof key & { key: string }) => void;
    let finishRefetch!: (value: { keys: (typeof key)[] }) => void;
    mocks.create.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishCreate = resolve;
        }),
    );
    setup();
    await openCreate();
    mocks.list.mockImplementation(
      () =>
        new Promise((resolve) => {
          finishRefetch = resolve;
        }),
    );
    fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
    submitKey();
    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
    expect(
      screen.getByRole("button", { name: "Back to agent" }),
    ).toHaveProperty("disabled", true);
    fireEvent.click(screen.getByRole("button", { name: "Back to agent" }));
    expect(screen.getByRole("button", { name: "Creating…" })).toBeTruthy();
    finishCreate({ ...key, key: "secret_example_once" });
    await screen.findByText("secret_example_once");
    await waitFor(() => expect(finishRefetch).toBeTypeOf("function"));
    expect(
      screen.getByRole("button", { name: "Back to agent" }),
    ).toHaveProperty("disabled", true);
    fireEvent.click(screen.getByRole("button", { name: "Back to agent" }));
    expect(screen.getByText("secret_example_once")).toBeTruthy();
    finishRefetch({ keys: [key] });
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Back to agent" }),
      ).toHaveProperty("disabled", false),
    );
    expect(screen.getByText("secret_example_once")).toBeTruthy();
  });
  it("reveals secrets once without copying them to query cache or storage", async () => {
    const storage = vi.spyOn(Storage.prototype, "setItem");
    const { client } = setup();
    await createWithGrant();
    await screen.findByText("secret_example_once");
    expect(
      JSON.stringify(
        client
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain("secret_example_once");
    await waitFor(() =>
      expect(client.getMutationCache().getAll()).toHaveLength(0),
    );
    expect(storage).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText("secret_example_once")).toBeNull();
    await openCreate();
    expect(screen.queryByText("secret_example_once")).toBeNull();
    storage.mockRestore();
  });
  it.each(["agent", "organization"])(
    "clears a revealed secret on %s change",
    async (kind) => {
      const { change } = setup();
      await createWithGrant();
      await screen.findByText("secret_example_once");
      if (kind === "organization") mocks.org = "org_other";
      change(kind === "agent" ? { ...agent, id: "agent_other" } : agent);
      expect(screen.queryByText("secret_example_once")).toBeNull();
    },
  );
  it("does not reveal an in-flight creation after switching agents", async () => {
    let resolve!: (value: unknown) => void;
    mocks.create.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const { change } = setup();
    await createWithGrant();
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());
    change({ ...agent, id: "agent_other" });
    resolve({ ...key, key: "secret_example_once" });
    await screen.findByText("Example key");
    expect(screen.queryByText("secret_example_once")).toBeNull();
  });
  it("closes creation and clears the secret on rollout loss", async () => {
    const { change } = setup();
    await createWithGrant();
    await screen.findByText("secret_example_once");
    mocks.flag = "disabled";
    change();
    expect(screen.queryByText("secret_example_once")).toBeNull();
    mocks.flag = "enabled";
    change();
    await openCreate();
    expect(screen.queryByText("secret_example_once")).toBeNull();
  });
  it("does not reveal an in-flight creation after rollout loss", async () => {
    let resolve!: (value: unknown) => void;
    mocks.create.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const { change } = setup();
    await createWithGrant();
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());
    mocks.flag = "disabled";
    change();
    mocks.list.mockClear();
    resolve({ ...key, key: "secret_example_once" });
    await waitFor(() =>
      expect(screen.queryByText("secret_example_once")).toBeNull(),
    );
    expect(mocks.list).not.toHaveBeenCalled();
    mocks.flag = "enabled";
    change();
    await openCreate();
    expect(screen.queryByText("secret_example_once")).toBeNull();
  });
  it.each(["agent", "organization", "permission"])(
    "clears known keys and confirmation on %s change while disabled",
    async (kind) => {
      const { change } = setup();
      fireEvent.click(
        await screen.findByRole("button", { name: /^Revoke API key/ }),
      );
      mocks.flag = "disabled";
      change();
      if (kind === "organization") mocks.org = "org_other";
      change(
        kind === "agent"
          ? { ...agent, id: "agent_other" }
          : kind === "permission"
            ? {
                ...agent,
                permissions: { ...agent.permissions, authorize: false },
              }
            : agent,
      );
      expect(screen.queryByText("Example key")).toBeNull();
      expect(
        screen.queryByRole("button", { name: "Confirm revoke" }),
      ).toBeNull();
    },
  );
  it("loads candidates only once creation opens, with nothing selected by default", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "server_one" },
      },
    ]);
    setup();
    expect(mocks.listDelegableGrants).not.toHaveBeenCalled();
    await openCreate();
    const checkbox = await screen.findByRole("checkbox", {
      name: /Use tools/,
    });
    expect(checkbox.getAttribute("aria-checked")).toBe("false");
    expect(screen.getByRole("button", { name: "Review key" })).toHaveProperty(
      "disabled",
      true,
    );
    fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
    expect(mocks.create).not.toHaveBeenCalled();
    fireEvent.click(checkbox);
    fireEvent.click(checkbox);
    expect(screen.getByRole("button", { name: "Review key" })).toHaveProperty(
      "disabled",
      true,
    );
    fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
    expect(mocks.create).not.toHaveBeenCalled();
    fireEvent.click(checkbox);
    submitKey();
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "server_one" },
      },
    ]);
  });
  it.each(["disabled", "missing", "error", "loading"])(
    "does not expose credential UI or discovery when flag is %s",
    async (status) => {
      mocks.flag = status;
      setup();
      expect(screen.queryByRole("button", { name: "Issue a key" })).toBeNull();
      expect(mocks.listDelegableGrants).not.toHaveBeenCalled();
      expect(mocks.list).not.toHaveBeenCalled();
    },
  );
  it.each([404, 500])(
    "shows list failure %s rather than empty",
    async (statusCode) => {
      mocks.list.mockRejectedValue({ statusCode });
      setup();
      expect(await screen.findByRole("alert")).toHaveProperty(
        "textContent",
        expect.stringContaining(
          statusCode === 404 ? "unavailable" : "Could not load",
        ),
      );
      expect(screen.queryByText(/No key issued yet/)).toBeNull();
    },
  );
  it("explains issuance validation failures without displaying server secrets", async () => {
    mocks.create.mockRejectedValue(new Error("secret_server_error"));
    setup();
    await createWithGrant();
    expect(
      await screen.findByText(
        /Could not create API key.*owner's live permissions/,
      ),
    ).toBeTruthy();
    expect(screen.queryByText("secret_server_error")).toBeNull();
  });
  it("reports revoke failures and keeps confirmation available", async () => {
    mocks.revoke.mockRejectedValue(new Error("failed"));
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: /^Revoke API key/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "Could not revoke API key. Try again.",
    );
  });
  it("submits delegated grants without a project binding or legacy scope", async () => {
    mocks.listDelegableGrants.mockResolvedValue([{ ...grant }]);
    setup();
    await openCreate();
    fireEvent.click(await screen.findByRole("checkbox", { name: /Use tools/ }));
    submitKey();
    await screen.findByText("secret_example_once");
    const payload = mocks.create.mock.calls[0]?.[0].request.createKeyForm;
    expect(payload).not.toHaveProperty("projectId");
    expect(payload.scopes).toEqual([]);
    expect(payload.requestedGrants).toEqual([grant]);
  });
  it.each([
    " plugins-example ",
    "\u0085litellm-example\u0085",
    "😀".repeat(256),
  ])("rejects invalid key name before mutation: %s", async (name) => {
    setup();
    await openCreate();
    fireEvent.change(screen.getByLabelText("Key name"), {
      target: { value: name },
    });
    fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
    submitKey();
    expect(screen.getByRole("alert").textContent).toMatch(
      /reserved|255 Unicode characters/,
    );
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("submits a trimmed name at the Unicode codepoint limit", async () => {
    setup();
    await openCreate();
    fireEvent.change(screen.getByLabelText("Key name"), {
      target: { value: `  ${"😀".repeat(255)}  ` },
    });
    fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
    submitKey();
    await screen.findByText("secret_example_once");
    expect(mocks.create.mock.calls[0]?.[0].request.createKeyForm.name).toBe(
      "😀".repeat(255),
    );
  });
  it("mirrors case-sensitive reserved prefixes and Go whitespace trimming", () => {
    expect(validateAgentAPIKeyName(" Plugins-example ")).toBe(
      "Plugins-example",
    );
    expect(validateAgentAPIKeyName("litellm")).toBe("litellm");
    expect(validateAgentAPIKeyName("\ufeffexample")).toBe("\ufeffexample");
    expect(() => validateAgentAPIKeyName("\u0085 ")).toThrow(
      "Enter a key name",
    );
  });
  it.each([
    ["missing", "", "Choose a valid expiration date."],
    ["invalid", "not-a-date", "Choose a valid expiration date."],
    ["past", "2000-01-01", "Expiration date must be in the future."],
    [
      "out-of-range",
      "2999-01-01",
      "Expiration date must be within 365 days minus a 5-minute clock-skew margin.",
    ],
  ])(
    "blocks %s custom expiry and explains the reason",
    async (_label, value, reason) => {
      setup();
      await openCreate();
      fireEvent.keyDown(screen.getByLabelText("Expiration"), { key: "Enter" });
      fireEvent.click(
        await screen.findByRole("option", { name: "Custom date" }),
      );
      fireEvent.change(screen.getByLabelText("Expiration date"), {
        target: { value },
      });
      const button = screen.getByRole("button", { name: "Review key" });
      expect((button as HTMLButtonElement).disabled).toBe(true);
      expect(button.getAttribute("title")).toContain(reason);
      fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
      expect(mocks.create).not.toHaveBeenCalled();
      // An invalid custom date must not prevent switching back to a preset.
      fireEvent.click(
        await screen.findByRole("checkbox", { name: /mcp:connect/ }),
      );
      fireEvent.keyDown(screen.getByLabelText("Expiration"), { key: "Enter" });
      fireEvent.click(await screen.findByRole("option", { name: "90 days" }));
      expect((button as HTMLButtonElement).disabled).toBe(false);
    },
  );
  it("drops a retained selection and blocks issuance after a discovery refetch failure", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        ...grant,
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ]);
    const { client } = setup();
    await openCreate();
    fireEvent.click(await screen.findByRole("checkbox", { name: /Use tools/ }));
    mocks.listDelegableGrants.mockRejectedValue(new Error("refetch failed"));
    await client.invalidateQueries({ queryKey: ["agent-delegable-grants"] });
    await screen.findByText(/Permissions could not be loaded/);
    expect(
      client.getQueryData([
        "agent-delegable-grants",
        mocks.org,
        mocks.user,
        agent.id,
        [{ projectId: "project_one", toolsetId: "server_one" }],
        agent.updatedAt,
        agent.ownerUserId,
      ]),
    ).toBeTruthy();
    expect(screen.queryByRole("checkbox", { name: /Use tools/ })).toBeNull();
    submitKey();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Review key" })).toHaveProperty(
      "disabled",
      true,
    );
    fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("discovers separately for each authorizer and resets the dialog on user change", async () => {
    mocks.listDelegableGrants.mockResolvedValue([grant]);
    const { change, client } = setup();
    await openCreate();
    fireEvent.click(await screen.findByRole("checkbox", { name: /Use tools/ }));
    mocks.user = "user_other";
    change();
    // The dialog belonged to the previous authorizer; nothing of it survives.
    expect(screen.queryByLabelText("Key name")).toBeNull();
    expect(
      client.getQueryData([
        "agent-delegable-grants",
        mocks.org,
        "user_other",
        agent.id,
        [{ projectId: "project_one", toolsetId: "server_one" }],
        agent.updatedAt,
        agent.ownerUserId,
      ]),
    ).toBeUndefined();
    await openCreate();
    const reopened = await screen.findByRole("checkbox", {
      name: /Use tools/,
    });
    expect(reopened.getAttribute("aria-checked")).toBe("false");
  });
  it("shows a future expiry as an absolute date, never as elapsed time", async () => {
    const expiresAt = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000);
    mocks.list.mockResolvedValue({ keys: [{ ...key, expiresAt }] });
    setup();
    await screen.findByText(key.name);
    const rendered = document.querySelector(
      `time[datetime="${expiresAt.toISOString()}"]`,
    );
    expect(rendered).toBeTruthy();
    // A day-precision label, and never an elapsed-time one: a 90-day key read
    // "3 months ago" when this column was relative.
    expect(rendered?.textContent).toContain(String(expiresAt.getFullYear()));
    expect(screen.queryByText(/ago/)).toBeNull();
  });
  it("shows loading separately from an empty list", () => {
    mocks.list.mockImplementation(() => new Promise(() => {}));
    setup();
    expect(screen.getByText("Loading API keys…")).toBeTruthy();
    expect(screen.queryByText(/No key issued yet/)).toBeNull();
  });
  it("requests inventory-scoped candidates and issues only selected MCP connect grants", async () => {
    mocks.serverScoping = true;
    mocks.listDelegableGrants.mockResolvedValue([
      creationGrant,
      { ...creationGrant, scope: "mcp:read" },
      {
        ...creationGrant,
        selector: { ...creationGrant.selector, resourceId: "other" },
      },
    ]);
    setup();
    await openCreate();
    expect(mocks.listDelegableGrants).toHaveBeenCalledWith(
      {
        agentId: agent.id,
        toolsetId: "server_one",
      },
      undefined,
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(screen.getAllByRole("checkbox")).toHaveLength(1);
    fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
    submitKey();
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([creationGrant]);
  });
  it("withdraws reviewed permissions during refetch and requires reselection", async () => {
    const { client } = setup();
    await openCreate();
    fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
    fireEvent.click(screen.getByRole("button", { name: "Review key" }));
    let resolve!: (grants: unknown[]) => void;
    mocks.listDelegableGrants.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    void client.invalidateQueries({ queryKey: ["agent-delegable-grants"] });
    await screen.findByText("Loading delegable permissions…");
    expect(screen.queryByRole("button", { name: "Create key" })).toBeNull();
    resolve([creationGrant]);
    const permission = await screen.findByRole("checkbox", {
      name: /Use tools/,
    });
    expect(permission).toHaveProperty("checked", false);
    expect(screen.getByRole("button", { name: "Review key" })).toHaveProperty(
      "disabled",
      true,
    );
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("clears review and refetches when returning to permissions", async () => {
    setup();
    await openCreate();
    fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
    fireEvent.click(screen.getByRole("button", { name: "Review key" }));
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    await waitFor(() =>
      expect(mocks.listDelegableGrants).toHaveBeenCalledTimes(2),
    );
    expect(
      await screen.findByRole("checkbox", { name: /Use tools/ }),
    ).toHaveProperty("checked", false);
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it.each(["loading", "error", "empty"])(
    "fails closed on %s discovery",
    async (state) => {
      if (state === "loading")
        mocks.listDelegableGrants.mockImplementation(
          () => new Promise(() => {}),
        );
      if (state === "error")
        mocks.listDelegableGrants.mockRejectedValue(new Error("unavailable"));
      if (state === "empty") mocks.listDelegableGrants.mockResolvedValue([]);
      setup();
      fireEvent.click(
        await screen.findByRole("button", { name: "Issue a key" }),
      );
      fireEvent.click(
        screen.getByRole("button", { name: "Choose fixture server" }),
      );
      await waitFor(() => expect(mocks.listDelegableGrants).toHaveBeenCalled());
      await waitFor(() =>
        expect(
          screen
            .getByRole("button", { name: "Choose fixture server" })
            .getAttribute("data-discovery-state"),
        ).toBe(state),
      );
      expect(screen.queryByRole("checkbox", { name: /Use tools/ })).toBeNull();
      expect(mocks.create).not.toHaveBeenCalled();
      expect(screen.getByRole("button", { name: "Continue" })).toHaveProperty(
        "disabled",
        true,
      );
    },
  );
  it("hides cached key rows and dialogs on rollout loss", async () => {
    const { change } = setup();
    await screen.findByText("Example key");
    mocks.flag = "disabled";
    change();
    expect(screen.queryByText("Example key")).toBeNull();
    expect(screen.queryByRole("button", { name: "Issue a key" })).toBeNull();
    expect(mocks.listDelegableGrants).not.toHaveBeenCalled();
  });
  it.each([403, 500])(
    "withdraws reviewed permissions and refetches after issuance fails with %s",
    async (statusCode) => {
      mocks.create.mockRejectedValue(
        Object.assign(new Error("issuance failed"), { statusCode }),
      );
      setup();
      await openCreate();
      fireEvent.click(screen.getByRole("checkbox", { name: /Use tools/ }));
      fireEvent.click(screen.getByRole("button", { name: "Review key" }));
      let resolve!: (grants: unknown[]) => void;
      mocks.listDelegableGrants.mockImplementation(
        () =>
          new Promise((done) => {
            resolve = done;
          }),
      );
      fireEvent.click(screen.getByRole("button", { name: "Create key" }));
      await screen.findByText(/Could not create API key/);
      expect(mocks.listDelegableGrants).toHaveBeenCalledTimes(2);
      expect(screen.queryByRole("button", { name: "Create key" })).toBeNull();
      expect(screen.queryByRole("checkbox", { name: /Use tools/ })).toBeNull();
      expect(screen.getByRole("button", { name: "Review key" })).toHaveProperty(
        "disabled",
        true,
      );
      resolve(statusCode === 403 ? [] : [creationGrant]);
      await waitFor(() =>
        expect(screen.queryByText("Loading delegable permissions…")).toBeNull(),
      );
      if (statusCode === 500)
        expect(
          screen.getByRole("checkbox", { name: /Use tools/ }),
        ).toHaveProperty("checked", false);
      else
        expect(
          screen.queryByRole("checkbox", { name: /Use tools/ }),
        ).toBeNull();
      expect(screen.getByRole("button", { name: "Review key" })).toHaveProperty(
        "disabled",
        true,
      );
      submitKey();
      expect(mocks.create).toHaveBeenCalledTimes(1);
    },
  );
});
