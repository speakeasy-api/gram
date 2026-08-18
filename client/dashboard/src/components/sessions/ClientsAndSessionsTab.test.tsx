import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ClientsAndSessionsTab } from "./ClientsAndSessionsTab";

const {
  useUserSessionClientsInfinite,
  useUserSessionsInfinite,
  invalidateAllUserSessionClients,
  revokeSessionMutate,
} = vi.hoisted(() => ({
  useUserSessionClientsInfinite: vi.fn(),
  useUserSessionsInfinite: vi.fn(),
  invalidateAllUserSessionClients: vi.fn(),
  revokeSessionMutate: vi.fn(),
}));

vi.mock("@gram/client/react-query/userSessionClients.js", () => ({
  useUserSessionClientsInfinite: (...args: unknown[]) =>
    useUserSessionClientsInfinite(...args),
  invalidateAllUserSessionClients: (...args: unknown[]) =>
    invalidateAllUserSessionClients(...args),
}));

vi.mock("@gram/client/react-query/userSessions.js", () => ({
  useUserSessionsInfinite: (...args: unknown[]) =>
    useUserSessionsInfinite(...args),
  invalidateAllUserSessions: vi.fn(),
}));

// The revoke dialogs are mounted (closed) by every row, and their mutation
// hooks reach for the SDK context that only GramProvider supplies.
vi.mock("@gram/client/react-query/revokeUserSessionClient.js", () => ({
  useRevokeUserSessionClientMutation: () => ({
    mutate: (_vars: unknown, opts?: { onSuccess?: () => void }) =>
      opts?.onSuccess?.(),
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/revokeUserSession.js", () => ({
  useRevokeUserSessionMutation: () => ({
    mutate: (...args: unknown[]) => revokeSessionMutate(...args),
    isPending: false,
  }),
}));

// Radix's dropdown does not open under happy-dom's pointer-event model, so
// the action menu is stubbed to plain buttons. The behaviour under test is the
// tab's filter wiring, not Radix.
vi.mock("@/components/ui/MoreActions", () => ({
  MoreActions: ({
    actions,
  }: {
    actions: { label: string; onClick: () => void }[];
  }) => (
    <>
      {actions.map((action) => (
        <button key={action.label} onClick={action.onClick}>
          {action.label}
        </button>
      ))}
    </>
  ),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, hasAnyScope: () => true }),
}));

// Reaches for the org route helpers and the PostHog flag context, neither of
// which this test mounts. Its own gating is covered in
// ViewOrgSessionsButton.test.tsx.
vi.mock("./ViewOrgSessionsButton", () => ({
  ViewOrgSessionsButton: () => null,
}));

// The chain row mounts a (closed) session revoke dialog per connection; its
// mutation hook reaches for SDK context this test does not provide.
vi.mock("./RevokeSessionDialog", () => ({
  RevokeSessionDialog: ({
    open,
    onRevoked,
  }: {
    open: boolean;
    onRevoked: () => void;
  }) =>
    open ? <button onClick={onRevoked}>Confirm session revoke</button> : null,
}));

vi.mock("./RevokeClientDialog", () => ({
  RevokeClientDialog: ({
    open,
    onRevoked,
  }: {
    open: boolean;
    onRevoked: () => void;
  }) => (open ? <button onClick={onRevoked}>Confirm revoke</button> : null),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "project-1" }),
}));

function client(overrides: Record<string, unknown>) {
  return {
    id: "client-1",
    userSessionIssuerId: "issuer-1",
    clientId: "abc123",
    clientName: "Test Client",
    redirectUris: [],
    clientIdIssuedAt: new Date("2026-01-01T00:00:00Z"),
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    activeSessionCount: 0,
    ...overrides,
  };
}

function session(overrides: Record<string, unknown>) {
  return {
    id: "session-1",
    userSessionIssuerId: "issuer-1",
    userSessionClientId: "client-1",
    issuerSlug: "the-server",
    jti: "jti-1",
    subjectUrn: "user:u1",
    subjectType: "user",
    subjectDisplayName: "Ada Lovelace",
    clientName: "Test Client",
    expiresAt: new Date("2027-01-01T00:00:00Z"),
    refreshExpiresAt: new Date("2027-01-01T00:00:00Z"),
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    ...overrides,
  };
}

function queryResult(
  items: unknown[],
  overrides: Record<string, unknown> = {},
) {
  return {
    data: { pages: [{ result: { items } }] },
    isPending: false,
    isError: false,
    hasNextPage: false,
    fetchNextPage: vi.fn(),
    isFetchingNextPage: false,
    refetch: vi.fn(),
    ...overrides,
  };
}

