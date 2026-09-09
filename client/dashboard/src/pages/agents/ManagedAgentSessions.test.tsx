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
import { ManagedAgentSessions } from "./ManagedAgentSessions";

const mocks = vi.hoisted(() => ({
  organizationId: "org_example",
  listSessions: vi.fn(),
  revokeSession: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: mocks.organizationId }),
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
const session = {
  id: "session_example",
  clientName: "Example client",
  issuerSlug: "example-issuer",
  createdAt: new Date("2026-01-01T00:00:00Z"),
  expiresAt: new Date("2099-01-01T00:00:00Z"),
};
function setup(currentAgent = agent) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ManagedAgentSessions agent={currentAgent} />
    </QueryClientProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.organizationId = "org_example";
  mocks.listSessions.mockResolvedValue({ result: { items: [session] } });
  mocks.revokeSession.mockResolvedValue(undefined);
});

describe("Agent-scoped session integration", () => {
  it.each(["organization", "agent"])(
    "clears session metadata and cache across %s changes",
    async (scope) => {
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      const view = render(
        <QueryClientProvider client={client}>
          <ManagedAgentSessions agent={agent} />
        </QueryClientProvider>,
      );
      fireEvent.click(
        await screen.findByRole("button", { name: "Revoke session" }),
      );
      expect(screen.getByRole("dialog")).toBeTruthy();
      mocks.listSessions.mockReturnValue(new Promise(() => {}));
      if (scope === "organization") mocks.organizationId = "org_other";
      const nextAgent =
        scope === "agent" ? { ...agent, id: "agent_other" } : agent;
      view.rerender(
        <QueryClientProvider client={client}>
          <ManagedAgentSessions agent={nextAgent} />
        </QueryClientProvider>,
      );
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(screen.queryByText(/Example client/)).toBeNull();
      expect(
        client.getQueryData([
          "managed-agent-sessions",
          mocks.organizationId,
          nextAgent.id,
        ]),
      ).toBeUndefined();
    },
  );
  it("does not fetch credentials with read permission alone", () => {
    setup({
      ...agent,
      permissions: { ...agent.permissions, authorize: false },
    });
    expect(mocks.listSessions).not.toHaveBeenCalled();
    expect(screen.getByText(/do not have permission to view/)).toBeTruthy();
  });
  it("fetches only the selected agent's sessions with server-provided authorize permission", async () => {
    setup();
    expect(await screen.findByText("Example client")).toBeTruthy();
    expect(mocks.listSessions).toHaveBeenCalledWith(
      { agentId: "agent_example", cursor: undefined, limit: 50 },
      undefined,
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });
  it("loads the next cursor without dropping the first page", async () => {
    mocks.listSessions
      .mockResolvedValueOnce({
        result: { items: [session], nextCursor: "cursor_example" },
      })
      .mockResolvedValueOnce({
        result: {
          items: [
            { ...session, id: "session_second", clientName: "Second client" },
          ],
        },
      });
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Load more sessions" }),
    );
    expect(await screen.findByText("Second client")).toBeTruthy();
    expect(screen.getByText("Example client")).toBeTruthy();
    expect(mocks.listSessions).toHaveBeenLastCalledWith(
      { agentId: "agent_example", cursor: "cursor_example", limit: 50 },
      undefined,
      expect.anything(),
    );
  });
  it("binds revocation to both the agent and selected session and refreshes the list", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke session" }),
    );
    mocks.listSessions.mockResolvedValue({ result: { items: [] } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    await waitFor(() =>
      expect(mocks.revokeSession).toHaveBeenCalledExactlyOnceWith({
        revokeSessionRequestBody: {
          agentId: "agent_example",
          sessionId: "session_example",
        },
      }),
    );
    expect(await screen.findByText("No sessions yet")).toBeTruthy();
  });
});
