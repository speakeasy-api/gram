import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { AgentAPIKeys } from "./AgentAPIKeys";

const mocks = vi.hoisted(() => ({
  listAPIKeys: vi.fn(),
  revokeAPIKey: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({ agents: mocks }) }));
const agent: ManagedAgent = {
  id: "agent_example",
  name: "Example agent",
  ownerUserId: "user_owner",
  lifecycle: "active",
  permissions: { read: true, write: false, authorize: true, transfer: false },
  createdAt: new Date("2026-01-01T00:00:00Z"),
  updatedAt: new Date("2026-01-01T00:00:00Z"),
};
const key = {
  id: "key_example",
  name: "Example key",
  createdAt: new Date("2026-01-01T00:00:00Z"),
};
function setup(currentAgent = agent) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <AgentAPIKeys agent={currentAgent} />
    </QueryClientProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => {
  vi.resetAllMocks();
  mocks.listAPIKeys.mockResolvedValue({ items: [key] });
  mocks.revokeAPIKey.mockResolvedValue(undefined);
});

describe("Agent-bound API keys", () => {
  it("does not fetch keys with agent read permission alone", () => {
    setup({
      ...agent,
      permissions: { ...agent.permissions, authorize: false },
    });
    expect(mocks.listAPIKeys).not.toHaveBeenCalled();
    expect(screen.getByText(/do not have permission to view/)).toBeTruthy();
    expect(screen.queryByText("No agent-bound API keys found")).toBeNull();
  });
  it("shows metadata only and explains that creation is unavailable", async () => {
    setup();
    expect(await screen.findByText("Example key")).toBeTruthy();
    expect(screen.queryByText("key_example")).toBeNull();
    expect(
      screen.getByText(/Agent API key creation is not available/),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: /create/i })).toBeNull();
    expect(mocks.listAPIKeys).toHaveBeenCalledWith(
      { agentId: "agent_example", cursor: undefined },
      undefined,
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });
  it("shows loading instead of claiming that no keys exist", () => {
    mocks.listAPIKeys.mockReturnValue(new Promise(() => {}));
    setup();
    expect(
      screen.getByRole("status", { name: "Loading agent API keys" }),
    ).toBeTruthy();
    expect(screen.queryByText("No agent-bound API keys found")).toBeNull();
  });
  it("shows an agent-scoped empty state", async () => {
    mocks.listAPIKeys.mockResolvedValue({ items: [] });
    setup();
    expect(
      await screen.findByText("No agent-bound API keys found"),
    ).toBeTruthy();
    expect(
      screen.getByText(/Agent API key creation is not available/),
    ).toBeTruthy();
  });
  it("reports load failure and retries without presenting it as an empty inventory", async () => {
    mocks.listAPIKeys.mockRejectedValueOnce(new Error("forbidden"));
    setup();
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "Unable to load agent API keys.Try again",
    );
    expect(screen.queryByText("No agent-bound API keys found")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Example key")).toBeTruthy();
  });
  it("paginates without dropping existing metadata", async () => {
    mocks.listAPIKeys
      .mockResolvedValueOnce({ items: [key], nextCursor: "cursor_example" })
      .mockResolvedValueOnce({
        items: [{ ...key, id: "key_second", name: "Second key" }],
      });
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Load more API keys" }),
    );
    expect(await screen.findByText("Second key")).toBeTruthy();
    expect(screen.getByText("Example key")).toBeTruthy();
    expect(mocks.listAPIKeys).toHaveBeenLastCalledWith(
      { agentId: "agent_example", cursor: "cursor_example" },
      undefined,
      expect.anything(),
    );
  });
  it("requires confirmation and binds revocation to both agent and key", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke API key" }),
    );
    expect(mocks.revokeAPIKey).not.toHaveBeenCalled();
    mocks.listAPIKeys.mockResolvedValue({ items: [] });
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    await waitFor(() =>
      expect(mocks.revokeAPIKey).toHaveBeenCalledExactlyOnceWith({
        requestBody: { agentId: "agent_example", keyId: "key_example" },
      }),
    );
    expect(
      await screen.findByText("No agent-bound API keys found"),
    ).toBeTruthy();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
  it("canceling the confirmation does not revoke the key", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke API key" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(mocks.revokeAPIKey).not.toHaveBeenCalled();
  });
  it("retains metadata and reports revocation errors", async () => {
    mocks.revokeAPIKey.mockRejectedValue(new Error("forbidden"));
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke API key" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "Unable to revoke the API key. Try again.",
    );
    expect(screen.getByRole("button", { name: "Confirm revoke" })).toBeTruthy();
    expect(screen.getByText("Example key")).toBeTruthy();
  });
});
