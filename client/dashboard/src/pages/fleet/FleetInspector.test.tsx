import { cleanup, render, screen } from "@testing-library/react";
import { BrowserRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { FleetInspector } from "./FleetInspector";
import { buildFleetRows } from "./fleet-model";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";

const mocks = vi.hoisted(() => ({
  slug: "customer",
  policy: vi.fn(),
  sessions: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ slug: mocks.slug }),
}));
vi.mock("@/hooks/use-mobile", () => ({ useIsMobile: () => false }));
vi.mock("@/hooks/useKillswitchAccess", () => ({
  useKillswitchAccess: () => ({ canAccess: false, reason: "demo" }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ agents: { href: () => "/agents" } }),
}));
vi.mock("@/pages/agents/AgentPolicySection", () => ({
  AgentPolicySection: () => {
    mocks.policy();
    return <p>Policy panel</p>;
  },
}));
vi.mock("@/pages/agents/ManagedAgentSessions", () => ({
  ManagedAgentSessions: () => {
    mocks.sessions();
    return <p>Credential sessions</p>;
  },
}));
vi.mock("@/pages/chatLogs/useChatDetailSheet", () => ({
  useChatDetailSheet: () => ({ sheet: null }),
}));
vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("./FleetCollection", () => ({ FleetStatus: () => null }));
vi.mock("./AgentMCPControls", () => ({ AgentMCPControls: () => null }));

const agent: ManagedAgent = {
  id: "agent-1",
  name: "Synthetic identity",
  ownerUserId: "synthetic-owner",
  lifecycle: "active",
  permissions: { read: true, write: false, authorize: false, transfer: false },
  createdAt: new Date("2026-09-01"),
  updatedAt: new Date("2026-09-01"),
  lastCredentialUsedAt: new Date("2026-09-25"),
};
function mount(tab: string, managedAgent = agent) {
  window.history.replaceState(null, "", `/fleet?detail=${tab}`);
  const rows = buildFleetRows({
    agents: [managedAgent],
    assistants: [],
    sessions: [],
    members: [],
    projectId: "project",
  });
  return render(
    <BrowserRouter>
      <FleetInspector
        row={rows[0]!}
        rows={rows}
        onClose={vi.fn<() => void>()}
        onSelect={vi.fn<(id: string) => void>()}
        blocked={false}
      />
    </BrowserRouter>,
  );
}
beforeEach(() => {
  mocks.slug = "customer";
  vi.clearAllMocks();
});
afterEach(cleanup);
it.each(["identity", "activity", "controls"])(
  "keeps the demo %s inspector read-only",
  (tab) => {
    mocks.slug = DEMO_ORG_SLUG;
    mount(tab);
    expect(mocks.policy).not.toHaveBeenCalled();
    expect(mocks.sessions).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("link", { name: /Manage agent|Suspend/ }),
    ).toBeNull();
    expect(screen.getByText(/unavailable in the read-only demo/)).toBeTruthy();
  },
);
it("retains the viewing link but avoids a forbidden policy read without write permission", () => {
  mount("identity");
  expect(mocks.policy).not.toHaveBeenCalled();
  expect(
    screen.getByText("Policy details require agent write access."),
  ).toBeTruthy();
  expect(
    screen.getByRole("link", { name: /Manage agent identity/ }),
  ).toBeTruthy();
});
it("retains policy details for a normal writer", () => {
  mount("identity", {
    ...agent,
    permissions: { ...agent.permissions, write: true },
  });
  expect(mocks.policy).toHaveBeenCalled();
  expect(screen.getByText("Policy panel")).toBeTruthy();
});
