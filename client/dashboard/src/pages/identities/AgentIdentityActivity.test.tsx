import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import {
  AgentIdentityAudit,
  AgentIdentityChallenges,
} from "./AgentIdentityActivity";

const mocks = vi.hoisted(() => ({ audit: vi.fn(), challenges: vi.fn() }));
const from = new Date("2026-01-01T00:00:00Z");
const to = new Date("2026-01-08T00:00:00Z");
vi.mock("./useIdentityQueries", () => ({
  useIdentityWindow: () => ({ from, to }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ access: { href: () => "/org/access" } }),
}));
vi.mock("@gram/client/react-query/auditLogs.js", () => ({
  useAuditLogs: (...args: unknown[]) => mocks.audit(...args),
}));
vi.mock("@gram/client/react-query/challenges.js", () => ({
  useChallenges: (...args: unknown[]) => mocks.challenges(...args),
}));
const agent = {
  id: "agent_example",
  ownerUserId: "owner_example",
} as ManagedAgent;
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.audit.mockReturnValue({ data: { result: { logs: [] } } });
  mocks.challenges.mockReturnValue({ data: { challenges: [], total: 0 } });
});
it("queries changes to the agent as the audit subject, including human permission edits", () => {
  render(<AgentIdentityAudit agent={agent} subject />);
  expect(mocks.audit).toHaveBeenCalledWith(
    { subjectType: "agent", subjectId: agent.id, from, to },
    undefined,
    { throwOnError: false },
  );
});
it("queries agent actions without using its owner's identity", () => {
  render(<AgentIdentityAudit agent={agent} />);
  expect(mocks.audit).toHaveBeenCalledWith(
    { actorId: agent.id, from, to },
    undefined,
    { throwOnError: false },
  );
});
it("queries authorization checks using the agent principal and preserves it in the handoff", () => {
  render(
    <MemoryRouter>
      <AgentIdentityChallenges agent={agent} />
    </MemoryRouter>,
  );
  expect(mocks.challenges).toHaveBeenCalledWith(
    { principalUrn: "agent:agent_example", from, to, limit: 25 },
    undefined,
    { throwOnError: false },
  );
  expect(screen.getByRole("link").getAttribute("href")).toContain(
    "identity=agent%3Aagent_example",
  );
});
it("shows failed authorization reads as an error rather than no checks", () => {
  mocks.challenges.mockReturnValue({ isError: true, refetch: vi.fn() });
  render(
    <MemoryRouter>
      <AgentIdentityChallenges agent={agent} />
    </MemoryRouter>,
  );
  expect(screen.getByText("This panel could not be loaded.")).toBeTruthy();
  expect(screen.queryByText(/No authorization checks recorded/)).toBeNull();
});
