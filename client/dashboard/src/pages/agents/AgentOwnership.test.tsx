import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AgentOwnership } from "./AgentOwnership";

const mocks = vi.hoisted(() => ({
  listMembers: vi.fn(),
  transfer: vi.fn(),
  success: vi.fn(),
  projectSlug: "default",
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => mocks.projectSlug,
  useSdkClient: () => ({ access: { listMembers: mocks.listMembers } }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-test" }),
  useSession: () => ({ user: { id: "user-test" } }),
}));
vi.mock("./agent-admin", () => ({
  AgentOwner: () => <span>Current owner display</span>,
}));
vi.mock("sonner", () => ({ toast: { success: mocks.success } }));
vi.mock("@gram/client/react-query/transferAgent.js", async () => {
  const { useMutation } = await import("@tanstack/react-query");
  return {
    useTransferAgentMutation: (options: {
      onSuccess: (agent: ManagedAgent) => Promise<void>;
    }) => useMutation({ mutationFn: mocks.transfer, ...options }),
  };
});

const owner = {
  id: "owner",
  name: "Original Owner",
  email: "owner@example.com",
  roleIds: [],
  directoryRoleIds: [],
};
const member = {
  id: "next-owner",
  name: "New Member",
  email: "new@example.com",
  roleIds: [],
  directoryRoleIds: [],
};
const clients: QueryClient[] = [];
function setup(canTransfer = true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  const agent = {
    id: "agent-test",
    ownerUserId: owner.id,
    permissions: { transfer: canTransfer },
  } as ManagedAgent;
  const view = render(
    <QueryClientProvider client={client}>
      <AgentOwnership agent={agent} />
    </QueryClientProvider>,
  );
  return { client, agent, ...view };
}
async function selectMember() {
  fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
  fireEvent.click(await screen.findByRole("radio", { name: /New Member/ }));
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.projectSlug = "default";
  mocks.listMembers.mockReset().mockResolvedValue({ members: [owner, member] });
  mocks.transfer.mockReset().mockResolvedValue({});
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
});

