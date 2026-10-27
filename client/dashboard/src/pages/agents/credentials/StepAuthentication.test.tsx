import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import { StepAuthentication } from "./StepAuthentication";
import type { InventoryServer } from "./useServerInventory";

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "editor", displayName: "Example Editor" } }),
}));
vi.mock("../AgentKeyServers", () => ({ ServerAccounts: () => null }));
afterEach(cleanup);

const server: InventoryServer = {
  id: "server_one",
  resourceId: "server_one",
  name: "Example server",
  slug: "example",
  projectId: "project_one",
  projectSlug: "project-one",
  projectName: "Project one",
  kind: "Remote",
  issuerId: "issuer_one",
};
const mine = {
  id: "mine",
  subjectUrn: "user:editor",
  hasRefreshToken: true,
} as RemoteSession;
const expired = {
  id: "expired",
  subjectUrn: "user:other",
  hasRefreshToken: false,
  accessExpiresAt: new Date(0),
} as RemoteSession;

describe("authentication assignment warnings", () => {
  it("warns about an expired delegated assignment even when the editor has a live account", () => {
    render(
      <StepAuthentication
        agent={{ id: "agent_one" } as ManagedAgent}
        servers={[server]}
        allServers
        onRemove={vi.fn<(id: string) => void>()}
        connections={{
          byServer: new Map([
            [server.id, { status: "ready", sessions: [mine] }],
          ]),
          assignedByServer: new Map([
            [server.id, { status: "ended", sessions: [expired] }],
          ]),
          refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
        }}
      />,
    );
    expect(screen.getByRole("alert").textContent).toContain("Example server");
    expect(screen.getByText("Expired")).toBeTruthy();
  });
  it("keeps a valid delegated assignment clear of candidate warnings", () => {
    render(
      <StepAuthentication
        agent={{ id: "agent_one" } as ManagedAgent}
        servers={[server]}
        allServers
        onRemove={vi.fn<(id: string) => void>()}
        connections={{
          byServer: new Map([[server.id, { status: "missing" }]]),
          assignedByServer: new Map([
            [server.id, { status: "ready", sessions: [mine] }],
          ]),
          refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
        }}
      />,
    );
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
