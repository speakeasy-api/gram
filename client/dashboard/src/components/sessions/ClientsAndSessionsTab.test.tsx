import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ClientsAndSessionsTab } from "./ClientsAndSessionsTab";

const { useUserSessionClientsInfinite, useUserSessionsInfinite } = vi.hoisted(
  () => ({
    useUserSessionClientsInfinite: vi.fn(),
    useUserSessionsInfinite: vi.fn(),
  }),
);

vi.mock("@gram/client/react-query/userSessionClients.js", () => ({
  useUserSessionClientsInfinite: (...args: unknown[]) =>
    useUserSessionClientsInfinite(...args),
}));

vi.mock("@gram/client/react-query/userSessions.js", () => ({
  useUserSessionsInfinite: (...args: unknown[]) =>
    useUserSessionsInfinite(...args),
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
  useRevokeUserSessionMutation: () => ({ mutate: vi.fn(), isPending: false }),
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
function wrap(ui: React.ReactElement) {
  // The tab invalidates session queries after a revoke, so it needs a real
  // QueryClient even though every data hook is mocked.
  return (
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>{ui}</TooltipProvider>
    </QueryClientProvider>
  );
}

function renderTab(ui: React.ReactElement) {
  return render(wrap(ui));
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

    expect(screen.getByText(/isn't gated by a session issuer/i)).toBeDefined();
    // Neither list should fetch without an issuer to scope to.
    expect(useUserSessionClientsInfinite).not.toHaveBeenCalled();
  });

  it("renders skeletons while the clients list loads", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([], { isPending: true, data: undefined }),
    );

    const { container } = renderTab(
      <ClientsAndSessionsTab issuerId="issuer-1" />,
    );

    expect(container.querySelectorAll(".skeleton").length).toBeGreaterThan(0);
  });

  it("shows an empty state when the issuer has no clients", () => {
    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(
      screen.getByText("No clients have registered with this server yet"),
    ).toBeDefined();
  });

  it("offers a retry when the clients list fails to load", () => {
    const refetch = vi.fn();
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([], { isError: true, data: undefined, refetch }),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(refetch).toHaveBeenCalled();
  });

  // The whole point of the issue: the two registration modes have to be
  // distinguishable at a glance, and the distinction has to come from the
  // backend field rather than from re-parsing client_id.
  it("distinguishes CIMD clients from DCR clients", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({ id: "dcr-1", clientName: "DCR Client" }),
        client({
          id: "cimd-1",
          clientName: "CIMD Client",
          clientId: "https://client.example.com/oauth.json",
          clientIdMetadataUri: "https://client.example.com/oauth.json",
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    // Scoped per row: asserting the two strings merely exist would still pass
    // if the badges were swapped between rows.
    const dcrRow = screen.getByText("DCR Client").closest("tr");
    const cimdRow = screen.getByText("CIMD Client").closest("tr");
    expect(within(dcrRow as HTMLElement).getByText("DCR")).toBeDefined();
    expect(within(cimdRow as HTMLElement).getByText("CIMD")).toBeDefined();
  });

  // A DCR client has no document origin, so the secondary line falls back to
  // the client_id Gram minted, which operators correlate against logs.
  it("labels a DCR client with its client_id", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ id: "dcr-1", clientId: "minted-client-id" })]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("minted-client-id")).toBeDefined();
  });

  // The origin parse has to fail closed rather than throw: client_id_metadata_uri
  // is validated server-side, but the view must not blow up on a bad value.
  it("falls back to the client_id when the metadata URI will not parse", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({
          id: "cimd-bad",
          clientId: "not-a-url",
          clientIdMetadataUri: "not-a-url",
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("CIMD")).toBeDefined();
    expect(screen.getByText("not-a-url")).toBeDefined();
  });

  // client_name is chosen by the client and verified by nobody, so the CIMD
  // document origin is what an operator can actually trust.
  it("shows the metadata document origin for a CIMD client", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({
          id: "cimd-1",
          clientName: "Totally Legit Agent",
          clientId: "https://evil.example.com/oauth.json",
          clientIdMetadataUri: "https://evil.example.com/oauth.json",
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("https://evil.example.com")).toBeDefined();
  });

  it("clears a client filter that pointed at a different issuer", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ id: "client-1", clientName: "Server A Client" })]),
    );

    const { rerender } = renderTab(
      <ClientsAndSessionsTab issuerId="issuer-1" />,
    );
    fireEvent.click(screen.getByRole("button", { name: "View sessions" }));
    expect(screen.getByText(/Filtered to Server A Client/)).toBeDefined();

    // Navigating to a server backed by a different issuer reuses this
    // component instance; the filter must not carry over.
    rerender(wrap(<ClientsAndSessionsTab issuerId="issuer-2" />));

    expect(screen.queryByText(/Filtered to/)).toBeNull();
    expect(useUserSessionsInfinite).toHaveBeenLastCalledWith(
      expect.objectContaining({
        userSessionIssuerId: "issuer-2",
        clientId: undefined,
      }),
    );
  });

  // A failed page-2 fetch must not take the rows already on screen with it.
  it("keeps loaded clients visible when a later page fails", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ id: "client-1", clientName: "Loaded Client" })], {
        isError: true,
      }),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("Loaded Client")).toBeDefined();
    expect(screen.queryByText("Couldn't load clients.")).toBeNull();
  });

  it("keeps a filter that points at a client other than the revoked one", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({ id: "client-1", clientName: "Filtered Client" }),
        client({ id: "client-2", clientName: "Other Client" }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    const filteredRow = screen.getByText("Filtered Client").closest("tr");
    fireEvent.click(
      within(filteredRow as HTMLElement).getByRole("button", {
        name: "View sessions",
      }),
    );
    expect(screen.getByText(/Filtered to Filtered Client/)).toBeDefined();

    // Revoking a different client refreshes the sessions list but must leave
    // the operator's drill-down alone.
    const otherRow = screen.getByText("Other Client").closest("tr");
    fireEvent.click(
      within(otherRow as HTMLElement).getByRole("button", { name: "Revoke" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));

    expect(screen.getByText(/Filtered to Filtered Client/)).toBeDefined();
  });

  it("scopes the sessions list to a client picked from the clients list", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ id: "client-1", clientName: "Test Client" })]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(useUserSessionsInfinite).toHaveBeenCalledWith(
      expect.objectContaining({ clientId: undefined }),
    );

    fireEvent.click(
      screen.getAllByRole("button", { name: "View sessions" })[0]!,
    );

    expect(useUserSessionsInfinite).toHaveBeenLastCalledWith(
      expect.objectContaining({ clientId: "client-1" }),
    );
    expect(screen.getByText(/Filtered to Test Client/)).toBeDefined();
  });
});
