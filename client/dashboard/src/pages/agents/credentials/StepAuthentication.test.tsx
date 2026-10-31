import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import { StepAuthentication } from "./StepAuthentication";
import type { InventoryServer } from "./useServerInventory";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "editor", displayName: "Example Editor" } }),
}));
vi.mock("../AgentKeyServers", () => ({ ServerAccounts: () => null }));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

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
      { wrapper: MemoryRouter },
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
      { wrapper: MemoryRouter },
    );
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("create connection reauthentication", () => {
  it.each(["ready", "ended", "missing"] as const)(
    "targets the known upstream client for %s connections",
    (status) => {
      const popup = {
        location: { href: "" },
        opener: window,
        focus: vi.fn(),
        closed: false,
      };
      vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
      const session = {
        ...mine,
        remoteSessionClientId: "client_actual",
        hasRefreshToken: false,
        accessExpiresAt: new Date(
          status === "ended" ? 0 : Date.now() + 3600000,
        ),
      };
      render(
        <StepAuthentication
          servers={[
            {
              ...server,
              connectUrl:
                "https://example.test/mcp/connect/first-party?existing=keep",
            },
          ]}
          allServers={false}
          onRemove={vi.fn<(id: string) => void>()}
          connections={{
            byServer: new Map([
              [
                server.id,
                status === "missing"
                  ? { status }
                  : { status, sessions: [session] },
              ],
            ]),
            refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
          }}
        />,
        { wrapper: MemoryRouter },
      );
      // The only row, so it starts expanded.
      fireEvent.click(
        screen.getByRole("button", {
          name: status === "missing" ? "Connect account" : "Reauthenticate",
        }),
      );
      const url = new URL(popup.location.href);
      expect(url.searchParams.get("reauthenticate_client_id")).toBe(
        status === "missing" ? null : "client_actual",
      );
      expect(url.searchParams.get("existing")).toBe("keep");
    },
  );
});