describe("AgentOwnership", () => {
  it("hides ownership and never queries members without transfer permission", () => {
    const { client } = setup(false);
    expect(screen.queryByText("Ownership")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(mocks.listMembers).not.toHaveBeenCalled();
    expect(client.getQueryCache().getAll()).toHaveLength(0);
  });

  it("loads all 106 eligible members including no-role members and searches names/emails case-insensitively", async () => {
    const roster = Array.from({ length: 105 }, (_, i) => ({
      id: `member-${i}`,
      name: `Person ${i}`,
      email: `person${i}@example.com`,
      roleIds: [],
      directoryRoleIds: [],
    }));
    mocks.listMembers.mockResolvedValue({
      members: [owner, ...roster, member],
    });
    setup();
    expect(mocks.listMembers).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
    expect(await screen.findAllByRole("radio")).toHaveLength(106);
    expect(screen.queryByRole("radio", { name: /Original Owner/ })).toBeNull();
    expect(screen.getByRole("radio", { name: /Person 104 / })).toBeTruthy();
    expect(mocks.listMembers).toHaveBeenCalledExactlyOnceWith({}, undefined, {
      signal: expect.objectContaining({
        aborted: false,
        addEventListener: expect.any(Function),
      }),
    });
    const search = screen.getByRole("textbox", { name: "Search members" });
    fireEvent.change(search, { target: { value: "  nEW mEMBER  " } });
    expect(screen.getAllByRole("radio")).toHaveLength(1);
    expect(screen.getByRole("radio", { name: /New Member/ })).toBeTruthy();
    fireEvent.change(search, { target: { value: "PERSON104@EXAMPLE.COM" } });
    expect(screen.getAllByRole("radio")).toHaveLength(1);
    expect(screen.getByRole("radio", { name: /Person 104 / })).toBeTruthy();
    fireEvent.change(search, { target: { value: "nobody" } });
    expect(screen.getByText("No members match your search.")).toBeTruthy();
  });

  it("requires selection and confirmation, preserves selection on Back, and sends the exact assignment", async () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(await screen.findByRole("radio", { name: /New Member/ }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(
      screen.getByRole("heading", { name: "Confirm ownership transfer" }),
    ).toBeTruthy();
    expect(screen.getByText("Current owner display")).toBeTruthy();
    expect(screen.getByText("new@example.com")).toBeTruthy();
    expect(mocks.transfer).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(
      (screen.getByRole("radio", { name: /New Member/ }) as HTMLInputElement)
        .checked,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(screen.getByRole("button", { name: "Transfer ownership" }));
    await waitFor(() => expect(mocks.transfer).toHaveBeenCalled());
    expect(mocks.transfer.mock.calls[0]?.[0]).toEqual({
      request: {
        agentOwnerAssignmentForm: {
          agentId: "agent-test",
          ownerUserId: "next-owner",
        },
      },
    });
  });

  it("does not reuse another project's member roster", async () => {
    const { client, agent, rerender } = setup();
    await selectMember();
    mocks.projectSlug = "other-project";
    mocks.listMembers.mockImplementation(() => new Promise(() => {}));
    rerender(
      <QueryClientProvider client={client}>
        <AgentOwnership agent={agent} />
      </QueryClientProvider>,
    );
    expect(await screen.findByText("Loading members…")).toBeTruthy();
    expect(screen.queryByText("new@example.com")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.queryKey),
    ).toContainEqual([
      "agent-transfer-members",
      "org-test",
      "user-test",
      "other-project",
    ]);
  });

  it("shows a failed background member refresh on confirmation and permits retry", async () => {
    const { client } = setup();
    await selectMember();
    mocks.listMembers.mockRejectedValueOnce(new Error("Member listing denied"));
    await act(async () => {
      await client.invalidateQueries({ queryKey: ["agent-transfer-members"] });
    });
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Unable to load organization members",
    );
    expect(
      screen
        .getByRole("button", { name: "Transfer ownership" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() =>
      expect(
        screen
          .getByRole("button", { name: "Transfer ownership" })
          .hasAttribute("disabled"),
      ).toBe(false),
    );
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it.each([403, 404, 500, undefined])(
    "refetches after transfer and replaces stale owner permissions even when read fails with %s",
    async (statusCode) => {
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      clients.push(client);
      const before = {
        id: "agent-test",
        ownerUserId: "owner",
        permissions: { transfer: true },
      } as ManagedAgent;
      const after = {
        ...before,
        ownerUserId: "next-owner",
        permissions: { ...before.permissions, transfer: false },
      };
      const error = Object.assign(new Error("Read failed"), { statusCode });
      const get = vi.fn().mockRejectedValue(error);
      mocks.transfer.mockResolvedValue(after);
      const detailKey = ["managed-agents", "org-test", "detail", before.id];
      const rosterKey = [
        "identities",
        "registered-agents",
        "org-test",
        "user-test",
      ];
      const readableKey = ["managed-agents", "org-test", "list", "user-test"];
      const otherOrgKey = ["managed-agents", "other-org", "list", "user-test"];
      for (const key of [rosterKey, readableKey, otherOrgKey])
        client.setQueryData(key, [before]);
      function ObservedAgent() {
        const query = useQuery({
          queryKey: detailKey,
          queryFn: get,
          initialData: before,
          staleTime: Infinity,
        });
        return (
          <>
            <output aria-label="Displayed owner">
              {query.data.ownerUserId}
            </output>
            <AgentOwnership agent={query.data} />
          </>
        );
      }
      render(
        <QueryClientProvider client={client}>
          <ObservedAgent />
        </QueryClientProvider>,
      );
      await selectMember();
      fireEvent.click(
        screen.getByRole("button", { name: "Transfer ownership" }),
      );
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
      await waitFor(() =>
        expect(client.getQueryState(detailKey)?.error).toBe(error),
      );
      expect(screen.getByLabelText("Displayed owner").textContent).toBe(
        "next-owner",
      );
      expect(screen.queryByText("Ownership")).toBeNull();
      expect(client.getQueryData(detailKey)).toEqual(after);
      expect(client.getQueryState(rosterKey)?.isInvalidated).toBe(true);
      expect(client.getQueryState(readableKey)?.isInvalidated).toBe(true);
      expect(client.getQueryState(otherOrgKey)?.isInvalidated).toBe(false);
    },
  );

  it("retains ownership controls when the authoritative read grants transfer through RBAC", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    clients.push(client);
    const before = {
      id: "agent-test",
      ownerUserId: "owner",
      permissions: { transfer: true },
    } as ManagedAgent;
    const after = { ...before, ownerUserId: "next-owner" };
    const get = vi.fn().mockResolvedValue(after);
    mocks.transfer.mockResolvedValue(after);
    function ObservedAgent() {
      const query = useQuery({
        queryKey: ["managed-agents", "org-test", "detail", before.id],
        queryFn: get,
        initialData: before,
        staleTime: Infinity,
      });
      return (
        <>
          <output aria-label="Displayed owner">{query.data.ownerUserId}</output>
          <AgentOwnership agent={query.data} />
        </>
      );
    }
    render(
      <QueryClientProvider client={client}>
        <ObservedAgent />
      </QueryClientProvider>,
    );
    await selectMember();
    fireEvent.click(screen.getByRole("button", { name: "Transfer ownership" }));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.getByLabelText("Displayed owner").textContent).toBe(
      "next-owner",
    );
    expect(screen.getByRole("button", { name: "Transfer" })).toBeTruthy();
  });

  it("shows loading then an empty state when only the current owner exists", async () => {
    let resolve!: (value: { members: (typeof owner)[] }) => void;
    mocks.listMembers.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
    expect(screen.getByRole("status").textContent).toContain("Loading members");
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);
    await act(async () => resolve({ members: [owner] }));
    expect(
      await screen.findByText(
        "No other eligible members in your organization.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("shows member-list errors and retries when requested", async () => {
    mocks.listMembers.mockRejectedValueOnce(new Error("Forbidden"));
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Unable to load organization members",
    );
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(mocks.listMembers).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByRole("radio", { name: /New Member/ }),
    ).toBeTruthy();
    expect(mocks.listMembers).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("disables actions and prevents dismissal while pending, then closes and refreshes on success", async () => {
    let resolve!: (value: object) => void;
    mocks.transfer.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    setup();
    await selectMember();
    fireEvent.click(screen.getByRole("button", { name: "Transfer ownership" }));
    expect(
      (
        await screen.findByRole("button", { name: "Transferring…" })
      ).hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen.getByRole("button", { name: "Back" }).hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(screen.getByRole("dialog")).toBeTruthy();
    await act(async () => resolve({}));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(mocks.success).toHaveBeenCalledWith("Agent ownership transferred");
  });

  it("displays transfer failure without refreshing and clears the error on Back before retrying", async () => {
    mocks.transfer.mockRejectedValueOnce(new Error("Transfer denied"));
    setup();
    await selectMember();
    fireEvent.click(screen.getByRole("button", { name: "Transfer ownership" }));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Transfer denied",
    );
    expect(mocks.success).not.toHaveBeenCalled();
    expect(
      screen
        .getByRole("button", { name: "Transfer ownership" })
        .hasAttribute("disabled"),
    ).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    expect(
      (screen.getByRole("radio", { name: /New Member/ }) as HTMLInputElement)
        .checked,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(screen.getByRole("button", { name: "Transfer ownership" }));
    await waitFor(() => expect(mocks.success).toHaveBeenCalledTimes(1));
  });
});
