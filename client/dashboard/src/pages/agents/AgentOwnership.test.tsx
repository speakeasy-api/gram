import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
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
}));
vi.mock("@/contexts/Sdk", () => ({
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
    useTransferAgentMutation: (options: { onSuccess: () => void }) =>
      useMutation({ mutationFn: mocks.transfer, ...options }),
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
  const refresh = vi.fn<() => void>();
  const agent = {
    id: "agent-test",
    ownerUserId: owner.id,
    permissions: { transfer: canTransfer },
  } as ManagedAgent;
  render(
    <QueryClientProvider client={client}>
      <AgentOwnership agent={agent} refresh={refresh} />
    </QueryClientProvider>,
  );
  return { client, refresh };
}
async function selectMember() {
  fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
  fireEvent.click(await screen.findByRole("radio", { name: /New Member/ }));
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
}
beforeEach(() => {
  vi.clearAllMocks();
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
    const { refresh } = setup();
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
    expect(refresh).not.toHaveBeenCalled();
    await act(async () => resolve({}));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(mocks.success).toHaveBeenCalledWith("Agent ownership transferred");
  });

  it("displays transfer failure without refreshing and clears the error on Back before retrying", async () => {
    mocks.transfer.mockRejectedValueOnce(new Error("Transfer denied"));
    const { refresh } = setup();
    await selectMember();
    fireEvent.click(screen.getByRole("button", { name: "Transfer ownership" }));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Transfer denied",
    );
    expect(refresh).not.toHaveBeenCalled();
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
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1));
  });
});
