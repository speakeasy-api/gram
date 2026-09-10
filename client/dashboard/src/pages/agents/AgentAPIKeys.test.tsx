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
  selector: { resourceKind: "mcp", resourceId: "example" },
};
function setup(current = agent) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = (value: ManagedAgent) => (
    <QueryClientProvider client={client}>
      <AgentAPIKeys agent={value} />
    </QueryClientProvider>
  );
  const result = render(view(current));
  return {
    ...result,
    client,
    change: (value = current) => result.rerender(view(value)),
  };
}
function optionText(select: HTMLElement): string {
  return Array.from(
    select.querySelectorAll("option"),
    (option) => option.textContent ?? "",
  ).join(" ");
}
const twoProjects = [
  { id: "project_one", name: "Project one", slug: "project-one" },
  { id: "project_two", name: "Project two", slug: "project-two" },
];
const twoServers = [
  {
    id: "server_one",
    name: "Server one",
    slug: "server-one",
    projectId: "project_one",
    tools: [{ id: "tool_one", name: "search", type: "http" }],
  },
  {
    id: "server_two",
    name: "Server two",
    slug: "server-two",
    projectId: "project_two",
    tools: [{ id: "tool_two", name: "lookup", type: "http" }],
  },
];
async function openCreate() {
  fireEvent.click(
    await screen.findByRole("button", { name: "Create API key" }),
  );
  fireEvent.change(screen.getByLabelText("Key name"), {
    target: { value: "New key" },
  });
  await waitFor(() =>
    expect(screen.queryByText("Loading delegable permissions…")).toBeNull(),
  );
}
async function selectedCreate() {
  await openCreate();
  fireEvent.click(await screen.findByRole("checkbox", { name: /mcp:connect/ }));
  fireEvent.click(screen.getByRole("button", { name: "Create key" }));
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.flag = "enabled";
  mocks.org = "org_example";
  mocks.user = "user_example";
  mocks.projects = [
    { id: "project_one", name: "Project one", slug: "project-one" },
  ];
  mocks.list.mockResolvedValue({ keys: [key] });
  mocks.create.mockResolvedValue({ ...key, key: "secret_example_once" });
  mocks.revoke.mockResolvedValue(undefined);
  mocks.listDelegableGrants.mockResolvedValue([grant]);
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
  it("requires authorize to list or issue credentials", () => {
    setup({
      ...agent,
      permissions: { ...agent.permissions, authorize: false },
    });
    expect(mocks.list).not.toHaveBeenCalled();
    expect(screen.getByText(/do not have permission/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Create API key" })).toBeNull();
  });
  it.each(["suspended", "revoked"] as const)(
    "keeps list and revoke usable for %s agents but blocks issuance",
    async (lifecycle) => {
      setup({ ...agent, lifecycle });
      expect(await screen.findByText("Example key")).toBeTruthy();
      expect(
        (
          screen.getByRole("button", {
            name: "Create API key",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(true);
      expect(
        screen.getByRole("button", { name: "Revoke API key" }),
      ).toBeTruthy();
    },
  );
  it("blocks issuance while owner reassignment is required", async () => {
    setup({ ...agent, ownerReassignmentRequiredAt: new Date() });
    expect(await screen.findByText("Example key")).toBeTruthy();
    expect(
      (
        screen.getByRole("button", {
          name: "Create API key",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
  it("lists by agent and confirms revocation by credential id", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke API key" }),
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
    expect(await screen.findByText("No API keys yet")).toBeTruthy();
    expect(mocks.list).toHaveBeenCalledWith(
      { agentId: agent.id },
      { sessionHeaderGramSession: "" },
    );
  });
  it("requires at least one permission without an empty-key bypass", async () => {
    mocks.listDelegableGrants.mockResolvedValue([]);
    setup();
    await openCreate();
    await screen.findByText(/No permissions can be delegated to this agent/);
    expect(
      screen.queryByRole("checkbox", { name: /Create without permissions/ }),
    ).toBeNull();
    expect(
      (screen.getByRole("button", { name: "Create key" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("reveals secrets once without copying them to query cache or storage", async () => {
    const storage = vi.spyOn(Storage.prototype, "setItem");
    const { client } = setup();
    await selectedCreate();
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
      await selectedCreate();
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
    await selectedCreate();
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());
    change({ ...agent, id: "agent_other" });
    resolve({ ...key, key: "secret_example_once" });
    await screen.findByText("Example key");
    expect(screen.queryByText("secret_example_once")).toBeNull();
  });
  it("retains a known key and open revocation when rollout is disabled", async () => {
    const { change, client } = setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke API key" }),
    );
    mocks.flag = "disabled";
    change();
    expect(screen.getByText("Example key")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Confirm revoke" })).toBeTruthy();
    mocks.list.mockClear();
    await client.invalidateQueries();
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    await waitFor(() => expect(screen.queryByText("Example key")).toBeNull());
    expect(mocks.revoke).toHaveBeenCalledWith(
      { security: { sessionHeaderGramSession: "" }, request: { id: key.id } },
      expect.anything(),
    );
    fireEvent.click(screen.getByRole("button", { name: "Create API key" }));
    expect(mocks.list).not.toHaveBeenCalled();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.listDelegableGrants).not.toHaveBeenCalled();
  });
  it("closes creation and clears the secret on rollout loss", async () => {
    const { change } = setup();
    await selectedCreate();
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
    await selectedCreate();
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
        await screen.findByRole("button", { name: "Revoke API key" }),
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
  it("stops discovery refetch and creation on rollout loss", async () => {
    mocks.listDelegableGrants.mockResolvedValue([]);
    const { change, client } = setup();
    await openCreate();
    await screen.findByText(/No permissions can be delegated to this agent/);
    mocks.flag = "disabled";
    change();
    mocks.listDelegableGrants.mockClear();
    mocks.list.mockClear();
    await client.invalidateQueries();
    fireEvent.click(screen.getByRole("button", { name: "Create API key" }));
    expect(screen.queryByLabelText("Key name")).toBeNull();
    expect(mocks.listDelegableGrants).not.toHaveBeenCalled();
    expect(mocks.list).not.toHaveBeenCalled();
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("loads candidates only once creation opens, with nothing selected by default", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "example" },
      },
    ]);
    setup();
    expect(mocks.listDelegableGrants).not.toHaveBeenCalled();
    await openCreate();
    const checkbox = await screen.findByRole("checkbox", {
      name: /mcp:connect/,
    });
    expect((checkbox as HTMLInputElement).checked).toBe(false);
    fireEvent.click(checkbox);
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "example" },
      },
    ]);
  });
  it.each(["disabled", "missing", "error", "loading"])(
    "distinguishes flag %s from empty",
    (flag) => {
      mocks.flag = flag;
      setup();
      expect(mocks.list).not.toHaveBeenCalled();
      expect(screen.queryByText("No API keys yet")).toBeNull();
      expect(
        (
          screen.getByRole("button", {
            name: "Create API key",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(true);
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
      expect(screen.queryByText("No API keys yet")).toBeNull();
    },
  );
  it("explains issuance validation failures without displaying server secrets", async () => {
    mocks.create.mockRejectedValue(new Error("secret_server_error"));
    setup();
    await selectedCreate();
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      expect.stringContaining("owner's live permissions"),
    );
    expect(screen.queryByText("secret_server_error")).toBeNull();
  });
  it("reports revoke failures and keeps confirmation available", async () => {
    mocks.revoke.mockRejectedValue(new Error("failed"));
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke API key" }),
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
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    const payload = mocks.create.mock.calls[0]?.[0].request.createKeyForm;
    expect(payload).not.toHaveProperty("projectId");
    expect(payload.scopes).toEqual([]);
    expect(payload.requestedGrants).toEqual([grant]);
  });
  it("does not treat a failed discovery read as an empty candidate set", async () => {
    mocks.listDelegableGrants.mockRejectedValue(new Error("forbidden"));
    setup();
    await openCreate();
    await screen.findByText(/Delegable permissions could not be loaded/);
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    expect(mocks.create).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Retry permissions" }),
    ).toBeTruthy();
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
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    expect(
      screen.getByLabelText(/reserved|255 Unicode characters/),
    ).toBeTruthy();
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("submits a trimmed name at the Unicode codepoint limit", async () => {
    setup();
    await openCreate();
    fireEvent.change(screen.getByLabelText("Key name"), {
      target: { value: `  ${"😀".repeat(255)}  ` },
    });
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
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
  it("blocks editing and issuance of cached candidates while a refetch is pending", async () => {
    mocks.listDelegableGrants.mockResolvedValue([grant]);
    const { client } = setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    let resolveRefetch!: (value: unknown) => void;
    mocks.listDelegableGrants.mockImplementation(
      () =>
        new Promise((done) => {
          resolveRefetch = done;
        }),
    );
    void client.invalidateQueries({ queryKey: ["agent-delegable-grants"] });
    await screen.findByText("Loading delegable permissions…");
    // The cached candidate is still in the query cache, but the current read
    // has not confirmed it, so it must not be editable or submittable.
    expect(screen.queryByRole("checkbox", { name: /mcp:connect/ })).toBeNull();
    expect(
      (
        screen.getByRole("button", {
          name: "Create key",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
    expect(mocks.create).not.toHaveBeenCalled();
    expect(
      screen.getByLabelText(/Delegable permissions are still loading/),
    ).toBeTruthy();

    resolveRefetch([grant]);
    const restored = await screen.findByRole("checkbox", {
      name: /mcp:connect/,
    });
    expect((restored as HTMLInputElement).checked).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([grant]);
  });
  it("never submits a stale selection the refreshed candidates no longer contain", async () => {
    mocks.listDelegableGrants.mockResolvedValue([grant]);
    const { client } = setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    const narrowed = {
      ...grant,
      scope: "mcp:read",
      selector: { resourceKind: "mcp", resourceId: "server_one" },
    };
    mocks.listDelegableGrants.mockResolvedValue([narrowed]);
    void client.invalidateQueries({ queryKey: ["agent-delegable-grants"] });
    await screen.findByRole("checkbox", { name: /mcp:read/ });
    expect(screen.queryByRole("checkbox", { name: /mcp:connect/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    expect(mocks.create).not.toHaveBeenCalled();
    expect(
      screen.getByLabelText(/Select at least one valid permission/),
    ).toBeTruthy();
    fireEvent.click(await screen.findByRole("checkbox", { name: /mcp:read/ }));
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([narrowed]);
  });
  it("accumulates loading, name and permission reasons on the accessible wrapper", async () => {
    mocks.listDelegableGrants.mockImplementation(() => new Promise(() => {}));
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Create API key" }),
    );
    await screen.findByText("Loading delegable permissions…");
    const wrapper = screen.getByLabelText(
      /Delegable permissions are still loading/,
    );
    expect(wrapper.getAttribute("aria-label")).toMatch(/Enter a key name/);
    expect(wrapper.getAttribute("aria-label")).toMatch(
      /Select at least one valid permission/,
    );
    expect(wrapper.tabIndex).toBe(0);
    fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("explains pending creation and loss of issuance eligibility together", async () => {
    mocks.create.mockImplementation(() => new Promise(() => {}));
    const { change } = setup();
    await selectedCreate();
    await screen.findByRole("button", { name: "Creating…" });
    change({ ...agent, lifecycle: "suspended" });
    const wrapper = screen.getByLabelText(/An API key is being created/);
    expect(wrapper.getAttribute("aria-label")).toMatch(
      /Issuance requires an active agent/,
    );
    fireEvent.focus(wrapper);
    expect((await screen.findByRole("tooltip")).textContent).toMatch(
      /An API key is being created/,
    );
    expect(screen.getByRole("tooltip").textContent).toMatch(
      /Issuance requires an active agent/,
    );
  });
  it("defaults expiry to 90 days", async () => {
    setup();
    const before = Date.now();
    await selectedCreate();
    await screen.findByText("secret_example_once");
    const expiry =
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.expiresAt;
    expect(expiry.getTime()).toBeGreaterThanOrEqual(before + 90 * 86400000);
    expect(expiry.getTime()).toBeLessThanOrEqual(Date.now() + 90 * 86400000);
  });
  it.each([7, 30, 90, 180, 365])(
    "submits a selected %i-day expiry",
    async (days) => {
      setup();
      await openCreate();
      fireEvent.click(
        await screen.findByRole("checkbox", { name: /mcp:connect/ }),
      );
      fireEvent.keyDown(screen.getByLabelText("Expiration"), { key: "Enter" });
      fireEvent.click(
        await screen.findByRole("option", { name: `${days} days` }),
      );
      const before = Date.now();
      fireEvent.click(screen.getByRole("button", { name: "Create key" }));
      await screen.findByText("secret_example_once");
      const expiry =
        mocks.create.mock.calls[0]?.[0].request.createKeyForm.expiresAt;
      expect(expiry.getTime()).toBeGreaterThanOrEqual(before + days * 86400000);
      expect(expiry.getTime()).toBeLessThanOrEqual(
        Date.now() + days * 86400000,
      );
    },
  );
  it("submits a custom expiry at local midnight", async () => {
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    fireEvent.keyDown(screen.getByLabelText("Expiration"), { key: "Enter" });
    fireEvent.click(await screen.findByRole("option", { name: "Custom date" }));
    const date = new Date(Date.now() + 10 * 86_400_000);
    const value = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
    fireEvent.change(screen.getByLabelText("Expiration date"), {
      target: { value },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.expiresAt,
    ).toEqual(new Date(`${value}T00:00:00`));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    await openCreate();
    expect(screen.getByLabelText("Expiration").textContent).toBe("90 days");
    expect(screen.queryByLabelText("Expiration date")).toBeNull();
    fireEvent.keyDown(screen.getByLabelText("Expiration"), { key: "Enter" });
    fireEvent.click(await screen.findByRole("option", { name: "Custom date" }));
    expect(
      (screen.getByLabelText("Expiration date") as HTMLInputElement).value,
    ).toBe("");
  });
  it.each([
    ["missing", "", "Choose a valid expiration date."],
    ["invalid", "not-a-date", "Choose a valid expiration date."],
    ["past", "2000-01-01", "Expiration date must be in the future."],
    ["out-of-range", "2999-01-01", "Expiration date must be within 365 days."],
  ])(
    "blocks %s custom expiry and accumulates tooltip reasons",
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
      const button = screen.getByRole("button", { name: "Create key" });
      expect((button as HTMLButtonElement).disabled).toBe(true);
      const wrapper = button.parentElement!;
      expect(wrapper.getAttribute("aria-label")).toContain(reason);
      expect(wrapper.getAttribute("aria-label")).toContain(
        "Select at least one valid permission.",
      );
      fireEvent.focus(wrapper);
      expect((await screen.findByRole("tooltip")).textContent).toContain(
        reason,
      );
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
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    mocks.listDelegableGrants.mockRejectedValue(new Error("refetch failed"));
    await client.invalidateQueries({ queryKey: ["agent-delegable-grants"] });
    await screen.findByText(/Delegable permissions could not be loaded/);
    expect(
      client.getQueryData([
        "agent-delegable-grants",
        mocks.org,
        mocks.user,
        agent.id,
      ]),
    ).toBeTruthy();
    expect(screen.queryByRole("checkbox", { name: /mcp:connect/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("lets writers narrow a broad grant to one server and tool", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        ...grant,
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ]);
    setup();
    await openCreate();
    const selectedGrant = await screen.findByRole("checkbox", {
      name: /mcp:connect/,
    });
    expect((selectedGrant as HTMLInputElement).checked).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    expect(mocks.create).not.toHaveBeenCalled();
    fireEvent.click(selectedGrant);
    const server = await screen.findByLabelText("Server for mcp:connect");
    fireEvent.change(server, { target: { value: "server_one" } });
    fireEvent.click(screen.getByRole("button", { name: "Specific tools" }));
    fireEvent.click(await screen.findByRole("button", { name: "search" }));
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_one",
          tool: "search",
        },
      },
    ]);
  });
  it("keeps a policy-pinned dimension out of the editor", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        ...grant,
        scope: "mcp:write",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_one",
          tool: "search",
        },
      },
    ]);
    setup();
    await openCreate();
    fireEvent.click(await screen.findByRole("checkbox", { name: /mcp:write/ }));
    expect(screen.queryByLabelText("Tool for mcp:write")).toBeNull();
    expect(screen.queryByLabelText("Server for mcp:write")).toBeNull();
    expect(screen.getByText("tool: search")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:write",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_one",
          tool: "search",
        },
      },
    ]);
  });
  it("narrows a remote-backed server to a stored tool, scoped to its project", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        ...grant,
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ]);
    mocks.toolsets.mockResolvedValue({ toolsets: [] });
    mocks.mcpServers.mockResolvedValue({
      mcpServers: [
        {
          id: "server_remote",
          projectId: "project_one",
          name: "Remote server",
          slug: "remote-server",
          remoteMcpServerId: "remote_one",
        },
      ],
    });
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    fireEvent.change(await screen.findByLabelText("Server for mcp:connect"), {
      target: { value: "server_remote" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Specific tools" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "remote_search" }),
    );
    expect(mocks.toolMetadata).toHaveBeenCalledWith({
      mcpServerId: "server_remote",
      gramProject: "project-one",
    });
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_remote",
          tool: "remote_search",
        },
      },
    ]);
  });
  it("offers no tool choice when remote tool metadata fails, and can retry", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        ...grant,
        selector: { resourceKind: "mcp", resourceId: "server_remote" },
      },
    ]);
    mocks.toolsets.mockResolvedValue({ toolsets: [] });
    mocks.mcpServers.mockResolvedValue({
      mcpServers: [
        {
          id: "server_remote",
          projectId: "project_one",
          name: "Remote server",
          slug: "remote-server",
          remoteMcpServerId: "remote_one",
        },
      ],
    });
    mocks.toolMetadata.mockRejectedValue(new Error("forbidden"));
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    await screen.findByText(/Couldn't load this server/);
    expect(screen.queryByLabelText("Tool for mcp:connect")).toBeNull();
    mocks.toolMetadata.mockResolvedValue({
      tools: [{ toolName: "remote_search" }],
    });
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "remote_search" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_remote",
          tool: "remote_search",
        },
      },
    ]);
  });
  it("explains that a tunneled server cannot be narrowed to one tool", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        ...grant,
        selector: { resourceKind: "mcp", resourceId: "server_tunneled" },
      },
    ]);
    mocks.toolsets.mockResolvedValue({ toolsets: [] });
    mocks.mcpServers.mockResolvedValue({
      mcpServers: [
        {
          id: "server_tunneled",
          projectId: "project_one",
          name: "Tunneled server",
          slug: "tunneled-server",
        },
      ],
    });
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    await screen.findByText(/Tools are discovered at runtime/);
    expect(screen.queryByLabelText("Tool for mcp:connect")).toBeNull();
    expect(mocks.toolMetadata).not.toHaveBeenCalled();
  });
  it.each([
    ["toolsets", "mcpServers"],
    ["mcpServers", "toolsets"],
  ])(
    "withholds narrowing when the %s half of the inventory fails",
    async (failing, succeeding) => {
      mocks.listDelegableGrants.mockResolvedValue([
        { ...grant, selector: { resourceKind: "mcp", resourceId: "*" } },
      ]);
      mocks.projects = twoProjects;
      // One half resolves with real rows; publishing them alone would let the
      // picker narrow against an inventory it cannot see all of.
      mocks[succeeding as "toolsets"].mockResolvedValue(
        succeeding === "toolsets"
          ? { toolsets: twoServers }
          : { mcpServers: [] },
      );
      mocks[failing as "toolsets"].mockRejectedValue(new Error("forbidden"));
      setup();
      await openCreate();
      fireEvent.click(
        await screen.findByRole("checkbox", { name: /mcp:connect/ }),
      );
      await screen.findByText(/Could not load this organization/);
      // Server and tool choices come from the withheld inventory.
      expect(screen.queryByLabelText("Server for mcp:connect")).toBeNull();
      expect(screen.queryByLabelText("Tool for mcp:connect")).toBeNull();
      expect(screen.queryByLabelText("Project for mcp:connect")).toBeNull();
      // The candidate is still delegable exactly as discovered.
      fireEvent.click(screen.getByRole("button", { name: "Create key" }));
      await screen.findByText("secret_example_once");
      expect(
        mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
      ).toEqual([
        {
          effect: "allow",
          scope: "mcp:connect",
          selector: { resourceKind: "mcp", resourceId: "*" },
        },
      ]);
    },
  );
  it("does not read the MCP inventory for a candidate that has no MCP grant", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        effect: "allow",
        scope: "project:read",
        selector: { resourceKind: "project", resourceId: "*" },
      },
    ]);
    mocks.toolsets.mockRejectedValue(new Error("forbidden"));
    mocks.mcpServers.mockRejectedValue(new Error("forbidden"));
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /project:read/ }),
    );
    // Nothing here needs the server inventory, so it is never read and its
    // failure is never surfaced.
    expect(mocks.toolsets).not.toHaveBeenCalled();
    expect(mocks.mcpServers).not.toHaveBeenCalled();
    expect(screen.queryByText(/Could not load this organization/)).toBeNull();
    expect(screen.queryByRole("button", { name: "Retry servers" })).toBeNull();
  });
  it("does not resolve a pinned server from a half-loaded inventory", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      { ...grant, selector: { resourceKind: "mcp", resourceId: "server_one" } },
    ]);
    mocks.projects = twoProjects;
    // The toolset half alone knows server_one; without the other half the
    // inventory is incomplete, so its tools must not drive narrowing.
    mocks.toolsets.mockResolvedValue({ toolsets: twoServers });
    mocks.mcpServers.mockRejectedValue(new Error("forbidden"));
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    await screen.findByText(/Could not load this organization/);
    expect(screen.queryByLabelText("Tool for mcp:connect")).toBeNull();
    expect(screen.queryByLabelText("Project for mcp:connect")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
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
  it("withholds the project choice too when a pinned server cannot be resolved", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        ...grant,
        selector: { resourceKind: "mcp", resourceId: "server_two" },
      },
    ]);
    mocks.projects = twoProjects;
    mocks.toolsets.mockRejectedValue(new Error("forbidden"));
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    await screen.findByText(/Could not load this organization/);
    // The candidate names one server, and no project can be shown to be
    // compatible with it while the inventory is unavailable.
    expect(screen.queryByLabelText("Project for mcp:connect")).toBeNull();
    expect(screen.queryByLabelText("Server for mcp:connect")).toBeNull();
  });
  it("withdraws a loaded inventory when a later refetch of one half fails", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      { ...grant, selector: { resourceKind: "mcp", resourceId: "*" } },
    ]);
    mocks.projects = twoProjects;
    mocks.toolsets.mockResolvedValue({ toolsets: twoServers });
    const { client } = setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    expect(
      optionText(await screen.findByLabelText("Server for mcp:connect")),
    ).toContain("Server one");
    mocks.mcpServers.mockRejectedValue(new Error("forbidden"));
    void client.invalidateQueries({ queryKey: ["org-mcp-servers"] });
    await screen.findByText(/Could not load this organization/);
    expect(screen.queryByLabelText("Server for mcp:connect")).toBeNull();
    // Recovering restores the complete inventory.
    mocks.mcpServers.mockResolvedValue({ mcpServers: [] });
    fireEvent.click(screen.getByRole("button", { name: "Retry servers" }));
    expect(
      optionText(await screen.findByLabelText("Server for mcp:connect")),
    ).toContain("Server one");
  });
  it("hides the server choice until the org inventory resolves", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        ...grant,
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ]);
    mocks.toolsets.mockRejectedValue(new Error("forbidden"));
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    await waitFor(() => expect(mocks.toolsets).toHaveBeenCalled());
    expect(screen.queryByLabelText("Server for mcp:connect")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ]);
  });
  it("discovers separately for each authorizer and resets the dialog on user change", async () => {
    mocks.listDelegableGrants.mockResolvedValue([grant]);
    const { change, client } = setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
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
      ]),
    ).toBeUndefined();
    await openCreate();
    const reopened = await screen.findByRole("checkbox", {
      name: /mcp:connect/,
    });
    expect((reopened as HTMLInputElement).checked).toBe(false);
  });
  it("omits toolsets with MCP explicitly disabled from the server choices", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      { ...grant, selector: { resourceKind: "mcp", resourceId: "*" } },
    ]);
    mocks.toolsets.mockResolvedValue({
      toolsets: [
        {
          id: "server_one",
          name: "Server one",
          slug: "server-one",
          projectId: "project_one",
          mcpEnabled: true,
          tools: [{ id: "tool_one", name: "search", type: "http" }],
        },
        {
          id: "server_off",
          name: "Server off",
          slug: "server-off",
          projectId: "project_one",
          mcpEnabled: false,
          tools: [{ id: "tool_two", name: "lookup", type: "http" }],
        },
      ],
    });
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    const options = optionText(
      await screen.findByLabelText("Server for mcp:connect"),
    );
    expect(options).toContain("Server one");
    expect(options).not.toContain("Server off");
  });
  it("omits a disabled toolset that also has an MCP servers row", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      { ...grant, selector: { resourceKind: "mcp", resourceId: "*" } },
    ]);
    mocks.toolsets.mockResolvedValue({
      toolsets: [
        {
          id: "server_one",
          name: "Server one",
          slug: "server-one",
          projectId: "project_one",
          tools: [{ id: "tool_one", name: "search", type: "http" }],
        },
        {
          id: "server_off",
          name: "Server off",
          slug: "server-off",
          projectId: "project_one",
          mcpEnabled: false,
          tools: [{ id: "tool_two", name: "lookup", type: "http" }],
        },
      ],
    });
    // The same disabled toolset also appears as an mcp_servers row, which the
    // merge would otherwise re-add under its toolset id.
    mocks.mcpServers.mockResolvedValue({
      mcpServers: [
        {
          id: "mcp_row_off",
          projectId: "project_one",
          name: "Server off",
          slug: "server-off",
          toolsetId: "server_off",
        },
      ],
    });
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    const server = await screen.findByLabelText("Server for mcp:connect");
    expect(optionText(server)).toContain("Server one");
    expect(optionText(server)).not.toContain("Server off");
    expect(server.querySelector('option[value="server_off"]')).toBeNull();
    expect(server.querySelector('option[value="mcp_row_off"]')).toBeNull();
  });
  it("withholds the project choice for a server missing from the inventory", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        ...grant,
        selector: { resourceKind: "mcp", resourceId: "server_unlisted" },
      },
    ]);
    mocks.projects = twoProjects;
    mocks.toolsets.mockResolvedValue({ toolsets: twoServers });
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    // The candidate names a server this caller's inventory does not contain,
    // so no project can be known to be compatible with it.
    expect(screen.queryByLabelText("Project for mcp:connect")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "server_unlisted" },
      },
    ]);
  });
  it("does not offer project narrowing after selecting a server", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      { ...grant, selector: { resourceKind: "mcp", resourceId: "*" } },
    ]);
    mocks.projects = twoProjects;
    mocks.toolsets.mockResolvedValue({ toolsets: twoServers });
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    fireEvent.change(await screen.findByLabelText("Server for mcp:connect"), {
      target: { value: "server_two" },
    });
    expect(screen.queryByLabelText("Project for mcp:connect")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_two",
        },
      },
    ]);
  });
  it("offers only the pinned project's servers when a candidate fixes the project", async () => {
    mocks.listDelegableGrants.mockResolvedValue([
      {
        ...grant,
        selector: {
          resourceKind: "mcp",
          resourceId: "*",
          projectId: "project_two",
        },
      },
    ]);
    mocks.projects = twoProjects;
    mocks.toolsets.mockResolvedValue({ toolsets: twoServers });
    setup();
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    const options = optionText(
      await screen.findByLabelText("Server for mcp:connect"),
    );
    expect(options).toContain("Server two");
    expect(options).not.toContain("Server one");
    expect(screen.queryByLabelText("Project for mcp:connect")).toBeNull();
  });
  it("shows a future expiry as an absolute date, never as elapsed time", async () => {
    const expiresAt = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000);
    mocks.list.mockResolvedValue({ keys: [{ ...key, expiresAt }] });
    setup();
    const rendered = await screen.findByText(expiresAt.toLocaleString());
    expect(rendered.getAttribute("datetime")).toBe(expiresAt.toISOString());
    expect(screen.queryByText(/ago/)).toBeNull();
  });
  it("shows loading separately from an empty list", () => {
    mocks.list.mockImplementation(() => new Promise(() => {}));
    setup();
    expect(screen.getByText("Loading API keys…")).toBeTruthy();
    expect(screen.queryByText("No API keys yet")).toBeNull();
  });
});
