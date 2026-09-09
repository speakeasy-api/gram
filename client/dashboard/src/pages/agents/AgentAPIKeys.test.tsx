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
import {
  parseDelegatedGrants,
  validateAgentAPIKeyName,
} from "./agent-api-key-grants";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  revoke: vi.fn(),
  listPolicyGrants: vi.fn(),
  flag: "enabled",
  org: "org_example",
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: mocks.org }),
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
  selector: { resource_kind: "mcp", resource_id: "example" },
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
async function openCreate() {
  fireEvent.click(
    await screen.findByRole("button", { name: "Create API key" }),
  );
  fireEvent.change(screen.getByLabelText("Key name"), {
    target: { value: "New key" },
  });
}
async function emptyCreate() {
  await openCreate();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /Create without permissions/ }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Create key" }));
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.flag = "enabled";
  mocks.org = "org_example";
  mocks.list.mockResolvedValue({ keys: [key] });
  mocks.create.mockResolvedValue({ ...key, key: "secret_example_once" });
  mocks.revoke.mockResolvedValue(undefined);
  mocks.listPolicyGrants.mockResolvedValue([]);
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
  it("does not infer empty policy for authorize-only users and requires explicit empty approval", async () => {
    setup();
    await openCreate();
    expect(mocks.listPolicyGrants).not.toHaveBeenCalled();
    expect(screen.getByText(/cannot read agent policy/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    expect(mocks.create).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Create without permissions/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(mocks.create.mock.calls[0]?.[0]).toEqual({
      security: { sessionHeaderGramSession: "" },
      request: {
        createKeyForm: {
          agentId: agent.id,
          name: "New key",
          delegatedGrantsVersion: 1,
          requestedGrants: [],
          scopes: [],
        },
      },
    });
  });
  it("reveals secrets once without copying them to query cache or storage", async () => {
    const storage = vi.spyOn(Storage.prototype, "setItem");
    const { client } = setup();
    await emptyCreate();
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
      await emptyCreate();
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
    await emptyCreate();
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
    expect(mocks.listPolicyGrants).not.toHaveBeenCalled();
  });
  it("closes creation and clears the secret on rollout loss", async () => {
    const { change } = setup();
    await emptyCreate();
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
    await emptyCreate();
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
  it("stops policy refetch and creation on rollout loss", async () => {
    const { change, client } = setup({
      ...agent,
      permissions: { ...agent.permissions, write: true },
    });
    await openCreate();
    await screen.findByText("The agent policy has no permissions.");
    mocks.flag = "disabled";
    change();
    mocks.listPolicyGrants.mockClear();
    mocks.list.mockClear();
    await client.invalidateQueries();
    fireEvent.click(screen.getByRole("button", { name: "Create API key" }));
    expect(screen.queryByLabelText("Key name")).toBeNull();
    expect(mocks.listPolicyGrants).not.toHaveBeenCalled();
    expect(mocks.list).not.toHaveBeenCalled();
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("loads policy only for writers, with nothing selected by default", async () => {
    mocks.listPolicyGrants.mockResolvedValue([
      {
        id: "grant_example",
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "example" },
      },
    ]);
    setup({ ...agent, permissions: { ...agent.permissions, write: true } });
    expect(mocks.listPolicyGrants).not.toHaveBeenCalled();
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
    await emptyCreate();
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
  it("submits typed delegated grants without a project binding or legacy scope", async () => {
    setup();
    await openCreate();
    fireEvent.change(screen.getByLabelText("Requested grants (JSON)"), {
      target: { value: JSON.stringify([grant]) },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    const payload = mocks.create.mock.calls[0]?.[0].request.createKeyForm;
    expect(payload).not.toHaveProperty("projectId");
    expect(payload.scopes).toEqual([]);
    expect(payload.requestedGrants).toEqual(
      parseDelegatedGrants(JSON.stringify([grant])),
    );
  });
  it("does not treat failed policy reads as empty policy", async () => {
    mocks.listPolicyGrants.mockRejectedValue(new Error("forbidden"));
    setup({ ...agent, permissions: { ...agent.permissions, write: true } });
    await openCreate();
    await screen.findByText(/Agent policy could not be loaded/);
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    expect(mocks.create).not.toHaveBeenCalled();
    expect(screen.getByLabelText("Requested grants (JSON)")).toBeTruthy();
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
      screen.getByRole("checkbox", { name: /Create without permissions/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
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
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Create without permissions/ }),
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
  it("uses explicit JSON rather than retained selected policy after refetch failure", async () => {
    mocks.listPolicyGrants.mockResolvedValue([
      {
        ...grant,
        id: "grant_example",
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ]);
    const { client } = setup({
      ...agent,
      permissions: { ...agent.permissions, write: true },
    });
    await openCreate();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /mcp:connect/ }),
    );
    mocks.listPolicyGrants.mockRejectedValue(new Error("refetch failed"));
    await client.invalidateQueries({ queryKey: ["agent-api-key-policy"] });
    await screen.findByText(/Agent policy could not be loaded/);
    expect(
      client.getQueryData(["agent-api-key-policy", mocks.org, agent.id]),
    ).toBeTruthy();
    expect(screen.queryByRole("checkbox", { name: /mcp:connect/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    expect(mocks.create).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Requested grants (JSON)"), {
      target: { value: JSON.stringify([grant]) },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual(parseDelegatedGrants(JSON.stringify([grant])));
  });
  it("lets writers explicitly narrow a loaded broad grant without default permissions", async () => {
    mocks.listPolicyGrants.mockResolvedValue([
      {
        ...grant,
        id: "grant_example",
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ]);
    setup({ ...agent, permissions: { ...agent.permissions, write: true } });
    await openCreate();
    const selectedGrant = await screen.findByRole("checkbox", {
      name: /mcp:connect/,
    });
    expect((selectedGrant as HTMLInputElement).checked).toBe(false);
    fireEvent.click(selectedGrant);
    fireEvent.click(
      screen.getByRole("radio", { name: "Edit requested grants (JSON)" }),
    );
    expect(
      (screen.getByLabelText("Requested grants (JSON)") as HTMLTextAreaElement)
        .value,
    ).toBe("");
    for (const value of [
      "[]",
      "{}",
      JSON.stringify([{ ...grant, extra: true }]),
      JSON.stringify([
        { ...grant, selector: { ...grant.selector, typo: "x" } },
      ]),
    ]) {
      fireEvent.change(screen.getByLabelText("Requested grants (JSON)"), {
        target: { value },
      });
      fireEvent.click(screen.getByRole("button", { name: "Create key" }));
      expect(mocks.create).not.toHaveBeenCalled();
    }
    fireEvent.change(screen.getByLabelText("Requested grants (JSON)"), {
      target: { value: JSON.stringify([grant]) },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByText("secret_example_once");
    expect(
      mocks.create.mock.calls[0]?.[0].request.createKeyForm.requestedGrants,
    ).toEqual(parseDelegatedGrants(JSON.stringify([grant])));
  });
  it("shows loading separately from an empty list", () => {
    mocks.list.mockImplementation(() => new Promise(() => {}));
    setup();
    expect(screen.getByText("Loading API keys…")).toBeTruthy();
    expect(screen.queryByText("No API keys yet")).toBeNull();
  });
  it("parses explicit wire-format policy and rejects malformed or deny grants", () => {
    expect(parseDelegatedGrants(JSON.stringify([grant]))).toEqual([
      { ...grant, selector: { resourceKind: "mcp", resourceId: "example" } },
    ]);
    expect(() => parseDelegatedGrants("{}")).toThrow();
    expect(() => parseDelegatedGrants('[{"effect":"deny"}]')).toThrow();
  });
});
