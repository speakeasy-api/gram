import { TooltipProvider } from "@/components/ui/Tooltip";
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

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  revoke: vi.fn(),
  onCreate: vi.fn<() => void>(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
  useSession: () => ({ user: { id: "user_example" } }),
}));
vi.mock("@gram/client/react-query/listAPIKeys", () => ({
  useListAPIKeys: (request: unknown, security: unknown, options: object) =>
    useQuery({
      queryKey: ["@gram/client", "keys", "list", request],
      queryFn: () => mocks.list(request, security),
      ...options,
    }),
}));
vi.mock("@gram/client/react-query/revokeAPIKey", () => ({
  useRevokeAPIKeyMutation: (options: object) =>
    useMutation({ mutationFn: mocks.revoke, ...options }),
}));
function KeyPage({
  agent,
  federated,
}: {
  agent: ManagedAgent;
  federated?: boolean | "unknown";
}) {
  return (
    <AgentAPIKeys
      agent={agent}
      federated={federated}
      onCreate={mocks.onCreate}
    />
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
function setup(current = agent) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <KeyPage agent={current} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue({ keys: [key] });
  mocks.revoke.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("Agent API keys", () => {
  it("hands issuance to the caller", async () => {
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Issue a key" }));
    expect(mocks.onCreate).toHaveBeenCalledTimes(1);
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
  it.each([404, 500])(
    "shows list failure %s rather than empty",
    async (statusCode) => {
      mocks.list.mockRejectedValue({ statusCode });
      setup();
      // The panel says a failed read failed, and offers the retry. An empty
      // list here would be a claim about the agent that we do not have.
      expect(
        await screen.findByText("This panel could not be loaded."),
      ).toBeTruthy();
      expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
      expect(screen.queryByText(/No key issued yet/)).toBeNull();
    },
  );
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
    const { container } = setup();
    expect(container.querySelector("[aria-busy=true]")).toBeTruthy();
    expect(screen.queryByText(/No key issued yet/)).toBeNull();
  });
});

describe("the empty state, while the credential is unknown", () => {
  it("says only that no key is issued", async () => {
    mocks.list.mockResolvedValue({ keys: [] });
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <TooltipProvider>
          <KeyPage agent={agent} federated="unknown" />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    expect(await screen.findByText("No key issued.")).toBeTruthy();
    // Neither of the settled answers: one tells a federated agent it cannot
    // authenticate, the other says a platform signs it in. Until the
    // admissions read lands, both are guesses.
    expect(screen.queryByText(/cannot authenticate/)).toBeNull();
    expect(screen.queryByText(/platform's own tokens/)).toBeNull();
  });
});