// The app mounts one TooltipProvider at the root (see App.tsx); the source
// badge relies on it, so tests supply their own rather than adding a nested
// provider to the component.
function wrap(ui: React.ReactElement, initialEntries: string[] = ["/"]) {
  // The tab invalidates session queries after a revoke, so it needs a real
  // QueryClient even though every data hook is mocked.
  return (
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={initialEntries}>
        <TooltipProvider>{ui}</TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

function renderTab(ui: React.ReactElement, initialEntries?: string[]) {
  return render(wrap(ui, initialEntries));
}

describe("ClientsAndSessionsTab", () => {
  beforeEach(() => {
    useUserSessionsInfinite.mockReturnValue(queryResult([]));
    useUserSessionClientsInfinite.mockReturnValue(queryResult([]));
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("explains why there is nothing to show when no issuer is attached", () => {
    renderTab(<ClientsAndSessionsTab issuerId={undefined} />);

    expect(screen.getByText(/has no session issuer/i)).toBeDefined();
    // Without an issuer to scope to, the listing endpoints would fall back to
    // the whole project, so both queries have to be disabled rather than
    // merely unrendered.
    expect(useUserSessionClientsInfinite).toHaveBeenCalledWith(
      expect.anything(),
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(useUserSessionsInfinite).toHaveBeenCalledWith(
      expect.anything(),
      undefined,
      expect.objectContaining({ enabled: false }),
    );
  });

  it("reads a connection as one chain: client, server, then upstream", () => {
    // The whole point of the surface. A sessions table shows the inbound half
    // and leaves an admin unable to see what Gram reaches on the subject's
    // behalf.
    useUserSessionsInfinite.mockReturnValue(
      queryResult([
        session({
          upstreams: [
            {
              remoteSessionId: "remote-1",
              remoteSessionClientId: "rclient-1",
              remoteSessionIssuerId: "rissuer-1",
              issuerSlug: "mcp.linear.app",
              hasRefreshToken: true,
              autoRefresh: false,
              scopes: [],
            },
          ],
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getAllByText("Test Client").length).toBeGreaterThan(0);
    expect(screen.getByText("the-server")).toBeDefined();
    expect(screen.getByText("mcp.linear.app")).toBeDefined();
  });

  it("says so when a connection reaches no upstream at all", () => {
    // Distinct from a missing value: reaching only Gram-native tools is a real
    // state, and a blank would read as data we failed to load.
    useUserSessionsInfinite.mockReturnValue(
      queryResult([session({ upstreams: [] })]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("Gram tools only")).toBeDefined();
  });

  it("groups connections by person and can regroup by provider", () => {
    useUserSessionsInfinite.mockReturnValue(
      queryResult([
        session({ id: "s1", subjectDisplayName: "Ada Lovelace" }),
        session({
          id: "s2",
          subjectUrn: "user:u2",
          subjectDisplayName: "Grace Hopper",
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("Ada Lovelace")).toBeDefined();
    expect(screen.getByText("Grace Hopper")).toBeDefined();

    fireEvent.click(screen.getByText("Provider"));

    // Neither session has an upstream, so both file under the same group and
    // the person headings give way to it.
    expect(screen.getByText("No upstream provider")).toBeDefined();
  });

  it("flags a connection whose upstream has lost its refresh grant", () => {
    // The inbound leg is healthy here. Only the upstream is broken, which is
    // exactly the case the old sessions-only table could not express.
    useUserSessionsInfinite.mockReturnValue(
      queryResult([
        session({
          upstreams: [
            {
              remoteSessionId: "remote-1",
              remoteSessionClientId: "rclient-1",
              remoteSessionIssuerId: "rissuer-1",
              issuerSlug: "mcp.linear.app",
              hasRefreshToken: false,
              accessExpiresAt: new Date("2020-01-01T00:00:00Z"),
              autoRefresh: false,
              scopes: [],
            },
          ],
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("Needs re-auth")).toBeDefined();
  });

  it("no longer offers the client drill-down that filtered the table above", () => {
    // The retired paradigm: clicking a client re-scoped a separate table
    // elsewhere on the page. Grouping replaced it, so the affordance must be
    // gone rather than left pointing at nothing.
    useUserSessionsInfinite.mockReturnValue(queryResult([session({})]));
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ activeSessionCount: 1 })]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.queryByText("View sessions")).toBeNull();
  });

  it("refreshes the clients listing after a connection is revoked", () => {
    // The clients table reports a live-session tally that a revoke just
    // decremented; it cannot learn that on its own.
    const refetch = vi.fn();
    useUserSessionsInfinite.mockReturnValue(
      queryResult([session({})], { refetch }),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    fireEvent.click(screen.getByText("Revoke connection"));
    fireEvent.click(screen.getByText("Confirm session revoke"));

    expect(invalidateAllUserSessionClients).toHaveBeenCalled();
  });

  it("surfaces a registration with no connections under client grouping", () => {
    // The registrations table used to be the only place an unused client was
    // visible. Folding it into the grouping must not make it disappear —
    // otherwise a registered-but-idle client could never be found or revoked.
    useUserSessionsInfinite.mockReturnValue(queryResult([]));
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ clientName: "Never Used Client" })]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    fireEvent.click(screen.getByText("Client"));

    expect(screen.getByText("Never Used Client")).toBeDefined();
    expect(
      screen.getByText(/registered but holds no connections/i),
    ).toBeDefined();
  });

  it("offers revoking the registration itself, not just its sessions", () => {
    // Revoking live sessions and revoking the registration are different acts:
    // only the latter stops future connections. Both have to be reachable.
    useUserSessionsInfinite.mockReturnValue(queryResult([session({})]));
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ clientName: "Test Client" })]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    fireEvent.click(screen.getByText("Client"));

    expect(screen.getByText("Revoke registration")).toBeDefined();
    expect(screen.getByText("Revoke all")).toBeDefined();
  });
});
